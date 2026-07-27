package pdpv0

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

const alertNameTerminateFWSSService = "TerminateFWSSService"

type pendingServiceTermination struct {
	DataSetId       int64  `db:"id"`
	TxHash          string `db:"terminate_tx_hash"`
	ClientRequested bool   `db:"client_requested_termination"`
}

type serviceTerminationMessageWait struct {
	TxHash  string       `db:"signed_tx_hash"`
	Status  string       `db:"tx_status"`
	Success sql.NullBool `db:"tx_success"`
}

func NewTerminateServiceWatcher(w *Watcher) {
	if err := w.AddWatcher(func(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, al curioalerting.AlertingInterface, revert, apply *chainTypes.TipSet) {
		err := processTerminations(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending service termination transactions: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameTerminateFWSSService,
				Message:   fmt.Sprintf("failed to process pending service termination transactions: %s", err),
			})
		}
	}, WatcherOrderTerminate); err != nil {
		panic(err)
	}
}

func processTerminations(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient) error {
	var pending []pendingServiceTermination
	err := db.Select(ctx, &pending, `
		SELECT id, terminate_tx_hash, client_requested_termination
		FROM pdp_delete_data_set
		WHERE service_termination_epoch IS NULL
		  AND after_terminate_service = TRUE
		  AND terminate_tx_hash IS NOT NULL
	`)
	if err != nil {
		return xerrors.Errorf("failed to select pending data set terminations: %w", err)
	}

	if len(pending) == 0 {
		return nil
	}

	byHash := make(map[string]pendingServiceTermination, len(pending))
	hashes := make([]string, 0, len(pending))
	for _, detail := range pending {
		hashes = append(hashes, detail.TxHash)
		byHash[detail.TxHash] = detail
	}

	var waits []serviceTerminationMessageWait
	err = db.Select(ctx, &waits, `
		SELECT signed_tx_hash, tx_status, tx_success
		FROM message_waits_eth
		WHERE signed_tx_hash = ANY($1)
	`, hashes)
	if err != nil {
		return xerrors.Errorf("failed to select service termination message waits: %w", err)
	}

	seen := map[string]struct{}{}
	var successes []pendingServiceTermination
	var failures []pendingServiceTermination
	for _, wait := range waits {
		seen[wait.TxHash] = struct{}{}
		detail := byHash[wait.TxHash]

		if wait.Status == "confirmed" && wait.Success.Valid && wait.Success.Bool {
			successes = append(successes, detail)
			continue
		}

		if wait.Status == "failed" || (wait.Status == "confirmed" && wait.Success.Valid && !wait.Success.Bool) {
			failures = append(failures, detail)
		}
	}

	for _, detail := range pending {
		if _, ok := seen[detail.TxHash]; ok {
			continue
		}
		log.Warnw("filecoin warm storage service termination tx missing message_waits_eth row", "txHash", detail.TxHash, "dataSetId", detail.DataSetId)
	}

	if err := processSuccessfulTerminations(ctx, db, ethClient, successes); err != nil {
		return err
	}
	return processFailedTerminations(ctx, db, ethClient, failures)
}

func processSuccessfulTerminations(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, successes []pendingServiceTermination) error {
	if len(successes) == 0 {
		return nil
	}

	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, sAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to get FWSS view address: %w", err)
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate FWSS service state view: %w", err)
	}

	for _, detail := range successes {
		ds, err := fwssv.GetDataSet(contract.EthCallOpts(ctx), big.NewInt(detail.DataSetId))
		if err != nil {
			return xerrors.Errorf("failed to get data set %d: %w", detail.DataSetId, err)
		}

		epoch := fwssServiceTerminationEpoch(ds)
		if epoch == nil {
			return xerrors.Errorf("data set %d has no termination epoch", detail.DataSetId)
		}

		if err := markServiceTerminationConfirmed(ctx, db, detail, *epoch); err != nil {
			return xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
		}

		log.Infow("Successfully confirmed data set termination", "dataSetId", detail.DataSetId, "epoch", *epoch, "txHash", detail.TxHash)
	}
	return nil
}

func processFailedTerminations(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, failures []pendingServiceTermination) error {
	if len(failures) == 0 {
		return nil
	}

	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, sAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to get FWSS view address: %w", err)
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate FWSS service state view: %w", err)
	}

	for _, detail := range failures {
		ds, err := fwssv.GetDataSet(contract.EthCallOpts(ctx), big.NewInt(detail.DataSetId))
		if err != nil {
			return xerrors.Errorf("failed to get data set %d: %w", detail.DataSetId, err)
		}

		if epoch := fwssServiceTerminationEpoch(ds); epoch != nil {
			if err := markServiceTerminationConfirmed(ctx, db, detail, *epoch); err != nil {
				return xerrors.Errorf("failed to reconcile failed service termination for data set %d: %w", detail.DataSetId, err)
			}

			log.Warnw("service termination tx failed but FWSS state is already terminated; advanced pipeline", "dataSetId", detail.DataSetId, "epoch", *epoch, "txHash", detail.TxHash)
			continue
		}

		if detail.ClientRequested {
			if err := deleteFailedClientTermination(ctx, db, detail); err != nil {
				return xerrors.Errorf("failed to delete failed client service termination for data set %d: %w", detail.DataSetId, err)
			}

			log.Warnw("deleted failed client service termination for retry", "dataSetId", detail.DataSetId, "txHash", detail.TxHash)
			continue
		}

		if err := resetFailedProviderTermination(ctx, db, detail); err != nil {
			return xerrors.Errorf("failed to reset failed service termination for data set %d: %w", detail.DataSetId, err)
		}

		log.Warnw("reset failed provider service termination for retry", "dataSetId", detail.DataSetId, "txHash", detail.TxHash)
	}

	return nil
}

func fwssServiceTerminationEpoch(ds FWSS.FilecoinWarmStorageServiceDataSetInfoView) *int64 {
	if ds.PdpEndEpoch == nil || ds.PdpEndEpoch.Sign() == 0 {
		return nil
	}
	epoch := ds.PdpEndEpoch.Int64()
	return &epoch
}

func markServiceTerminationConfirmed(ctx context.Context, db *harmonydb.DB, detail pendingServiceTermination, epoch int64) error {
	n, err := db.Exec(ctx, `
		UPDATE pdp_delete_data_set
		SET service_termination_epoch = $1,
		    terminate_service_task_id = NULL,
		    client_terminate_service_task_id = NULL
		WHERE id = $2
		  AND terminate_tx_hash = $3
		  AND after_terminate_service = TRUE
		  AND service_termination_epoch IS NULL
	`, epoch, detail.DataSetId, detail.TxHash)
	if err != nil {
		return err
	}
	if n > 1 {
		return xerrors.Errorf("expected to update 0 or 1 rows, updated %d", n)
	}
	if n == 0 {
		log.Warnw("service termination row was already reconciled or removed", "dataSetId", detail.DataSetId, "txHash", detail.TxHash)
	}
	return nil
}

func deleteFailedClientTermination(ctx context.Context, db *harmonydb.DB, detail pendingServiceTermination) error {
	n, err := db.Exec(ctx, `
		DELETE FROM pdp_delete_data_set
		WHERE id = $1
		  AND terminate_tx_hash = $2
		  AND client_requested_termination = TRUE
		  AND after_terminate_service = TRUE
		  AND service_termination_epoch IS NULL
	`, detail.DataSetId, detail.TxHash)
	if err != nil {
		return err
	}
	if n > 1 {
		return xerrors.Errorf("expected to delete 0 or 1 rows, deleted %d", n)
	}
	if n == 0 {
		log.Warnw("failed client service termination row was already reconciled or removed", "dataSetId", detail.DataSetId, "txHash", detail.TxHash)
	}
	return nil
}

func resetFailedProviderTermination(ctx context.Context, db *harmonydb.DB, detail pendingServiceTermination) error {
	n, err := db.Exec(ctx, `
		UPDATE pdp_delete_data_set
		SET terminate_tx_hash = NULL,
		    after_terminate_service = FALSE,
		    terminate_service_task_id = NULL
		WHERE id = $1
		  AND terminate_tx_hash = $2
		  AND client_requested_termination = FALSE
		  AND after_terminate_service = TRUE
		  AND service_termination_epoch IS NULL
	`, detail.DataSetId, detail.TxHash)
	if err != nil {
		return err
	}
	if n > 1 {
		return xerrors.Errorf("expected to update 0 or 1 rows, updated %d", n)
	}
	if n == 0 {
		log.Warnw("failed provider service termination row was already reconciled or removed", "dataSetId", detail.DataSetId, "txHash", detail.TxHash)
	}
	return nil
}
