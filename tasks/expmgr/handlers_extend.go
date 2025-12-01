package expmgr

import (
	"context"
	"fmt"
	"strings"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

const MaxExtendMsgGasLimit = 7_000_000_000

var MaxExtendMsgFee = types.MustParseFIL("0.5 FIL")

type SectorMeta struct {
	SectorNum  uint64 `db:"sector_num"`
	Expiration int64  `db:"expiration_epoch"`
	Deadline   uint64 `db:"deadline"`
	Partition  uint64 `db:"partition"`
	IsCC       bool   `db:"is_cc"`
	MinClaim   *int64 `db:"min_claim_epoch"`
	MaxClaim   *int64 `db:"max_claim_epoch"`
}

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

func (e *ExpMgrTask) handleExtend(ctx context.Context, cfg extendPresetConfig) (bool, error) {
	log.Infow("handling extend preset",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"info_bucket", fmt.Sprintf("%d-%d days", cfg.InfoBucketAboveDays, cfg.InfoBucketBelowDays),
		"target_expiration", fmt.Sprintf("%d days", cfg.TargetExpirationDays),
		"max_candidate", fmt.Sprintf("%d days", cfg.MaxCandidateDays))

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
	targetExpEpoch := currEpoch + abi.ChainEpoch(cfg.TargetExpirationDays*epochsPerDay)
	maxCandidateEpoch := currEpoch + abi.ChainEpoch(cfg.MaxCandidateDays*epochsPerDay)

	// Query candidate sectors: expiration between now and max_candidate_days
	// Use pre-crawled claim data to filter sectors:
	// - If drop_claims=false: exclude sectors where min_claim_epoch < target_expiration
	// - If drop_claims=true: include sectors, we'll check max_claim_epoch later
	var sectors []SectorMeta

	// Use conditional queries based on CC filter and drop_claims setting
	// Exclude sectors in snap pipeline or with open pieces (not yet finalized)
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
			ORDER BY sm.deadline, sm.partition, sm.sector_num`,
			cfg.SpID, int64(currEpoch), int64(maxCandidateEpoch), *cfg.CC, int64(targetExpEpoch))
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
			ORDER BY sm.deadline, sm.partition, sm.sector_num`,
			cfg.SpID, int64(currEpoch), int64(maxCandidateEpoch), *cfg.CC)
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
			ORDER BY sm.deadline, sm.partition, sm.sector_num`,
			cfg.SpID, int64(currEpoch), int64(maxCandidateEpoch), int64(targetExpEpoch))
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
			ORDER BY sm.deadline, sm.partition, sm.sector_num`,
			cfg.SpID, int64(currEpoch), int64(maxCandidateEpoch))
	}
	if err != nil {
		return false, xerrors.Errorf("querying candidate sectors: %w", err)
	}

	log.Infow("found candidate sectors",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"count", len(sectors))

	if len(sectors) == 0 {
		log.Infow("no sectors to extend", "preset", cfg.Name, "sp_id", cfg.SpID)
		return false, nil
	}

	maddr, err := address.NewIDAddress(uint64(cfg.SpID))
	if err != nil {
		return false, xerrors.Errorf("creating miner address: %w", err)
	}

	// Get miner info for max lifetime calculations
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
			if s.MaxClaim != nil && abi.ChainEpoch(*s.MaxClaim) < targetExpEpoch {
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

			// Check if target expiration exceeds sector's max lifetime
			maxLifetime := si.Activation + maxExtension
			if targetExpEpoch > maxLifetime {
				log.Warnw("skipping sector: target expiration exceeds max lifetime",
					"sector", sn,
					"target_expiration", targetExpEpoch,
					"max_lifetime", maxLifetime)
				continue
			}

			// Skip if new expiration is not significantly different
			if targetExpEpoch <= si.Expiration || (targetExpEpoch-si.Expiration) < 7*epochsPerDay {
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
					"target_expiration", targetExpEpoch)
				continue
			}

			// drop_claims=true: check if any claims need dropping
			if dbSector.MaxClaim == nil || abi.ChainEpoch(*dbSector.MaxClaim) >= targetExpEpoch {
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
				if claimExpiration > targetExpEpoch {
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
		return false, nil
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

	// Build and estimate gas for messages (handles splitting if needed)
	msgs, err := e.buildExtendMessage(ctx, cfg, maddr, mi.Worker, &params)
	if err != nil {
		return false, xerrors.Errorf("building extend messages: %w", err)
	}

	log.Infow("prepared extension messages",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"message_count", len(msgs))

	// Send all messages
	mss := &api.MessageSendSpec{
		MaxFee:         abi.TokenAmount(MaxExtendMsgFee),
		MaximizeFeeCap: true,
	}

	var sentCids []cid.Cid
	for i, msg := range msgs {
		msgCid, err := e.sender.Send(ctx, msg, mss, fmt.Sprintf("expmgr-extend-%s", cfg.Name))
		if err != nil {
			return false, xerrors.Errorf("sending message %d: %w", i, err)
		}

		sentCids = append(sentCids, msgCid)

		// Insert into message_waits
		_, err = e.db.Exec(ctx, `INSERT INTO message_waits (signed_message_cid) VALUES ($1)`, msgCid.String())
		if err != nil {
			return false, xerrors.Errorf("inserting message %d into message_waits: %w", i, err)
		}

		log.Infow("sent extension message",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"message_index", i+1,
			"total_messages", len(msgs),
			"msg_cid", msgCid,
			"worker", mi.Worker,
			"miner", maddr)
	}

	// Update the database with the first message CID (trigger will update last_message_landed_at when it lands)
	_, err = e.db.Exec(ctx, `
		UPDATE sectors_exp_manager_sp
		SET last_message_cid = $2,
		    last_run_at = CURRENT_TIMESTAMP
		WHERE sp_id = $1 AND preset_name = $3
	`, cfg.SpID, sentCids[0].String(), cfg.Name)
	if err != nil {
		return false, xerrors.Errorf("updating sectors_exp_manager_sp: %w", err)
	}

	log.Infow("completed extend action",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"total_messages_sent", len(sentCids),
		"tracked_message_cid", sentCids[0].String())

	return true, nil
}

func (e *ExpMgrTask) buildExtendMessage(ctx context.Context, cfg extendPresetConfig, maddr, worker address.Address, params *miner.ExtendSectorExpiration2Params) ([]*types.Message, error) {
	var err error
	sp, err := actors.SerializeParams(params)
	if err != nil {
		return nil, xerrors.Errorf("serializing params: %w", err)
	}

	msg := &types.Message{
		From:   worker,
		To:     maddr,
		Method: builtin.MethodsMiner.ExtendSectorExpiration2,
		Value:  big.Zero(),
		Params: sp,
	}

	estimatedGas, err := e.chain.GasEstimateMessageGas(ctx, msg, nil, types.EmptyTSK)
	if (err != nil && strings.Contains(err.Error(), "call ran out of gas")) || (err == nil && estimatedGas.GasLimit > MaxExtendMsgGasLimit) {
		// Split params and recursively build messages
		log.Infow("message out of gas, splitting",
			"preset", cfg.Name,
			"sp_id", cfg.SpID)

		splitParamsList, err := splitParams(params)
		if err != nil {
			return nil, xerrors.Errorf("splitting params: %w", err)
		}

		var allMessages []*types.Message
		for i, splitParams := range splitParamsList {
			msgs, err := e.buildExtendMessage(ctx, cfg, maddr, worker, splitParams)
			if err != nil {
				return nil, xerrors.Errorf("building split message %d: %w", i, err)
			}
			allMessages = append(allMessages, msgs...)
		}

		log.Infow("successfully split message",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"resulting_messages", len(allMessages))

		return allMessages, nil
	} else if err != nil {
		return nil, xerrors.Errorf("failed to estimate gas: %w", err)
	}

	log.Infow("gas estimation passed",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"extensions", params.Extensions,
		"gas_limit", estimatedGas.GasLimit,
		"gas_fee_cap", types.FIL(estimatedGas.GasFeeCap).Short(),
		"max_fee", types.FIL(estimatedGas.RequiredFunds()).Short())

	return []*types.Message{msg}, nil
}

func splitParams(params *miner.ExtendSectorExpiration2Params) ([]*miner.ExtendSectorExpiration2Params, error) {
	if len(params.Extensions) == 0 {
		return nil, xerrors.Errorf("no extensions to split")
	}

	if len(params.Extensions) == 1 {
		// Only one extension - split by sectors within it
		ext := params.Extensions[0]

		// Convert sectors bitfield to slice
		var sectorNums []abi.SectorNumber
		err := ext.Sectors.ForEach(func(i uint64) error {
			sectorNums = append(sectorNums, abi.SectorNumber(i))
			return nil
		})
		if err != nil {
			return nil, xerrors.Errorf("iterating sectors bitfield: %w", err)
		}

		if len(sectorNums) < 2 {
			return nil, xerrors.Errorf("cannot split single sector")
		}

		mid := len(sectorNums) / 2

		// Split SectorsWithClaims based on sector numbers
		claims1 := make([]miner.SectorClaim, 0)
		claims2 := make([]miner.SectorClaim, 0)

		sectorSet1 := make(map[abi.SectorNumber]bool)
		for _, sn := range sectorNums[:mid] {
			sectorSet1[sn] = true
		}

		for _, claim := range ext.SectorsWithClaims {
			if sectorSet1[claim.SectorNumber] {
				claims1 = append(claims1, claim)
			} else {
				claims2 = append(claims2, claim)
			}
		}

		return []*miner.ExtendSectorExpiration2Params{
			{
				Extensions: []miner.ExpirationExtension2{
					{
						Deadline:          ext.Deadline,
						Partition:         ext.Partition,
						Sectors:           sectorNumsToBitfield(sectorNums[:mid]),
						SectorsWithClaims: claims1,
						NewExpiration:     ext.NewExpiration,
					},
				},
			},
			{
				Extensions: []miner.ExpirationExtension2{
					{
						Deadline:          ext.Deadline,
						Partition:         ext.Partition,
						Sectors:           sectorNumsToBitfield(sectorNums[mid:]),
						SectorsWithClaims: claims2,
						NewExpiration:     ext.NewExpiration,
					},
				},
			},
		}, nil
	}

	// Multiple extensions - split them in half
	mid := len(params.Extensions) / 2

	return []*miner.ExtendSectorExpiration2Params{
		{
			Extensions: params.Extensions[:mid],
		},
		{
			Extensions: params.Extensions[mid:],
		},
	}, nil
}

func sectorNumsToBitfield(nums []abi.SectorNumber) bitfield.BitField {
	bf := bitfield.New()
	for _, n := range nums {
		bf.Set(uint64(n))
	}
	return bf
}

// validateSectorAgainstChain validates a single sector's metadata against on-chain state.
// Returns an error if validation fails, indicating the SP should be skipped.
func validateSectorAgainstChain(
	sn abi.SectorNumber,
	dbSector SectorMeta,
	si *miner.SectorOnChainInfo,
	claimsMap map[verifreg.ClaimId]verifreg.Claim,
	claimIdsBySector map[abi.SectorNumber][]verifreg.ClaimId,
) error {
	// Validate expiration
	if int64(si.Expiration) != dbSector.Expiration {
		return xerrors.Errorf("expiration mismatch for sector %d: db=%d chain=%d",
			sn, dbSector.Expiration, si.Expiration)
	}

	// Validate CC status
	isCC := len(si.DeprecatedDealIDs) == 0 && si.DealWeight.IsZero() && si.VerifiedDealWeight.IsZero()
	if isCC != dbSector.IsCC {
		return xerrors.Errorf("CC status mismatch for sector %d: db=%v chain=%v",
			sn, dbSector.IsCC, isCC)
	}

	// Validate claim terms if sector has claims in DB
	if dbSector.MinClaim != nil || dbSector.MaxClaim != nil {
		if claimsMap == nil || claimIdsBySector == nil {
			return xerrors.Errorf("sector %d has claims in DB but verifreg state not loaded", sn)
		}

		claimIds, hasClaims := claimIdsBySector[sn]
		if !hasClaims {
			return xerrors.Errorf("claim mismatch for sector %d: db has claims but chain doesn't", sn)
		}

		// Calculate actual claim terms from chain
		var minTermEnd *abi.ChainEpoch
		var maxTermEnd *abi.ChainEpoch
		for _, claimId := range claimIds {
			claim, ok := claimsMap[claimId]
			if !ok {
				continue
			}
			termEnd := claim.TermStart + claim.TermMax
			if minTermEnd == nil || termEnd < *minTermEnd {
				minTermEnd = &termEnd
			}
			if maxTermEnd == nil || termEnd > *maxTermEnd {
				maxTermEnd = &termEnd
			}
		}

		// Validate min claim term (must match exactly)
		if minTermEnd != nil && dbSector.MinClaim != nil && int64(*minTermEnd) != *dbSector.MinClaim {
			return xerrors.Errorf("min claim term mismatch for sector %d: db=%d chain=%d",
				sn, *dbSector.MinClaim, *minTermEnd)
		}

		// Validate max claim term (must match exactly)
		if maxTermEnd != nil && dbSector.MaxClaim != nil && int64(*maxTermEnd) != *dbSector.MaxClaim {
			return xerrors.Errorf("max claim term mismatch for sector %d: db=%d chain=%d",
				sn, *dbSector.MaxClaim, *maxTermEnd)
		}
	}

	return nil
}
