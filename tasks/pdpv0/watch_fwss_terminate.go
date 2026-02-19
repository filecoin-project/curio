package pdpv0

import (
	"context"
	"database/sql"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

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

	var details []struct {
		DataSetId int64        `db:"id"`
		TxHash    string       `db:"terminate_tx_hash"`
		Success   sql.NullBool `db:"tx_success"`
	}

	err := db.Select(ctx, &details, `SELECT 
    										pdds.id, 
    										pdds.terminate_tx_hash,
    										mwe.tx_success
										FROM pdp_delete_data_set pdds
										LEFT JOIN message_waits_eth mwe ON mwe.signed_tx_hash = pdds.terminate_tx_hash
										WHERE pdds.service_termination_epoch IS NULL 
										  AND pdds.after_terminate_service = TRUE`)
	if err != nil {
		return xerrors.Errorf("failed to select pending data sets: %w", err)
	}

	if len(details) == 0 {
		return nil
	}

	sAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(sAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to get FWSS view address: %w", err)
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate FWSS service state view: %w", err)
	}

	for _, detail := range details {
		if !detail.Success.Valid {
			log.Debugw("filecoin warm storage service termination tx not yet mined", "txHash", detail.TxHash, "dataSetId", detail.DataSetId)
			continue
		}

		if !detail.Success.Bool {
			return xerrors.Errorf("filecoin warm storage service termination tx %s failed for data set %d", detail.TxHash, detail.DataSetId)
		}

		ds, err := fwssv.GetDataSet(&bind.CallOpts{Context: ctx}, big.NewInt(detail.DataSetId))
		if err != nil {
			return xerrors.Errorf("failed to get data set %d: %w", detail.DataSetId, err)
		}

		if ds.PdpEndEpoch.Int64() == 0 {
			// Huston! we have a serious problem
			return xerrors.Errorf("data set %d has no termination epoch", detail.DataSetId)
		}

		n, err := db.Exec(ctx, `UPDATE pdp_delete_data_set SET service_termination_epoch = $1 WHERE id = $2`, ds.PdpEndEpoch.Int64(), detail.DataSetId)
		if err != nil {
			return xerrors.Errorf("failed to update pdp_delete_data_set: %w", err)
		}

		if n != 1 {
			return xerrors.Errorf("expected to update 1 row, updated %d", n)
		}

		log.Infow("Successfully confirmed data set termination", "dataSetId", detail.DataSetId, "epoch", ds.PdpEndEpoch.Int64(), "txHash", detail.TxHash)
	}
	return nil
}
