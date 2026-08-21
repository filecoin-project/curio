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

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

const alertNameProvingPeriod = "ProvingPeriod"
const provingPeriodReconcileBatchLimit = 128

// NewProvingPeriodWatcher reconciles confirmed proving-period side effects
// before the prove watcher runs. nextProvingPeriod is what finally applies
// scheduled piece removals and can also clear PDPVerifier's next challenge when
// the dataset becomes empty, so both local states must be fixed before prove
// task scheduling looks at the dataset.
//
// Empty-period reconciliation runs first because there is no proof to submit for
// a zero on-chain challenge epoch. Clearing the stale schedule before delete
// reconciliation prevents the prove watcher from disabling proving after a new
// add already made the dataset ready again.
func NewProvingPeriodWatcher(w *Watcher) {
	if err := w.AddWatcher(func(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, al curioalerting.AlertingInterface, revert, apply *chainTypes.TipSet) {
		if err := clearFailedProvingPeriodReconciliations(ctx, db, provingPeriodReconcileBatchLimit); err != nil {
			log.Warnf("Failed to clear failed PDP proving period reconciliation flags: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProvingPeriod,
				Message:   fmt.Sprintf("failed to clear failed PDP proving period reconciliation flags: %s", err),
			})
			return
		}

		readyPeriods, err := selectProvingPeriodsNeedingReconcile(ctx, db, provingPeriodReconcileBatchLimit)
		if err != nil {
			log.Warnf("Failed to select ready PDP proving periods: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProvingPeriod,
				Message:   fmt.Sprintf("failed to select ready PDP proving periods: %s", err),
			})
			return
		}
		if len(readyPeriods) == 0 {
			return
		}
		readyDataSets := provingPeriodDataSetIDs(readyPeriods)

		if err := processEmptyProvingPeriods(ctx, db, ethClient, readyPeriods); err != nil {
			log.Warnf("Failed to process empty PDP proving periods: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProvingPeriod,
				Message:   fmt.Sprintf("failed to process empty PDP proving periods: %s", err),
			})
			return
		}

		if err := processPendingPieceDeletes(ctx, db, ethClient, readyDataSets); err != nil {
			log.Warnf("Failed to process pending PDP piece deletes: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProvingPeriod,
				Message:   fmt.Sprintf("failed to process pending PDP piece deletes: %s", err),
			})
			return
		}

		if err := clearProvingPeriodReconcileNeeded(ctx, db, readyPeriods); err != nil {
			log.Warnf("Failed to clear PDP proving period reconciliation flags: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameProvingPeriod,
				Message:   fmt.Sprintf("failed to clear PDP proving period reconciliation flags: %s", err),
			})
		}

	}, WatcherOrderCleanupPieces); err != nil {
		panic(err)
	}
}

type confirmedProvingPeriod struct {
	DataSetID int64          `db:"id"`
	TxHash    sql.NullString `db:"challenge_request_msg_hash"`
}

func selectProvingPeriodsNeedingReconcile(ctx context.Context, db *harmonydb.DB, limit int) ([]confirmedProvingPeriod, error) {
	var periods []confirmedProvingPeriod

	// Pick datasets whose nextPP side effects can be reconciled now. The NULL
	// hash branch is for partial progress: empty-period reconciliation already
	// cleared the local proving schedule, but delete reconciliation or final flag
	// clearing did not finish. The non-NULL branch requires a confirmed,
	// successful message_waits_eth row before reconciliation runs.
	//
	// Keep the confirmed-message branch as LATERAL with OFFSET 0. Benchmarks on
	// Postgres and Yugabyte showed the obvious EXISTS/join forms scan/hash
	// message_waits_eth at million-row scale, while this shape stays bounded by
	// the pp_reconcile_needed index and message_waits_eth primary-key lookups.
	err := db.Select(ctx, &periods, `
		SELECT ready.id,
		       ready.challenge_request_msg_hash
		FROM (
		    (
		        SELECT pds.id,
		               pds.challenge_request_msg_hash
		        FROM pdp_data_sets pds
		        WHERE pds.pp_reconcile_needed = TRUE
		          AND pds.unrecoverable_proving_failure_epoch IS NULL
		          AND pds.challenge_request_msg_hash IS NULL
		        ORDER BY pds.id
		        LIMIT $1
		    )
		    UNION ALL
		    (
		        SELECT pds.id,
		               pds.challenge_request_msg_hash
		        FROM pdp_data_sets pds
		        INNER JOIN LATERAL (
		            SELECT 1
		            FROM message_waits_eth mwe
		            WHERE mwe.signed_tx_hash = pds.challenge_request_msg_hash
		              AND mwe.tx_status = 'confirmed'
		              AND mwe.tx_success = TRUE
		            OFFSET 0
		        ) mwe ON TRUE
		        WHERE pds.pp_reconcile_needed = TRUE
		          AND pds.unrecoverable_proving_failure_epoch IS NULL
		          AND pds.challenge_request_msg_hash IS NOT NULL
		        ORDER BY pds.id
		        LIMIT $1
		    )
		) ready
		ORDER BY ready.id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, xerrors.Errorf("failed to select proving periods needing reconciliation: %w", err)
	}

	return periods, nil
}

func provingPeriodDataSetIDs(periods []confirmedProvingPeriod) []int64 {
	dataSets := make([]int64, 0, len(periods))
	for _, period := range periods {
		dataSets = append(dataSets, period.DataSetID)
	}
	return dataSets
}

func clearFailedProvingPeriodReconciliations(ctx context.Context, db *harmonydb.DB, limit int) error {
	_, err := db.Exec(ctx, `
		WITH failed AS (
			SELECT pds.id,
			       pds.challenge_request_msg_hash
			FROM pdp_data_sets pds
			INNER JOIN message_waits_eth mwe ON mwe.signed_tx_hash = pds.challenge_request_msg_hash
			WHERE pds.pp_reconcile_needed = TRUE
			  AND pds.challenge_request_msg_hash IS NOT NULL
			  AND (mwe.tx_status = 'failed' OR mwe.tx_success = FALSE)
			ORDER BY pds.id
			LIMIT $1
		)
		UPDATE pdp_data_sets pds
		SET pp_reconcile_needed = FALSE
		FROM failed
		WHERE pds.id = failed.id
		  AND pds.challenge_request_msg_hash = failed.challenge_request_msg_hash
	`, limit)
	if err != nil {
		return xerrors.Errorf("failed to clear failed proving period reconciliations: %w", err)
	}
	return nil
}

// processEmptyProvingPeriods reconciles datasets whose confirmed initPP/nextPP
// message left no next challenge on-chain. This happens when the final piece is
// removed: PDPVerifier clears the challenge, but Curio may have already stored
// the now-stale prove_at_epoch.
//
// A zero next challenge epoch is ambiguous since FilOzone/pdp#297:
// processPieceDeletions also clears it, on a perfectly healthy data set that
// still has leaves and is simply due a fresh challenge at the next proving period.
// Leaf count is the discriminator -- only a zero challenge with zero leaves is a
// genuinely emptied data set.
func processEmptyProvingPeriods(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, periods []confirmedProvingPeriod) error {
	if len(periods) == 0 {
		return nil
	}

	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	for _, period := range periods {
		if !period.TxHash.Valid {
			continue
		}

		dataSetID := big.NewInt(period.DataSetID)

		nextChallengeEpoch, err := verifier.GetNextChallengeEpoch(contract.EthCallOpts(ctx), dataSetID)
		if err != nil {
			return xerrors.Errorf("failed to get next challenge epoch for data set %d: %w", period.DataSetID, err)
		}
		if nextChallengeEpoch.Sign() != 0 {
			continue
		}

		leafCount, err := verifier.GetDataSetLeafCount(contract.EthCallOpts(ctx), dataSetID)
		if err != nil {
			return xerrors.Errorf("failed to get leaf count for data set %d: %w", period.DataSetID, err)
		}
		if leafCount.Sign() > 0 {
			log.Debugw("skipping empty-period reset; challenge cleared by processed deletions",
				"dataSetId", period.DataSetID, "leafCount", leafCount.String())
			continue
		}

		affected, err := db.Exec(ctx, `
			UPDATE pdp_data_sets
			SET challenge_request_msg_hash = NULL,
			    prove_at_epoch = NULL,
			    prev_challenge_request_epoch = NULL,
			    init_ready = FALSE
			WHERE id = $1
			  AND challenge_request_msg_hash = $2
			  AND unrecoverable_proving_failure_epoch IS NULL
		`, period.DataSetID, period.TxHash.String)
		if err != nil {
			return xerrors.Errorf("failed to reset empty proving period for data set %d: %w", period.DataSetID, err)
		}
		if affected > 1 {
			return xerrors.Errorf("expected to update at most 1 proving period row, updated %d", affected)
		}
		if affected == 1 {
			log.Infow("reset empty proving period",
				"dataSetId", period.DataSetID,
				"txHash", period.TxHash.String)
		}
	}

	return nil
}

func clearProvingPeriodReconcileNeeded(ctx context.Context, db *harmonydb.DB, periods []confirmedProvingPeriod) error {
	for _, period := range periods {
		_, err := db.Exec(ctx, `
			UPDATE pdp_data_sets
			SET pp_reconcile_needed = FALSE
			WHERE id = $1
			  AND (challenge_request_msg_hash = $2 OR challenge_request_msg_hash IS NULL)
		`, period.DataSetID, period.TxHash.String)
		if err != nil {
			return xerrors.Errorf("failed to clear proving period reconciliation flag for data set %d: %w", period.DataSetID, err)
		}
	}
	return nil
}
