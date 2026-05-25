package pdpv0

import (
	"context"
	"database/sql"
	"math/big"
	"time"

	"golang.org/x/xerrors"

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
)

type TaskChainSync struct {
	db        *harmonydb.DB
	ethClient ethchain.EthClient
	sender    *message.SenderETH
}

func NewTaskChainSync(db *harmonydb.DB, ethClient ethchain.EthClient, sender *message.SenderETH) *TaskChainSync {
	return &TaskChainSync{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
	}
}

func (t *TaskChainSync) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	if !stillOwned() {
		return false, nil
	}
	if err := t.syncStaleDeletionTaskIDs(ctx); err != nil {
		return false, xerrors.Errorf("syncing stale PDP deletion task ids: %w", err)
	}

	if !stillOwned(){
		return false, nil
	}
	if err := t.syncMissingDataSetTerminationMessageWaits(ctx); err != nil {
		return false, xerrors.Errorf("syncing missing PDP termination message waits: %w", err)
	}

	if !stillOwned(){
		return false, nil
	}
	if err := t.syncMissingDataSetDeleteMessageWaits(ctx); err != nil {
		return false, xerrors.Errorf("syncing missing PDP delete message waits: %w", err)
	}

	if !stillOwned() {
		return false, nil
	}
	if err := t.syncProvenDataSetFailureState(ctx); err != nil {
		return false, xerrors.Errorf("syncing proven PDP data set failure state: %w", err)
	}

	if !stillOwned() {
		return false, nil
	}
	err = t.syncFinalizedDataSetDeletionRails(ctx)
	if err != nil {
		return false, xerrors.Errorf("syncing finalized PDP deletion rails: %w", err)
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
			Cpu: 1,
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

// syncStaleDeletionTaskIDs clears deletion task ids that point at Harmony tasks which no longer exist.
// It does not advance any deletion state; it only unpins rows whose terminate/delete task exhausted retries so the normal
// schedulers can pick them up again.
func (t *TaskChainSync) syncStaleDeletionTaskIDs(ctx context.Context) error {
	comm, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		terminated, err := tx.Exec(`UPDATE pdp_delete_data_set pdds
			SET terminate_service_task_id = NULL
			WHERE pdds.terminate_service_task_id IS NOT NULL
			  AND pdds.after_terminate_service = FALSE
			  AND NOT EXISTS (
				SELECT 1
				FROM harmony_task ht
				WHERE ht.id = pdds.terminate_service_task_id
			  )`)
		if err != nil {
			return false, xerrors.Errorf("failed to clear stale terminate service task ids: %w", err)
		}

		deleted, err := tx.Exec(`UPDATE pdp_delete_data_set pdds
			SET delete_data_set_task_id = NULL
			WHERE pdds.delete_data_set_task_id IS NOT NULL
			  AND pdds.after_delete_data_set = FALSE
			  AND NOT EXISTS (
				SELECT 1
				FROM harmony_task ht
				WHERE ht.id = pdds.delete_data_set_task_id
			  )`)
		if err != nil {
			return false, xerrors.Errorf("failed to clear stale delete data set task ids: %w", err)
		}

		if terminated > 0 || deleted > 0 {
			log.Infow("cleared stale PDP deletion task ids",
				"terminateServiceTasks", terminated,
				"deleteDataSetTasks", deleted)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("failed to commit stale task id cleanup: %w", err)
	}
	if !comm {
		return xerrors.Errorf("failed to commit stale task id cleanup")
	}

	return nil
}

type missingTerminationMessageWait struct {
	ID     int64  `db:"id"`
	TxHash string `db:"terminate_tx_hash"`
}

type missingDeleteMessageWait struct {
	ID     int64  `db:"id"`
	TxHash string `db:"delete_tx_hash"`
}

func (t *TaskChainSync) syncMissingDataSetTerminationMessageWaits(ctx context.Context) error {
	var missing []missingTerminationMessageWait
	if err := t.db.Select(ctx, &missing, `
		SELECT id, terminate_tx_hash
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
				    terminate_service_task_id = NULL
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

		n, err := t.db.Exec(ctx, `
			UPDATE pdp_delete_data_set
			SET terminate_tx_hash = NULL,
			    after_terminate_service = FALSE,
			    terminate_service_task_id = NULL
			WHERE id = $1
			  AND terminate_tx_hash = $2
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

	if len(missing) == 0 {
		return nil
	}

	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, t.ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	for _, detail := range missing {
		live, err := verifier.DataSetLive(contract.EthCallOpts(ctx), big.NewInt(detail.ID))
		if err != nil {
			return xerrors.Errorf("failed to check if data set %d is live: %w", detail.ID, err)
		}

		if !live {
			if err := cleanupDeletedDataSet(ctx, t.db, detail.ID, detail.TxHash); err != nil {
				return xerrors.Errorf("failed to reconcile deleted data set %d: %w", detail.ID, err)
			}
			log.Infow("reconciled missing data set delete message wait from chain state", "dataSetId", detail.ID, "txHash", detail.TxHash)
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

// syncProvenDataSetFailureState reconciles local proving backoff after PDPVerifier
// reports that the data set has advanced past the local failure state.
func (t *TaskChainSync) syncProvenDataSetFailureState(ctx context.Context) error {
	var dataSets []struct {
		ID                    int64         `db:"id"`
		ProveAtEpoch          sql.NullInt64 `db:"prove_at_epoch"`
		ConsecutiveFailures   int           `db:"consecutive_prove_failures"`
		NextProveAttemptEpoch sql.NullInt64 `db:"next_prove_attempt_at"`
	}
	if err := t.db.Select(ctx, &dataSets, `SELECT id, prove_at_epoch, consecutive_prove_failures, next_prove_attempt_at
		FROM pdp_data_sets
		WHERE unrecoverable_proving_failure_epoch IS NULL
		  AND (consecutive_prove_failures > 0 OR next_prove_attempt_at IS NOT NULL)
		ORDER BY id`); err != nil {
		return xerrors.Errorf("failed to select data sets with proving failure state: %w", err)
	}

	if len(dataSets) == 0 {
		log.Debugw("no PDP data set proving failure state to reconcile")
		return nil
	}

	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, t.ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	for _, dataSet := range dataSets {
		dataSetID := big.NewInt(dataSet.ID)

		live, err := pdpVerifier.DataSetLive(contract.EthCallOpts(ctx), dataSetID)
		if err != nil {
			return xerrors.Errorf("failed to check if data set %d is live: %w", dataSet.ID, err)
		}
		if !live {
			continue
		}

		lastProvenEpoch, err := pdpVerifier.GetDataSetLastProvenEpoch(contract.EthCallOpts(ctx), dataSetID)
		if err != nil {
			return xerrors.Errorf("failed to get last proven epoch for data set %d: %w", dataSet.ID, err)
		}
		if lastProvenEpoch == nil || lastProvenEpoch.Sign() <= 0 {
			continue
		}

		proofAfterLocalFailure := false
		if dataSet.ProveAtEpoch.Valid && lastProvenEpoch.Cmp(big.NewInt(dataSet.ProveAtEpoch.Int64)) >= 0 {
			proofAfterLocalFailure = true
		} else if dataSet.ConsecutiveFailures > 0 && dataSet.NextProveAttemptEpoch.Valid {
			lastFailureEpoch := dataSet.NextProveAttemptEpoch.Int64 - int64(CalculateBackoffBlocks(dataSet.ConsecutiveFailures))
			proofAfterLocalFailure = lastProvenEpoch.Cmp(big.NewInt(lastFailureEpoch)) > 0
		}
		if !proofAfterLocalFailure {
			continue
		}

		updated, err := t.db.Exec(ctx, `UPDATE pdp_data_sets
			SET consecutive_prove_failures = 0,
				next_prove_attempt_at = NULL
			WHERE id = $1
			  AND unrecoverable_proving_failure_epoch IS NULL
			  AND (consecutive_prove_failures > 0 OR next_prove_attempt_at IS NOT NULL)`, dataSet.ID)
		if err != nil {
			return xerrors.Errorf("failed to reset proving failure state for data set %d: %w", dataSet.ID, err)
		}
		if updated != 0 && updated != 1 {
			return xerrors.Errorf("expected to update 0 or 1 rows for data set %d, updated %d", dataSet.ID, updated)
		}
		if updated == 1 {
			log.Infow("reset PDP data set proving failure state after confirmed PDPVerifier progress",
				"dataSetId", dataSet.ID,
				"lastProvenEpoch", lastProvenEpoch)
		}
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
				if err := t.ensureDataSetDeletion(ctx, dataSet.ID); err != nil {
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
			if err := t.ensureDataSetDeletion(ctx, dataSet.ID); err != nil {
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

// ensureDataSetDeletion marks a terminated data set as eligible for the normal delete task once chain sync has confirmed rail finality.
// The WHERE clause preserves idempotency and prevents this helper from moving rows that have not reached the post-terminate state.
func (t *TaskChainSync) ensureDataSetDeletion(ctx context.Context, dataSetID int64) error {
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
