package pdp

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type DataSetCreate struct {
	CreateMessageHash string `db:"tx_hash"`
	ID                string `db:"id"`
	Client            string `db:"client"`
}

func NewWatcherDataSetCreate(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDataSetCreates(ctx, db, ethClient)
		if err != nil {
			log.Errorf("Failed to process pending data set creates: %s", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingDataSetCreates(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Query for pdp_data_set_create entries tx_hash is NOT NULL
	var dataSetCreates []DataSetCreate

	err := db.Select(ctx, &dataSetCreates, `
        SELECT id, client, tx_hash 
        FROM pdp_data_set_create
        WHERE tx_hash IS NOT NULL`)
	if err != nil {
		return xerrors.Errorf("failed to select data set creates: %w", err)
	}

	if len(dataSetCreates) == 0 {
		// No pending data set creates
		return nil
	}

	// Process each data set create
	for _, dsc := range dataSetCreates {
		err := processDataSetCreate(ctx, db, dsc, ethClient)
		if err != nil {
			log.Errorf("Failed to process data set create for tx %s: %s", dsc.CreateMessageHash, err)
			continue
		}
	}

	return nil
}

func processDataSetCreate(ctx context.Context, db *harmonydb.DB, dsc DataSetCreate, ethClient *ethclient.Client) error {
	// Retrieve the tx_receipt from message_waits_eth
	var txReceiptJSON []byte
	var txSuccess bool
	err := db.QueryRow(ctx, `SELECT tx_receipt, tx_success FROM message_waits_eth 
                              WHERE signed_tx_hash = $1 
                                 AND tx_success IS NOT NULL
                                 AND tx_receipt IS NOT NULL`, dsc.CreateMessageHash).Scan(&txReceiptJSON, &txSuccess)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return xerrors.Errorf("tx hash %s is either missing from watch table or is not yet processed by watcher", dsc.CreateMessageHash)
		}
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", dsc.CreateMessageHash, err)
	}

	// Unmarshal the tx_receipt JSON into types.Receipt
	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", dsc.CreateMessageHash, err)
	}

	// Exit early if transaction executed with failure
	if !txSuccess {
		// This means msg failed, we should let the user know
		// TODO: Review if error would be in receipt
		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			n, err := tx.Exec(`UPDATE market_mk20_deal
									SET pdp_v1 = jsonb_set(
													jsonb_set(pdp_v1, '{error}', to_jsonb($1::text), true),
													'{complete}', to_jsonb(true), true
												 )
									WHERE id = $2;`, "Transaction failed", dsc.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
			}
			_, err = tx.Exec(`DELETE FROM pdp_data_set_create WHERE id = $1`, dsc.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete pdp_data_set_create: %w", err)
			}
			return true, nil
		})
		if err != nil {
			return xerrors.Errorf("failed to commit transaction: %w", err)
		}
		if !comm {
			return xerrors.Errorf("failed to commit transaction")
		}
		return nil
	}

	// Parse the logs to extract the dataSetId
	dataSetId, err := extractDataSetIdFromReceipt(&txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to extract dataSetId from receipt for tx %s: %w", dsc.CreateMessageHash, err)
	}

	// Get the listener address for this data set from the PDPVerifier contract
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	listenerAddr, err := pdpVerifier.GetDataSetListener(nil, big.NewInt(int64(dataSetId)))
	if err != nil {
		return xerrors.Errorf("failed to get listener address for data set %d: %w", dataSetId, err)
	}

	// Get the proving period from the listener
	// Assumption: listener is a PDP Service with proving window informational methods
	provingPeriod, challengeWindow, err := getProvingPeriodChallengeWindow(ctx, ethClient, listenerAddr)
	if err != nil {
		return xerrors.Errorf("failed to get max proving period: %w", err)
	}

	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`INSERT INTO pdp_data_set (id, client, proving_period, challenge_window, create_deal_id, create_message_hash) 
								VALUES ($1, $2, $3, $4, $5, $6)`, dataSetId, dsc.Client, provingPeriod, challengeWindow, dsc.ID, dsc.CreateMessageHash)
		if err != nil {
			return false, xerrors.Errorf("failed to insert pdp_data_set_create: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected 1 row to be inserted, got %d", n)
		}

		n, err = tx.Exec(`UPDATE market_mk20_deal
							SET pdp_v1 = jsonb_set(pdp_v1, '{complete}', 'true'::jsonb, true)
							WHERE id = $1;`, dsc.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
		}

		_, err = tx.Exec(`DELETE FROM pdp_data_set_create WHERE id = $1`, dsc.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to delete pdp_data_set_create: %w", err)
		}
		return true, nil
	})
	if err != nil {
		return xerrors.Errorf("failed to commit transaction: %w", err)
	}
	if !comm {
		return xerrors.Errorf("failed to commit transaction")
	}

	return nil
}

func extractDataSetIdFromReceipt(receipt *types.Receipt) (uint64, error) {
	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return 0, xerrors.Errorf("failed to get PDP ABI: %w", err)
	}

	event, exists := pdpABI.Events["DataSetCreated"]
	if !exists {
		return 0, xerrors.Errorf("DataSetCreated event not found in ABI")
	}

	for _, vLog := range receipt.Logs {
		if len(vLog.Topics) > 0 && vLog.Topics[0] == event.ID {
			if len(vLog.Topics) < 2 {
				return 0, xerrors.Errorf("log does not contain setId topic")
			}

			setIdBigInt := new(big.Int).SetBytes(vLog.Topics[1].Bytes())
			return setIdBigInt.Uint64(), nil
		}
	}

	return 0, xerrors.Errorf("DataSetCreated event not found in receipt")
}

func getProvingPeriodChallengeWindow(ctx context.Context, ethClient *ethclient.Client, listenerAddr common.Address) (uint64, uint64, error) {
	// ProvingPeriod
	schedule, err := contract.NewIPDPProvingSchedule(listenerAddr, ethClient)
	if err != nil {
		return 0, 0, xerrors.Errorf("failed to create proving schedule binding, check that listener has proving schedule methods: %w", err)
	}

	period, err := schedule.GetMaxProvingPeriod(&bind.CallOpts{Context: ctx})
	if err != nil {
		return 0, 0, xerrors.Errorf("failed to get proving period: %w", err)
	}

	// ChallengeWindow
	challengeWindow, err := schedule.ChallengeWindow(&bind.CallOpts{Context: ctx})
	if err != nil {
		return 0, 0, xerrors.Errorf("failed to get challenge window: %w", err)
	}

	return period, challengeWindow.Uint64(), nil
}
