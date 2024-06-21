package snap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	miner13 "github.com/filecoin-project/go-state-types/builtin/v13/miner"
	verifregtypes9 "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/go-state-types/exitcode"

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
	"github.com/filecoin-project/lotus/chain/actors/adt"
	builtin2 "github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("update")

var initialPledgeNum = types.NewInt(110)
var initialPledgeDen = types.NewInt(100)

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
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
	StateVMCirculatingSupplyInternal(ctx context.Context, tsk types.TipSetKey) (api.CirculatingSupply, error)
}

type submitConfig struct {
	maxFee                     types.FIL
	RequireActivationSuccess   bool
	RequireNotificationSuccess bool
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
			maxFee:                     cfg.Fees.MaxCommitGasFee, // todo snap-specific
			RequireActivationSuccess:   cfg.Subsystems.RequireActivationSuccess,
			RequireNotificationSuccess: cfg.Subsystems.RequireNotificationSuccess,
		},
	}
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
	}

	ctx := context.Background()

	err = s.db.Select(ctx, &tasks, `
		SELECT snp.sp_id, snp.sector_number, snp.upgrade_proof, sm.reg_seal_proof, snp.update_sealed_cid, snp.update_unsealed_cid, snp.proof
		FROM sectors_snap_pipeline snp
		INNER JOIN sectors_meta sm ON snp.sp_id = sm.sp_id AND snp.sector_number = sm.sector_num
		WHERE snp.task_id_submit = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(tasks))
	}

	update := tasks[0]

	var pieces []struct {
		Manifest json.RawMessage `db:"direct_piece_activation_manifest"`
		Size     int64           `db:"piece_size"`
	}
	err = s.db.Select(ctx, &pieces, `
		SELECT direct_piece_activation_manifest, piece_size
		FROM sectors_snap_initial_pieces
		WHERE sp_id = $1 AND sector_number = $2 ORDER BY piece_index ASC`, update.SpID, update.SectorNumber)
	if err != nil {
		return false, xerrors.Errorf("getting pieces: %w", err)
	}

	ts, err := s.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting chain head: %w", err)
	}

	var pams []miner.PieceActivationManifest
	var weight, weightVerif = big.Zero(), big.Zero()
	for _, piece := range pieces {
		var pam *miner.PieceActivationManifest
		err = json.Unmarshal(piece.Manifest, &pam)
		if err != nil {
			return false, xerrors.Errorf("marshalling json to PieceManifest: %w", err)
		}
		err = seal.AllocationCheck(ctx, s.api, pam, nil, abi.ActorID(update.SpID), ts)
		if err != nil {
			return false, err
		}

		if pam.VerifiedAllocationKey != nil {
			weightVerif = big.Add(weightVerif, abi.NewStoragePower(piece.Size))
		} else {
			weight = big.Add(weight, abi.NewStoragePower(piece.Size))
		}

		pams = append(pams, *pam)
	}

	newSealedCID, err := cid.Parse(update.UpdateSealedCID)
	if err != nil {
		return false, xerrors.Errorf("parsing new sealed cid: %w", err)
	}

	maddr, err := address.NewIDAddress(uint64(update.SpID))
	if err != nil {
		return false, xerrors.Errorf("parsing miner address: %w", err)
	}

	snum := abi.SectorNumber(update.SectorNumber)

	sl, err := s.api.StateSectorPartition(ctx, maddr, snum, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting sector location: %w", err)
	}

	params := miner.ProveReplicaUpdates3Params{
		SectorUpdates: []miner13.SectorUpdateManifest{
			{
				Sector:       snum,
				Deadline:     sl.Deadline,
				Partition:    sl.Partition,
				NewSealedCID: newSealedCID,
				Pieces:       pams,
			},
		},
		SectorProofs:               [][]byte{update.Proof},
		AggregateProof:             nil,
		UpdateProofsType:           abi.RegisteredUpdateProof(update.UpdateProof),
		AggregateProofType:         nil,
		RequireActivationSuccess:   s.cfg.RequireActivationSuccess,
		RequireNotificationSuccess: s.cfg.RequireNotificationSuccess,
	}

	enc := new(bytes.Buffer)
	if err := params.MarshalCBOR(enc); err != nil {
		return false, xerrors.Errorf("could not serialize commit params: %w", err)
	}

	mi, err := s.api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting miner info: %w", err)
	}

	onChainInfo, err := s.api.StateSectorGetInfo(ctx, maddr, snum, ts.Key())
	if err != nil {
		return false, xerrors.Errorf("getting sector info: %w", err)
	}
	if onChainInfo == nil {
		return false, xerrors.Errorf("sector not found on chain")
	}

	ssize, err := onChainInfo.SealProof.SectorSize()
	if err != nil {
		return false, xerrors.Errorf("getting sector size: %w", err)
	}

	duration := onChainInfo.Expiration - ts.Height()
	weightUpdate := builtin2.QAPowerForWeight(ssize, duration, weight, weightVerif)

	collateral, err := s.pledgeForPower(ctx, weightUpdate)
	if err != nil {
		return false, xerrors.Errorf("calculating pledge: %w", err)
	}

	collateral = big.Sub(collateral, onChainInfo.InitialPledge)
	if collateral.LessThan(big.Zero()) {
		collateral = big.Zero()
	}

	a, _, err := s.as.AddressFor(ctx, s.api, maddr, mi, api.CommitAddr, collateral, big.Zero())
	if err != nil {
		return false, xerrors.Errorf("getting address for precommit: %w", err)
	}

	msg := &types.Message{
		To:     maddr,
		From:   a,
		Method: builtin.MethodsMiner.ProveReplicaUpdates3,
		Params: enc.Bytes(),
		Value:  collateral, // todo config for pulling from miner balance!!
	}

	mss := &api.MessageSendSpec{
		MaxFee: abi.TokenAmount(s.cfg.maxFee),
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

	if err := s.transferUpdatedSectorData(ctx, update.SpID, update.SectorNumber, newSealedCID, cid.Undef, mcid); err != nil {
		return false, xerrors.Errorf("updating sector meta: %w", err)
	}

	return true, nil
}

func (s *SubmitTask) transferUpdatedSectorData(ctx context.Context, spID, sectorNum int64, newUns, newSl, mcid cid.Cid) error {
	if _, err := s.db.Exec(ctx, `UPDATE sectors_meta SET cur_sealed_cid = $1,
	                        		cur_unsealed_cid = $2, msg_cid_update = $3
	                        		WHERE sp_id = $4 AND sector_num = $5`, newSl.String(), newUns.String(), mcid.String(), spID, sectorNum); err != nil {
		return xerrors.Errorf("updating sector meta: %w", err)
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
            sector_number = $2
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
    `, spID, sectorNum); err != nil {
		return fmt.Errorf("failed to insert/update sector_meta_pieces: %w", err)
	}

	return nil
}

func (s *SubmitTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SubmitTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "UpdateSubmit",
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

func (s *SubmitTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		AftSubmit    bool  `db:"after_submit"`
	}

	err := s.db.Select(ctx, &tasks, `SELECT sp_id, sector_number, after_submit FROM sectors_snap_pipeline WHERE after_encode = TRUE AND after_prove = TRUE AND after_prove_msg_success = FALSE AND task_id_submit IS NULL`)
	if err != nil {
		return xerrors.Errorf("getting tasks: %w", err)
	}

	for _, t := range tasks {
		if t.AftSubmit {
			if err := s.updateLanded(ctx, t.SpID, t.SectorNumber); err != nil {
				return xerrors.Errorf("updating landed: %w", err)
			}
			continue
		}

		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			_, err := tx.Exec(`UPDATE sectors_snap_pipeline SET task_id_submit = $1 WHERE sp_id = $2 AND sector_number = $3`, id, t.SpID, t.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating task id: %w", err)
			}

			return true, nil
		})
	}

	return nil
}

func (s *SubmitTask) updateLanded(ctx context.Context, spId, sectorNum int64) error {
	var execResult []struct {
		ProveMsgCID          string `db:"prove_msg_cid"`
		UpdateSealedCID      string `db:"update_sealed_cid"`
		ExecutedTskCID       string `db:"executed_tsk_cid"`
		ExecutedTskEpoch     int64  `db:"executed_tsk_epoch"`
		ExecutedMsgCID       string `db:"executed_msg_cid"`
		ExecutedRcptExitCode int64  `db:"executed_rcpt_exitcode"`
		ExecutedRcptGasUsed  int64  `db:"executed_rcpt_gas_used"`
	}

	err := s.db.Select(ctx, &execResult, `SELECT spipeline.prove_msg_cid, spipeline.update_sealed_cid, executed_tsk_cid, executed_tsk_epoch, executed_msg_cid, executed_rcpt_exitcode, executed_rcpt_gas_used
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

		if exitcode.ExitCode(execResult[0].ExecutedRcptExitCode) != exitcode.Ok {
			//return s.pollCommitMsgFail(ctx, task, execResult[0])
			log.Errorw("todo handle failed snap prove", "sp", spId, "sector", sectorNum, "exec_epoch", execResult[0].ExecutedTskEpoch, "exec_tskcid", execResult[0].ExecutedTskCID, "msg_cid", execResult[0].ExecutedMsgCID)
			return nil
		}

		si, err := s.api.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(sectorNum), types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("get sector info: %w", err)
		}

		if si == nil {
			log.Errorw("todo handle missing sector info (not found after cron)", "sp", spId, "sector", sectorNum, "exec_epoch", execResult[0].ExecutedTskEpoch, "exec_tskcid", execResult[0].ExecutedTskCID, "msg_cid", execResult[0].ExecutedMsgCID)
			// todo handdle missing sector info (not found after cron)
		} else {
			if si.SealedCID.String() != execResult[0].UpdateSealedCID {
				log.Errorw("sector sealed CID mismatch after update?!", "sp", spId, "sector", sectorNum, "exec_epoch", execResult[0].ExecutedTskEpoch, "exec_tskcid", execResult[0].ExecutedTskCID, "msg_cid", execResult[0].ExecutedMsgCID)
				return nil
			}
			// yay!

			_, err := s.db.Exec(ctx, `UPDATE sectors_snap_pipeline SET
						after_prove_msg_success = TRUE, prove_msg_tsk = $1
						WHERE sp_id = $2 AND sector_number = $3 AND after_prove_msg_success = FALSE`,
				execResult[0].ExecutedTskCID, spId, sectorNum)
			if err != nil {
				return xerrors.Errorf("update sectors_snap_pipeline: %w", err)
			}
		}
	}

	return nil
}

func (s *SubmitTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (s *SubmitTask) pledgeForPower(ctx context.Context, addedPower abi.StoragePower) (abi.TokenAmount, error) {
	store := adt.WrapStore(ctx, cbor.NewCborStore(s.bstore))

	// load power actor
	var (
		powerSmoothed    builtin2.FilterEstimate
		pledgeCollateral abi.TokenAmount
	)
	if act, err := s.api.StateGetActor(ctx, power.Address, types.EmptyTSK); err != nil {
		return types.EmptyInt, xerrors.Errorf("loading power actor: %w", err)
	} else if s, err := power.Load(store, act); err != nil {
		return types.EmptyInt, xerrors.Errorf("loading power actor state: %w", err)
	} else if p, err := s.TotalPowerSmoothed(); err != nil {
		return types.EmptyInt, xerrors.Errorf("failed to determine total power: %w", err)
	} else if c, err := s.TotalLocked(); err != nil {
		return types.EmptyInt, xerrors.Errorf("failed to determine pledge collateral: %w", err)
	} else {
		powerSmoothed = p
		pledgeCollateral = c
	}

	// load reward actor
	rewardActor, err := s.api.StateGetActor(ctx, reward.Address, types.EmptyTSK)
	if err != nil {
		return types.EmptyInt, xerrors.Errorf("loading reward actor: %w", err)
	}

	rewardState, err := reward.Load(store, rewardActor)
	if err != nil {
		return types.EmptyInt, xerrors.Errorf("loading reward actor state: %w", err)
	}

	// get circulating supply
	circSupply, err := s.api.StateVMCirculatingSupplyInternal(ctx, types.EmptyTSK)
	if err != nil {
		return big.Zero(), xerrors.Errorf("getting circulating supply: %w", err)
	}

	// do the calculation
	initialPledge, err := rewardState.InitialPledgeForPower(
		addedPower,
		pledgeCollateral,
		&powerSmoothed,
		circSupply.FilCirculating,
	)
	if err != nil {
		return big.Zero(), xerrors.Errorf("calculating initial pledge: %w", err)
	}

	return types.BigDiv(types.BigMul(initialPledge, initialPledgeNum), initialPledgeDen), nil
}

var _ harmonytask.TaskInterface = &SubmitTask{}
