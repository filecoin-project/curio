package pay

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type settled struct {
	Hash  string  `db:"tx_hash"`
	Rails []int64 `db:"rail_ids"`
}

type mwe struct {
	Hash   string `db:"signed_tx_hash"`
	Status string `db:"tx_status"`
	// tx_success is NULL for pending rows, set to true/false only on
	// confirmed/failed. Pointer type is required so the scanner can handle
	// NULL; by the time we check this field we've already filtered to
	// confirmed rows where it will be non-nil.
	Success *bool `db:"tx_success"`
}

func NewSettleWatcher(db *harmonydb.DB, ethClient ethchain.EthClient, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingTransactions(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending settle transactions: %s", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingTransactions(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient) error {
	// Handle failed settlements first - log error and clean up
	// This JOINLESS query structure is a critical optimization to prevent the query planner from iterating the entire
	// massive mwe table on each filecoin head change.  We've observed that mwe gets selected as driving table if we
	// use JOIN or WHERE EXIST clauses.
	var settles []settled
	err := db.Select(ctx, &settles, `
	SELECT fpt.tx_hash, fpt.rail_ids
	FROM filecoin_payment_transactions fpt`)
	if err != nil {
		return xerrors.Errorf("failed to get settlements from DB: %w", err)
	}
	if len(settles) == 0 {
		return nil
	}
	var hashes []string
	goodSettles := make(map[string]settled)
	for _, settle := range settles {
		hashes = append(hashes, settle.Hash)
		goodSettles[settle.Hash] = settle
	}
	var mwes []mwe
	err = db.Select(ctx, &mwes, `
	SELECT signed_tx_hash, tx_status, tx_success
	FROM message_waits_eth
	WHERE signed_tx_hash = ANY($1)
	`, hashes)
	if err != nil {
		return xerrors.Errorf("failed to get message waits from DB: %w", err)
	}

	for _, mwe := range mwes {
		// wait for message to land
		if mwe.Status != "confirmed" {
			delete(goodSettles, mwe.Hash)
			continue
		}
		// Confirmed rows should always have tx_success set, so the nil check
		// is defensive. Settlement errors are not expected in any circumstances;
		// any settlement failure logs should be investigated and prioritize
		// proper settlement handling:
		// https://github.com/filecoin-project/curio/issues/897
		if mwe.Success == nil || !*mwe.Success {
			delete(goodSettles, mwe.Hash)
			log.Errorw("settlement transaction failed on chain", "txHash", mwe.Hash)
			_, err = db.Exec(ctx, `DELETE FROM filecoin_payment_transactions WHERE tx_hash = $1`, mwe.Hash)
			if err != nil {
				return xerrors.Errorf("failed to delete failed settlement from DB: %w", err)
			}
		}

		// since mwe updated atomically with fpt iterating all mwe should iterate all settles
	}

	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, serviceAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to get FWSS view address: %w", err)
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to create fwssv: %w", err)
	}

	for _, settle := range goodSettles {
		err := verifySettle(ctx, db, ethClient, fwssv, serviceAddr, settle)
		if err != nil {
			log.Errorw("failed to verify settle, skipping", "txHash", settle.Hash, "error", err)
		}
	}

	return nil
}

func verifySettle(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, fwssv *FWSS.FilecoinWarmStorageServiceStateView, fwssAddr common.Address, settle settled) error {
	paymentContractAddr, err := filecoinpayment.PaymentContractAddress()
	if err != nil {
		return fmt.Errorf("failed to get payment contract address: %w", err)
	}

	payment, err := filecoinpayment.NewPayments(paymentContractAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to create payments: %w", err)
	}

	current, err := ethClient.BlockNumber(ctx)
	if err != nil {
		return xerrors.Errorf("failed to get current block number: %w", err)
	}

	for _, railId := range settle.Rails {
		view, getRailErr := payment.GetRail(contract.EthCallOpts(ctx), big.NewInt(railId))

		dataSet, dsErr := fwssv.RailToDataSet(contract.EthCallOpts(ctx), big.NewInt(railId))
		if dsErr != nil {
			log.Errorw("failed to get rail to data set, skipping termination checks", "railId", railId, "error", dsErr)
			continue
		}

		// FilecoinPay zeroes the rail struct atomically with the final settle, so
		// post-confirm GetRail reverts and the readable EndEpoch==SettledUpTo path
		// below cannot fire. Treat the revert as finalization when FWSS still maps
		// the rail and the lockup has elapsed.
		if getRailErr != nil {
			if dataSet.Int64() == 0 {
				log.Errorw("failed to get rail, skipping termination checks", "railId", railId, "error", getRailErr)
				continue
			}
			finalized, finErr := pipelineFinalizationElapsed(ctx, db, dataSet.Int64(), current)
			if finErr != nil {
				log.Warnw("failed to check deletion pipeline state, skipping", "railId", railId, "dataSetId", dataSet.Int64(), "error", finErr)
				continue
			}
			if !finalized {
				log.Errorw("failed to get rail, skipping termination checks", "railId", railId, "error", getRailErr)
				continue
			}
			log.Infow("rail finalized, inferred from getRail revert", "railId", railId, "dataSetId", dataSet.Int64())
			if err := ensureDataSetDeletion(ctx, db, dataSet.Int64()); err != nil {
				return err
			}
			continue
		}

		if dataSet.Int64() == int64(0) {
			// FWSS rails have FWSS as both operator and validator - skip rails that don't match
			if view.Operator != fwssAddr || view.Validator != fwssAddr {
				continue
			}
			log.Errorw("FWSS rail has no associated dataset, skipping termination checks", "railId", railId)
			continue
		}

		// If the rail is terminated ensure we are terminating the service in the deletion pipeline
		if view.EndEpoch.Int64() > 0 {
			// Termination is also detected by proving-task error handling, so a
			// transient DB failure here just delays cleanup rather than losing it.
			if err := FWSS.EnsureServiceTermination(ctx, db, dataSet.Int64()); err != nil {
				log.Warnw("failed to ensure service termination", "dataSetId", dataSet.Int64(), "railId", railId, "error", err)
			}
			// When finalized, schedule dataset deletion. This may be the last
			// time we see this rail in a finalized state, so we perform a hard-fail
			// here to prevent tx cleanup to allow retry on next loop.
			if view.EndEpoch.Cmp(view.SettledUpTo) == 0 {
				if err := ensureDataSetDeletion(ctx, db, dataSet.Int64()); err != nil {
					return err
				}
			}
		}

		// For live rails, check if we are fully unsettled 1 day before the lockup period ends.
		// If so assume payer is in default and schedule deletion
		threshold := big.NewInt(0).Add(view.SettledUpTo, view.LockupPeriod)
		thresholdWithGrace := big.NewInt(0).Sub(threshold, big.NewInt(builtin.EpochsInDay))

		if thresholdWithGrace.Uint64() < current {
			log.Infow("Rail soon to default, terminating dataSet", "dataSetId", dataSet.Int64(), "railId", railId, "settleTxHash", settle.Hash)
			if err := FWSS.EnsureServiceTermination(ctx, db, dataSet.Int64()); err != nil {
				log.Warnw("failed to ensure service termination for defaulting rail", "dataSetId", dataSet.Int64(), "railId", railId, "error", err)
			}
		}
	}

	// Delete the settle message from the DB
	_, err = db.Exec(ctx, `DELETE FROM filecoin_payment_transactions WHERE tx_hash = $1`, settle.Hash)
	if err != nil {
		return xerrors.Errorf("failed to delete settle message from DB: %w", err)
	}

	return nil
}

func ensureDataSetDeletion(ctx context.Context, db *harmonydb.DB, dataSetID int64) error {
	n, err := db.Exec(ctx, `UPDATE pdp_delete_data_set SET deletion_allowed = TRUE WHERE id = $1`, dataSetID)
	if err != nil {
		return xerrors.Errorf("failed to set deletion_allowed for data set %d: %w", dataSetID, err)
	}
	if n != 1 {
		return xerrors.Errorf("expected to update 1 row, updated %d", n)
	}
	return nil
}

// pipelineFinalizationElapsed reports whether after_terminate_service is set
// and service_termination_epoch (pdpEndEpoch) has elapsed. With both met, a
// GetRail revert on the rail uniquely indicates finalization.
func pipelineFinalizationElapsed(ctx context.Context, db *harmonydb.DB, dataSetID int64, current uint64) (bool, error) {
	type row struct {
		Epoch *int64 `db:"service_termination_epoch"`
		After bool   `db:"after_terminate_service"`
	}
	var rows []row
	err := db.Select(ctx, &rows, `
		SELECT service_termination_epoch, after_terminate_service
		FROM pdp_delete_data_set
		WHERE id = $1
	`, dataSetID)
	if err != nil {
		return false, xerrors.Errorf("query pdp_delete_data_set: %w", err)
	}
	if len(rows) == 0 || !rows[0].After || rows[0].Epoch == nil {
		return false, nil
	}
	return uint64(*rows[0].Epoch) <= current, nil
}
