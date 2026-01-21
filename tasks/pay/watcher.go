package pay

import (
	"context"
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
	// Get payee from chain
	payee, err := getProviderPayee(ctx, db, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to get payee from chain: %w", err)
	}

	// Get all rails from chain state (source of truth)
	paymentContractAddr, err := filecoinpayment.PaymentContractAddress()
	if err != nil {
		return xerrors.Errorf("failed to get payment contract address: %w", err)
	}

	payment, err := filecoinpayment.NewPayments(paymentContractAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to create payments: %w", err)
	}

	tokenAddress, err := contract.USDFCAddress()
	if err != nil {
		return xerrors.Errorf("failed to get USDFC address: %w", err)
	}

	rails, err := payment.GetRailsForPayeeAndToken(&bind.CallOpts{Context: ctx}, payee, tokenAddress, big.NewInt(0), big.NewInt(0))
	if err != nil {
		return xerrors.Errorf("failed to get rails for payee: %w", err)
	}

	// Build a map of rail ID -> tx_hash from DB for matching
	var settles []settled
	err = db.Select(ctx, &settles, `SELECT tx_hash, rail_ids FROM filecoin_payment_transactions`)
	if err != nil {
		return xerrors.Errorf("failed to get settled message hash from DB: %w", err)
	}

	railToTxHash := make(map[int64]string)
	for _, settle := range settles {
		for _, railId := range settle.Rails {
			railToTxHash[railId] = settle.Hash
		}
	}

	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(serviceAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to create fwssv: %w", err)
	}

	current, err := ethClient.BlockNumber(ctx)
	if err != nil {
		return xerrors.Errorf("failed to get current block number: %w", err)
	}

	// Track which tx hashes we've processed to delete them at the end
	processedTxHashes := make(map[string]bool)

	// Verify each rail from chain state
	for _, railInfo := range rails.Results {
		railId := railInfo.RailId.Int64()
		txHash := railToTxHash[railId] // May be empty if rail not in DB

		err := verifyRail(ctx, db, ethClient, payment, fwssv, railId, current, txHash)
		if err != nil {
			log.Warnw("failed to verify rail", "railId", railId, "error", err)
			continue
		}

		// Mark this tx hash as processed (if it exists)
		if txHash != "" {
			processedTxHashes[txHash] = true
		}
	}

	// Delete processed transactions from DB
	for txHash := range processedTxHashes {
		_, err = db.Exec(ctx, `DELETE FROM filecoin_payment_transactions WHERE tx_hash = $1`, txHash)
		if err != nil {
			log.Warnw("failed to delete transaction from DB", "txHash", txHash, "error", err)
		}
	}

	return nil
}

// verifyRail checks a single rail from chain state and schedules dataset deletion if needed.
// The txHash parameter is optional - if empty, the rail was not found in the DB transactions table.
func verifyRail(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, payment *filecoinpayment.Payments, fwssv *FWSS.FilecoinWarmStorageServiceStateView, railId int64, current uint64, txHash string) error {
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
		return nil
	}

	// Rail was not settled completely, terminate dataSet
	dataSet, err := fwssv.RailToDataSet(&bind.CallOpts{Context: ctx}, big.NewInt(railId))
	if err != nil {
		return xerrors.Errorf("failed to get rail to data set: %w", err)
	}

	if dataSet.Int64() == int64(0) {
		return nil
	}

	n, err := db.Exec(ctx, `INSERT INTO pdp_delete_data_set (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, dataSet.Int64())
	if err != nil {
		return xerrors.Errorf("failed to insert into pdp_delete_data_set: %w", err)
	}
	// n can be 0 if the row already exists due to ON CONFLICT DO NOTHING
	if n > 1 {
		return xerrors.Errorf("expected to insert 0 or 1 rows, inserted %d", n)
	}

	if n == 1 {
		log.Infow("Rail was not settled completely, terminating dataSet", "dataSetId", dataSet.Int64(), "railId", railId, "settleTxHash", txHash)
	}

	return nil
}
