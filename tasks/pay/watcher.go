package pay

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/lib/pdp"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type settled struct {
	Hash  string  `db:"tx_hash"`
	Rails []int64 `db:"rail_ids"`
}

type mwe struct {
	Hash    string `db:"signed_tx_hash"`
	Status  string `db:"tx_status"`
	Success bool   `db:"tx_success"`
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
	// This JOINLESS query structure is a critical optimization to prevent the query planner from iterating the entire
	// massive mwe table on each filecoin head change.  We've observed that mwe gets selected as driving table if we
	// use JOIN or WHERE EXIST clauses.
	var settles []settled
	err := db.Select(ctx, &settles, `
	SELECT fpt.tx_hash, fpt.rail_ids
	FROM filecoin_payment_transactions fpt`)
	if err != nil {
		return xerrors.Errorf("failed to get failed settlements from DB: %w", err)
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
		// Note that settlement errors are not expected in any circumstances. Any
		// settlement failure logs should be investigated and prioritize proper
		// settlement handling: https://github.com/filecoin-project/curio/issues/897
		if !mwe.Success {
			delete(goodSettles, mwe.Hash)
			log.Errorw("settlement transaction failed on chain", "txHash", mwe.Hash)
			_, err = db.Exec(ctx, `DELETE FROM filecoin_payment_transactions WHERE tx_hash = $1`, mwe.Hash)
			if err != nil {
				return xerrors.Errorf("failed to delete failed settlement from DB: %w", err)
			}
		}
	}

	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(serviceAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to create fwssv: %w", err)
	}

	for _, settle := range goodSettles {
		err := verifySettle(ctx, db, ethClient, fwssv, serviceAddr, settle)
		if err != nil {
			return xerrors.Errorf("failed to verify settle: %w", err)
		}
	}

	return nil
}

func verifySettle(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, fwssv *FWSS.FilecoinWarmStorageServiceStateView, fwssAddr common.Address, settle settled) error {
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
			// FWSS rails have FWSS as both operator and validator - skip rails that don't match
			if view.Operator != fwssAddr || view.Validator != fwssAddr {
				continue
			}
			return xerrors.Errorf("FWSS rail %d has no associated dataset", railId)
		}

		// If the rail is terminated ensure we are terminating the service in the deletion pipeline
		if view.EndEpoch.Int64() > 0 {
			if err := pdp.EnsureServiceTermination(ctx, db, dataSet.Int64()); err != nil {
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
			if err := pdp.EnsureServiceTermination(ctx, db, dataSet.Int64()); err != nil {
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
