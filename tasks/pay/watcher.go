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
	var settles []settled

	err := db.Select(ctx, &settles, `SELECT tx_hash, rail_ids FROM filecoin_payment_transactions`)
	if err != nil {
		return xerrors.Errorf("failed to get settled message hash from DB: %w", err)
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

		var requiresDeletion bool

		// If the rail is already terminated either by us or the payer, schedule deletion if required
		if view.EndEpoch.Int64() > 0 && view.EndEpoch.Cmp(view.SettledUpTo) == 0 {
			requiresDeletion = true
		}

		// If the rail is not terminated, check if we are 1 day before the lockup period ends. If so, schedule deletion
		threshold := big.NewInt(0).Add(view.SettledUpTo, view.LockupPeriod)
		thresholdWithGrace := big.NewInt(0).Sub(threshold, big.NewInt(builtin.EpochsInDay))

		if thresholdWithGrace.Uint64() < current {
			requiresDeletion = true
		}

		if !requiresDeletion {
			continue
		}

		// Rail was not settled completely, terminate dataSet
		dataSet, err := fwssv.RailToDataSet(&bind.CallOpts{Context: ctx}, big.NewInt(railId))
		if err != nil {
			return xerrors.Errorf("failed to get rail to data set: %w", err)
		}

		if dataSet.Int64() == int64(0) {
			continue
		}

		n, err := db.Exec(ctx, `INSERT INTO pdp_delete_data_set (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, dataSet.Int64())
		if err != nil {
			return xerrors.Errorf("failed to insert into pdp_delete_data_set: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("expected to insert 1 row, inserted %d", n)
		}

		log.Infow("Rail was not settled completely, terminating dataSet", "dataSetId", dataSet.Int64(), "railId", railId, "settleTxHash", settle.Hash)
	}

	// Delete the settle message from the DB
	_, err = db.Exec(ctx, `DELETE FROM filecoin_payment_transactions WHERE tx_hash = $1`, settle.Hash)
	if err != nil {
		return xerrors.Errorf("failed to delete settle message from DB: %w", err)
	}

	return nil
}
