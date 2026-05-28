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
	logging "github.com/ipfs/go-log/v2"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/tasks/tasknames"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

// to simplify log auditing.
var logReorgCheck = logging.Logger("pdpv0-reorg-check")

const (
	// reorgCheckInterval is how often the singleton reorg check task is scheduled.
	reorgCheckInterval = 30 * time.Minute
	// reorgCheckMinAge is the newest messages we will inclusion-check.
	// These are comfortably far from typical reorg scenarios.
	reorgCheckMinAge = 20 * time.Minute
	// reorgCheckMaxLookbackEpochs caps how far back we walk from chain head (older sends are skipped).
	reorgCheckMaxLookbackEpochs = 2000

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

type reorgCheckCandidate struct {
	TxHash          string        `db:"signed_tx_hash"`
	SendReason      string        `db:"send_reason"`
	SendTime        time.Time     `db:"send_time"`
	ConfirmEpoch    sql.NullInt64 `db:"confirmed_block_number"`
	StoredBlockHash string        `db:"stored_block_hash"`
}

func (t *ReorgCheckTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	head, err := t.chain.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("chain head: %w", err)
	}
	headEpoch := int64(head.Height())

	since, until, skipped, err := t.reorgCheckTimeWindow(ctx, head)
	if err != nil {
		return false, err
	}
	if skipped > 0 {
		logReorgCheck.Warnw("reorg check: skipping sends older than max lookback because last successful run was too long ago",
			"skipped", skipped,
			"maxLookbackEpochs", reorgCheckMaxLookbackEpochs,
			"since", since,
		)
	}

	var candidates []reorgCheckCandidate
	// reorgCheckCandidatesSQL selects sends to verify for canonical inclusion.
	// Split into UNION ALL branches so each side can use indexes (avoids a wide
	// LEFT JOIN + OR that hash-joins the full send/wait tables). Branch 2 uses
	// anti-joins (LEFT JOIN ... IS NULL) instead of NOT EXISTS.
	err = t.db.Select(ctx, &candidates, `
		SELECT LOWER(TRIM(BOTH FROM mse.signed_hash)) AS signed_tx_hash,
					mse.send_reason,
					mse.send_time,
					mwe.confirmed_block_number,
					COALESCE(mwe.tx_receipt->>'blockHash', '') AS stored_block_hash
		FROM message_waits_eth mwe
		INNER JOIN message_sends_eth mse
			ON LOWER(TRIM(BOTH FROM mse.signed_hash)) = mwe.signed_tx_hash
		WHERE mwe.tx_status = 'confirmed'
			AND mwe.tx_success = TRUE
			AND mwe.confirmed_block_number IS NOT NULL
			AND ($4 - mwe.confirmed_block_number) >= $5
			AND mse.send_success = TRUE
			AND mse.send_time IS NOT NULL
			AND mse.send_time >= $1
			AND mse.send_time <= $2
			AND mse.send_reason = ANY($3)

		UNION ALL
		
		SELECT LOWER(TRIM(BOTH FROM mse.signed_hash)) AS signed_tx_hash,
					mse.send_reason,
					mse.send_time,
					NULL::bigint AS confirmed_block_number,
					'' AS stored_block_hash
		FROM message_sends_eth mse
		LEFT JOIN message_waits_eth mwe
			ON mwe.signed_tx_hash = LOWER(TRIM(BOTH FROM mse.signed_hash))
		LEFT JOIN pdpv0_reorg_events re
			ON re.tx_hash = LOWER(TRIM(BOTH FROM mse.signed_hash))
		WHERE mse.send_success = TRUE
			AND mse.send_time IS NOT NULL
			AND mse.send_time >= $1
			AND mse.send_time <= $2
			AND mse.send_reason = ANY($3)
			AND mwe.signed_tx_hash IS NULL
			AND re.tx_hash IS NULL
		`, since, until, pdpv0SendReasons, headEpoch, int64(policy.ChainFinality))
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
		committed, err := t.rollbackReorgCandidate(ctx, c)
		if err != nil {
			return false, err
		}
		if !committed {
			logReorgCheck.Warnw("reorg check: rollback candidate already claimed", "tx_hash", c.TxHash)
			continue
		}
	}

	return true, nil
}

// confirmationForCheck returns inclusion height/hash for reorg comparison.
// Sends without message_waits_eth (e.g. pdp-prove, pdp-terminate-service) are
// resolved from the chain receipt once past finality.
func (t *ReorgCheckTask) confirmationForCheck(ctx context.Context, c reorgCheckCandidate, headEpoch int64) (confirmEpoch int64, storedBlockHash common.Hash, ready, dropped bool, err error) {
	finality := int64(policy.ChainFinality)
	finalityAge := time.Duration(finality) * time.Duration(build.BlockDelaySecs) * time.Second
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
			if time.Since(c.SendTime) < finalityAge {
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

// reorgCheckTimeWindow returns the send_time window [since, until] for this run.
// since is the later of the last successful run end and (head - reorgCheckMaxLookbackEpochs).
// until is now minus reorgCheckMinAge. skipped counts sends in the gap when the last run
// was older than the max epoch lookback.
func (t *ReorgCheckTask) reorgCheckTimeWindow(ctx context.Context, head *chainTypes.TipSet) (since, until time.Time, skipped int, err error) {
	now := time.Now().UTC()
	until = now.Add(-reorgCheckMinAge)

	lookbackDur := time.Duration(reorgCheckMaxLookbackEpochs) * time.Duration(build.BlockDelaySecs) * time.Second
	epochLookback := time.Unix(int64(head.MinTimestamp()), 0).UTC().Add(-lookbackDur)

	lastEnd, hasLast, err := t.lastSuccessfulRunEnd(ctx)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}

	if !hasLast {
		since = epochLookback
		return since, until, 0, nil
	}

	since = lastEnd
	if epochLookback.After(since) {
		since = epochLookback
	}

	if lastEnd.Before(epochLookback) {
		err = t.db.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM message_sends_eth mse
			LEFT JOIN pdpv0_reorg_events re
			  ON re.tx_hash = LOWER(TRIM(BOTH FROM mse.signed_hash))
			WHERE mse.send_success = TRUE
			  AND mse.send_time IS NOT NULL
			  AND mse.send_time >= $1
			  AND mse.send_time < $2
			  AND mse.send_reason = ANY($3)
			  AND re.tx_hash IS NULL
		`, lastEnd, since, pdpv0SendReasons).Scan(&skipped)
		if err != nil {
			return time.Time{}, time.Time{}, 0, xerrors.Errorf("count skipped reorg check sends: %w", err)
		}
	}

	return since, until, skipped, nil
}

func (t *ReorgCheckTask) lastSuccessfulRunEnd(ctx context.Context) (lastEnd time.Time, ok bool, err error) {
	err = t.db.QueryRow(ctx, `
		SELECT work_end FROM harmony_task_history
		WHERE name = $1 AND result = TRUE
		ORDER BY work_end DESC
		LIMIT 1
	`, tasknames.PDPv0_ReorgChk).Scan(&lastEnd)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, xerrors.Errorf("last successful reorg check: %w", err)
	}
	if lastEnd.IsZero() {
		return time.Time{}, false, nil
	}
	return lastEnd, true, nil
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
			delete(pending, tx.Hash())
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

// rollbackReorgCandidate claims the message tx in pdpv0_reorg_events and rolls back local state in one DB transaction (or just logs).
// Returns committed=false when another run already claimed the tx (ON CONFLICT).
func (t *ReorgCheckTask) rollbackReorgCandidate(ctx context.Context, c reorgCheckCandidate) (committed bool, err error) {
	txh := strings.ToLower(strings.TrimSpace(c.TxHash))
	var summary string
	committed, err = t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			INSERT INTO pdpv0_reorg_events (tx_hash, send_reason, rollback_summary, data_set_id)
			VALUES ($1, $2, $3, NULL)
			ON CONFLICT (tx_hash) DO NOTHING
		`, txh, c.SendReason, "rollback pending")
		if err != nil {
			return false, err
		}
		if n == 0 {
			return false, nil
		}

		summary, err = t.rollbackByReasonTx(ctx, tx, c.SendReason, txh)
		if err != nil {
			return false, err
		}

		_, err = tx.Exec(`
			UPDATE pdpv0_reorg_events SET rollback_summary = $2 WHERE tx_hash = $1
		`, txh, summary)
		if err != nil {
			return false, err
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("reorg rollback %s (%s): %w", c.TxHash, c.SendReason, err)
	}
	return committed, nil
}

func (t *ReorgCheckTask) rollbackByReasonTx(ctx context.Context, tx *harmonydb.Tx, sendReason, txHash string) (summary string, err error) {
	switch sendReason {
	case reasonPDPMkDataset, reasonPDPCreateAndAdd:
		return t.rollbackCreateTx(ctx, tx, txHash, sendReason)
	case reasonPDPAddPieces:
		return t.rollbackAddPiecesTx(ctx, tx, txHash)
	case reasonPDPDeletePiece:
		return t.rollbackDeletePieceTx(ctx, tx, txHash)
	case reasonPDPProvingInit, reasonPDPProvingPeriod:
		return t.rollbackProvingPeriodTx(ctx, tx, txHash)
	case reasonPDPProve:
		return t.rollbackProveTx(ctx, tx, txHash)
	case reasonPDPTerminateSvc:
		return t.rollbackTerminateServiceTx(ctx, tx, txHash)
	case reasonPDPTerminateDataSet:
		return t.rollbackTerminateDataSetTx(ctx, tx, txHash)
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

func (t *ReorgCheckTask) rollbackCreateTx(ctx context.Context, tx *harmonydb.Tx, txHash, sendReason string) (string, error) {
	var nDS, nCreates, nAdds int
	err := tx.QueryRow(`SELECT COUNT(*) FROM pdp_data_sets WHERE LOWER(create_message_hash) = $1`, txHash).Scan(&nDS)
	if err != nil {
		return "", err
	}
	err = tx.QueryRow(`SELECT COUNT(*) FROM pdp_data_set_creates WHERE LOWER(create_message_hash) = $1`, txHash).Scan(&nCreates)
	if err != nil {
		return "", err
	}
	err = tx.QueryRow(`SELECT COUNT(*) FROM pdp_data_set_piece_adds WHERE LOWER(add_message_hash) = $1`, txHash).Scan(&nAdds)
	if err != nil {
		return "", err
	}
	logReorgCheck.Warnw("reorg create rollback skipped deletes (log only)",
		"tx", txHash, "send_reason", sendReason,
		"would_delete_data_sets", nDS, "would_delete_creates", nCreates, "would_delete_adds", nAdds)
	if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
		return "", err
	}
	return fmt.Sprintf("create rollback log-only sendReason=%s would_delete_data_sets=%d would_delete_creates=%d would_delete_adds=%d",
		sendReason, nDS, nCreates, nAdds), nil
}

func (t *ReorgCheckTask) rollbackAddPiecesTx(ctx context.Context, tx *harmonydb.Tx, txHash string) (string, error) {
	var nPieces, nAdds int
	err := tx.QueryRow(`SELECT COUNT(*) FROM pdp_data_set_pieces WHERE LOWER(add_message_hash) = $1`, txHash).Scan(&nPieces)
	if err != nil {
		return "", err
	}
	err = tx.QueryRow(`SELECT COUNT(*) FROM pdp_data_set_piece_adds WHERE LOWER(add_message_hash) = $1`, txHash).Scan(&nAdds)
	if err != nil {
		return "", err
	}
	logReorgCheck.Warnw("reorg addPieces rollback skipped deletes (log only)",
		"tx", txHash, "would_delete_pieces", nPieces, "would_delete_add_rows", nAdds)
	if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
		return "", err
	}
	return fmt.Sprintf("addPieces rollback log-only would_delete_pieces=%d would_delete_add_rows=%d", nPieces, nAdds), nil
}

func (t *ReorgCheckTask) rollbackDeletePieceTx(ctx context.Context, tx *harmonydb.Tx, txHash string) (string, error) {
	var refs []struct {
		PieceRef int64 `db:"pdp_pieceref"`
	}
	err := tx.Select(&refs, `
		SELECT DISTINCT pdp_pieceref FROM pdp_data_set_pieces
		WHERE LOWER(rm_message_hash) = $1 AND removed = TRUE`, txHash)
	if err != nil {
		return "", err
	}
	lostRefs := 0
	for _, r := range refs {
		var cnt int
		err = tx.QueryRow(`SELECT COUNT(*) FROM pdp_piecerefs WHERE id = $1`, r.PieceRef).Scan(&cnt)
		if err != nil {
			return "", err
		}
		if cnt == 0 {
			lostRefs++
		}
	}
	var n int
	err = tx.QueryRow(`
		SELECT COUNT(*) FROM pdp_data_set_pieces
		WHERE LOWER(rm_message_hash) = $1 AND removed = TRUE`, txHash).Scan(&n)
	if err != nil {
		return "", err
	}
	logReorgCheck.Warnw("reorg deletePiece rollback skipped unmark (log only)",
		"tx", txHash, "would_unmark_rows", n, "piecerefs_already_missing", lostRefs)
	if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
		return "", err
	}
	if lostRefs > 0 {
		logReorgCheck.Errorw("reorg deletePiece rollback would unmark rows but pieceref data already cleaned up — possible DATA LOSS",
			"tx", txHash, "lost_piecerefs", lostRefs)
	}
	return fmt.Sprintf("deletePiece rollback log-only would_unmark_rows=%d piecerefs_already_missing=%d", n, lostRefs), nil
}

func (t *ReorgCheckTask) rollbackProvingPeriodTx(ctx context.Context, tx *harmonydb.Tx, txHash string) (string, error) {
	if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
		return "", err
	}
	return "provingPeriod rollback log-only: marked message_waits_eth reorged", nil
}

func (t *ReorgCheckTask) rollbackProveTx(ctx context.Context, tx *harmonydb.Tx, txHash string) (string, error) {
	if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
		return "", err
	}
	return "prove tx reorged: marked message_waits_eth reorged (possible missed proof / on-chain fault)", nil
}

func (t *ReorgCheckTask) rollbackTerminateServiceTx(ctx context.Context, tx *harmonydb.Tx, txHash string) (string, error) {
	var n int
	err := tx.QueryRow(`
		SELECT COUNT(*) FROM pdp_delete_data_set
		WHERE LOWER(terminate_tx_hash) = $1`, txHash).Scan(&n)
	if err != nil {
		return "", err
	}
	logReorgCheck.Warnw("reorg terminateService rollback skipped update (log only)",
		"tx", txHash, "would_clear_rows", n)
	if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
		return "", err
	}
	return fmt.Sprintf("terminateService rollback log-only would_clear_rows=%d", n), nil
}

func (t *ReorgCheckTask) rollbackTerminateDataSetTx(ctx context.Context, tx *harmonydb.Tx, txHash string) (string, error) {
	var dsRow sql.NullInt64
	err := tx.QueryRow(`
		SELECT id FROM pdp_delete_data_set WHERE LOWER(delete_tx_hash) = $1 LIMIT 1
	`, txHash).Scan(&dsRow)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}

	var stillHasDS bool
	if dsRow.Valid {
		err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM pdp_data_sets WHERE id = $1)`, dsRow.Int64).Scan(&stillHasDS)
		if err != nil {
			return "", err
		}
	}

	var summary string
	if dsRow.Valid && stillHasDS {
		var n int
		err = tx.QueryRow(`
			SELECT COUNT(*) FROM pdp_delete_data_set
			WHERE id = $1 AND LOWER(delete_tx_hash) = $2`, dsRow.Int64, txHash).Scan(&n)
		if err != nil {
			return "", err
		}
		logReorgCheck.Warnw("reorg deleteDataSet rollback skipped update (log only)",
			"tx", txHash, "data_set_id", dsRow.Int64, "would_clear_rows", n)
		summary = fmt.Sprintf("deleteDataSet rollback log-only would_clear_rows=%d data_set_id=%d", n, dsRow.Int64)
	} else {
		notes := "deleteDataSet tx reorged after local DELETE of dataset; manual recovery may be required"
		if dsRow.Valid {
			notes = fmt.Sprintf("%s data_set_id=%d", notes, dsRow.Int64)
		}
		var dataSetID any
		if dsRow.Valid {
			dataSetID = dsRow.Int64
		}
		logReorgCheck.Warnw("reorg deleteDataSet rollback skipped orphan insert (log only)",
			"tx", txHash, "data_set_id", dataSetID, "notes", notes)
		summary = "deleteDataSet rollback log-only: " + notes
	}
	if err := t.markWaitReorged(ctx, tx, txHash); err != nil {
		return "", err
	}
	return summary, nil
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
