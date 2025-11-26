package expmgr

import (
	"context"
	"fmt"

	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

type extendPresetConfig struct {
	Name                 string
	SpID                 int64
	InfoBucketAboveDays  int
	InfoBucketBelowDays  int
	TargetExpirationDays int64
	MaxCandidateDays     int64
	CC                   *bool
	DropClaims           bool
}

func (e *ExpMgrTask) handleExtend(ctx context.Context, cfg extendPresetConfig) error {
	log.Infow("handling extend preset",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"info_bucket", fmt.Sprintf("%d-%d days", cfg.InfoBucketAboveDays, cfg.InfoBucketBelowDays),
		"target_expiration", fmt.Sprintf("%d days", cfg.TargetExpirationDays),
		"max_candidate", fmt.Sprintf("%d days", cfg.MaxCandidateDays))

	head, err := e.chain.ChainHead(ctx)
	if err != nil {
		return xerrors.Errorf("getting chain head: %w", err)
	}
	currEpoch := head.Height()

	nv, err := e.chain.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting network version: %w", err)
	}

	maxExtension, err := policy.GetMaxSectorExpirationExtension(nv)
	if err != nil {
		return xerrors.Errorf("getting max extension: %w", err)
	}

	const epochsPerDay = 2880
	targetExpEpoch := currEpoch + abi.ChainEpoch(cfg.TargetExpirationDays*epochsPerDay)
	maxCandidateEpoch := currEpoch + abi.ChainEpoch(cfg.MaxCandidateDays*epochsPerDay)

	// Query candidate sectors: expiration between now and max_candidate_days
	// Use pre-crawled claim data to filter sectors:
	// - If drop_claims=false: exclude sectors where min_claim_epoch < target_expiration
	// - If drop_claims=true: include sectors, we'll check max_claim_epoch later
	var sectors []struct {
		SectorNum  uint64 `db:"sector_num"`
		Expiration int64  `db:"expiration_epoch"`
		Deadline   uint64 `db:"deadline"`
		Partition  uint64 `db:"partition"`
		IsCC       bool   `db:"is_cc"`
		MinClaim   *int64 `db:"min_claim_epoch"`
		MaxClaim   *int64 `db:"max_claim_epoch"`
	}

	// Use conditional queries based on CC filter and drop_claims setting
	if cfg.CC != nil && !cfg.DropClaims {
		// CC filter + claim filter
		err = e.db.Select(ctx, &sectors, `
			SELECT sector_num, expiration_epoch, deadline, partition, is_cc, min_claim_epoch, max_claim_epoch
			FROM sectors_meta
			WHERE sp_id = $1
			  AND expiration_epoch IS NOT NULL
			  AND expiration_epoch > $2
			  AND expiration_epoch < $3
			  AND deadline IS NOT NULL
			  AND partition IS NOT NULL
			  AND is_cc = $4
			  AND (min_claim_epoch IS NULL OR min_claim_epoch > $5)
			ORDER BY deadline, partition, sector_num`,
			cfg.SpID, int64(currEpoch), int64(maxCandidateEpoch), *cfg.CC, int64(targetExpEpoch))
	} else if cfg.CC != nil && cfg.DropClaims {
		// CC filter only
		err = e.db.Select(ctx, &sectors, `
			SELECT sector_num, expiration_epoch, deadline, partition, is_cc, min_claim_epoch, max_claim_epoch
			FROM sectors_meta
			WHERE sp_id = $1
			  AND expiration_epoch IS NOT NULL
			  AND expiration_epoch > $2
			  AND expiration_epoch < $3
			  AND deadline IS NOT NULL
			  AND partition IS NOT NULL
			  AND is_cc = $4
			ORDER BY deadline, partition, sector_num`,
			cfg.SpID, int64(currEpoch), int64(maxCandidateEpoch), *cfg.CC)
	} else if cfg.CC == nil && !cfg.DropClaims {
		// Claim filter only
		err = e.db.Select(ctx, &sectors, `
			SELECT sector_num, expiration_epoch, deadline, partition, is_cc, min_claim_epoch, max_claim_epoch
			FROM sectors_meta
			WHERE sp_id = $1
			  AND expiration_epoch IS NOT NULL
			  AND expiration_epoch > $2
			  AND expiration_epoch < $3
			  AND deadline IS NOT NULL
			  AND partition IS NOT NULL
			  AND (min_claim_epoch IS NULL OR min_claim_epoch > $4)
			ORDER BY deadline, partition, sector_num`,
			cfg.SpID, int64(currEpoch), int64(maxCandidateEpoch), int64(targetExpEpoch))
	} else {
		// No filters
		err = e.db.Select(ctx, &sectors, `
			SELECT sector_num, expiration_epoch, deadline, partition, is_cc, min_claim_epoch, max_claim_epoch
			FROM sectors_meta
			WHERE sp_id = $1
			  AND expiration_epoch IS NOT NULL
			  AND expiration_epoch > $2
			  AND expiration_epoch < $3
			  AND deadline IS NOT NULL
			  AND partition IS NOT NULL
			ORDER BY deadline, partition, sector_num`,
			cfg.SpID, int64(currEpoch), int64(maxCandidateEpoch))
	}
	if err != nil {
		return xerrors.Errorf("querying candidate sectors: %w", err)
	}

	log.Infow("found candidate sectors",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"count", len(sectors))

	if len(sectors) == 0 {
		log.Infow("no sectors to extend", "preset", cfg.Name, "sp_id", cfg.SpID)
		return nil
	}

	maddr, err := address.NewIDAddress(uint64(cfg.SpID))
	if err != nil {
		return xerrors.Errorf("creating miner address: %w", err)
	}

	// Get miner info for max lifetime calculations
	mi, err := e.chain.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting miner info: %w", err)
	}

	// Load actor state for sector info
	mact, err := e.chain.StateGetActor(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting miner actor: %w", err)
	}

	tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(e.chain), blockstore.NewMemory())
	adtStore := adt.WrapStore(ctx, cbor.NewCborStore(tbs))
	mas, err := miner.Load(adtStore, mact)
	if err != nil {
		return xerrors.Errorf("loading miner state: %w", err)
	}

	// Only load verifreg state if we need to drop claims
	// Check if any sector has claims that might need dropping
	var needsClaimProcessing bool
	if cfg.DropClaims {
		for _, s := range sectors {
			if s.MaxClaim != nil && abi.ChainEpoch(*s.MaxClaim) < targetExpEpoch {
				needsClaimProcessing = true
				break
			}
		}
	}

	var claimsMap map[verifreg.ClaimId]verifreg.Claim
	var claimIdsBySector map[abi.SectorNumber][]verifreg.ClaimId

	if needsClaimProcessing {
		// Get verifreg state for claim handling
		verifregAct, err := e.chain.StateGetActor(ctx, builtin.VerifiedRegistryActorAddr, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("getting verifreg actor: %w", err)
		}

		verifregSt, err := verifreg.Load(adtStore, verifregAct)
		if err != nil {
			return xerrors.Errorf("loading verifreg state: %w", err)
		}

		claimsMap, err = verifregSt.GetClaims(maddr)
		if err != nil {
			return xerrors.Errorf("getting claims: %w", err)
		}

		claimIdsBySector, err = verifregSt.GetClaimIdsBySector(maddr)
		if err != nil {
			return xerrors.Errorf("getting claim IDs by sector: %w", err)
		}

		log.Infow("loaded verifreg state for claim processing",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"total_claims", len(claimsMap))
	}

	// Group sectors by deadline/partition
	type dlPartKey struct {
		Deadline  uint64
		Partition uint64
	}
	sectorsByDlPart := make(map[dlPartKey][]abi.SectorNumber)
	sectorInfoMap := make(map[abi.SectorNumber]*miner.SectorOnChainInfo)

	for _, s := range sectors {
		si, err := mas.GetSector(abi.SectorNumber(s.SectorNum))
		if err != nil {
			log.Warnw("failed to get sector info", "sector", s.SectorNum, "error", err)
			continue
		}
		if si == nil {
			log.Warnw("sector not found on chain", "sector", s.SectorNum)
			continue
		}

		sectorInfoMap[abi.SectorNumber(s.SectorNum)] = si

		key := dlPartKey{Deadline: s.Deadline, Partition: s.Partition}
		sectorsByDlPart[key] = append(sectorsByDlPart[key], abi.SectorNumber(s.SectorNum))
	}

	// Build extension params
	var extensions []miner.ExpirationExtension2
	totalSectors := 0

	for dlPart, sectorNums := range sectorsByDlPart {
		if totalSectors >= MaxExtendsPerMessage {
			log.Warnw("hit max sectors per message limit",
				"preset", cfg.Name,
				"sp_id", cfg.SpID,
				"limit", MaxExtendsPerMessage)
			break
		}

		sectorsWithoutClaims := bitfield.New()
		var sectorsWithClaims []miner.SectorClaim
		numbersToExtend := make([]abi.SectorNumber, 0, len(sectorNums))

		for _, sn := range sectorNums {
			if totalSectors >= MaxExtendsPerMessage {
				break
			}

			si := sectorInfoMap[sn]
			if si == nil {
				continue
			}

			// Calculate new expiration (clamped to max lifetime)
			newExp := targetExpEpoch
			maxLifetime := si.Activation + maxExtension
			if newExp > maxLifetime {
				newExp = maxLifetime
			}

			// Skip if new expiration is not significantly different
			if newExp <= si.Expiration || (newExp-si.Expiration) < 7*epochsPerDay {
				continue
			}

			// Get the sector's claim info from the database query result
			dbSector := sectors[0] // Find matching sector from our query
			for _, s := range sectors {
				if abi.SectorNumber(s.SectorNum) == sn {
					dbSector = s
					break
				}
			}

			// Handle claims using pre-crawled data
			if dbSector.MinClaim == nil && dbSector.MaxClaim == nil {
				// No claims - simple case
				sectorsWithoutClaims.Set(uint64(sn))
				numbersToExtend = append(numbersToExtend, sn)
				totalSectors++
				continue
			}

			// Sector has claims
			// If drop_claims=false, we already filtered these out at query time
			// If drop_claims=true, check if we need to process individual claims
			if !cfg.DropClaims {
				// This shouldn't happen due to query filtering, but just in case
				log.Warnw("sector with short claims in non-drop-claims mode",
					"sector", sn,
					"min_claim_epoch", dbSector.MinClaim,
					"target_expiration", newExp)
				continue
			}

			// drop_claims=true: check if any claims need dropping
			if dbSector.MaxClaim == nil || abi.ChainEpoch(*dbSector.MaxClaim) >= newExp {
				// All claims live long enough, maintain all
				claimIds := claimIdsBySector[sn]
				if len(claimIds) > 0 {
					sectorsWithClaims = append(sectorsWithClaims, miner.SectorClaim{
						SectorNumber:   sn,
						MaintainClaims: claimIds,
						DropClaims:     []verifreg.ClaimId{},
					})
				} else {
					sectorsWithoutClaims.Set(uint64(sn))
				}
				numbersToExtend = append(numbersToExtend, sn)
				totalSectors++
				continue
			}

			// Some claims need to be dropped - process individually
			claimIds, hasClaims := claimIdsBySector[sn]
			if !hasClaims {
				sectorsWithoutClaims.Set(uint64(sn))
				numbersToExtend = append(numbersToExtend, sn)
				totalSectors++
				continue
			}

			claimIdsToMaintain := make([]verifreg.ClaimId, 0)
			claimIdsToDrop := make([]verifreg.ClaimId, 0)
			canExtend := true

			for _, claimId := range claimIds {
				claim, ok := claimsMap[claimId]
				if !ok {
					log.Warnw("claim not found", "sector", sn, "claim_id", claimId)
					canExtend = false
					break
				}

				claimExpiration := claim.TermStart + claim.TermMax
				if claimExpiration > newExp {
					// Claim lives long enough, maintain it
					claimIdsToMaintain = append(claimIdsToMaintain, claimId)
				} else {
					// Check FIP-0045 requirements for claim dropping
					if currEpoch <= (claim.TermStart + claim.TermMin) {
						log.Infow("skipping sector: claim minimum duration not met",
							"sector", sn,
							"claim_id", claimId,
							"term_start", claim.TermStart,
							"term_min", claim.TermMin,
							"curr_epoch", currEpoch)
						canExtend = false
						break
					}

					if currEpoch <= si.Expiration-builtin.EndOfLifeClaimDropPeriod {
						log.Infow("skipping sector: not in last 30 days of life",
							"sector", sn,
							"expiration", si.Expiration,
							"curr_epoch", currEpoch)
						canExtend = false
						break
					}

					claimIdsToDrop = append(claimIdsToDrop, claimId)
				}
			}

			if !canExtend {
				continue
			}

			if len(claimIdsToMaintain)+len(claimIdsToDrop) > 0 {
				sectorsWithClaims = append(sectorsWithClaims, miner.SectorClaim{
					SectorNumber:   sn,
					MaintainClaims: claimIdsToMaintain,
					DropClaims:     claimIdsToDrop,
				})
			}
			numbersToExtend = append(numbersToExtend, sn)
			totalSectors++
		}

		if len(numbersToExtend) == 0 {
			continue
		}

		extensions = append(extensions, miner.ExpirationExtension2{
			Deadline:          dlPart.Deadline,
			Partition:         dlPart.Partition,
			Sectors:           sectorNumsToBitfield(numbersToExtend),
			SectorsWithClaims: sectorsWithClaims,
			NewExpiration:     targetExpEpoch,
		})
	}

	if len(extensions) == 0 {
		log.Infow("no sectors can be extended after filtering", "preset", cfg.Name, "sp_id", cfg.SpID)
		return nil
	}

	params := miner.ExtendSectorExpiration2Params{
		Extensions: extensions,
	}

	log.Infow("would extend sectors",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"total_sectors", totalSectors,
		"extensions", len(extensions),
		"target_epoch", targetExpEpoch,
		"worker", mi.Worker)

	// Estimate gas for the message
	msg, err := e.buildExtendMessage(ctx, maddr, mi.Worker, &params)
	if err != nil {
		return xerrors.Errorf("building extend message: %w", err)
	}

	estimatedGas, err := e.chain.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
	if err != nil {
		log.Errorw("failed to estimate gas",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"error", err)
		// Continue anyway to log what we would do
	} else {
		log.Infow("gas estimation",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"gas_limit", estimatedGas.GasLimit,
			"gas_fee_cap", types.FIL(estimatedGas.GasFeeCap).Short(),
			"max_fee", types.FIL(estimatedGas.RequiredFunds()).Short())
	}

	// TODO: If gas estimation fails with out-of-gas, split message in half and retry

	// Update the database to record we evaluated this
	_, err = e.db.Exec(ctx, `
		UPDATE sectors_exp_manager_sp
		SET last_run_at = CURRENT_TIMESTAMP
		WHERE sp_id = $1 AND preset_name = $2
	`, cfg.SpID, cfg.Name)
	if err != nil {
		return xerrors.Errorf("updating last_run_at: %w", err)
	}

	return nil
}

func (e *ExpMgrTask) buildExtendMessage(ctx context.Context, maddr, worker address.Address, params *miner.ExtendSectorExpiration2Params) (*types.Message, error) {
	sp, err := actors.SerializeParams(params)
	if err != nil {
		return nil, xerrors.Errorf("serializing params: %w", err)
	}

	return &types.Message{
		From:   worker,
		To:     maddr,
		Method: builtin.MethodsMiner.ExtendSectorExpiration2,
		Value:  big.Zero(),
		Params: sp,
	}, nil
}

func sectorNumsToBitfield(nums []abi.SectorNumber) bitfield.BitField {
	bf := bitfield.New()
	for _, n := range nums {
		bf.Set(uint64(n))
	}
	return bf
}
