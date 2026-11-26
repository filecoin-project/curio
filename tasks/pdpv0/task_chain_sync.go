package pdpv0

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ethchain"
	payment "github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/tasknames"

	"github.com/filecoin-project/lotus/chain/actors/policy"
)

const alertNameChainSync = "ChainSync"

type TaskChainSync struct {
	db        *harmonydb.DB
	ethClient ethchain.EthClient
	sender    *message.SenderETH
	al        curioalerting.AlertingInterface
}

func NewTaskChainSync(db *harmonydb.DB, ethClient ethchain.EthClient, sender *message.SenderETH, al curioalerting.AlertingInterface) *TaskChainSync {
	return &TaskChainSync{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
		al:        al,
	}
}

func (t *TaskChainSync) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	if !stillOwned() {
		return false, nil
	}
	// Runs first: a later step ends the whole run when one data set fails.
	if err := t.syncServiceTerminations(ctx); err != nil {
		return false, xerrors.Errorf("syncing PDP service terminations: %w", err)
	}

	if !stillOwned() {
		return false, nil
	}
	if err := SyncStaleCreateAddReceipts(ctx, t.db, t.ethClient); err != nil {
		return false, xerrors.Errorf("syncing stale Create/Add receipt waits: %w", err)
	}

	if !stillOwned() {
		return false, nil
	}
	if err := t.syncMissingDataSetTerminationMessageWaits(ctx); err != nil {
		return false, xerrors.Errorf("syncing missing PDP termination message waits: %w", err)
	}

	if !stillOwned() {
		return false, nil
	}
	if err := t.syncMissingDataSetDeleteMessageWaits(ctx); err != nil {
		return false, xerrors.Errorf("syncing missing PDP delete message waits: %w", err)
	}

	if !stillOwned() {
		return false, nil
	}
	if err := t.syncMissingCleanupPiecesMessageWaits(ctx); err != nil {
		return false, xerrors.Errorf("syncing missing PDP cleanupPieces message waits: %w", err)
	}

	if !stillOwned() {
		return false, nil
	}
	err = t.syncFinalizedDataSetDeletionRails(ctx)
	if err != nil {
		return false, xerrors.Errorf("syncing finalized PDP deletion rails: %w", err)
	}

	if !stillOwned() {
		return false, nil
	}
	if err := t.syncPDPVerifierDeletionState(ctx); err != nil {
		return false, xerrors.Errorf("syncing PDPVerifier deletion state: %w", err)
	}

	return true, nil
}

func (t *TaskChainSync) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *TaskChainSync) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: tasknames.PDPv0_ChainSync,
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(time.Hour*8, t),
	}
}

func (t *TaskChainSync) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&TaskChainSync{})
var _ harmonytask.TaskInterface = &TaskChainSync{}

type missingTerminationMessageWait struct {
	ID              int64  `db:"id"`
	TxHash          string `db:"terminate_tx_hash"`
	ClientRequested bool   `db:"client_requested_termination"`
}

type missingDeleteMessageWait struct {
	ID     int64  `db:"id"`
	TxHash string `db:"delete_tx_hash"`
}

type missingCleanupMessageWait struct {
	ID     int64  `db:"id"`
	TxHash string `db:"cleanup_pieces_tx_hash"`
}

func (t *TaskChainSync) syncMissingDataSetTerminationMessageWaits(ctx context.Context) error {
	var missing []missingTerminationMessageWait
	if err := t.db.Select(ctx, &missing, `
		SELECT id, terminate_tx_hash, client_requested_termination
		FROM pdp_delete_data_set pdds
		WHERE pdds.service_termination_epoch IS NULL
		  AND pdds.after_terminate_service = TRUE
		  AND pdds.terminate_tx_hash IS NOT NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM message_waits_eth mwe
			WHERE mwe.signed_tx_hash = pdds.terminate_tx_hash
		  )
		ORDER BY id
	`); err != nil {
		return xerrors.Errorf("failed to select terminations missing message wait rows: %w", err)
	}

	if len(missing) == 0 {
		return nil
	}

	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, sAddr, t.ethClient)
	if err != nil {
		return xerrors.Errorf("failed to get FWSS view address: %w", err)
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, t.ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate FWSS service state view: %w", err)
	}

	for _, detail := range missing {
		ds, err := fwssv.GetDataSet(contract.EthCallOpts(ctx), big.NewInt(detail.ID))
		if err != nil {
			return xerrors.Errorf("failed to get data set %d from FWSS view: %w", detail.ID, err)
		}

		if ds.PdpEndEpoch.Int64() != 0 {
			n, err := t.db.Exec(ctx, `
				UPDATE pdp_delete_data_set
				SET service_termination_epoch = $1,
				    terminate_service_task_id = NULL,
				    client_terminate_service_task_id = NULL
				WHERE id = $2
				  AND terminate_tx_hash = $3
				  AND after_terminate_service = TRUE
				  AND service_termination_epoch IS NULL
			`, ds.PdpEndEpoch.Int64(), detail.ID, detail.TxHash)
			if err != nil {
				return xerrors.Errorf("failed to reconcile terminated data set %d: %w", detail.ID, err)
			}
			if n > 1 {
				return xerrors.Errorf("expected to update 0 or 1 rows for data set %d, updated %d", detail.ID, n)
			}
			if n == 1 {
				log.Infow("reconciled missing service termination message wait from chain state", "dataSetId", detail.ID, "txHash", detail.TxHash, "epoch", ds.PdpEndEpoch.Int64())
			}
			continue
		}

		if detail.ClientRequested {
			n, err := t.db.Exec(ctx, `
				DELETE FROM pdp_delete_data_set
				WHERE id = $1
				  AND terminate_tx_hash = $2
				  AND client_requested_termination = TRUE
				  AND after_terminate_service = TRUE
				  AND service_termination_epoch IS NULL
			`, detail.ID, detail.TxHash)
			if err != nil {
				return xerrors.Errorf("failed to delete client termination missing message wait for data set %d: %w", detail.ID, err)
			}
			if n > 1 {
				return xerrors.Errorf("expected to delete 0 or 1 rows for data set %d, deleted %d", detail.ID, n)
			}
			if n == 1 {
				log.Warnw("deleted client termination missing message wait for retry", "dataSetId", detail.ID, "txHash", detail.TxHash)
			}
			continue
		}

		n, err := t.db.Exec(ctx, `
			UPDATE pdp_delete_data_set
			SET terminate_tx_hash = NULL,
			    after_terminate_service = FALSE,
			    terminate_service_task_id = NULL
			WHERE id = $1
			  AND terminate_tx_hash = $2
			  AND client_requested_termination = FALSE
			  AND after_terminate_service = TRUE
			  AND service_termination_epoch IS NULL
		`, detail.ID, detail.TxHash)
		if err != nil {
			return xerrors.Errorf("failed to reset service termination missing message wait for data set %d: %w", detail.ID, err)
		}
		if n > 1 {
			return xerrors.Errorf("expected to update 0 or 1 rows for data set %d, updated %d", detail.ID, n)
		}
		if n == 1 {
			log.Warnw("reset service termination missing message wait for retry", "dataSetId", detail.ID, "txHash", detail.TxHash)
		} else {
			continue
		}
	}

	return nil
}

func (t *TaskChainSync) syncMissingDataSetDeleteMessageWaits(ctx context.Context) error {
	var missing []missingDeleteMessageWait
	if err := t.db.Select(ctx, &missing, `
		SELECT id, delete_tx_hash
		FROM pdp_delete_data_set pdds
		WHERE pdds.service_termination_epoch IS NOT NULL
		  AND pdds.terminated = FALSE
		  AND pdds.after_delete_data_set = TRUE
		  AND pdds.delete_tx_hash IS NOT NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM message_waits_eth mwe
			WHERE mwe.signed_tx_hash = pdds.delete_tx_hash
		  )
		ORDER BY id
	`); err != nil {
		return xerrors.Errorf("failed to select data set deletes missing message wait rows: %w", err)
	}

	for _, detail := range missing {
		state, err := readDataSetCleanupState(ctx, t.ethClient, detail.ID)
		if err != nil {
			return xerrors.Errorf("failed to read PDP cleanup state for data set %d: %w", detail.ID, err)
		}

		if state.Finalized() {
			if err := cleanupFinalizedDataSet(ctx, t.db, detail.ID); err != nil {
				return xerrors.Errorf("failed to reconcile deleted data set %d: %w", detail.ID, err)
			}
			log.Infow("reconciled missing data set delete message wait from chain state", "dataSetId", detail.ID, "txHash", detail.TxHash)
			continue
		}

		if state.CleanupMode {
			if err := markDataSetDeleteConfirmed(ctx, t.db, detail.ID, detail.TxHash); err != nil {
				return xerrors.Errorf("failed to reconcile cleanup mode for data set %d: %w", detail.ID, err)
			}
			log.Infow("reconciled missing data set delete message wait into cleanup mode",
				"dataSetId", detail.ID,
				"txHash", detail.TxHash,
				"remainingPieceSlots", state.NextPieceID)
			continue
		}

		n, err := t.db.Exec(ctx, `
			UPDATE pdp_delete_data_set
			SET delete_tx_hash = NULL,
			    after_delete_data_set = FALSE,
			    delete_data_set_task_id = NULL
			WHERE id = $1
			  AND delete_tx_hash = $2
			  AND after_delete_data_set = TRUE
			  AND service_termination_epoch IS NOT NULL
			  AND terminated = FALSE
		`, detail.ID, detail.TxHash)
		if err != nil {
			return xerrors.Errorf("failed to reset data set delete missing message wait for data set %d: %w", detail.ID, err)
		}
		if n > 1 {
			return xerrors.Errorf("expected to update 0 or 1 rows for data set %d, updated %d", detail.ID, n)
		}
		if n == 1 {
			log.Warnw("reset data set delete missing message wait for retry", "dataSetId", detail.ID, "txHash", detail.TxHash)
		} else {
			continue
		}
	}

	return nil
}

func (t *TaskChainSync) syncMissingCleanupPiecesMessageWaits(ctx context.Context) error {
	var missing []missingCleanupMessageWait
	if err := t.db.Select(ctx, &missing, `
		SELECT id, cleanup_pieces_tx_hash
		FROM pdp_delete_data_set pdds
		WHERE pdds.service_termination_epoch IS NOT NULL
		  AND pdds.terminated = FALSE
		  AND pdds.after_delete_data_set = TRUE
		  AND pdds.cleanup_pieces_tx_hash IS NOT NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM message_waits_eth mwe
			WHERE mwe.signed_tx_hash = pdds.cleanup_pieces_tx_hash
		  )
		ORDER BY id
	`); err != nil {
		return xerrors.Errorf("failed to select cleanupPieces missing message wait rows: %w", err)
	}

	for _, detail := range missing {
		state, err := readDataSetCleanupState(ctx, t.ethClient, detail.ID)
		if err != nil {
			return xerrors.Errorf("failed to read PDP cleanup state for data set %d: %w", detail.ID, err)
		}

		if state.Finalized() {
			if err := cleanupFinalizedDataSet(ctx, t.db, detail.ID); err != nil {
				return xerrors.Errorf("failed to reconcile finalized cleanup for data set %d: %w", detail.ID, err)
			}
			log.Infow("reconciled missing cleanupPieces message wait from finalized chain state", "dataSetId", detail.ID, "txHash", detail.TxHash)
			continue
		}

		if state.CleanupMode {
			if err := clearCleanupTxHash(ctx, t.db, detail.ID, detail.TxHash); err != nil {
				return xerrors.Errorf("failed to clear missing cleanupPieces tx hash for data set %d: %w", detail.ID, err)
			}
			log.Infow("reconciled missing cleanupPieces message wait into another cleanup batch",
				"dataSetId", detail.ID,
				"txHash", detail.TxHash,
				"remainingPieceSlots", state.NextPieceID)
			continue
		}

		if state.Live {
			if err := resetCleanupTxToDelete(ctx, t.db, detail.ID, detail.TxHash); err != nil {
				return xerrors.Errorf("failed to reset cleanupPieces missing message wait for data set %d: %w", detail.ID, err)
			}
			log.Warnw("cleanupPieces missing message wait saw live data set; reset to deleteDataSet stage",
				"dataSetId", detail.ID,
				"txHash", detail.TxHash)
			continue
		}

		return xerrors.Errorf("data set %d is not live, in cleanup mode, or finalized", detail.ID)
	}

	return nil
}

// syncFinalizedDataSetDeletionRails moves terminated PDP data sets to the local deletion-allowed state once the payment rail is final.
// A RailInactiveOrSettled revert or EndEpoch == SettledUpTo means settlement is complete; this pass
// only toggles deletion_allowed and leaves the actual delete to the existing delete task.
func (t *TaskChainSync) syncFinalizedDataSetDeletionRails(ctx context.Context) error {
	current, err := t.ethClient.BlockNumber(ctx)
	if err != nil {
		return xerrors.Errorf("failed to get current block number: %w", err)
	}

	var pending []struct {
		ID int64 `db:"id"`
	}
	if err := t.db.Select(ctx, &pending, `SELECT id
		FROM pdp_delete_data_set
		WHERE after_terminate_service = TRUE
		  AND deletion_allowed = FALSE
		  AND service_termination_epoch IS NOT NULL
		  AND service_termination_epoch <= $1
		ORDER BY service_termination_epoch, id`, current); err != nil {
		return xerrors.Errorf("failed to select pending data sets: %w", err)
	}

	if len(pending) == 0 {
		log.Debugw("no PDP deletion rails waiting for finalization")
		return nil
	}

	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, sAddr, t.ethClient)
	if err != nil {
		return xerrors.Errorf("failed to get FWSS view address: %w", err)
	}

	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, t.ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate FWSS service state view: %w", err)
	}

	paymentAddr, err := payment.PaymentContractAddress()
	if err != nil {
		return xerrors.Errorf("failed to get payment contract address: %w", err)
	}

	payments, err := payment.NewPayments(paymentAddr, t.ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate Payments contract: %w", err)
	}

	for _, dataSet := range pending {
		ds, err := fwssv.GetDataSet(contract.EthCallOpts(ctx), big.NewInt(dataSet.ID))
		if err != nil {
			return xerrors.Errorf("failed to get data set %d from FWSS view: %w", dataSet.ID, err)
		}

		rail, err := payments.GetRail(contract.EthCallOpts(ctx), ds.PdpRailId)
		if err != nil {
			if payment.IsRailInactiveOrSettledError(err) {
				if err := t.allowDataSetDeletion(ctx, dataSet.ID); err != nil {
					return err
				}
				log.Infow("allowed PDP data set deletion after finalized rail lookup reverted",
					"dataSetId", dataSet.ID,
					"pdpRailId", ds.PdpRailId)
				continue
			}
			return xerrors.Errorf("failed to get payment rail %s for data set %d: %w", ds.PdpRailId, dataSet.ID, err)
		}

		if rail.EndEpoch != nil && rail.SettledUpTo != nil && rail.EndEpoch.Sign() > 0 && rail.EndEpoch.Cmp(rail.SettledUpTo) == 0 {
			if err := t.allowDataSetDeletion(ctx, dataSet.ID); err != nil {
				return err
			}
			log.Infow("allowed PDP data set deletion after rail finalized",
				"dataSetId", dataSet.ID,
				"pdpRailId", ds.PdpRailId,
				"endEpoch", rail.EndEpoch,
				"settledUpTo", rail.SettledUpTo)
		}
	}

	return nil
}

// syncPDPVerifierDeletionState reconciles rows that have a deletion in
// progress and a final payment rail with PDPVerifier state. This catches
// externally submitted deleteDataSet and cleanupPieces transactions and keeps
// local cleanup behind final on-chain PDP cleanup. Any other row keeps its
// local data, also when its data set is gone on chain: the operator decides.
func (t *TaskChainSync) syncPDPVerifierDeletionState(ctx context.Context) error {
	var pending []struct {
		ID int64 `db:"id"`
	}
	if err := t.db.Select(ctx, &pending, `SELECT id
		FROM pdp_delete_data_set
		WHERE deletion_allowed = TRUE
		  AND service_termination_epoch IS NOT NULL
		  AND terminated = FALSE
		  AND (client_requested_termination
		       OR operator_requested_deletion
		       OR after_delete_data_set
		       OR delete_data_set_task_id IS NOT NULL)
		ORDER BY id`); err != nil {
		return xerrors.Errorf("failed to select PDP deletion rows for verifier sync: %w", err)
	}

	for _, detail := range pending {
		state, err := readDataSetCleanupState(ctx, t.ethClient, detail.ID)
		if err != nil {
			return xerrors.Errorf("failed to read PDP cleanup state for data set %d: %w", detail.ID, err)
		}

		if state.Live {
			continue
		}

		if state.Finalized() {
			if err := cleanupFinalizedDataSet(ctx, t.db, detail.ID); err != nil {
				return xerrors.Errorf("failed to reconcile finalized PDP cleanup for data set %d: %w", detail.ID, err)
			}
			log.Infow("reconciled finalized PDP cleanup from chain state", "dataSetId", detail.ID)
			continue
		}

		if state.CleanupMode {
			n, err := t.db.Exec(ctx, `UPDATE pdp_delete_data_set
				SET after_delete_data_set = TRUE,
				    delete_data_set_task_id = NULL,
				    delete_tx_hash = NULL
				WHERE id = $1
				  AND after_delete_data_set = FALSE
				  AND service_termination_epoch IS NOT NULL
				  AND terminated = FALSE`, detail.ID)
			if err != nil {
				return xerrors.Errorf("failed to move data set %d into cleanup stage: %w", detail.ID, err)
			}
			if n > 1 {
				return xerrors.Errorf("expected to update 0 or 1 rows for data set %d, updated %d", detail.ID, n)
			}
			if n == 1 {
				log.Infow("reconciled PDP cleanup mode from chain state",
					"dataSetId", detail.ID,
					"remainingPieceSlots", state.NextPieceID)
			}
			continue
		}

		return xerrors.Errorf("data set %d is not live, in cleanup mode, or finalized", detail.ID)
	}

	return nil
}

// allowDataSetDeletion marks a terminated data set as eligible for the normal delete task once chain sync has confirmed rail finality.
// The WHERE clause preserves idempotency and prevents this helper from moving rows that have not reached the post-terminate state.
func (t *TaskChainSync) allowDataSetDeletion(ctx context.Context, dataSetID int64) error {
	n, err := t.db.Exec(ctx, `UPDATE pdp_delete_data_set
		SET deletion_allowed = TRUE
		WHERE id = $1
		  AND after_terminate_service = TRUE
		  AND deletion_allowed = FALSE
		  AND service_termination_epoch IS NOT NULL`, dataSetID)
	if err != nil {
		return xerrors.Errorf("failed to allow data set deletion for %d: %w", dataSetID, err)
	}
	if n != 0 && n != 1 {
		return xerrors.Errorf("expected to update 0 or 1 rows for data set %d, updated %d", dataSetID, n)
	}

	return nil
}

// syncServiceTerminations records data sets with no deletion in progress whose
// FWSS service was terminated on chain, or that are no longer live in
// PDPVerifier, and alerts the operator once per data set. It reads
// ChainFinality epochs behind the head, because a record is never checked
// against the chain again and must not rest on state a reorg can undo. A chain
// that cannot be read is logged and skipped, so it does not hold back the steps
// that follow; database errors are returned.
func (t *TaskChainSync) syncServiceTerminations(ctx context.Context) error {
	var pending []struct {
		ID             int64 `db:"id"`
		ProvingStopped bool  `db:"proving_stopped"`
	}
	if err := t.db.Select(ctx, &pending, `
		SELECT ds.id, ds.unrecoverable_proving_failure_epoch IS NOT NULL AS proving_stopped
		FROM pdp_data_sets ds
		LEFT JOIN pdp_data_set_review r ON r.data_set = ds.id
		WHERE r.service_end_epoch IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM pdp_delete_data_set dd
			WHERE dd.id = ds.id
			  AND (dd.client_requested_termination
			       OR dd.operator_requested_deletion
			       OR dd.after_delete_data_set
			       OR dd.delete_data_set_task_id IS NOT NULL)
		  )
		ORDER BY ds.id
	`); err != nil {
		return xerrors.Errorf("failed to select data sets without a recorded service end: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	headCtx, cancel := context.WithTimeout(ctx, contract.EthCallTimeout)
	head, err := t.ethClient.BlockNumber(headCtx)
	cancel()
	if err != nil {
		log.Warnw("skipping PDP service termination sync, chain head unavailable", "error", err)
		return nil
	}
	height := int64(head) - int64(policy.ChainFinality)
	if height < 1 {
		return nil
	}
	callOpts := func() *bind.CallOpts {
		opts := contract.EthCallOpts(ctx)
		opts.BlockNumber = big.NewInt(height)
		return opts
	}

	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, sAddr, t.ethClient)
	if err != nil {
		log.Warnw("skipping PDP service termination sync, FWSS view address unavailable", "error", err)
		return nil
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, t.ethClient)
	if err != nil {
		log.Warnw("skipping PDP service termination sync, FWSS view unavailable", "error", err)
		return nil
	}

	var unread int
	var noRecord []int64
	for _, dataSet := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}

		info, err := fwssv.GetDataSet(callOpts(), big.NewInt(dataSet.ID))
		if err != nil {
			unread++
			log.Debugw("failed to read data set from FWSS view", "dataSetId", dataSet.ID, "error", err)
			continue
		}
		if info.PdpEndEpoch == nil || info.PdpEndEpoch.Sign() <= 0 {
			// FWSS drops its record when the data set is deleted on chain.
			if !dataSet.ProvingStopped && (info.PdpRailId == nil || info.PdpRailId.Sign() == 0) {
				noRecord = append(noRecord, dataSet.ID)
			}
			continue
		}

		endEpoch := info.PdpEndEpoch.Int64()
		alert, err := recordServiceTermination(ctx, t.db, dataSet.ID, endEpoch)
		if err != nil {
			return err
		}
		if alert {
			emitServiceTerminatedAlert(ctx, t.al, dataSet.ID, endEpoch, info.PdpRailId)
		}
	}

	if len(noRecord) > 0 {
		unreadRemoved, err := t.recordRemovedDataSets(ctx, height, callOpts, noRecord)
		if err != nil {
			return err
		}
		unread += unreadRemoved
	}

	if unread > 0 {
		log.Warnw("PDP service termination sync could not read some data sets from FWSS or PDPVerifier",
			"unread", unread, "checked", len(pending))
	}
	return nil
}

// recordRemovedDataSets marks the data sets that are no longer live in
// PDPVerifier as unrecoverable, which records them for review, and alerts once
// for each. Data sets created after height are not live there either, so they
// are skipped.
func (t *TaskChainSync) recordRemovedDataSets(ctx context.Context, height int64, callOpts func() *bind.CallOpts, dataSetIDs []int64) (unread int, err error) {
	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, t.ethClient)
	if err != nil {
		log.Warnw("skipping PDP removed data set check, PDPVerifier unavailable", "error", err)
		return len(dataSetIDs), nil
	}
	next, err := verifier.GetNextDataSetId(callOpts())
	if err != nil {
		log.Debugw("failed to read the next data set id from PDPVerifier", "error", err)
		return len(dataSetIDs), nil
	}

	for _, dataSetID := range dataSetIDs {
		if err := ctx.Err(); err != nil {
			return unread, err
		}
		if uint64(dataSetID) >= next {
			continue
		}

		live, err := verifier.DataSetLive(callOpts(), big.NewInt(dataSetID))
		if err != nil {
			unread++
			log.Debugw("failed to read data set liveness from PDPVerifier", "dataSetId", dataSetID, "error", err)
			continue
		}
		if live {
			continue
		}

		var alert bool
		committed, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			var markErr error
			alert, markErr = markDatasetProvingUnrecoverable(tx, dataSetID, height)
			return markErr == nil, markErr
		}, harmonydb.OptionRetry())
		if err != nil {
			return unread, xerrors.Errorf("failed to record removed data set %d: %w", dataSetID, err)
		}
		if !committed {
			return unread, xerrors.Errorf("failed to commit record of removed data set %d", dataSetID)
		}
		if alert {
			emitProvingStoppedAlert(t.al, alertNameChainSync, dataSetID)
		}
	}
	return unread, nil
}

// recordServiceTermination stores the service end epoch FWSS reports for a
// data set in pdp_data_set_review. It reports whether the operator still has
// to be told: this call stored the epoch and no deletion of the data set is
// in progress. endEpoch must be greater than zero.
func recordServiceTermination(ctx context.Context, db *harmonydb.DB, dataSetID int64, endEpoch int64) (alert bool, err error) {
	n, err := db.Exec(ctx, `
		INSERT INTO pdp_data_set_review (data_set, service_end_epoch)
		SELECT ds.id, $2
		FROM pdp_data_sets ds
		WHERE ds.id = $1
		  AND NOT EXISTS (
			SELECT 1
			FROM pdp_delete_data_set dd
			WHERE dd.id = ds.id
			  AND (dd.client_requested_termination
			       OR dd.operator_requested_deletion
			       OR dd.after_delete_data_set
			       OR dd.delete_data_set_task_id IS NOT NULL)
		  )
		ON CONFLICT (data_set) DO UPDATE
		SET service_end_epoch = EXCLUDED.service_end_epoch
		WHERE pdp_data_set_review.service_end_epoch IS NULL
	`, dataSetID, endEpoch)
	if err != nil {
		return false, xerrors.Errorf("failed to record service termination of data set %d: %w", dataSetID, err)
	}
	return n == 1, nil
}

// emitServiceTerminatedAlert tells the operator that a data set's service was
// terminated on chain. The data set is already recorded for review, so a
// failed alert is only logged.
func emitServiceTerminatedAlert(ctx context.Context, al curioalerting.AlertingInterface, dataSetID int64, endEpoch int64, railID *big.Int) {
	eventContext := map[string]string{
		curioalerting.CONTEXT_KEY_DATASET: strconv.FormatInt(dataSetID, 10),
	}
	if railID != nil && railID.Sign() > 0 {
		eventContext[curioalerting.CONTEXT_KEY_RAIL] = railID.String()
	}

	err := al.EmitEvent(ctx, curioalerting.AlertEvent{
		System:    alertType,
		Subsystem: alertNameChainSync,
		Message:   fmt.Sprintf("Service for dataset %d was terminated on chain (end epoch %d). Curio will not delete it on its own.", dataSetID, endEpoch),
		Context:   eventContext,
	})
	if err != nil {
		log.Warnw("failed to raise the service termination alert", "dataSetId", dataSetID, "error", err)
	}
}
