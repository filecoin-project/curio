package seal

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	actorstypes "github.com/filecoin-project/go-state-types/actors"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	miner12 "github.com/filecoin-project/go-state-types/builtin/v12/miner"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/curio/deps/config"
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
	GasEstimateMessageGas(context.Context, *types.Message, *api.MessageSendSpec, types.TipSetKey) (*types.Message, error)
	ctladdr.NodeApi
}

type SubmitPrecommitTask struct {
	sp     *SealPoller
	db     *harmonydb.DB
	api    SubmitPrecommitTaskApi
	sender *message.Sender
	as     *multictladdr.MultiAddressSelector
	feeCfg *config.CurioFees
}

func NewSubmitPrecommitTask(sp *SealPoller, db *harmonydb.DB, api SubmitPrecommitTaskApi, sender *message.Sender, as *multictladdr.MultiAddressSelector, cfg *config.CurioConfig) *SubmitPrecommitTask {
	return &SubmitPrecommitTask{
		sp:     sp,
		db:     db,
		api:    api,
		sender: sender,
		as:     as,
		feeCfg: &cfg.Fees,
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
		WHERE task_id_precommit_msg = $1 ORDER BY sector_number ASC`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) == 0 {
		return false, xerrors.Errorf("expected at least 1 sector params, got 0")
	}

	head, err := s.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting chain head: %w", err)
	}

	maddr, err := address.NewIDAddress(uint64(sectorParamsArr[0].SpID))
	if err != nil {
		return false, xerrors.Errorf("getting miner address: %w", err)
	}

	proof := sectorParamsArr[0].RegSealProof

	nv, err := s.api.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting network version: %w", err)
	}
	av, err := actorstypes.VersionForNetwork(nv)
	if err != nil {
		return false, xerrors.Errorf("failed to get actors version: %w", err)
	}
	msd, err := policy.GetMaxProveCommitDuration(av, proof)
	if err != nil {
		return false, xerrors.Errorf("failed to get max prove commit duration: %w", err)
	}

	//never commit P2 message before, check ticket expiration
	ticketEarliest := head.Height() - policy.MaxPreCommitRandomnessLookback

	params := miner.PreCommitSectorBatchParams2{}
	collateral := big.Zero()

	// 2. Prepare preCommit info and PreCommitSectorBatchParams
	for _, sectorParams := range sectorParamsArr {
		sectorParams := sectorParams

		// Check miner ID is same for all sectors in batch
		tmpMaddr, err := address.NewIDAddress(uint64(sectorParams.SpID))
		if err != nil {
			return false, xerrors.Errorf("getting miner address: %w", err)
		}

		if maddr != tmpMaddr {
			return false, xerrors.Errorf("expected miner IDs to be same (%s) for all sectors in a batch but found %s", maddr.String(), tmpMaddr.String())
		}

		// Check proof types is same for all sectors in batch
		if sectorParams.RegSealProof != proof {
			return false, xerrors.Errorf("expected proofs type to be same (%d) for all sectors in a batch but found %d", proof, sectorParams.RegSealProof)
		}

		// Skip sectors where ticket has already expired
		if sectorParams.TicketEpoch < ticketEarliest {
			_, perr := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline
					SET failed = TRUE, failed_at = NOW(), failed_reason = 'precommit-check', failed_reason_msg = $1, task_id_precommit_msg = NULL
					WHERE task_id_precommit_msg = $2 AND sp_id = $3 AND sector_number = $4`,
				fmt.Sprintf("ticket expired: seal height: %d, head: %d", sectorParams.TicketEpoch+policy.SealRandomnessLookback, head.Height()),
				taskID, sectorParams.SpID, sectorParams.SectorNumber)
			if perr != nil {
				return false, xerrors.Errorf("persisting precommit check error: %w", perr)
			}
			continue
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

		param := miner.SectorPreCommitInfo{
			SealedCID:     sealedCID,
			SealProof:     sectorParams.RegSealProof,
			SectorNumber:  abi.SectorNumber(sectorParams.SectorNumber),
			SealRandEpoch: sectorParams.TicketEpoch,
		}

		expiration := sectorParams.TicketEpoch + miner12.MaxSectorExpirationExtension
		if sectorParams.UserSectorDurationEpochs != nil {
			expiration = sectorParams.TicketEpoch + abi.ChainEpoch(*sectorParams.UserSectorDurationEpochs)
		}

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
			param.UnsealedCid = &unsealedCID
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
			if endEpoch != expiration {
				expiration = endEpoch
			}
		}

		if minExpiration := sectorParams.TicketEpoch + policy.MaxPreCommitRandomnessLookback + msd + miner.MinSectorExpiration; expiration < minExpiration {
			expiration = minExpiration
		}

		param.Expiration = expiration

		collateralPerSector, err := s.api.StateMinerPreCommitDepositForPower(ctx, maddr, param, types.EmptyTSK)
		if err != nil {
			return false, xerrors.Errorf("getting precommit deposit: %w", err)
		}

		collateral = big.Add(collateral, collateralPerSector)

		params.Sectors = append(params.Sectors, param)
	}

	// 3. Prepare and send message

	var pbuf bytes.Buffer
	if err := params.MarshalCBOR(&pbuf); err != nil {
		return false, xerrors.Errorf("serializing params: %w", err)
	}

	mi, err := s.api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting miner info: %w", err)
	}

	maxFee := s.feeCfg.MaxPreCommitBatchGasFee.FeeForSectors(len(params.Sectors))
	aggFeeRaw, err := policy.AggregatePreCommitNetworkFee(nv, len(params.Sectors), head.MinTicketBlock().ParentBaseFee)
	if err != nil {
		return false, xerrors.Errorf("getting aggregate precommit network fee: %w", err)
	}
	aggFee := big.Div(big.Mul(aggFeeRaw, big.NewInt(110)), big.NewInt(100))
	needFunds := big.Add(collateral, aggFee)

	if s.feeCfg.CollateralFromMinerBalance {
		if s.feeCfg.DisableCollateralFallback {
			needFunds = big.Zero()
		}
		balance, err := s.api.StateMinerAvailableBalance(ctx, maddr, types.EmptyTSK)
		if err != nil {
			if err != nil {
				return false, xerrors.Errorf("getting miner balance: %w", err)
			}
		}
		needFunds = big.Sub(needFunds, balance)
		if needFunds.LessThan(big.Zero()) {
			needFunds = big.Zero()
		}
	}

	goodFunds := big.Add(maxFee, needFunds)

	a, _, err := s.as.AddressFor(ctx, s.api, maddr, mi, api.PreCommitAddr, goodFunds, collateral)
	if err != nil {
		return false, xerrors.Errorf("getting address for precommit: %w", err)
	}

	msg := &types.Message{
		To:     maddr,
		From:   a,
		Method: builtin.MethodsMiner.PreCommitSectorBatch2,
		Params: pbuf.Bytes(),
		Value:  needFunds,
	}

	mss := &api.MessageSendSpec{
		MaxFee: maxFee,
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

func (s *SubmitPrecommitTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SubmitPrecommitTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1024),
		Name: "PreCommitBatch",
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
