package pay

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type settled struct {
	Hash      string  `db:"tx_hash"`
	Rails     []int64 `db:"rail_ids"`
	SettledAt int64   `db:"settled_at"`
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

	err := db.Select(ctx, &settles, `SELECT tx_hash, rail_ids, settled_at FROM filecoin_payment_transactions`)
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

	for _, railId := range settle.Rails {
		view, err := payment.GetRail(&bind.CallOpts{Context: ctx}, big.NewInt(railId))
		if err != nil {
			return xerrors.Errorf("failed to get rail: %w", err)
		}

		settledUpto := view.SettledUpTo.Int64()
		requiresDeletion := view.EndEpoch.Int64() > 0 && view.EndEpoch.Cmp(view.SettledUpTo) == 0 || // If the rail is already terminated either by us or the payer, schedule deletion if required
			!(settle.SettledAt-10 < settledUpto && settledUpto < settle.SettledAt+10) // If settledUpto is +-10 of settle.SettledAt, rail was settled

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
	}

	// Delete the settle message from the DB
	_, err = db.Exec(ctx, `DELETE FROM filecoin_payment_transactions WHERE tx_hash = $1`, settle.Hash)
	if err != nil {
		return xerrors.Errorf("failed to delete settle message from DB: %w", err)
	}

	return nil
}
