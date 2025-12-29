package seal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ipfs/go-cid"
	"go.uber.org/multierr"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	miner2 "github.com/filecoin-project/go-state-types/builtin/v13/miner"
	verifreg13 "github.com/filecoin-project/go-state-types/builtin/v13/verifreg"
	verifregtypes9 "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/multictladdr"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/message"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/ctladdr"
)

type SubmitCommitAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateMinerInitialPledgeForSector(ctx context.Context, sectorDuration abi.ChainEpoch, sectorSize abi.SectorSize, verifiedSize uint64, tsk types.TipSetKey) (types.BigInt, error)
	StateSectorPreCommitInfo(context.Context, address.Address, abi.SectorNumber, types.TipSetKey) (*miner.SectorPreCommitOnChainInfo, error)
	StateGetAllocation(ctx context.Context, clientAddr address.Address, allocationId verifregtypes9.AllocationId, tsk types.TipSetKey) (*verifregtypes9.Allocation, error)
	StateGetAllocationIdForPendingDeal(ctx context.Context, dealId abi.DealID, tsk types.TipSetKey) (verifregtypes9.AllocationId, error)
	StateMinerAvailableBalance(context.Context, address.Address, types.TipSetKey) (big.Int, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (network.Version, error)
	GasEstimateMessageGas(ctx context.Context, msg *types.Message, spec *api.MessageSendSpec, tsk types.TipSetKey) (*types.Message, error)
	ctladdr.NodeApi
}

type commitConfig struct {
	feeCfg                     *config.CurioFees
	RequireActivationSuccess   bool
	RequireNotificationSuccess bool
}

type SubmitCommitTask struct {
	sp     *SealPoller
	db     *harmonydb.DB
	api    SubmitCommitAPI
	prover storiface.Prover

	sender *message.Sender
	as     *multictladdr.MultiAddressSelector
	cfg    commitConfig
}

func NewSubmitCommitTask(sp *SealPoller, db *harmonydb.DB, api SubmitCommitAPI, sender *message.Sender, as *multictladdr.MultiAddressSelector, cfg *config.CurioConfig, prover storiface.Prover) *SubmitCommitTask {

	cnfg := commitConfig{
		feeCfg:                     &cfg.Fees,
		RequireActivationSuccess:   cfg.Subsystems.RequireActivationSuccess,
		RequireNotificationSuccess: cfg.Subsystems.RequireNotificationSuccess,
	}

	return &SubmitCommitTask{
		sp:     sp,
		db:     db,
		api:    api,
		prover: prover,
		sender: sender,
		as:     as,
		cfg:    cnfg,
	}
}

func (s *SubmitCommitTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := s.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (s *SubmitCommitTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_sdr_pipeline WHERE task_id_commit_msg = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&SubmitCommitTask{})

func (s *SubmitCommitTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectorParamsArr []struct {
		SpID         int64                   `db:"sp_id"`
		SectorNumber int64                   `db:"sector_number"`
		Proof        []byte                  `db:"porep_proof"`
		RegSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`
		TicketValue  []byte                  `db:"ticket_value"`
		SealedCID    string                  `db:"tree_r_cid"`
		UnsealedCID  string                  `db:"tree_d_cid"`
		SeedValue    []byte                  `db:"seed_value"`
	}

	err = s.db.Select(ctx, &sectorParamsArr, `
		SELECT sp_id, sector_number, reg_seal_proof, porep_proof, ticket_value, tree_r_cid, tree_d_cid, seed_value
		FROM sectors_sdr_pipeline
		WHERE task_id_commit_msg = $1 ORDER BY sector_number ASC`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) == 0 {
		return true, xerrors.Errorf("expected at least 1 sector params, got 0")
	}

	maddr, err := address.NewIDAddress(uint64(sectorParamsArr[0].SpID))
	if err != nil {
		return false, xerrors.Errorf("getting miner address: %w", err)
	}

	ts, err := s.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting chain head: %w", err)
	}

	regProof := sectorParamsArr[0].RegSealProof

	balance, err := s.api.StateMinerAvailableBalance(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting miner balance: %w", err)
	}

	mi, err := s.api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting miner info: %w", err)
	}

	params := miner.ProveCommitSectors3Params{
		RequireActivationSuccess:   s.cfg.RequireActivationSuccess,
		RequireNotificationSuccess: s.cfg.RequireNotificationSuccess,
		SectorActivations:          make([]miner.SectorActivationManifest, 0, len(sectorParamsArr)),
		SectorProofs:               make([][]byte, 0, len(sectorParamsArr)),
	}

	collateral := big.Zero()
	infos := make([]proof.AggregateSealVerifyInfo, 0, len(sectorParamsArr))
	sectors := make([]int64, 0, len(sectorParamsArr))

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
		if sectorParams.RegSealProof != regProof {
			return false, xerrors.Errorf("expected proofs type to be same (%d) for all sectors in a batch but found %d", regProof, sectorParams.RegSealProof)
		}

		var pieces []struct {
			PieceIndex int64           `db:"piece_index"`
			PieceCID   string          `db:"piece_cid"`
			PieceSize  int64           `db:"piece_size"`
			Proposal   json.RawMessage `db:"f05_deal_proposal"`
			Manifest   json.RawMessage `db:"direct_piece_activation_manifest"`
			DealID     abi.DealID      `db:"f05_deal_id"`
		}

		err = s.db.Select(ctx, &pieces, `
		SELECT piece_index,
		       piece_cid,
		       piece_size,
		       f05_deal_proposal,
		       direct_piece_activation_manifest,
		       COALESCE(f05_deal_id, 0) AS f05_deal_id
		FROM sectors_sdr_initial_pieces
		WHERE sp_id = $1 AND sector_number = $2 ORDER BY piece_index ASC`, sectorParams.SpID, sectorParams.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("getting pieces: %w", err)
		}

		pci, err := s.api.StateSectorPreCommitInfo(ctx, maddr, abi.SectorNumber(sectorParams.SectorNumber), ts.Key())
		if err != nil {
			return false, xerrors.Errorf("getting precommit info: %w", err)
		}
		if pci == nil {
			return false, xerrors.Errorf("precommit info not found on chain")
		}

		var verifiedSize abi.PaddedPieceSize

		pams := make([]miner.PieceActivationManifest, 0, len(pieces))

		var sectorFailed bool

		for _, piece := range pieces {
			var pam *miner.PieceActivationManifest
			if piece.Proposal != nil {
				var prop *market.DealProposal
				err = json.Unmarshal(piece.Proposal, &prop)
				if err != nil {
					return false, xerrors.Errorf("marshalling json to deal proposal: %w", err)
				}
				alloc, err := s.api.StateGetAllocationIdForPendingDeal(ctx, piece.DealID, types.EmptyTSK)
				if err != nil {
					return false, xerrors.Errorf("getting allocation for deal %d: %w", piece.DealID, err)
				}
				clid, err := s.api.StateLookupID(ctx, prop.Client, types.EmptyTSK)
				if err != nil {
					return false, xerrors.Errorf("getting client address for deal %d: %w", piece.DealID, err)
				}

				clientId, err := address.IDFromAddress(clid)
				if err != nil {
					return false, xerrors.Errorf("getting client address for deal %d: %w", piece.DealID, err)
				}

				var vac *miner2.VerifiedAllocationKey
				if alloc != verifregtypes9.NoAllocationID {
					vac = &miner2.VerifiedAllocationKey{
						Client: abi.ActorID(clientId),
						ID:     verifreg13.AllocationId(alloc),
					}
				}

				payload, err := cborutil.Dump(piece.DealID)
				if err != nil {
					return false, xerrors.Errorf("serializing deal id: %w", err)
				}

				pam = &miner.PieceActivationManifest{
					CID:                   prop.PieceCID,
					Size:                  prop.PieceSize,
					VerifiedAllocationKey: vac,
					Notify: []miner2.DataActivationNotification{
						{
							Address: market.Address,
							Payload: payload,
						},
					},
				}
			} else {
				err = json.Unmarshal(piece.Manifest, &pam)
				if err != nil {
					return false, xerrors.Errorf("marshalling json to PieceManifest: %w", err)
				}
			}
			unrecoverable, err := AllocationCheck(ctx, s.api, pam, pci.Info.Expiration, abi.ActorID(sectorParams.SpID), ts)
			if err != nil {
				if unrecoverable {
					_, err2 := s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET 
                                 failed = TRUE, failed_at = NOW(), failed_reason = 'alloc-check', failed_reason_msg = $1,
                                 task_id_commit_msg = NULL, after_commit_msg = FALSE
                             WHERE task_id_commit_msg = $2 AND sp_id = $3 AND sector_number = $4`, err.Error(), sectorParams.SpID, sectorParams.SectorNumber)
					if err2 != nil {
						return false, xerrors.Errorf("allocation check failed with an unrecoverable issue: %w", multierr.Combine(err, err2))
					}
					log.Errorw("allocation check failed with an unrecoverable issue", "sp", sectorParams.SpID, "sector", sectorParams.SectorNumber, "err", err)
					sectorFailed = true
					break
				}
			}
			if pam.VerifiedAllocationKey != nil {
				if pam.VerifiedAllocationKey.ID != verifreg13.NoAllocationID {
					verifiedSize += pam.Size
				}
			}

			pams = append(pams, *pam)
		}

		if sectorFailed {
			continue // Skip this sector
		}

		ssize, err := pci.Info.SealProof.SectorSize()
		if err != nil {
			return false, xerrors.Errorf("could not get sector size: %w", err)
		}

		collateralPerSector, err := s.api.StateMinerInitialPledgeForSector(ctx, pci.Info.Expiration-ts.Height(), ssize, uint64(verifiedSize), ts.Key())
		if err != nil {
			return false, xerrors.Errorf("getting initial pledge collateral: %w", err)
		}

		collateralPerSector = big.Sub(collateralPerSector, pci.PreCommitDeposit)
		if collateralPerSector.LessThan(big.Zero()) {
			collateralPerSector = big.Zero()
		}

		simulateSendParam := miner.ProveCommitSectors3Params{
			SectorActivations: []miner.SectorActivationManifest{
				{
					SectorNumber: abi.SectorNumber(sectorParams.SectorNumber),
					Pieces:       pams,
				},
			},
			SectorProofs: [][]byte{
				sectorParams.Proof,
			},
			RequireActivationSuccess:   s.cfg.RequireActivationSuccess,
			RequireNotificationSuccess: s.cfg.RequireNotificationSuccess,
		}

		err = s.simuateCommitPerSector(ctx, maddr, mi, balance, collateral, ts, simulateSendParam)
		if err != nil {
			log.Errorw("failed to simulate commit for sector", "Miner", maddr.String(), "Sector", sectorParams.SectorNumber, "err", err)
			continue
		}

		collateral = big.Add(collateral, collateralPerSector)

		params.SectorActivations = append(params.SectorActivations, miner.SectorActivationManifest{
			SectorNumber: abi.SectorNumber(sectorParams.SectorNumber),
			Pieces:       pams,
		})
		params.SectorProofs = append(params.SectorProofs, sectorParams.Proof)

		sealedCID, err := cid.Parse(sectorParams.SealedCID)
		if err != nil {
			return false, xerrors.Errorf("parsing sealed CID: %w", err)
		}

		unsealedCID, err := cid.Parse(sectorParams.UnsealedCID)
		if err != nil {
			return false, xerrors.Errorf("parsing unsealed CID: %w", err)
		}

		infos = append(infos, proof.AggregateSealVerifyInfo{
			Number:                abi.SectorNumber(sectorParams.SectorNumber),
			Randomness:            sectorParams.TicketValue,
			InteractiveRandomness: sectorParams.SeedValue,
			SealedCID:             sealedCID,
			UnsealedCID:           unsealedCID,
		})

		sectors = append(sectors, sectorParams.SectorNumber)
	}

	if len(infos) == 0 {
		return false, xerrors.Errorf("no eligible sectors to commit")
	}

	maxFee := s.cfg.feeCfg.MaxCommitBatchGasFee.FeeForSectors(len(infos))

	msg, err := s.createCommitMessage(ctx, maddr, mi, balance, sectorParamsArr[0].RegSealProof, sectorParamsArr[0].SpID, collateral, params, infos, ts)
	if err != nil {
		return false, xerrors.Errorf("failed to create the commit message: %w", err)
	}

	mcid, err := s.sender.Send(ctx, msg, &api.MessageSendSpec{MaxFee: maxFee}, "commit")
	if err != nil {
		return false, xerrors.Errorf("pushing message to mpool: %w", err)
	}

	_, err = s.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET commit_msg_cid = $1, after_commit_msg = TRUE, 
                                task_id_commit_msg = NULL WHERE task_id_commit_msg = $2 AND sp_id = $3 AND sector_number = ANY($4)`, mcid, taskID, sectorParamsArr[0].SpID, sectors)
	if err != nil {
		return false, xerrors.Errorf("updating commit_msg_cid: %w", err)
	}

	_, err = s.db.Exec(ctx, `INSERT INTO message_waits (signed_message_cid) VALUES ($1)`, mcid)
	if err != nil {
		return false, xerrors.Errorf("inserting into message_waits: %w", err)
	}

	if err := s.transferFinalizedSectorData(ctx, sectorParamsArr[0].SpID, sectors); err != nil {
		return false, xerrors.Errorf("transferring finalized sector data: %w", err)
	}

	return true, nil
}

func (s *SubmitCommitTask) simuateCommitPerSector(ctx context.Context, maddr address.Address, mi api.MinerInfo, balance big.Int, collateral abi.TokenAmount, ts *types.TipSet, param miner.ProveCommitSectors3Params) error {
	maxFee := s.cfg.feeCfg.MaxCommitBatchGasFee.FeeForSectors(1)

	collateral = s.calculateCollateral(balance, collateral)
	goodFunds := big.Add(maxFee, collateral)
	enc := new(bytes.Buffer)
	if err := param.MarshalCBOR(enc); err != nil {
		return xerrors.Errorf("could not serialize commit params: %w", err)
	}
	_, err := s.gasEstimateCommit(ctx, maddr, enc.Bytes(), mi, goodFunds, collateral, maxFee, ts.Key())
	if err != nil {
		return err
	}
	return nil
}

func (s *SubmitCommitTask) createCommitMessage(ctx context.Context, maddr address.Address, mi api.MinerInfo, balance big.Int, sealProof abi.RegisteredSealProof, SpID int64, collateral abi.TokenAmount, params miner.ProveCommitSectors3Params, infos []proof.AggregateSealVerifyInfo, ts *types.TipSet) (*types.Message, error) {
	aggParams := params
	var aggCost, cost big.Int
	var msg, aggMsg *types.Message
	maxFee := s.cfg.feeCfg.MaxCommitBatchGasFee.FeeForSectors(len(infos))

	nv, err := s.api.StateNetworkVersion(ctx, ts.Key())
	if err != nil {
		return nil, xerrors.Errorf("getting network version: %s", err)
	}

	if len(infos) >= miner.MinAggregatedSectors {
		arp, err := aggregateProofType(nv)
		if err != nil {
			return nil, xerrors.Errorf("getting aggregate proof type: %w", err)
		}
		aggParams.AggregateProofType = &arp
		if len(aggParams.SectorProofs) != len(infos) {
			return nil, xerrors.Errorf("mismatched number of proofs and infos")
		}

		aggParams.AggregateProof, err = s.prover.AggregateSealProofs(proof.AggregateSealVerifyProofAndInfos{
			Miner:          abi.ActorID(SpID),
			SealProof:      sealProof,
			AggregateProof: arp,
			Infos:          infos,
		}, aggParams.SectorProofs)

		if err != nil {
			return nil, xerrors.Errorf("aggregating proofs: %w", err)
		}
		aggParams.SectorProofs = nil // can't be set when aggregating

		aggCollateral := s.calculateCollateral(balance, collateral)
		goodFunds := big.Add(maxFee, aggCollateral)
		aggEnc := new(bytes.Buffer)
		if err := aggParams.MarshalCBOR(aggEnc); err != nil {
			return nil, xerrors.Errorf("could not serialize commit params: %w", err)
		}
		aggMsg, err = s.gasEstimateCommit(ctx, maddr, aggEnc.Bytes(), mi, goodFunds, aggCollateral, maxFee, ts.Key())
		if err != nil {
			return nil, xerrors.Errorf("gas estimate aggregate commit: %w", err)
		}
		aggGas := big.Mul(big.Add(ts.MinTicketBlock().ParentBaseFee, aggMsg.GasPremium), big.NewInt(aggMsg.GasLimit))
		aggCost = aggGas
	}

	{
		collateral = s.calculateCollateral(balance, collateral)
		goodFunds := big.Add(maxFee, collateral)
		enc := new(bytes.Buffer)
		if err := params.MarshalCBOR(enc); err != nil {
			return nil, xerrors.Errorf("could not serialize commit params: %w", err)
		}
		msg, err = s.gasEstimateCommit(ctx, maddr, enc.Bytes(), mi, goodFunds, collateral, maxFee, ts.Key())
		if err != nil && !strings.Contains(err.Error(), "call ran out of gas") {
			return nil, xerrors.Errorf("gas estimate individual commit: %w", err)
		} else if err != nil && !aggCost.Nil() {
			log.Errorw("gas estimate individual commit failed", "err", err, "sp", SpID, "sector", infos[0].Number)
			log.Infow("Sending commit message with aggregate due to no alternative", "Batch Cost", cost, "Aggregate Cost", aggCost)
			return aggMsg, nil
		}

		log.Infow("gas estimate individual commit succeeded", "sp", SpID, "sector", infos[0].Number)
		gas := big.Mul(big.Add(ts.MinTicketBlock().ParentBaseFee, msg.GasPremium), big.NewInt(msg.GasLimit))
		cost = gas
	}

	if aggCost.Nil() || cost.LessThan(aggCost) {
		log.Infow("Sending commit message without aggregate", "Batch Cost", cost, "Aggregate Cost", aggCost)
		return msg, nil
	}

	log.Infow("Sending commit message with aggregate", "Batch Cost", cost, "Aggregate Cost", aggCost)
	return aggMsg, nil
}

func (s *SubmitCommitTask) gasEstimateCommit(ctx context.Context, maddr address.Address, params []byte, mi api.MinerInfo, goodFunds, collateral, maxFee abi.TokenAmount, ts types.TipSetKey) (*types.Message, error) {
	a, _, err := s.as.AddressFor(ctx, s.api, maddr, mi, api.CommitAddr, goodFunds, collateral)
	if err != nil {
		return nil, xerrors.Errorf("getting address for precommit: %w", err)
	}

	msg := &types.Message{
		To:     maddr,
		From:   a,
		Method: builtin.MethodsMiner.ProveCommitSectors3,
		Params: params,
		Value:  collateral,
	}

	mss := &api.MessageSendSpec{
		MaxFee: maxFee,
	}

	return s.api.GasEstimateMessageGas(ctx, msg, mss, ts)
}

func (s *SubmitCommitTask) calculateCollateral(minerBalance abi.TokenAmount, collateral abi.TokenAmount) abi.TokenAmount {
	if s.cfg.feeCfg.CollateralFromMinerBalance {
		if s.cfg.feeCfg.DisableCollateralFallback {
			collateral = big.Zero()
		}

		collateral = big.Sub(collateral, minerBalance)
		if collateral.LessThan(big.Zero()) {
			collateral = big.Zero()
		}
	}
	return collateral
}

func (s *SubmitCommitTask) transferFinalizedSectorData(ctx context.Context, spID int64, sectors []int64) error {
	if _, err := s.db.Exec(ctx, `
        INSERT INTO sectors_meta (
            sp_id,
            sector_num,
            reg_seal_proof,
            ticket_epoch,
            ticket_value,
            orig_sealed_cid,
            orig_unsealed_cid,
            cur_sealed_cid,
            cur_unsealed_cid,
            msg_cid_precommit,
            msg_cid_commit,
            seed_epoch,
            seed_value
        )
        SELECT
            sp_id,
            sector_number as sector_num,
            reg_seal_proof,
            ticket_epoch,
            ticket_value,
            tree_r_cid as orig_sealed_cid,
            tree_d_cid as orig_unsealed_cid,
            tree_r_cid as cur_sealed_cid,
            tree_d_cid as cur_unsealed_cid,
            precommit_msg_cid,
            commit_msg_cid,
            seed_epoch,
            seed_value
        FROM
            sectors_sdr_pipeline
        WHERE
            sp_id = $1 AND
            sector_number = ANY($2)
        ON CONFLICT (sp_id, sector_num) DO UPDATE SET
            reg_seal_proof = excluded.reg_seal_proof,
            ticket_epoch = excluded.ticket_epoch,
            ticket_value = excluded.ticket_value,
            orig_sealed_cid = excluded.orig_sealed_cid,
            cur_sealed_cid = excluded.cur_sealed_cid,
            msg_cid_precommit = excluded.msg_cid_precommit,
            msg_cid_commit = excluded.msg_cid_commit,
            seed_epoch = excluded.seed_epoch,
            seed_value = excluded.seed_value;
    `, spID, sectors); err != nil {
		return fmt.Errorf("failed to insert/update sectors_meta: %w", err)
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
            COALESCE(f05_deal_start_epoch, direct_start_epoch) as start_epoch,
            COALESCE(f05_deal_end_epoch, direct_end_epoch) as orig_end_epoch,
            f05_deal_id,
            direct_piece_activation_manifest as ddo_pam,
            f05_deal_proposal
        FROM
            sectors_sdr_initial_pieces
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
    `, spID, sectors); err != nil {
		return fmt.Errorf("failed to insert/update sector_meta_pieces: %w", err)
	}

	return nil
}

func (s *SubmitCommitTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SubmitCommitTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(128),
		Name: "CommitBatch",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 16,
	}
}

func (s *SubmitCommitTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	s.sp.pollers[pollerCommitMsg].Set(taskFunc)
}

type AllocNodeApi interface {
	StateGetAllocation(ctx context.Context, clientAddr address.Address, allocationId verifregtypes9.AllocationId, tsk types.TipSetKey) (*verifregtypes9.Allocation, error)
}

func AllocationCheck(ctx context.Context, api AllocNodeApi, piece *miner.PieceActivationManifest, expiration abi.ChainEpoch, miner abi.ActorID, ts *types.TipSet) (permanent bool, err error) {
	// skip pieces not claiming an allocation
	if piece.VerifiedAllocationKey == nil {
		return false, nil
	}
	addr, err := address.NewIDAddress(uint64(piece.VerifiedAllocationKey.Client))
	if err != nil {
		return false, err
	}

	alloc, err := api.StateGetAllocation(ctx, addr, verifregtypes9.AllocationId(piece.VerifiedAllocationKey.ID), ts.Key())
	if err != nil {
		return false, err
	}
	if alloc == nil {
		return true, xerrors.Errorf("no allocation found for piece %s with allocation ID %d", piece.CID.String(), piece.VerifiedAllocationKey.ID)
	}
	if alloc.Provider != miner {
		return true, xerrors.Errorf("provider id mismatch for piece %s: expected %d and found %d", piece.CID.String(), miner, alloc.Provider)
	}
	if alloc.Size != piece.Size {
		return true, xerrors.Errorf("size mismatch for piece %s: expected %d and found %d", piece.CID.String(), piece.Size, alloc.Size)
	}

	if expiration < ts.Height()+alloc.TermMin {
		tooLittleBy := ts.Height() + alloc.TermMin - expiration

		return true, xerrors.Errorf("sector expiration %d is before than allocation TermMin %d for piece %s (should be at least %d epochs more)", expiration, ts.Height()+alloc.TermMin, piece.CID.String(), tooLittleBy)
	}
	if expiration > ts.Height()+alloc.TermMax {
		tooMuchBy := expiration - (ts.Height() + alloc.TermMax)

		return true, xerrors.Errorf("sector expiration %d is later than allocation TermMax %d for piece %s (should be at least %d epochs less)", expiration, ts.Height()+alloc.TermMax, piece.CID.String(), tooMuchBy)
	}

	log.Infow("allocation check details", "piece", piece.CID.String(), "client", alloc.Client, "provider", alloc.Provider, "size", alloc.Size, "term_min", alloc.TermMin, "term_max", alloc.TermMax, "sector_expiration", expiration)

	return false, nil
}

var _ harmonytask.TaskInterface = &SubmitCommitTask{}

func aggregateProofType(nv network.Version) (abi.RegisteredAggregationProof, error) {
	if nv < network.Version16 {
		return abi.RegisteredAggregationProof_SnarkPackV1, nil
	}
	return abi.RegisteredAggregationProof_SnarkPackV2, nil
}
