package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
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

// ReorgCheckEthAPI is the minimal Ethereum client surface for canonical inclusion checks.
type ReorgCheckEthAPI interface {
	BlockByNumber(ctx context.Context, number *big.Int) (*ethtypes.Block, error)
	BlockByHash(ctx context.Context, hash common.Hash) (*ethtypes.Block, error)
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*ethtypes.Receipt, error)
}

// ReorgCheckTask periodically verifies PDPv0 ETH transactions remain canonical past chain finality.
type ReorgCheckTask struct {
	db    *harmonydb.DB
	eth   ReorgCheckEthAPI
	chain ReorgCheckFilAPI
}

func NewReorgCheckTask(db *harmonydb.DB, eth ReorgCheckEthAPI, chain ReorgCheckFilAPI) *ReorgCheckTask {
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

	var candidates []reorgCheckCandidate
	err = t.db.Select(ctx, &candidates, `
		SELECT LOWER(TRIM(BOTH FROM mse.signed_hash)) AS signed_tx_hash,
		       mse.send_reason,
		       mse.send_time,
		       mwe.confirmed_block_number,
		       COALESCE(mwe.tx_receipt->>'blockHash', '') AS stored_block_hash
		FROM message_sends_eth mse
		LEFT JOIN message_waits_eth mwe
		  ON LOWER(TRIM(BOTH FROM mse.signed_hash)) = LOWER(TRIM(BOTH FROM mwe.signed_tx_hash))
		WHERE mse.send_success = TRUE
		  AND mse.send_time IS NOT NULL
		  AND mse.send_reason = ANY($2)
		  AND (
		    (mwe.tx_status = 'confirmed'
		     AND mwe.tx_success = TRUE
		     AND mwe.confirmed_block_number IS NOT NULL
		     AND ($3 - mwe.confirmed_block_number) >= $4
		     AND mse.send_time >= $1)
		    OR (
		      mwe.signed_tx_hash IS NULL
		      AND mse.send_time <= NOW() - ($4::bigint * INTERVAL '30 seconds')
		      AND NOT EXISTS (
		        SELECT 1 FROM pdpv0_reorg_events re
		        WHERE LOWER(re.tx_hash) = LOWER(TRIM(BOTH FROM mse.signed_hash))
		      )
		    )
		  )
	`, since, pdpv0SendReasons, headEpoch, int64(policy.ChainFinality))
	if err != nil {
		return false, xerrors.Errorf("select candidates: %w", err)
	}

	type pendingInclusion struct {
		candidate reorgCheckCandidate
		check     reorgInclusionCheck
	}
	var toVerify []pendingInclusion
	var toRollback []reorgCheckCandidate

	for _, c := range candidates {
		if !stillOwned() {
			return false, nil
		}
		confirmEpoch, _, ready, dropped, chkErr := t.confirmationForCheck(ctx, c, headEpoch)
		if chkErr != nil {
			return false, xerrors.Errorf("confirmation for check %s: %w", c.TxHash, chkErr)
		}
		if !ready {
			continue
		}
		if dropped {
			toRollback = append(toRollback, c)
			continue
		}
		toVerify = append(toVerify, pendingInclusion{
			candidate: c,
			check: reorgInclusionCheck{
				TxHash:        common.HexToHash(c.TxHash),
				ConfirmHeight: confirmEpoch,
			},
		})
	}

	inclusionChecks := make([]reorgInclusionCheck, len(toVerify))
	for i, p := range toVerify {
		inclusionChecks[i] = p.check
	}
	notIncluded, chkErr := t.txsNotIncludedInCanonicalChain(ctx, inclusionChecks)
	if chkErr != nil {
		return false, xerrors.Errorf("canonical inclusion walk: %w", chkErr)
	}
	for _, p := range toVerify {
		if notIncluded[p.check.TxHash] {
			toRollback = append(toRollback, p.candidate)
		}
	}

	for _, c := range toRollback {
		if !stillOwned() {
			return false, nil
		}
		summary, rbErr := t.rollbackByReason(ctx, c.SendReason, c.TxHash)
		if rbErr != nil {
			return false, xerrors.Errorf("reorg rollback %s (%s): %w", c.TxHash, c.SendReason, rbErr)
		}

		_, err = t.db.Exec(ctx, `
			INSERT INTO pdpv0_reorg_events (tx_hash, send_reason, rollback_summary, data_set_id)
			VALUES ($1, $2, $3, NULL)
			ON CONFLICT (tx_hash) DO NOTHING
		`, strings.ToLower(c.TxHash), c.SendReason, summary)
		if err != nil {
			return false, xerrors.Errorf("insert reorg event %s: %w", c.TxHash, err)
		}
	}

	return true, nil
}

type reorgCheckCandidate struct {
	TxHash          string       `db:"signed_tx_hash"`
	SendReason      string       `db:"send_reason"`
	SendTime        time.Time    `db:"send_time"`
	ConfirmEpoch    sql.NullInt64 `db:"confirmed_block_number"`
	StoredBlockHash string       `db:"stored_block_hash"`
}

// confirmationForCheck returns inclusion height/hash for reorg comparison.
// Sends without message_waits_eth (e.g. pdp-prove, pdp-terminate-service) are
// resolved from the chain receipt once past finality.
func (t *ReorgCheckTask) confirmationForCheck(ctx context.Context, c reorgCheckCandidate, headEpoch int64) (confirmEpoch int64, storedBlockHash common.Hash, ready, dropped bool, err error) {
	finality := int64(policy.ChainFinality)
	if c.ConfirmEpoch.Valid && c.StoredBlockHash != "" {
		confirmEpoch = c.ConfirmEpoch.Int64
		if headEpoch-confirmEpoch < finality {
			return 0, common.Hash{}, false, false, nil
		}
		return confirmEpoch, common.HexToHash(c.StoredBlockHash), true, false, nil
	}

	receipt, err := t.eth.TransactionReceipt(ctx, common.HexToHash(c.TxHash))
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			// Only treat as dropped after finality; younger sends may still be pending.
			if time.Since(c.SendTime) < time.Duration(finality)*30*time.Second {
				return 0, common.Hash{}, false, false, nil
			}
			return 0, common.Hash{}, true, true, nil
		}
		return 0, common.Hash{}, false, false, err
	}
	if receipt == nil {
		return 0, common.Hash{}, false, false, nil
	}
	confirmEpoch = int64(receipt.BlockNumber.Uint64())
	if headEpoch-confirmEpoch < finality {
		return 0, common.Hash{}, false, false, nil
	}
	return confirmEpoch, receipt.BlockHash, true, false, nil
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

type reorgInclusionCheck struct {
	TxHash        common.Hash
	ConfirmHeight int64
}

// txsNotIncludedInCanonicalChain walks the canonical chain from head down to the
// earliest expected confirmation height, scanning transactions in each block.
// Returns true for txs that never appear on the walk (not included in chain).
// A tx re-included in a different block after a reorg is still considered included.
func (t *ReorgCheckTask) txsNotIncludedInCanonicalChain(ctx context.Context, checks []reorgInclusionCheck) (map[common.Hash]bool, error) {
	notIncluded := make(map[common.Hash]bool, len(checks))
	if len(checks) == 0 {
		return notIncluded, nil
	}

	minHeight := uint64(math.MaxUint64)
	for _, c := range checks {
		h := uint64(c.ConfirmHeight)
		if h < minHeight {
			minHeight = h
		}
	}

	pending := make(map[common.Hash]struct{}, len(checks))
	for _, c := range checks {
		pending[c.TxHash] = struct{}{}
	}

	blk, err := t.eth.BlockByNumber(ctx, nil)
	if err != nil {
		return nil, xerrors.Errorf("head block: %w", err)
	}

	for blk != nil {
		for _, tx := range blk.Transactions() {
			if _, ok := pending[tx.Hash()]; ok {
				delete(pending, tx.Hash())
			}
		}
		if len(pending) == 0 {
			break
		}
		if blk.NumberU64() <= minHeight {
			break
		}
		if blk.NumberU64() == 0 {
			break
		}
		parentHash := blk.ParentHash()
		blk, err = t.eth.BlockByHash(ctx, parentHash)
		if err != nil {
			return nil, xerrors.Errorf("parent block %s: %w", parentHash, err)
		}
	}

	for _, c := range checks {
		_, stillPending := pending[c.TxHash]
		notIncluded[c.TxHash] = stillPending
	}
	return notIncluded, nil
}

// TxNotIncludedInChain reports whether txHash does not appear in the canonical
// chain at or above confirmedHeight (walks from head, scanning block transactions).
func (t *ReorgCheckTask) TxNotIncludedInChain(ctx context.Context, txHash common.Hash, confirmedHeight int64) (notIncluded bool, err error) {
	m, err := t.txsNotIncludedInCanonicalChain(ctx, []reorgInclusionCheck{{
		TxHash:        txHash,
		ConfirmHeight: confirmedHeight,
	}})
	if err != nil {
		return false, err
	}
	return m[txHash], nil
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
