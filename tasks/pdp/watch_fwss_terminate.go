package pdp

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
)

type termination struct {
	DataSetId int64  `db:"id"`
	TxHash    string `db:"terminate_tx_hash"`
}

func NewTerminateServiceWatcher(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingTerminations(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending service termination transactions: %s", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingTerminations(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {

	var details []termination
	err := db.Select(ctx, &details, `SELECT id, terminate_tx_hash FROM pdp_delete_data_set WHERE termination_epoch IS NULL AND after_terminate_service = TRUE`)
	if err != nil {
		return xerrors.Errorf("failed to select pending data sets: %w", err)
	}

	for _, detail := range details {
		err = processPendingTermination(ctx, db, ethClient, detail)
		if err != nil {
			return xerrors.Errorf("failed to process pending termination: %w", err)
		}
	}
	return nil
}

func processPendingTermination(ctx context.Context, db *harmonydb.DB, client *ethclient.Client, term termination) error {
	// Retrieve the tx_receipt from message_waits_eth
	var success bool
	err := db.QueryRow(ctx, `
        SELECT tx_success
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
        AND tx_success IS NOT NULL
    `, term.TxHash).Scan(&success)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", term.TxHash, err)
	}

	if !success {
		return xerrors.Errorf("tx %s failed", term.TxHash)
	}

	// Verify termination
	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(sAddr, client)
	if err != nil {
		return xerrors.Errorf("failed to instantiate FWSS service state view: %w", err)
	}

	ds, err := fwssv.GetDataSet(&bind.CallOpts{Context: ctx}, big.NewInt(term.DataSetId))
	if err != nil {
		return xerrors.Errorf("failed to get data set %d: %w", term.DataSetId, err)
	}

	if ds.PdpEndEpoch.Int64() == 0 {
		// Huston! we have a serious problem
		return xerrors.Errorf("data set %d has no termination epoch", term.DataSetId)
	}

	n, err := db.Exec(ctx, `UPDATE pdp_delete_data_set SET termination_epoch = $1 WHERE id = $2`, ds.PdpEndEpoch.Int64(), term.DataSetId)
	if err != nil {
		return xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
	}

	if n != 1 {
		return xerrors.Errorf("expected to update 1 row, updated %d", n)
	}

	log.Infof("Successfully confirmed data set %d termination", term.DataSetId)

	return nil
}
