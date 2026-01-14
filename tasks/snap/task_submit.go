package snap

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"go.uber.org/multierr"
	"golang.org/x/exp/maps"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	miner13 "github.com/filecoin-project/go-state-types/builtin/v13/miner"
	verifregtypes9 "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/go-state-types/exitcode"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/curiochain"
	"github.com/filecoin-project/curio/lib/multictladdr"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/seal"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("update")

var ImmutableSubmitGate = abi.ChainEpoch(2) // don't submit more than 2 minutes before the deadline becomes immutable

type SubmitTaskNodeAPI interface {
	StateSectorPartition(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tsk types.TipSetKey) (*miner.SectorLocation, error)
	StateGetAllocation(ctx context.Context, clientAddr address.Address, allocationId verifregtypes9.AllocationId, tsk types.TipSetKey) (*verifregtypes9.Allocation, error)
	ChainHead(ctx context.Context) (*types.TipSet, error)

	WalletBalance(context.Context, address.Address) (types.BigInt, error)
	WalletHas(context.Context, address.Address) (bool, error)
	StateAccountKey(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	StateLookupID(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	StateSectorGetInfo(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tsk types.TipSetKey) (*miner.SectorOnChainInfo, error)

	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateMinerAvailableBalance(context.Context, address.Address, types.TipSetKey) (big.Int, error)
	StateMinerInitialPledgeForSector(ctx context.Context, sectorDuration abi.ChainEpoch, sectorSize abi.SectorSize, verifiedSize uint64, tsk types.TipSetKey) (types.BigInt, error)
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
	StateVMCirculatingSupplyInternal(ctx context.Context, tsk types.TipSetKey) (api.CirculatingSupply, error)

	StateMinerProvingDeadline(context.Context, address.Address, types.TipSetKey) (*dline.Info, error)
}

type updateBatchingConfig struct {
	MaxUpdateBatch   int
	Slack            time.Duration
	Timeout          time.Duration
	BaseFeeThreshold abi.TokenAmount
}

type submitConfig struct {
	batch                      updateBatchingConfig
	feeCfg                     *config.CurioFees
	RequireActivationSuccess   bool
	RequireNotificationSuccess bool
	CollateralFromMinerBalance bool
	DisableCollateralFallback  bool
}

type SubmitTask struct {
	db     *harmonydb.DB
	api    SubmitTaskNodeAPI
	bstore curiochain.CurioBlockstore

	sender *message.Sender
	as     *multictladdr.MultiAddressSelector
	cfg    submitConfig
}

func NewSubmitTask(db *harmonydb.DB, api SubmitTaskNodeAPI, bstore curiochain.CurioBlockstore,
	sender *message.Sender, as *multictladdr.MultiAddressSelector, cfg *config.CurioConfig) *SubmitTask {

	return &SubmitTask{
		db:     db,
		api:    api,
		bstore: bstore,

		sender: sender,
		as:     as,

		cfg: submitConfig{
			batch: updateBatchingConfig{
				// max mpool message is 64k
				// snap has 16x192 snarks + 50 or so bytes of other message overhead,
				// so we can only really fit ~20-16 updates in a message
				MaxUpdateBatch:   16,
				Slack:            cfg.Batching.Update.Slack,
				Timeout:          cfg.Batching.Update.Timeout,
				BaseFeeThreshold: abi.TokenAmount(cfg.Batching.Update.BaseFeeThreshold),
			},
			feeCfg:                     &cfg.Fees,
			RequireActivationSuccess:   cfg.Subsystems.RequireActivationSuccess,
			RequireNotificationSuccess: cfg.Subsystems.RequireNotificationSuccess,

			CollateralFromMinerBalance: cfg.Fees.CollateralFromMinerBalance,
			DisableCollateralFallback:  cfg.Fees.DisableCollateralFallback,
		},
	}
}

type updateCids struct {
	sealed   cid.Cid
	unsealed cid.Cid
}

func (s *SubmitTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		UpdateProof  int64 `db:"upgrade_proof"`

		RegSealProof int64 `db:"reg_seal_proof"`

		UpdateSealedCID   string `db:"update_sealed_cid"`
		UpdateUnsealedCID string `db:"update_unsealed_cid"`

		Proof []byte

		Deadline uint64 `db:"deadline"`
	}

	ctx := context.Background()

	err = s.db.Select(ctx, &tasks, `
		SELECT snp.sp_id, snp.sector_number, snp.upgrade_proof, sm.reg_seal_proof, snp.update_sealed_cid, snp.update_unsealed_cid, snp.proof, sm.deadline
		FROM sectors_snap_pipeline snp
		INNER JOIN sectors_meta sm ON snp.sp_id = sm.sp_id AND snp.sector_number = sm.sector_num
		WHERE snp.task_id_submit = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(tasks) == 0 {
		log.Errorw("expected at least 1 sector params, got 0 in submit task")
		return true, nil
	}

	maddr, err := address.NewIDAddress(uint64(tasks[0].SpID))
	if err != nil {
		return false, xerrors.Errorf("getting miner address: %w", err)
	}

	ts, err := s.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting chain head: %w", err)
	}

	mi, err := s.api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting miner info: %w", err)
	}

	regProof := tasks[0].RegSealProof
	updateProof := tasks[0].UpdateProof

	params := miner.ProveReplicaUpdates3Params{
		AggregateProof:             nil,
		UpdateProofsType:           abi.RegisteredUpdateProof(updateProof),
		AggregateProofType:         nil,
		RequireActivationSuccess:   s.cfg.RequireActivationSuccess,
		RequireNotificationSuccess: s.cfg.RequireNotificationSuccess,
		SectorUpdates:              make([]miner13.SectorUpdateManifest, 0, len(tasks)),
		SectorProofs:               make([][]byte, 0, len(tasks)),
	}

	collateral := big.Zero()
	transferMap := make(map[int64]*updateCids)

	for _, update := range tasks {
		update := update

		// Check miner ID is same for all sectors in batch
		tmpMaddr, err := address.NewIDAddress(uint64(update.SpID))
		if err != nil {
			return false, xerrors.Errorf("getting miner address: %w", err)
		}

		if maddr != tmpMaddr {
			return false, xerrors.Errorf("expected miner IDs to be same (%s) for all sectors in a batch but found %s", maddr.String(), tmpMaddr.String())
		}

		// Check proof types is same for all sectors in batch
		if update.RegSealProof != regProof {
			return false, xerrors.Errorf("expected proofs type to be same (%d) for all sectors in a batch but found %d for sector %d of miner %d", regProof, update.RegSealProof, update.SectorNumber, update.SpID)
		}

		if update.UpdateProof != updateProof {
			return false, xerrors.Errorf("expected registered proofs type to be same (%d) for all sectors in a batch but found %d for sector %d of miner %d", updateProof, update.UpdateProof, update.SectorNumber, update.SpID)
		}

		var pieces []struct {
			Manifest json.RawMessage `db:"direct_piece_activation_manifest"`
			Size     int64           `db:"piece_size"`
			Start    int64           `db:"direct_start_epoch"`
		}
		err = s.db.Select(ctx, &pieces, `
		SELECT direct_piece_activation_manifest, piece_size, direct_start_epoch
		FROM sectors_snap_initial_pieces
		WHERE sp_id = $1 AND sector_number = $2 ORDER BY piece_index ASC`, update.SpID, update.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("getting pieces: %w", err)
		}

		snum := abi.SectorNumber(update.SectorNumber)

		onChainInfo, err := s.api.StateSectorGetInfo(ctx, maddr, snum, ts.Key())
		if err != nil {
			return false, xerrors.Errorf("getting sector info: %w", err)
		}
		if onChainInfo == nil {
			return false, xerrors.Errorf("sector not found on chain")
		}
		if onChainInfo.SealProof != abi.RegisteredSealProof(regProof) {
			return false, xerrors.Errorf("Proof mismatch between on chain %d and local database %d for sector %d of miner %d", onChainInfo.SealProof, regProof, update.SectorNumber, update.SpID)
		}

		sl, err := s.api.StateSectorPartition(ctx, maddr, snum, types.EmptyTSK)
		if err != nil {
			return false, xerrors.Errorf("getting sector location: %w", err)
		}

		// Check that the sector isn't in an immutable deadline (or isn't about to be)
		curDl, err := s.api.StateMinerProvingDeadline(ctx, maddr, ts.Key())
		if err != nil {
			return false, xerrors.Errorf("getting current proving deadline: %w", err)
		}

		// Matches actor logic - https://github.com/filecoin-project/builtin-actors/blob/76abc47726bdbd8b478ef10e573c25957c786d1d/actors/miner/src/deadlines.rs#L65
		sectorDl := dline.NewInfo(curDl.PeriodStart, sl.Deadline, curDl.CurrentEpoch,
			curDl.WPoStPeriodDeadlines,
			curDl.WPoStProvingPeriod,
			curDl.WPoStChallengeWindow,
			curDl.WPoStChallengeLookback,
			curDl.FaultDeclarationCutoff)

		sectorDl = sectorDl.NextNotElapsed()
		firstImmutableEpoch := sectorDl.Open - curDl.WPoStChallengeWindow
		firstUnsafeEpoch := firstImmutableEpoch - ImmutableSubmitGate
		lastImmutableEpoch := sectorDl.Close

		if ts.Height() > firstUnsafeEpoch && ts.Height() < lastImmutableEpoch {
			closeTime := curiochain.EpochTime(ts, sectorDl.Close)

			log.Warnw("sector in unsafe window, delaying submit", "sp", update.SpID, "sector", update.SectorNumber, "cur_dl", curDl, "sector_dl", sectorDl, "close_time", closeTime)

			_, err := s.db.Exec(ctx, `UPDATE sectors_snap_pipeline SET
                                 task_id_submit = NULL, after_submit = FALSE, submit_after = $1
                             WHERE sp_id = $2 AND sector_number = $3`, closeTime, update.SpID, update.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating sector params: %w", err)
			}

			continue // Skip this sector
		}
		if ts.Height() >= lastImmutableEpoch {
			// the deadline math shouldn't allow this to ever happen, buuut just in case the math is wrong we also check the
			// upper bound of the proving window
			// (should never happen because if the current epoch is at deadline Close, NextNotElapsed will give us the next deadline)
			log.Errorw("sector in somehow past immutable window", "sp", update.SpID, "sector", update.SectorNumber, "cur_dl", curDl, "sector_dl", sectorDl)
		}

		// Process pieces, prepare PAMs
		pams := make([]miner.PieceActivationManifest, 0, len(pieces))
		var verifiedSize int64
		pieceCheckFailed := false
		for _, piece := range pieces {
			var pam *miner.PieceActivationManifest
			err = json.Unmarshal(piece.Manifest, &pam)
			if err != nil {
				return false, xerrors.Errorf("marshalling json to PieceManifest: %w", err)
			}
			unrecoverable, err := seal.AllocationCheck(ctx, s.api, pam, onChainInfo.Expiration, abi.ActorID(update.SpID), ts)
			if err != nil {
				if unrecoverable {
					_, err2 := s.db.Exec(ctx, `UPDATE sectors_snap_pipeline SET 
                                 failed = TRUE, failed_at = NOW(), failed_reason = 'alloc-check', failed_reason_msg = $1,
                                 task_id_submit = NULL, after_submit = FALSE
                             WHERE sp_id = $2 AND sector_number = $3`, err.Error(), update.SpID, update.SectorNumber)

					log.Errorw("allocation check failed with an unrecoverable issue", "sp", update.SpID, "sector", update.SectorNumber, "err", err)
					return true, xerrors.Errorf("allocation check failed with an unrecoverable issue: %w", multierr.Combine(err, err2))
				}

				pieceCheckFailed = true
				break
			}

			if pam.VerifiedAllocationKey != nil {
				verifiedSize += piece.Size
			}

			pams = append(pams, *pam)
		}

		if pieceCheckFailed {
			continue // Skip this sector
		}

		newSealedCID, err := cid.Parse(update.UpdateSealedCID)
		if err != nil {
			return false, xerrors.Errorf("parsing new sealed cid: %w", err)
		}
		newUnsealedCID, err := cid.Parse(update.UpdateUnsealedCID)
		if err != nil {
			return false, xerrors.Errorf("parsing new unsealed cid: %w", err)
		}

		transferMap[update.SectorNumber] = &updateCids{
			sealed:   newSealedCID,
			unsealed: newUnsealedCID,
		}

		ssize, err := onChainInfo.SealProof.SectorSize()
		if err != nil {
			return false, xerrors.Errorf("getting sector size: %w", err)
		}

		duration := onChainInfo.Expiration - ts.Height()

		secCollateral, err := s.api.StateMinerInitialPledgeForSector(ctx, duration, ssize, uint64(verifiedSize), ts.Key())
		if err != nil {
			return false, xerrors.Errorf("calculating pledge: %w", err)
		}

		secCollateral = big.Sub(secCollateral, onChainInfo.InitialPledge)
		if secCollateral.LessThan(big.Zero()) {
			secCollateral = big.Zero()
		}

		collateral = big.Add(collateral, secCollateral)

		// Prepare params
		params.SectorUpdates = append(params.SectorUpdates, miner13.SectorUpdateManifest{
			Sector:       snum,
			Deadline:     sl.Deadline,
			Partition:    sl.Partition,
			NewSealedCID: newSealedCID,
			Pieces:       pams,
		})
		params.SectorProofs = append(params.SectorProofs, update.Proof)
	}

	if len(params.SectorUpdates) == 0 {
		return false, xerrors.Errorf("no sector updates")
	}

	enc := new(bytes.Buffer)
	if err := params.MarshalCBOR(enc); err != nil {
		return false, xerrors.Errorf("could not serialize commit params: %w", err)
	}

	if s.cfg.CollateralFromMinerBalance {
		if s.cfg.DisableCollateralFallback {
			collateral = big.Zero()
		}
		balance, err := s.api.StateMinerAvailableBalance(ctx, maddr, types.EmptyTSK)
		if err != nil {
			if err != nil {
				return false, xerrors.Errorf("getting miner balance: %w", err)
			}
		}
		collateral = big.Sub(collateral, balance)
		if collateral.LessThan(big.Zero()) {
			collateral = big.Zero()
		}
	}

	maxFee := s.cfg.feeCfg.MaxUpdateBatchGasFee.FeeForSectors(len(params.SectorUpdates))

	a, _, err := s.as.AddressFor(ctx, s.api, maddr, mi, api.CommitAddr, collateral, big.Zero())
	if err != nil {
		return false, xerrors.Errorf("getting address for precommit: %w", err)
	}

	msg := &types.Message{
		To:     maddr,
		From:   a,
		Method: builtin.MethodsMiner.ProveReplicaUpdates3,
		Params: enc.Bytes(),
		Value:  collateral,
	}

	mss := &api.MessageSendSpec{
		MaxFee: maxFee,
	}

	mcid, err := s.sender.Send(ctx, msg, mss, "update")
	if err != nil {
		return false, xerrors.Errorf("pushing message to mpool: %w", err)
	}

	_, err = s.db.Exec(ctx, `UPDATE sectors_snap_pipeline SET prove_msg_cid = $1, task_id_submit = NULL, after_submit = TRUE WHERE task_id_submit = $2`, mcid.String(), taskID)
	if err != nil {
		return false, xerrors.Errorf("updating sector params: %w", err)
	}

	_, err = s.db.Exec(ctx, `INSERT INTO message_waits (signed_message_cid) VALUES ($1)`, mcid)
	if err != nil {
		return false, xerrors.Errorf("inserting into message_waits: %w", err)
	}

	if err := s.transferUpdatedSectorData(ctx, tasks[0].SpID, transferMap, mcid); err != nil {
		return false, xerrors.Errorf("updating sector meta: %w", err)
	}

	return true, nil
}

func (s *SubmitTask) transferUpdatedSectorData(ctx context.Context, spID int64, transferMap map[int64]*updateCids, mcid cid.Cid) error {

	commit, err := s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		for sectorNum, cids := range transferMap {
			sectorNum, cids := sectorNum, cids
			n, err := tx.Exec(`UPDATE sectors_meta SET cur_sealed_cid = $1,
	                        		cur_unsealed_cid = $2, msg_cid_update = $3
	                        		WHERE sp_id = $4 AND sector_num = $5`, cids.sealed.String(), cids.unsealed.String(), mcid.String(), spID, sectorNum)

			if err != nil {
				return false, xerrors.Errorf("updating sector meta: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("updating sector meta: expected to update 1 row, but updated %d rows", n)
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return err
	}

	if !commit {
		return xerrors.Errorf("updating sector meta: transaction failed")
	}

	// Execute the query for piece metadata
	if _, err := s.db.Exec(ctx, `
        INSERT INTO sectors_meta_pieces (
            sp_id,
            sector_num,
            piece_num,
            piece_cid,
            piece_size,
            requested_keep_data,
            raw_data_size,
            start_epoch,
            orig_end_epoch,
            f05_deal_id,
            ddo_pam,
            f05_deal_proposal                          
        )
        SELECT
            sp_id,
            sector_number AS sector_num,
            piece_index AS piece_num,
            piece_cid,
            piece_size,
            not data_delete_on_finalize as requested_keep_data,
            data_raw_size,
            direct_start_epoch as start_epoch,
            direct_end_epoch as orig_end_epoch,
            NULL,
            direct_piece_activation_manifest as ddo_pam,
            NULL
        FROM
            sectors_snap_initial_pieces
        WHERE
            sp_id = $1 AND
            sector_number = ANY($2)
        ON CONFLICT (sp_id, sector_num, piece_num) DO UPDATE SET
            piece_cid = excluded.piece_cid,
            piece_size = excluded.piece_size,
            requested_keep_data = excluded.requested_keep_data,
            raw_data_size = excluded.raw_data_size,
            start_epoch = excluded.start_epoch,
            orig_end_epoch = excluded.orig_end_epoch,
            f05_deal_id = excluded.f05_deal_id,
            ddo_pam = excluded.ddo_pam,
            f05_deal_proposal = excluded.f05_deal_proposal;
    `, spID, maps.Keys(transferMap)); err != nil {
		return fmt.Errorf("failed to insert/update sector_meta_pieces: %w", err)
	}

	return nil
}

func (s *SubmitTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (s *SubmitTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "UpdateBatch",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(MinSnapSchedInterval, func(taskFunc harmonytask.AddTaskFunc) error {
			return s.schedule(context.Background(), taskFunc)
		}),
	}
}

func (s *SubmitTask) schedule(ctx context.Context, addTaskFunc harmonytask.AddTaskFunc) error {
	var done bool

	for !done {
		addTaskFunc(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			//----------------------------------
			// 1) Gather candidate tasks to schedule
			//----------------------------------
			var rawRows []struct {
				SpID          int64        `db:"sp_id"`
				SectorNumber  int64        `db:"sector_number"`
				UpgradeProof  int64        `db:"upgrade_proof"`
				UpdateReadyAt sql.NullTime `db:"update_ready_at"`
				StartEpoch    int64        `db:"smallest_direct_start_epoch"`
			}

			err := tx.Select(&rawRows, `
				SELECT 
					ssp.sp_id,
					ssp.sector_number,
					ssp.upgrade_proof,
					ssp.update_ready_at,
					MIN(ssip.direct_start_epoch) AS smallest_direct_start_epoch
				FROM 
					sectors_snap_pipeline ssp
				JOIN 
					sectors_snap_initial_pieces ssip
				  ON (ssp.sp_id = ssip.sp_id AND ssp.sector_number = ssip.sector_number)
				WHERE 
					ssp.failed = FALSE
					AND ssp.after_encode = TRUE
					AND ssp.after_prove = TRUE
					AND ssp.after_submit = FALSE
					AND (ssp.submit_after IS NULL OR ssp.submit_after < NOW())
					AND ssp.task_id_submit IS NULL
					AND ssp.update_ready_at IS NOT NULL
				GROUP BY 
					ssp.sp_id, ssp.sector_number, ssp.upgrade_proof, ssp.update_ready_at
				ORDER BY 
					ssp.sp_id, ssp.sector_number
			`)
			if err != nil {
				return false, xerrors.Errorf("selecting candidate snap updates: %w", err)
			}

			if len(rawRows) == 0 {
				// No tasks left to schedule => set done=true and do not commit
				done = true
				return false, nil
			}

			//----------------------------------
			// 2) Group them by (sp_id, upgradeProof)
			//----------------------------------
			type rowInfo struct {
				SectorNumber  int64
				UpdateReadyAt *time.Time
				StartEpoch    int64
			}
			batchMap := make(map[int64]map[int64][]rowInfo) // sp_id -> upgradeProof -> []rowInfo

			for _, row := range rawRows {
				upMap, ok := batchMap[row.SpID]
				if !ok {
					upMap = make(map[int64][]rowInfo)
					batchMap[row.SpID] = upMap
				}
				if row.UpdateReadyAt.Valid {
					upMap[row.UpgradeProof] = append(upMap[row.UpgradeProof], rowInfo{
						SectorNumber:  row.SectorNumber,
						UpdateReadyAt: &row.UpdateReadyAt.Time,
						StartEpoch:    row.StartEpoch,
					})
				}
			}

			//----------------------------------
			// 3) Try to find exactly one group that meets scheduling conditions
			//----------------------------------
			ts, err := s.api.ChainHead(ctx)
			if err != nil {
				// Serious error => rollback
				return false, xerrors.Errorf("chain head error: %w", err)
			}

			// We'll iterate all groups, and if we find one that meets conditions, we schedule & commit.
			for spID, proofMap := range batchMap {
				for _, rows := range proofMap {
					//----------------------------------
					// 4) Possibly do sub-batching
					//----------------------------------
					maxBatch := s.cfg.batch.MaxUpdateBatch

					toSchedule := rows
					if maxBatch != 0 && len(rows) > maxBatch {
						toSchedule = rows[:maxBatch]
					}

					//----------------------------------
					// 5) Check scheduling conditions (slack, baseFee, etc.)
					//----------------------------------
					var earliestStart int64 = math.MaxInt64
					var earliestTime time.Time

					for _, row := range toSchedule {
						if row.StartEpoch < earliestStart {
							earliestStart = row.StartEpoch
						}
						if row.UpdateReadyAt != nil {
							if earliestTime.IsZero() || row.UpdateReadyAt.Before(earliestTime) {
								earliestTime = *row.UpdateReadyAt
							}
						}
					}

					deltaBlocks := earliestStart - int64(ts.Height())
					timeUntil := time.Duration(deltaBlocks*int64(build.BlockDelaySecs)) * time.Second
					scheduleNow := false

					// Slack
					if timeUntil < s.cfg.batch.Slack {
						scheduleNow = true
					}

					// Base fee check
					if !scheduleNow {
						if ts.MinTicketBlock().ParentBaseFee.LessThan(s.cfg.batch.BaseFeeThreshold) {
							scheduleNow = true
						}
					}

					// Timeout since earliestTime
					if !scheduleNow && !earliestTime.IsZero() {
						if time.Since(earliestTime) > s.cfg.batch.Timeout {
							scheduleNow = true
						}
					}

					if !scheduleNow {
						// This group isn't ready to schedule. Move on to the next group.
						continue
					}

					//----------------------------------
					// 6) Actually schedule: set task_id_submit=taskID for chosen rows
					//----------------------------------
					var scheduled int
					for _, row := range toSchedule {
						n, err := tx.Exec(`
							UPDATE sectors_snap_pipeline
							SET task_id_submit = $1,
							    submit_after = NULL
							WHERE sp_id = $2
							  AND sector_number = $3
							  AND task_id_submit IS NULL
						`, taskID, spID, row.SectorNumber)
						if err != nil {
							return false, xerrors.Errorf("failed to set task_id_submit: %w", err)
						}
						scheduled += n
					}

					// We scheduled this group => commit & exit the transaction callback
					return scheduled > 0, nil
				}
			}

			// If we got here, we didn't find *any* group meeting conditions => no scheduling
			// So let's set done = true to avoid indefinite looping.
			done = true
			return false, nil
		})
	}

	comm, err := s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// update landed
		var tasks []struct {
			SpID         int64 `db:"sp_id"`
			SectorNumber int64 `db:"sector_number"`
		}

		err = tx.Select(&tasks, `SELECT sp_id, sector_number FROM sectors_snap_pipeline WHERE after_encode = TRUE AND after_prove = TRUE AND after_prove_msg_success = FALSE AND after_submit = TRUE`)
		if err != nil {
			return false, xerrors.Errorf("getting tasks: %w", err)
		}

		for _, t := range tasks {
			if err := s.updateLanded(ctx, tx, t.SpID, t.SectorNumber); err != nil {
				log.Errorw("updating landed", "sp", t.SpID, "sector", t.SectorNumber, "err", err)
				return false, xerrors.Errorf("updating landed for sp %d, sector %d: %w", t.SpID, t.SectorNumber, err)
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("updating landed: %w", err)
	}

	if !comm {
		return xerrors.Errorf("updating landed: transaction failed")
	}

	return nil
}

func (s *SubmitTask) updateLanded(ctx context.Context, tx *harmonydb.Tx, spId, sectorNum int64) error {
	var execResult []struct {
		ProveMsgCID          string `db:"prove_msg_cid"`
		UpdateSealedCID      string `db:"update_sealed_cid"`
		ExecutedTskCID       string `db:"executed_tsk_cid"`
		ExecutedTskEpoch     int64  `db:"executed_tsk_epoch"`
		ExecutedMsgCID       string `db:"executed_msg_cid"`
		ExecutedRcptExitCode int64  `db:"executed_rcpt_exitcode"`
		ExecutedRcptGasUsed  int64  `db:"executed_rcpt_gas_used"`
	}

	err := tx.Select(&execResult, `SELECT spipeline.prove_msg_cid, spipeline.update_sealed_cid, executed_tsk_cid, executed_tsk_epoch, executed_msg_cid, executed_rcpt_exitcode, executed_rcpt_gas_used
					FROM sectors_snap_pipeline spipeline
					JOIN message_waits ON spipeline.prove_msg_cid = message_waits.signed_message_cid
					WHERE sp_id = $1 AND sector_number = $2 AND executed_tsk_epoch IS NOT NULL`, spId, sectorNum)
	if err != nil {
		return xerrors.Errorf("failed to query message_waits: %w", err)
	}

	if len(execResult) > 0 {
		maddr, err := address.NewIDAddress(uint64(spId))
		if err != nil {
			return err
		}
		switch exitcode.ExitCode(execResult[0].ExecutedRcptExitCode) {
		case exitcode.Ok:
			// good, noop
		case exitcode.SysErrInsufficientFunds, exitcode.ErrInsufficientFunds:
			fallthrough
		case exitcode.SysErrOutOfGas, exitcode.ErrIllegalArgument:
			// just retry

			// illegal argument typically stems from immutable deadline
			// err message like 'message failed with backtrace: 00: f0123 (method 35) -- invalid update 0 while requiring activation success: cannot upgrade sectors in immutable deadline 27, skipping sector 6123 (16) (RetCode=16)'
			n, err := tx.Exec(`UPDATE sectors_snap_pipeline SET
						after_prove_msg_success = FALSE, after_submit = FALSE
						WHERE sp_id = $1 AND sector_number = $2 AND after_prove_msg_success = FALSE AND after_submit = TRUE`, spId, sectorNum)
			if err != nil {
				return xerrors.Errorf("update sectors_snap_pipeline to retry prove send: %w", err)
			}
			if n == 0 {
				return xerrors.Errorf("update sectors_snap_pipeline to retry prove send: no rows updated")
			}
			return nil
		case exitcode.ErrNotFound:
			// message not found, but maybe it's fine?

			si, err := s.api.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(sectorNum), types.EmptyTSK)
			if err != nil {
				return xerrors.Errorf("get sector info: %w", err)
			}
			if si != nil && si.SealedCID.String() == execResult[0].UpdateSealedCID {
				return nil
			}

			return xerrors.Errorf("sector info after prove message not found not as expected")
		default:
			return xerrors.Errorf("prove message (m:%s) failed with exit code %s", execResult[0].ProveMsgCID, exitcode.ExitCode(execResult[0].ExecutedRcptExitCode))
		}

		si, err := s.api.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(sectorNum), types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("get sector info: %w", err)
		}

		if si == nil {
			log.Errorw("todo handle missing sector info (not found after cron)", "sp", spId, "sector", sectorNum, "exec_epoch", execResult[0].ExecutedTskEpoch, "exec_tskcid", execResult[0].ExecutedTskCID, "msg_cid", execResult[0].ExecutedMsgCID)
			return xerrors.Errorf("sector info not found after prove message SP %d, sector %d, exec_epoch %d, exec_tskcid %s, msg_cid %s", spId, sectorNum, execResult[0].ExecutedTskEpoch, execResult[0].ExecutedTskCID, execResult[0].ExecutedMsgCID)
		} else {
			if si.SealedCID.String() != execResult[0].UpdateSealedCID {
				log.Errorw("sector sealed CID mismatch after update?!", "sp", spId, "sector", sectorNum, "exec_epoch", execResult[0].ExecutedTskEpoch, "exec_tskcid", execResult[0].ExecutedTskCID, "msg_cid", execResult[0].ExecutedMsgCID)
				return xerrors.Errorf("sector sealed CID mismatch after update?! SP %d, sector %d, exec_epoch %d, exec_tskcid %s, msg_cid %s", spId, sectorNum, execResult[0].ExecutedTskEpoch, execResult[0].ExecutedTskCID, execResult[0].ExecutedMsgCID)
			}
			// yay!

			_, err := tx.Exec(`UPDATE sectors_snap_pipeline SET
						after_prove_msg_success = TRUE, prove_msg_tsk = $1
						WHERE sp_id = $2 AND sector_number = $3 AND after_prove_msg_success = FALSE`,
				execResult[0].ExecutedTskCID, spId, sectorNum)
			if err != nil {
				return xerrors.Errorf("update sectors_snap_pipeline: %w", err)
			}

			n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET sealed = TRUE WHERE sp_id = $1 AND sector = $2 AND sealed = FALSE`, spId, sectorNum)
			if err != nil {
				return xerrors.Errorf("update market_mk12_deal_pipeline: %w", err)
			}
			log.Debugw("marked mk12 deals as sealed", "sp", spId, "sector", sectorNum, "count", n)

			n, err = tx.Exec(`UPDATE market_mk20_pipeline SET sealed = TRUE WHERE sp_id = $1 AND sector = $2 AND sealed = FALSE`, spId, sectorNum)
			if err != nil {
				return xerrors.Errorf("update market_mk20_pipeline: %w", err)
			}
			log.Debugw("marked mk20 deals as sealed", "sp", spId, "sector", sectorNum, "count", n)
		}
	}

	return nil
}

func (s *SubmitTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (s *SubmitTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := s.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (s *SubmitTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_snap_pipeline WHERE task_id_submit = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&SubmitTask{})
var _ harmonytask.TaskInterface = &SubmitTask{}
