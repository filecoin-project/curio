package seal

import (
	"bytes"
	"context"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	actorstypes "github.com/filecoin-project/go-state-types/actors"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	miner12 "github.com/filecoin-project/go-state-types/builtin/v12/miner"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/multictladdr"
	"github.com/filecoin-project/curio/tasks/message"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/ctladdr"
)

type SubmitPrecommitTaskApi interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateMinerPreCommitDepositForPower(context.Context, address.Address, miner.SectorPreCommitInfo, types.TipSetKey) (big.Int, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (network.Version, error)
	StateMinerAvailableBalance(context.Context, address.Address, types.TipSetKey) (big.Int, error)
	ctladdr.NodeApi
}

type SubmitPrecommitTask struct {
	sp                         *SealPoller
	db                         *harmonydb.DB
	api                        SubmitPrecommitTaskApi
	sender                     *message.Sender
	as                         *multictladdr.MultiAddressSelector
	CollateralFromMinerBalance bool
	DisableCollateralFallback  bool

	maxFee types.FIL
}

func NewSubmitPrecommitTask(sp *SealPoller, db *harmonydb.DB, api SubmitPrecommitTaskApi, sender *message.Sender, as *multictladdr.MultiAddressSelector, maxFee types.FIL, CollateralFromMinerBalance, DisableCollateralFallback bool) *SubmitPrecommitTask {
	return &SubmitPrecommitTask{
		sp:     sp,
		db:     db,
		api:    api,
		sender: sender,
		as:     as,

		maxFee:                     maxFee,
		CollateralFromMinerBalance: CollateralFromMinerBalance,
		DisableCollateralFallback:  DisableCollateralFallback,
	}
}

func (s *SubmitPrecommitTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := s.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (s *SubmitPrecommitTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_sdr_pipeline WHERE task_id_precommit_msg = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&SubmitPrecommitTask{})

func (s *SubmitPrecommitTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// 1. Load sector info

	var sectorParamsArr []struct {
		SpID                     int64                   `db:"sp_id"`
		SectorNumber             int64                   `db:"sector_number"`
		RegSealProof             abi.RegisteredSealProof `db:"reg_seal_proof"`
		UserSectorDurationEpochs *int64                  `db:"user_sector_duration_epochs"`
		TicketEpoch              abi.ChainEpoch          `db:"ticket_epoch"`
		SealedCID                string                  `db:"tree_r_cid"`
		UnsealedCID              string                  `db:"tree_d_cid"`
	}

	err = s.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number, reg_seal_proof, user_sector_duration_epochs, ticket_epoch, tree_r_cid, tree_d_cid
		FROM sectors_sdr_pipeline
		WHERE task_id_precommit_msg = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(sectorParamsArr))
	}
	sectorParams := sectorParamsArr[0]

	maddr, err := address.NewIDAddress(uint64(sectorParams.SpID))
	if err != nil {
		return false, xerrors.Errorf("getting miner address: %w", err)
	}

	sealedCID, err := cid.Parse(sectorParams.SealedCID)
	if err != nil {
		return false, xerrors.Errorf("parsing sealed CID: %w", err)
	}

	unsealedCID, err := cid.Parse(sectorParams.UnsealedCID)
	if err != nil {
		return false, xerrors.Errorf("parsing unsealed CID: %w", err)
	}

	// 2. Prepare message params

	head, err := s.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting chain head: %w", err)
	}

	params := miner.PreCommitSectorBatchParams2{}

	expiration := sectorParams.TicketEpoch + miner12.MaxSectorExpirationExtension
	if sectorParams.UserSectorDurationEpochs != nil {
		expiration = sectorParams.TicketEpoch + abi.ChainEpoch(*sectorParams.UserSectorDurationEpochs)
	}

	params.Sectors = append(params.Sectors, miner.SectorPreCommitInfo{
		SealProof:     sectorParams.RegSealProof,
		SectorNumber:  abi.SectorNumber(sectorParams.SectorNumber),
		SealedCID:     sealedCID,
		SealRandEpoch: sectorParams.TicketEpoch,
		Expiration:    expiration,
	})

	{
		var pieces []struct {
			PieceIndex     int64  `db:"piece_index"`
			PieceCID       string `db:"piece_cid"`
			PieceSize      int64  `db:"piece_size"`
			DealStartEpoch int64  `db:"deal_start_epoch"`
			DealEndEpoch   int64  `db:"deal_end_epoch"`
		}

		err = s.db.Select(ctx, &pieces, `
               SELECT piece_index,
                      piece_cid,
                      piece_size,
                      COALESCE(f05_deal_end_epoch, direct_end_epoch, 0) AS deal_end_epoch,
                      COALESCE(f05_deal_start_epoch, direct_start_epoch, 0) AS deal_start_epoch
		FROM sectors_sdr_initial_pieces
		WHERE sp_id = $1 AND sector_number = $2 ORDER BY piece_index ASC`, sectorParams.SpID, sectorParams.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("getting pieces: %w", err)
		}

		if len(pieces) > 0 {
			var endEpoch abi.ChainEpoch
			params.Sectors[0].UnsealedCid = &unsealedCID
			for _, p := range pieces {
				if p.DealStartEpoch > 0 && abi.ChainEpoch(p.DealStartEpoch) < head.Height() {
					// deal start epoch is in the past, can't precommit this sector anymore
					_, perr := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline
					SET failed = TRUE, failed_at = NOW(), failed_reason = 'past-start-epoch', failed_reason_msg = 'precommit: start epoch is in the past', task_id_precommit_msg = NULL
					WHERE task_id_precommit_msg = $1`, taskID)
					if perr != nil {
						return false, xerrors.Errorf("persisting precommit start epoch expiry: %w", perr)
					}
					return true, xerrors.Errorf("deal start epoch is in the past")
				}
				if p.DealEndEpoch > 0 && abi.ChainEpoch(p.DealEndEpoch) > endEpoch {
					endEpoch = abi.ChainEpoch(p.DealEndEpoch)
				}
			}
			if endEpoch != params.Sectors[0].Expiration {
				params.Sectors[0].Expiration = endEpoch
			}
		}
	}

	nv, err := s.api.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting network version: %w", err)
	}
	av, err := actorstypes.VersionForNetwork(nv)
	if err != nil {
		return false, xerrors.Errorf("failed to get actors version: %w", err)
	}
	msd, err := policy.GetMaxProveCommitDuration(av, sectorParams.RegSealProof)
	if err != nil {
		return false, xerrors.Errorf("failed to get max prove commit duration: %w", err)
	}

	if minExpiration := sectorParams.TicketEpoch + policy.MaxPreCommitRandomnessLookback + msd + miner.MinSectorExpiration; params.Sectors[0].Expiration < minExpiration {
		params.Sectors[0].Expiration = minExpiration
	}

	// 3. Check precommit

	{
		record, err := s.checkPrecommit(ctx, params)
		if err != nil {
			if record {
				_, perr := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline
					SET failed = TRUE, failed_at = NOW(), failed_reason = 'precommit-check', failed_reason_msg = $1, task_id_precommit_msg = NULL
					WHERE task_id_precommit_msg = $2`, err.Error(), taskID)
				if perr != nil {
					return false, xerrors.Errorf("persisting precommit check error: %w", perr)
				}
			}

			return record, xerrors.Errorf("checking precommit: %w", err)
		}
	}

	// 4. Prepare and send message

	var pbuf bytes.Buffer
	if err := params.MarshalCBOR(&pbuf); err != nil {
		return false, xerrors.Errorf("serializing params: %w", err)
	}

	collateral, err := s.api.StateMinerPreCommitDepositForPower(ctx, maddr, params.Sectors[0], types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting precommit deposit: %w", err)
	}

	mi, err := s.api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting miner info: %w", err)
	}

	if s.CollateralFromMinerBalance {
		if s.DisableCollateralFallback {
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

	a, _, err := s.as.AddressFor(ctx, s.api, maddr, mi, api.PreCommitAddr, collateral, big.Zero())
	if err != nil {
		return false, xerrors.Errorf("getting address for precommit: %w", err)
	}

	msg := &types.Message{
		To:     maddr,
		From:   a,
		Method: builtin.MethodsMiner.PreCommitSectorBatch2,
		Params: pbuf.Bytes(),
		Value:  collateral,
	}

	mss := &api.MessageSendSpec{
		MaxFee: abi.TokenAmount(s.maxFee),
	}

	mcid, err := s.sender.Send(ctx, msg, mss, "precommit")
	if err != nil {
		return false, xerrors.Errorf("sending message: %w", err)
	}

	// set precommit_msg_cid
	_, err = s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline
		SET precommit_msg_cid = $1, after_precommit_msg = TRUE, task_id_precommit_msg = NULL
		WHERE task_id_precommit_msg = $2`, mcid, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating precommit_msg_cid: %w", err)
	}

	_, err = s.db.Exec(ctx, `INSERT INTO message_waits (signed_message_cid) VALUES ($1)`, mcid)
	if err != nil {
		return false, xerrors.Errorf("inserting into message_waits: %w", err)
	}

	return true, nil
}

func (s *SubmitPrecommitTask) checkPrecommit(ctx context.Context, params miner.PreCommitSectorBatchParams2) (record bool, err error) {
	if len(params.Sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector")
	}

	preCommitInfo := params.Sectors[0]

	head, err := s.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting chain head: %w", err)
	}
	height := head.Height()

	//never commit P2 message before, check ticket expiration
	ticketEarliest := height - policy.MaxPreCommitRandomnessLookback

	if preCommitInfo.SealRandEpoch < ticketEarliest {
		return true, xerrors.Errorf("ticket expired: seal height: %d, head: %d", preCommitInfo.SealRandEpoch+policy.SealRandomnessLookback, height)
	}

	return true, nil
}

func (s *SubmitPrecommitTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SubmitPrecommitTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1024),
		Name: "PreCommitSubmit",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 16,
	}
}

func (s *SubmitPrecommitTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	s.sp.pollers[pollerPrecommitMsg].Set(taskFunc)
}

var _ harmonytask.TaskInterface = &SubmitPrecommitTask{}
