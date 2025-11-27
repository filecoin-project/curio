package expmgr

import (
	"context"
	"fmt"

	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

type topUpPresetConfig struct {
	Name                    string
	SpID                    int64
	InfoBucketAboveDays     int
	InfoBucketBelowDays     int
	TopUpCountLowWaterMark  int64
	TopUpCountHighWaterMark int64
	CC                      *bool
	DropClaims              bool
}

func (e *ExpMgrTask) handleTopUp(ctx context.Context, cfg topUpPresetConfig) (bool, error) {
	log.Infow("handling top_up preset",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"info_bucket", fmt.Sprintf("%d-%d days", cfg.InfoBucketAboveDays, cfg.InfoBucketBelowDays),
		"low_water_mark", cfg.TopUpCountLowWaterMark,
		"high_water_mark", cfg.TopUpCountHighWaterMark)

	head, err := e.chain.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting chain head: %w", err)
	}
	currEpoch := head.Height()

	nv, err := e.chain.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting network version: %w", err)
	}

	maxExtension, err := policy.GetMaxSectorExpirationExtension(nv)
	if err != nil {
		return false, xerrors.Errorf("getting max extension: %w", err)
	}

	const epochsPerDay = 2880
	bucketAboveEpoch := currEpoch + abi.ChainEpoch(cfg.InfoBucketAboveDays*epochsPerDay)
	bucketBelowEpoch := currEpoch + abi.ChainEpoch(cfg.InfoBucketBelowDays*epochsPerDay)

	// Count sectors in the target bucket (exclude sectors in snap pipeline or with open pieces)
	var count struct {
		Count int64 `db:"count"`
	}

	if cfg.CC != nil {
		err = e.db.QueryRow(ctx, `
			SELECT COUNT(*) as count
			FROM sectors_meta sm
			WHERE sm.sp_id = $1
			  AND sm.expiration_epoch IS NOT NULL
			  AND sm.expiration_epoch > $2
			  AND sm.expiration_epoch < $3
			  AND sm.is_cc = $4
			  AND NOT EXISTS (SELECT 1 FROM sectors_snap_pipeline ssp WHERE ssp.sp_id = sm.sp_id AND ssp.sector_number = sm.sector_num)
			  AND NOT EXISTS (SELECT 1 FROM open_sector_pieces osp WHERE osp.sp_id = sm.sp_id AND osp.sector_number = sm.sector_num)`,
			cfg.SpID, int64(bucketAboveEpoch), int64(bucketBelowEpoch), *cfg.CC).Scan(&count.Count)
	} else {
		err = e.db.QueryRow(ctx, `
			SELECT COUNT(*) as count
			FROM sectors_meta sm
			WHERE sm.sp_id = $1
			  AND sm.expiration_epoch IS NOT NULL
			  AND sm.expiration_epoch > $2
			  AND sm.expiration_epoch < $3
			  AND NOT EXISTS (SELECT 1 FROM sectors_snap_pipeline ssp WHERE ssp.sp_id = sm.sp_id AND ssp.sector_number = sm.sector_num)
			  AND NOT EXISTS (SELECT 1 FROM open_sector_pieces osp WHERE osp.sp_id = sm.sp_id AND osp.sector_number = sm.sector_num)`,
			cfg.SpID, int64(bucketAboveEpoch), int64(bucketBelowEpoch)).Scan(&count.Count)
	}
	if err != nil {
		return false, xerrors.Errorf("counting sectors in bucket: %w", err)
	}

	log.Infow("bucket count",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"current_count", count.Count,
		"low_water_mark", cfg.TopUpCountLowWaterMark,
		"high_water_mark", cfg.TopUpCountHighWaterMark)

	if count.Count >= cfg.TopUpCountLowWaterMark {
		log.Infow("bucket has sufficient sectors, no top-up needed",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"count", count.Count)
		return false, nil
	}

	// Need to top up - select sectors from expiring sooner than the bucket
	needCount := cfg.TopUpCountHighWaterMark - count.Count
	if needCount <= 0 {
		log.Infow("no sectors needed for top-up",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"count", count.Count,
			"low_water_mark", cfg.TopUpCountLowWaterMark,
			"high_water_mark", cfg.TopUpCountHighWaterMark)
		return false, nil
	}

	var sectors []SectorMeta

	// Select sectors expiring before the bucket lower bound
	// Use pre-crawled claim data to filter sectors:
	// - If drop_claims=false: exclude sectors where min_claim_epoch < bucket_below_epoch
	// - If drop_claims=true: include sectors, we'll check max_claim_epoch later
	// Exclude sectors in snap pipeline or with open pieces (not yet finalized)

	// Use conditional queries based on CC filter and drop_claims setting
	if cfg.CC != nil && !cfg.DropClaims {
		// CC filter + claim filter
		err = e.db.Select(ctx, &sectors, `
			SELECT sm.sector_num, sm.expiration_epoch, sm.deadline, sm.partition, sm.is_cc, sm.min_claim_epoch, sm.max_claim_epoch
			FROM sectors_meta sm
			WHERE sm.sp_id = $1
			  AND sm.expiration_epoch IS NOT NULL
			  AND sm.expiration_epoch > $2
			  AND sm.expiration_epoch < $3
			  AND sm.deadline IS NOT NULL
			  AND sm.partition IS NOT NULL
			  AND sm.is_cc = $4
			  AND (sm.min_claim_epoch IS NULL OR sm.min_claim_epoch > $5)
			  AND NOT EXISTS (SELECT 1 FROM sectors_snap_pipeline ssp WHERE ssp.sp_id = sm.sp_id AND ssp.sector_number = sm.sector_num)
			  AND NOT EXISTS (SELECT 1 FROM open_sector_pieces osp WHERE osp.sp_id = sm.sp_id AND osp.sector_number = sm.sector_num)
			ORDER BY sm.expiration_epoch ASC, sm.deadline, sm.partition, sm.sector_num
			LIMIT $6`,
			cfg.SpID, int64(currEpoch), int64(bucketAboveEpoch), *cfg.CC, int64(bucketBelowEpoch), needCount)
	} else if cfg.CC != nil && cfg.DropClaims {
		// CC filter only
		err = e.db.Select(ctx, &sectors, `
			SELECT sm.sector_num, sm.expiration_epoch, sm.deadline, sm.partition, sm.is_cc, sm.min_claim_epoch, sm.max_claim_epoch
			FROM sectors_meta sm
			WHERE sm.sp_id = $1
			  AND sm.expiration_epoch IS NOT NULL
			  AND sm.expiration_epoch > $2
			  AND sm.expiration_epoch < $3
			  AND sm.deadline IS NOT NULL
			  AND sm.partition IS NOT NULL
			  AND sm.is_cc = $4
			  AND NOT EXISTS (SELECT 1 FROM sectors_snap_pipeline ssp WHERE ssp.sp_id = sm.sp_id AND ssp.sector_number = sm.sector_num)
			  AND NOT EXISTS (SELECT 1 FROM open_sector_pieces osp WHERE osp.sp_id = sm.sp_id AND osp.sector_number = sm.sector_num)
			ORDER BY sm.expiration_epoch ASC, sm.deadline, sm.partition, sm.sector_num
			LIMIT $5`,
			cfg.SpID, int64(currEpoch), int64(bucketAboveEpoch), *cfg.CC, needCount)
	} else if cfg.CC == nil && !cfg.DropClaims {
		// Claim filter only
		err = e.db.Select(ctx, &sectors, `
			SELECT sm.sector_num, sm.expiration_epoch, sm.deadline, sm.partition, sm.is_cc, sm.min_claim_epoch, sm.max_claim_epoch
			FROM sectors_meta sm
			WHERE sm.sp_id = $1
			  AND sm.expiration_epoch IS NOT NULL
			  AND sm.expiration_epoch > $2
			  AND sm.expiration_epoch < $3
			  AND sm.deadline IS NOT NULL
			  AND sm.partition IS NOT NULL
			  AND (sm.min_claim_epoch IS NULL OR sm.min_claim_epoch > $4)
			  AND NOT EXISTS (SELECT 1 FROM sectors_snap_pipeline ssp WHERE ssp.sp_id = sm.sp_id AND ssp.sector_number = sm.sector_num)
			  AND NOT EXISTS (SELECT 1 FROM open_sector_pieces osp WHERE osp.sp_id = sm.sp_id AND osp.sector_number = sm.sector_num)
			ORDER BY sm.expiration_epoch ASC, sm.deadline, sm.partition, sm.sector_num
			LIMIT $5`,
			cfg.SpID, int64(currEpoch), int64(bucketAboveEpoch), int64(bucketBelowEpoch), needCount)
	} else {
		// No filters
		err = e.db.Select(ctx, &sectors, `
			SELECT sm.sector_num, sm.expiration_epoch, sm.deadline, sm.partition, sm.is_cc, sm.min_claim_epoch, sm.max_claim_epoch
			FROM sectors_meta sm
			WHERE sm.sp_id = $1
			  AND sm.expiration_epoch IS NOT NULL
			  AND sm.expiration_epoch > $2
			  AND sm.expiration_epoch < $3
			  AND sm.deadline IS NOT NULL
			  AND sm.partition IS NOT NULL
			  AND NOT EXISTS (SELECT 1 FROM sectors_snap_pipeline ssp WHERE ssp.sp_id = sm.sp_id AND ssp.sector_number = sm.sector_num)
			  AND NOT EXISTS (SELECT 1 FROM open_sector_pieces osp WHERE osp.sp_id = sm.sp_id AND osp.sector_number = sm.sector_num)
			ORDER BY sm.expiration_epoch ASC, sm.deadline, sm.partition, sm.sector_num
			LIMIT $4`,
			cfg.SpID, int64(currEpoch), int64(bucketAboveEpoch), needCount)
	}
	if err != nil {
		return false, xerrors.Errorf("querying sectors for top-up: %w", err)
	}

	log.Infow("found sectors for top-up",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"needed", needCount,
		"found", len(sectors))

	if len(sectors) == 0 {
		log.Infow("no sectors available for top-up", "preset", cfg.Name, "sp_id", cfg.SpID)
		return false, nil
	}

	maddr, err := address.NewIDAddress(uint64(cfg.SpID))
	if err != nil {
		return false, xerrors.Errorf("creating miner address: %w", err)
	}

	// Get miner info
	mi, err := e.chain.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting miner info: %w", err)
	}

	// Load actor state for sector info
	mact, err := e.chain.StateGetActor(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting miner actor: %w", err)
	}

	tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(e.chain), blockstore.NewMemory())
	adtStore := adt.WrapStore(ctx, cbor.NewCborStore(tbs))
	mas, err := miner.Load(adtStore, mact)
	if err != nil {
		return false, xerrors.Errorf("loading miner state: %w", err)
	}

	// Check if we need to load verifreg state (for validation or claim processing)
	var needsVerifregState bool

	// Need verifreg if any sector has claims in DB (for validation)
	for _, s := range sectors {
		if s.MinClaim != nil || s.MaxClaim != nil {
			needsVerifregState = true
			break
		}
	}

	// Also need verifreg if we're dropping claims
	if !needsVerifregState && cfg.DropClaims {
		for _, s := range sectors {
			if s.MaxClaim != nil && abi.ChainEpoch(*s.MaxClaim) < bucketBelowEpoch {
				needsVerifregState = true
				break
			}
		}
	}

	var claimsMap map[verifreg.ClaimId]verifreg.Claim
	var claimIdsBySector map[abi.SectorNumber][]verifreg.ClaimId

	if needsVerifregState {
		// Get verifreg state (used for both validation and claim processing)
		verifregAct, err := e.chain.StateGetActor(ctx, builtin.VerifiedRegistryActorAddr, types.EmptyTSK)
		if err != nil {
			return false, xerrors.Errorf("getting verifreg actor: %w", err)
		}

		verifregSt, err := verifreg.Load(adtStore, verifregAct)
		if err != nil {
			return false, xerrors.Errorf("loading verifreg state: %w", err)
		}

		claimsMap, err = verifregSt.GetClaims(maddr)
		if err != nil {
			return false, xerrors.Errorf("getting claims: %w", err)
		}

		claimIdsBySector, err = verifregSt.GetClaimIdsBySector(maddr)
		if err != nil {
			return false, xerrors.Errorf("getting claim IDs by sector: %w", err)
		}

		log.Infow("loaded verifreg state",
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

	// Build extension params - extend to the bucket's upper bound (info_bucket_below_days)
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

			// Top-up extends to the info bucket's upper bound
			newExp := bucketBelowEpoch

			// Clamp to max lifetime
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

			// Validate sector metadata against on-chain state
			if err := validateSectorAgainstChain(sn, dbSector, si, claimsMap, claimIdsBySector); err != nil {
				log.Errorw("sector metadata validation failed, bailing for this SP",
					"preset", cfg.Name,
					"sp_id", cfg.SpID,
					"sector", sn,
					"error", err)
				return true, nil
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
					claimIdsToMaintain = append(claimIdsToMaintain, claimId)
				} else {
					// Check FIP-0045 requirements
					if currEpoch <= (claim.TermStart + claim.TermMin) {
						log.Infow("skipping sector: claim minimum duration not met",
							"sector", sn,
							"claim_id", claimId)
						canExtend = false
						break
					}

					if currEpoch <= si.Expiration-builtin.EndOfLifeClaimDropPeriod {
						log.Infow("skipping sector: not in last 30 days of life",
							"sector", sn)
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
			NewExpiration:     bucketBelowEpoch,
		})
	}

	if len(extensions) == 0 {
		log.Infow("no sectors can be extended for top-up after filtering", "preset", cfg.Name, "sp_id", cfg.SpID)
		return false, nil
	}

	params := miner.ExtendSectorExpiration2Params{
		Extensions: extensions,
	}

	log.Infow("would top-up sectors",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"total_sectors", totalSectors,
		"extensions", len(extensions),
		"target_epoch", bucketBelowEpoch,
		"worker", mi.Worker)

	// Estimate gas for the message
	msg, err := e.buildExtendMessage(ctx, maddr, mi.Worker, &params)
	if err != nil {
		return false, xerrors.Errorf("building extend message: %w", err)
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
		return false, xerrors.Errorf("updating last_run_at: %w", err)
	}

	return true, nil
}
