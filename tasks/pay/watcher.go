package pay

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type settled struct {
	Hash  string  `db:"tx_hash"`
	Rails []int64 `db:"rail_ids"`
}

func NewSettleWatcher(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
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

func processPendingTransactions(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Handle failed settlements first - log error and clean up
	var failedSettles []settled
	err := db.Select(ctx, &failedSettles, `
		SELECT fpt.tx_hash, fpt.rail_ids
		FROM filecoin_payment_transactions fpt
		JOIN message_waits_eth mwe ON fpt.tx_hash = mwe.signed_tx_hash
		WHERE mwe.tx_status = 'confirmed' AND mwe.tx_success = FALSE`)
	if err != nil {
		return xerrors.Errorf("failed to get failed settlements from DB: %w", err)
	}

	// Note that settlement errors are not expected in any circumstances. Any
	// settlement failure logs should be investigated and prioritize proper
	// settlement handling: https://github.com/filecoin-project/curio/issues/897
	for _, settle := range failedSettles {
		log.Errorw("settlement transaction failed on chain", "txHash", settle.Hash, "railIDs", settle.Rails)
		_, err = db.Exec(ctx, `DELETE FROM filecoin_payment_transactions WHERE tx_hash = $1`, settle.Hash)
		if err != nil {
			return xerrors.Errorf("failed to delete failed settlement from DB: %w", err)
		}
	}

	// Process confirmed successful settlements
	var settles []settled
	err = db.Select(ctx, &settles, `
		SELECT fpt.tx_hash, fpt.rail_ids
		FROM filecoin_payment_transactions fpt
		JOIN message_waits_eth mwe ON fpt.tx_hash = mwe.signed_tx_hash
		WHERE mwe.tx_status = 'confirmed' AND mwe.tx_success = TRUE`)
	if err != nil {
		return xerrors.Errorf("failed to get confirmed settlements from DB: %w", err)
	}

	if len(settles) == 0 {
		return nil
	}

	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(serviceAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to create fwssv: %w", err)
	}

	for _, settle := range settles {
		err := verifySettle(ctx, db, ethClient, fwssv, settle)
		if err != nil {
			return xerrors.Errorf("failed to verify settle: %w", err)
		}
	}

	return nil
}

func verifySettle(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, fwssv *FWSS.FilecoinWarmStorageServiceStateView, settle settled) error {
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
		view, err := payment.GetRail(&bind.CallOpts{Context: ctx}, big.NewInt(railId))
		if err != nil {
			return xerrors.Errorf("failed to get rail: %w", err)
		}

		dataSet, err := fwssv.RailToDataSet(&bind.CallOpts{Context: ctx}, big.NewInt(railId))
		if err != nil {
			return xerrors.Errorf("failed to get rail to data set: %w", err)
		}

		if dataSet.Int64() == int64(0) {
			return xerrors.New("invalid dataSetID 0 returned from RailToDataSet")
		}

		// If the rail is terminated ensure we are terminating the service in the deletion pipeline
		if view.EndEpoch.Int64() > 0 {
			if err := ensureServiceTermination(ctx, db, dataSet.Int64()); err != nil {
				return err
			}
			// When finalized schedule dataset deletion
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
			if err := ensureServiceTermination(ctx, db, dataSet.Int64()); err != nil {
				return err
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

func ensureServiceTermination(ctx context.Context, db *harmonydb.DB, dataSetID int64) error {
	n, err := db.Exec(ctx, `INSERT INTO pdp_delete_data_set (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, dataSetID)
	if err != nil {
		return xerrors.Errorf("failed to insert into pdp_delete_data_set: %w", err)
	}
	if n != 1 & n != 0) {
		return xerrors.Errorf("expected to insert 0 or 1 rows, inserted %d", n)
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
