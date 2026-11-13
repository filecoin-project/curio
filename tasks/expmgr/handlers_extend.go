package expmgr

import (
	"bytes"
	"context"
	"encoding/hex"
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
}

type extendPresetConfig struct {
	Name                 string
	SpID                 int64
	InfoBucketAboveDays  int
	InfoBucketBelowDays  int
	TargetExpirationDays int64
	MaxCandidateDays     int64
	CC                   *bool
}

// handleExtend extends eligible sectors for a validated preset.
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

	nv, err := e.chain.StateNetworkVersion(ctx, head.Key())
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
	var sectors []SectorMeta

	// Exclude sectors in snap pipeline or with open pieces (not yet finalized).
	if cfg.CC != nil {
		// CC filter only
		err = e.db.Select(ctx, &sectors, `
			SELECT sm.sector_num, sm.expiration_epoch, sm.deadline, sm.partition, sm.is_cc
			FROM sectors_meta sm
			WHERE sm.sp_id = $1
			  AND sm.expiration_epoch IS NOT NULL
			  AND sm.expiration_epoch > $2
			  AND sm.expiration_epoch < $3
			  AND sm.is_live = TRUE
			  AND sm.is_faulty = FALSE
			  AND sm.deadline IS NOT NULL
			  AND sm.partition IS NOT NULL
			  AND sm.is_cc = $4
			  AND NOT EXISTS (SELECT 1 FROM sectors_snap_pipeline ssp WHERE ssp.sp_id = sm.sp_id AND ssp.sector_number = sm.sector_num)
			  AND NOT EXISTS (SELECT 1 FROM open_sector_pieces osp WHERE osp.sp_id = sm.sp_id AND osp.sector_number = sm.sector_num)
			ORDER BY sm.deadline, sm.partition, sm.sector_num`,
			cfg.SpID, int64(currEpoch), int64(maxCandidateEpoch), *cfg.CC)
	} else {
		// No filters
		err = e.db.Select(ctx, &sectors, `
			SELECT sm.sector_num, sm.expiration_epoch, sm.deadline, sm.partition, sm.is_cc
			FROM sectors_meta sm
			WHERE sm.sp_id = $1
			  AND sm.expiration_epoch IS NOT NULL
			  AND sm.expiration_epoch > $2
			  AND sm.expiration_epoch < $3
			  AND sm.is_live = TRUE
			  AND sm.is_faulty = FALSE
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
	mi, err := e.chain.StateMinerInfo(ctx, maddr, head.Key())
	if err != nil {
		return false, xerrors.Errorf("getting miner info: %w", err)
	}

	// Load actor state for sector info
	mact, err := e.chain.StateGetActor(ctx, maddr, head.Key())
	if err != nil {
		return false, xerrors.Errorf("getting miner actor: %w", err)
	}

	tbs := blockstore.NewTieredBstore(blockstore.NewAPIBlockstore(e.chain), blockstore.NewMemory())
	adtStore := adt.WrapStore(ctx, cbor.NewCborStore(tbs))
	mas, err := miner.Load(adtStore, mact)
	if err != nil {
		return false, xerrors.Errorf("loading miner state: %w", err)
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
		if totalSectors >= MaxExtendsPerRound {
			log.Warnw("hit max sectors per message limit",
				"preset", cfg.Name,
				"sp_id", cfg.SpID,
				"limit", MaxExtendsPerRound)
			break
		}

		// Load partition to check LiveSectors - sectors not in LiveSectors are terminated
		// and cannot be extended (they're not in the expiration queue)
		dl, err := mas.LoadDeadline(dlPart.Deadline)
		if err != nil {
			log.Warnw("failed to load deadline, skipping partition",
				"preset", cfg.Name,
				"sp_id", cfg.SpID,
				"deadline", dlPart.Deadline,
				"error", err)
			continue
		}

		part, err := dl.LoadPartition(dlPart.Partition)
		if err != nil {
			log.Warnw("failed to load partition, skipping",
				"preset", cfg.Name,
				"sp_id", cfg.SpID,
				"deadline", dlPart.Deadline,
				"partition", dlPart.Partition,
				"error", err)
			continue
		}

		liveSectors, err := part.LiveSectors()
		if err != nil {
			log.Warnw("failed to get live sectors, skipping partition",
				"preset", cfg.Name,
				"sp_id", cfg.SpID,
				"deadline", dlPart.Deadline,
				"partition", dlPart.Partition,
				"error", err)
			continue
		}

		faultySectors, err := part.FaultySectors()
		if err != nil {
			log.Warnw("failed to get faulty sectors, skipping partition",
				"preset", cfg.Name,
				"sp_id", cfg.SpID,
				"deadline", dlPart.Deadline,
				"partition", dlPart.Partition,
				"error", err)
			continue
		}

		numbersToExtend := make([]abi.SectorNumber, 0, len(sectorNums))

		for _, sn := range sectorNums {
			if totalSectors >= MaxExtendsPerRound {
				break
			}

			si := sectorInfoMap[sn]
			if si == nil {
				continue
			}

			// Check if sector is live - non-live sectors are terminated and not in expiration queue
			isLive, err := liveSectors.IsSet(uint64(sn))
			if err != nil {
				log.Warnw("failed to check if sector is live",
					"sector", sn,
					"error", err)
				continue
			}
			if !isLive {
				log.Warnw("skipping sector: not in LiveSectors (terminated?)",
					"preset", cfg.Name,
					"sp_id", cfg.SpID,
					"sector", sn,
					"deadline", dlPart.Deadline,
					"partition", dlPart.Partition)
				continue
			}

			// Check if sector is not faulty
			isFaulty, err := faultySectors.IsSet(uint64(sn))
			if err != nil {
				log.Warnw("failed to check if sector is faulty",
					"sector", sn,
					"error", err)
				continue
			}
			if isFaulty {
				log.Debugw("skipping sector: sector is faulty",
					"preset", cfg.Name,
					"sp_id", cfg.SpID,
					"sector", sn,
					"deadline", dlPart.Deadline,
					"partition", dlPart.Partition)
				continue
			}

			// Check if target expiration exceeds sector's max lifetime
			maxLifetime := si.Activation + policy.GetSectorMaxLifetime(si.SealProof, nv)
			if targetExpEpoch > maxLifetime {
				log.Warnw("skipping sector: target expiration exceeds max lifetime",
					"sector", sn,
					"target_expiration", targetExpEpoch,
					"max_lifetime", maxLifetime)
				continue
			}

			// Check if extension from current expiration exceeds max allowed extension
			if targetExpEpoch-si.Expiration > maxExtension {
				log.Warnw("skipping sector: extension exceeds max allowed extension",
					"sector", sn,
					"current_expiration", si.Expiration,
					"target_expiration", targetExpEpoch,
					"extension", targetExpEpoch-si.Expiration,
					"max_extension", maxExtension)
				continue
			}

			// Skip if new expiration is not significantly different
			if targetExpEpoch <= si.Expiration || (targetExpEpoch-si.Expiration) < 7*epochsPerDay {
				continue
			}

			// Get the sector's metadata from the database query result
			dbSector := sectors[0] // Find matching sector from our query
			for _, s := range sectors {
				if abi.SectorNumber(s.SectorNum) == sn {
					dbSector = s
					break
				}
			}

			// Validate sector metadata against on-chain state
			if err := validateSectorAgainstChain(sn, dbSector, si); err != nil {
				log.Errorw("sector metadata validation failed, skipping sector",
					"preset", cfg.Name,
					"sp_id", cfg.SpID,
					"sector", sn,
					"error", err)
				continue
			}

			numbersToExtend = append(numbersToExtend, sn)
			totalSectors++
		}

		if len(numbersToExtend) == 0 {
			continue
		}

		extensions = append(extensions, miner.ExpirationExtension2{
			Deadline:      dlPart.Deadline,
			Partition:     dlPart.Partition,
			Sectors:       sectorNumsToBitfield(numbersToExtend),
			NewExpiration: targetExpEpoch,
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
			if len(sentCids) > 0 {
				log.Errorw("failed to send message, proceeding with already sent messages",
					"preset", cfg.Name,
					"sp_id", cfg.SpID,
					"failed_index", i,
					"total_messages", len(msgs),
					"sent_so_far", len(sentCids),
					"error", err)
				break
			}
			return false, xerrors.Errorf("sending message %d: %w", i, err)
		}

		sentCids = append(sentCids, msgCid)

		log.Infow("sent extension message",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"message_index", i+1,
			"total_messages", len(msgs),
			"msg_cid", msgCid,
			"worker", mi.Worker,
			"miner", maddr)
	}

	trackedCid := sentCids[len(sentCids)-1]

	// Update the database with the last message CID before inserting into message_waits,
	// so the landing trigger can't race-miss the update.
	_, err = e.db.Exec(ctx, `
		UPDATE sectors_exp_manager_sp
		SET last_message_cid = $2,
		    last_run_at = CURRENT_TIMESTAMP
		WHERE sp_id = $1 AND preset_name = $3
	`, cfg.SpID, trackedCid.String(), cfg.Name)
	if err != nil {
		return false, xerrors.Errorf("updating sectors_exp_manager_sp: %w", err)
	}

	for i, sc := range sentCids {
		_, err = e.db.Exec(ctx, `INSERT INTO message_waits (signed_message_cid) VALUES ($1)`, sc.String())
		if err != nil {
			return false, xerrors.Errorf("inserting message %d into message_waits: %w", i, err)
		}
	}

	log.Infow("completed extend action",
		"preset", cfg.Name,
		"sp_id", cfg.SpID,
		"total_messages_sent", len(sentCids),
		"tracked_message_cid", trackedCid.String())

	return true, nil
}

func countParamsExtensions(params *miner.ExtendSectorExpiration2Params) (uint64, error) {
	var total uint64
	for _, ext := range params.Extensions {
		count, err := ext.Sectors.Count()
		if err != nil {
			return 0, xerrors.Errorf("counting sectors bitfield: %w", err)
		}
		total += count
	}
	return total, nil
}

func (e *ExpMgrTask) buildExtendMessage(ctx context.Context, cfg extendPresetConfig, maddr, worker address.Address, params *miner.ExtendSectorExpiration2Params) ([]*types.Message, error) {
	extCount, err := countParamsExtensions(params)
	if err != nil {
		return nil, xerrors.Errorf("counting params extensions: %w", err)
	}

	if extCount > MaxExtendPerMessage {
		log.Infow("pre-splitting message before gas estimation",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"extension_count", extCount,
			"max_per_message", MaxExtendPerMessage)

		splitParamsList, err := splitParams(params)
		if err != nil {
			return nil, xerrors.Errorf("pre-splitting params: %w", err)
		}

		var allMessages []*types.Message
		for i, sp := range splitParamsList {
			msgs, err := e.buildExtendMessage(ctx, cfg, maddr, worker, sp)
			if err != nil {
				return nil, xerrors.Errorf("building pre-split message %d: %w", i, err)
			}
			allMessages = append(allMessages, msgs...)
		}

		return allMessages, nil
	}

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
		var b bytes.Buffer
		cerr := msg.MarshalCBOR(&b)
		if cerr != nil {
			return nil, xerrors.Errorf("marshalling message: %w", cerr)
		}

		log.Errorw("failed to estimate gas",
			"preset", cfg.Name,
			"sp_id", cfg.SpID,
			"error", err,
			"msghex", hex.EncodeToString(b.Bytes()))
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

// splitParams divides extension declarations into smaller messages; callers must omit claims.
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

		return []*miner.ExtendSectorExpiration2Params{
			{
				Extensions: []miner.ExpirationExtension2{
					{
						Deadline:      ext.Deadline,
						Partition:     ext.Partition,
						Sectors:       sectorNumsToBitfield(sectorNums[:mid]),
						NewExpiration: ext.NewExpiration,
					},
				},
			},
			{
				Extensions: []miner.ExpirationExtension2{
					{
						Deadline:      ext.Deadline,
						Partition:     ext.Partition,
						Sectors:       sectorNumsToBitfield(sectorNums[mid:]),
						NewExpiration: ext.NewExpiration,
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

// sectorNumsToBitfield collects sector numbers into a bitfield.
func sectorNumsToBitfield(nums []abi.SectorNumber) bitfield.BitField {
	bf := bitfield.New()
	for _, n := range nums {
		bf.Set(uint64(n))
	}
	return bf
}

// validateSectorAgainstChain validates a single sector's metadata against on-chain state.
// The caller must supply on-chain info for sn.
func validateSectorAgainstChain(
	sn abi.SectorNumber,
	dbSector SectorMeta,
	si *miner.SectorOnChainInfo,
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

	return nil
}
