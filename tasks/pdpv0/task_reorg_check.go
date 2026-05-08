package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/tasks/tasknames"

	"github.com/filecoin-project/lotus/chain/actors/policy"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

const (
	reorgCheckInterval    = 8 * time.Hour
	reorgCheckLookbackPad = 30 * time.Minute
	reorgCheckDefaultPast = 24 * time.Hour

	reasonPDPMkDataset        = "pdp-mkdataset"
	reasonPDPCreateAndAdd     = "pdp-create-and-add"
	reasonPDPAddPieces        = "pdp-addpieces"
	reasonPDPDeletePiece      = "pdp-delete-piece"
	reasonPDPProvingInit      = "pdp-proving-init"
	reasonPDPProvingPeriod    = "pdp-proving-period"
	reasonPDPProve            = "pdp-prove"
	reasonPDPTerminateSvc     = "pdp-terminate-service"
	reasonPDPTerminateDataSet = "pdp-terminate-data-set"
)

var pdpv0SendReasons = []string{
	reasonPDPMkDataset,
	reasonPDPCreateAndAdd,
	reasonPDPAddPieces,
	reasonPDPDeletePiece,
	reasonPDPProvingInit,
	reasonPDPProvingPeriod,
	reasonPDPProve,
	reasonPDPTerminateSvc,
	reasonPDPTerminateDataSet,
}

// ReorgCheckFilAPI is the minimal Filecoin API needed for finality depth.
type ReorgCheckFilAPI interface {
	ChainHead(context.Context) (*chainTypes.TipSet, error)
}

// ReorgCheckEthAPI is the minimal Ethereum client surface for canonical receipt checks.
type ReorgCheckEthAPI interface {
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*ethtypes.Receipt, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*ethtypes.Block, error)
}

// ReorgCheckTask periodically verifies PDPv0 ETH transactions remain canonical past chain finality.
type ReorgCheckTask struct {
	db    *harmonydb.DB
	eth   ReorgCheckEthAPI
	chain ReorgCheckFilAPI
}

func NewReorgCheckTask(db *harmonydb.DB, eth ethchain.EthClient, chain ReorgCheckFilAPI) *ReorgCheckTask {
	return &ReorgCheckTask{
		db:    db,
		eth:   eth,
		chain: chain,
	}
}

func (t *ReorgCheckTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	head, err := t.chain.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("chain head: %w", err)
	}
	headEpoch := int64(head.Height())

	since, err := t.lastSuccessfulRunCutoff(ctx)
	if err != nil {
		return false, err
	}

	var candidates []struct {
		TxHash       string `db:"signed_tx_hash"`
		SendReason   string `db:"send_reason"`
		ConfirmEpoch int64  `db:"confirmed_block_number"`
	}
	err = t.db.Select(ctx, &candidates, `
		SELECT mwe.signed_tx_hash, mse.send_reason, mwe.confirmed_block_number
		FROM message_waits_eth mwe
		INNER JOIN message_sends_eth mse ON LOWER(TRIM(BOTH FROM mse.signed_hash)) = LOWER(TRIM(BOTH FROM mwe.signed_tx_hash))
		WHERE mwe.tx_status = 'confirmed'
		  AND mwe.tx_success = TRUE
		  AND mwe.confirmed_block_number IS NOT NULL
		  AND mse.send_success = TRUE
		  AND mse.send_time IS NOT NULL
		  AND mse.send_time >= $1
		  AND mse.send_reason = ANY($2)
		  AND ($3 - mwe.confirmed_block_number) >= $4
	`, since, pdpv0SendReasons, headEpoch, int64(policy.ChainFinality))
	if err != nil {
		return false, xerrors.Errorf("select candidates: %w", err)
	}

	for _, c := range candidates {
		if !stillOwned() {
			return false, nil
		}
		reorged, chkErr := t.txReorgedFromChain(ctx, common.HexToHash(c.TxHash))
		if chkErr != nil {
			log.Errorw("reorg check RPC error", "tx", c.TxHash, "err", chkErr)
			continue
		}
		if !reorged {
			continue
		}

		summary, rbErr := t.rollbackByReason(ctx, c.SendReason, c.TxHash)
		if rbErr != nil {
			log.Errorw("reorg rollback failed", "tx", c.TxHash, "reason", c.SendReason, "err", rbErr)
			continue
		}

		_, err = t.db.Exec(ctx, `
			INSERT INTO pdpv0_reorg_events (tx_hash, send_reason, rollback_summary, data_set_id)
			VALUES ($1, $2, $3, NULL)
			ON CONFLICT (tx_hash) DO NOTHING
		`, strings.ToLower(c.TxHash), c.SendReason, summary)
		if err != nil {
			log.Errorw("insert reorg event", "tx", c.TxHash, "err", err)
		}
	}

	return true, nil
}

func (t *ReorgCheckTask) lastSuccessfulRunCutoff(ctx context.Context) (time.Time, error) {
	var lastEnd time.Time
	err := t.db.QueryRow(ctx, `
		SELECT work_end FROM harmony_task_history
		WHERE name = $1 AND result = TRUE
		ORDER BY work_end DESC
		LIMIT 1
	`, tasknames.PDPv0_ReorgChk).Scan(&lastEnd)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Now().UTC().Add(-reorgCheckDefaultPast), nil
		}
		return time.Time{}, xerrors.Errorf("last successful reorg check: %w", err)
	}
	if lastEnd.IsZero() {
		return time.Now().UTC().Add(-reorgCheckDefaultPast), nil
	}
	cutoff := lastEnd.Add(-reorgCheckLookbackPad)
	if cutoff.Before(time.Now().UTC().Add(-reorgCheckDefaultPast * 2)) {
		cutoff = time.Now().UTC().Add(-reorgCheckDefaultPast * 2)
	}
	return cutoff, nil
}

func (t *ReorgCheckTask) txReorgedFromChain(ctx context.Context, txHash common.Hash) (reorged bool, err error) {
	receipt, err := t.eth.TransactionReceipt(ctx, txHash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			return true, nil
		}
		return false, err
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return false, nil
	}
	blk, err := t.eth.BlockByNumber(ctx, receipt.BlockNumber)
	if err != nil {
		return false, xerrors.Errorf("block by number %s: %w", receipt.BlockNumber.String(), err)
	}
	if blk.Hash() != receipt.BlockHash {
		return true, nil
	}
	return false, nil
}

func (t *ReorgCheckTask) rollbackByReason(ctx context.Context, sendReason, txHash string) (summary string, err error) {
	txh := strings.ToLower(strings.TrimSpace(txHash))
	switch sendReason {
	case reasonPDPMkDataset, reasonPDPCreateAndAdd:
		return t.rollbackCreate(ctx, txh, sendReason)
	case reasonPDPAddPieces:
		return t.rollbackAddPieces(ctx, txh)
	case reasonPDPDeletePiece:
		return t.rollbackDeletePiece(ctx, txh)
	case reasonPDPProvingInit, reasonPDPProvingPeriod:
		return t.rollbackProvingPeriod(ctx, txh)
	case reasonPDPProve:
		return t.rollbackProve(ctx, txh)
	case reasonPDPTerminateSvc:
		return t.rollbackTerminateService(ctx, txh)
	case reasonPDPTerminateDataSet:
		return t.rollbackTerminateDataSet(ctx, txh)
	default:
		return "", xerrors.Errorf("unknown send_reason %q", sendReason)
	}
}

func (t *ReorgCheckTask) markWaitReorged(ctx context.Context, tx *harmonydb.Tx, txHash string) error {
	_, err := tx.Exec(`
		UPDATE message_waits_eth
		SET tx_status = 'reorged', tx_success = NULL, tx_receipt = NULL,
		    confirmed_block_number = NULL, confirmed_tx_hash = NULL, confirmed_tx_data = NULL
		WHERE signed_tx_hash = $1
	`, txHash)
	return err
}

func (t *ReorgCheckTask) rollbackCreate(ctx context.Context, txHash, sendReason string) (string, error) {
	var summary string
	_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		nDS, err := tx.Exec(`DELETE FROM pdp_data_sets WHERE LOWER(create_message_hash) = $1`, txHash)
		if err != nil {
			return false, err
		}
		_, err = tx.Exec(`DELETE FROM pdp_data_set_creates WHERE LOWER(create_message_hash) = $1`, txHash)
		if err != nil {
			return false, err
		}
		_, err = tx.Exec(`DELETE FROM pdp_data_set_piece_adds WHERE LOWER(add_message_hash) = $1`, txHash)
		if err != nil {
			return false, err
		}
		if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
			return false, err
		}
		summary = fmt.Sprintf("create rollback sendReason=%s deleted_data_sets_rows=%d", sendReason, nDS)
		return true, nil
	}, harmonydb.OptionRetry())
	return summary, err
}

func (t *ReorgCheckTask) rollbackAddPieces(ctx context.Context, txHash string) (string, error) {
	var summary string
	_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		nPieces, err := tx.Exec(`DELETE FROM pdp_data_set_pieces WHERE LOWER(add_message_hash) = $1`, txHash)
		if err != nil {
			return false, err
		}
		nAdds, err := tx.Exec(`DELETE FROM pdp_data_set_piece_adds WHERE LOWER(add_message_hash) = $1`, txHash)
		if err != nil {
			return false, err
		}
		if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
			return false, err
		}
		summary = fmt.Sprintf("addPieces rollback deleted_pieces=%d deleted_add_rows=%d", nPieces, nAdds)
		return true, nil
	}, harmonydb.OptionRetry())
	return summary, err
}

func (t *ReorgCheckTask) rollbackDeletePiece(ctx context.Context, txHash string) (string, error) {
	var summary string
	var lostRefs int
	_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var refs []struct {
			PieceRef int64 `db:"pdp_pieceref"`
		}
		err := tx.Select(&refs, `
			SELECT DISTINCT pdp_pieceref FROM pdp_data_set_pieces
			WHERE LOWER(rm_message_hash) = $1 AND removed = TRUE`, txHash)
		if err != nil {
			return false, err
		}
		for _, r := range refs {
			var cnt int
			err = tx.QueryRow(`SELECT COUNT(*) FROM pdp_piecerefs WHERE id = $1`, r.PieceRef).Scan(&cnt)
			if err != nil {
				return false, err
			}
			if cnt == 0 {
				lostRefs++
			}
		}
		n, err := tx.Exec(`
			UPDATE pdp_data_set_pieces
			SET removed = FALSE, rm_message_hash = NULL
			WHERE LOWER(rm_message_hash) = $1 AND removed = TRUE`, txHash)
		if err != nil {
			return false, err
		}
		if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
			return false, err
		}
		summary = fmt.Sprintf("deletePiece rollback unmarked_rows=%d piecerefs_already_missing=%d", n, lostRefs)
		return true, nil
	}, harmonydb.OptionRetry())
	if err == nil && lostRefs > 0 {
		log.Errorw("reorg rolled back piece deletion but pieceref data already cleaned up — DATA LOSS",
			"tx", txHash, "lost_piecerefs", lostRefs)
	}
	return summary, err
}

func (t *ReorgCheckTask) rollbackProvingPeriod(ctx context.Context, txHash string) (string, error) {
	var summary string
	_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_data_sets
			SET challenge_request_msg_hash = NULL,
			    prove_at_epoch = NULL,
			    prev_challenge_request_epoch = NULL
			WHERE LOWER(challenge_request_msg_hash) = $1`, txHash)
		if err != nil {
			return false, err
		}
		if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
			return false, err
		}
		summary = fmt.Sprintf("provingPeriod rollback updated_rows=%d", n)
		return true, nil
	}, harmonydb.OptionRetry())
	return summary, err
}

func (t *ReorgCheckTask) rollbackProve(ctx context.Context, txHash string) (string, error) {
	_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
			return false, err
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return "", err
	}
	return "prove tx reorged: marked message_waits_eth reorged (possible missed proof / on-chain fault)", nil
}

func (t *ReorgCheckTask) rollbackTerminateService(ctx context.Context, txHash string) (string, error) {
	var summary string
	_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_delete_data_set
			SET service_termination_epoch = NULL,
			    terminate_tx_hash = NULL,
			    after_terminate_service = FALSE
			WHERE LOWER(terminate_tx_hash) = $1`, txHash)
		if err != nil {
			return false, err
		}
		if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
			return false, err
		}
		summary = fmt.Sprintf("terminateService rollback rows=%d", n)
		return true, nil
	}, harmonydb.OptionRetry())
	return summary, err
}

func (t *ReorgCheckTask) rollbackTerminateDataSet(ctx context.Context, txHash string) (string, error) {
	var dsRow sql.NullInt64
	_ = t.db.QueryRow(ctx, `
		SELECT id FROM pdp_delete_data_set WHERE LOWER(delete_tx_hash) = $1 LIMIT 1
	`, txHash).Scan(&dsRow)

	var stillHasDS bool
	if dsRow.Valid {
		_ = t.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_data_sets WHERE id = $1)`, dsRow.Int64).Scan(&stillHasDS)
	}

	var summary string
	_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if dsRow.Valid && stillHasDS {
			_, err := tx.Exec(`
				UPDATE pdp_delete_data_set
				SET delete_tx_hash = NULL, after_delete_data_set = FALSE
				WHERE id = $1 AND LOWER(delete_tx_hash) = $2`, dsRow.Int64, txHash)
			if err != nil {
				return false, err
			}
			summary = "deleteDataSet tx reorged before watch: cleared delete_tx_hash on pdp_delete_data_set"
		} else {
			notes := "deleteDataSet tx reorged after local DELETE of dataset; manual recovery may be required"
			var dsIDArg any
			if dsRow.Valid {
				dsIDArg = dsRow.Int64
				notes = fmt.Sprintf("%s data_set_id=%d", notes, dsRow.Int64)
			} else {
				dsIDArg = nil
			}
			_, err := tx.Exec(`
				INSERT INTO pdpv0_reorg_orphans (tx_hash, data_set_id, notes)
				VALUES ($1, $2, $3)
				ON CONFLICT (tx_hash) DO UPDATE SET notes = EXCLUDED.notes, detected_at = NOW()
			`, txHash, dsIDArg, notes)
			if err != nil {
				return false, err
			}
			summary = notes
		}
		if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
			return false, err
		}
		return true, nil
	}, harmonydb.OptionRetry())
	return summary, err
}

func (t *ReorgCheckTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *ReorgCheckTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: tasknames.PDPv0_ReorgChk,
		Max:  taskhelp.Max(1),
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 256 << 20,
			Gpu: 0,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(reorgCheckInterval, t),
	}
}

func (t *ReorgCheckTask) Adder(_ harmonytask.AddTaskFunc) {}

var (
	_                           = harmonytask.Reg(&ReorgCheckTask{})
	_ harmonytask.TaskInterface = &ReorgCheckTask{}
)
