package pdp

import (
	"context"
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract"
)

type DataSetCreate struct {
	CreateMessageHash string `db:"create_message_hash"`
	Service           string `db:"service"`
}

// processPendingDataSetCreates finalises data set creation best on transactions logs
// it is called from proofset_watch.go
func processPendingDataSetCreates(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Query for pdp_data_set_creates entries where ok = TRUE and data_set_created = FALSE
	var dataSetCreates []DataSetCreate

	err := db.Select(ctx, &dataSetCreates, `
        SELECT create_message_hash, service
        FROM pdp_data_set_creates
        WHERE ok = TRUE AND data_set_created = FALSE
    `)
	if err != nil {
		return xerrors.Errorf("failed to select data set creates: %w", err)
	}

	if len(dataSetCreates) == 0 {
		// No pending data set creates
		return nil
	}

	log.Infow("DataSetCreate watcher has pending data sets", "count", len(dataSetCreates))

	// Process each data set create
	for _, psc := range dataSetCreates {
		log.Infow("Processing data set create",
			"txHash", psc.CreateMessageHash,
			"service", psc.Service)
		err := processDataSetCreate(ctx, db, psc, ethClient)
		if err != nil {
			log.Warnf("Failed to process data set create for tx %s: %v", psc.CreateMessageHash, err)
			continue
		}
		log.Infow("Successfully processed data set create", "txHash", psc.CreateMessageHash)
	}

	return nil
}

func processDataSetCreate(ctx context.Context, db *harmonydb.DB, psc DataSetCreate, ethClient *ethclient.Client) error {
	// Retrieve the tx_receipt from message_waits_eth
	var txReceiptJSON []byte
	log.Debugw("Fetching tx_receipt from message_waits_eth", "txHash", psc.CreateMessageHash)
	err := db.QueryRow(ctx, `
        SELECT tx_receipt
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
    `, psc.CreateMessageHash).Scan(&txReceiptJSON)
	if err != nil {
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", psc.CreateMessageHash, err)
	}
	log.Debugw("Retrieved tx_receipt", "txHash", psc.CreateMessageHash, "receiptLength", len(txReceiptJSON))

	// Unmarshal the tx_receipt JSON into types.Receipt
	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", psc.CreateMessageHash, err)
	}
	log.Debugw("Unmarshalled receipt", "txHash", psc.CreateMessageHash, "status", txReceipt.Status, "logs", len(txReceipt.Logs))

	// Parse the logs to extract the dataSetId
	dataSetId, err := extractDataSetIdFromReceipt(&txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to extract dataSetId from receipt for tx %s: %w", psc.CreateMessageHash, err)
	}
	log.Infow("Extracted dataSetId from receipt", "txHash", psc.CreateMessageHash, "dataSetId", dataSetId)

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
	_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert a new entry into pdp_data_sets
		_, err = tx.Exec(`
        INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window)
        VALUES ($1, $2, $3, $4, $5)
    `, dataSetId, psc.CreateMessageHash, psc.Service, provingPeriod, challengeWindow)
		if err != nil {
			return false, xerrors.Errorf("failed to insert data set %d for tx %+v: %w", dataSetId, psc, err)
		}

		// Update pdp_data_set_creates to set data_set_created = TRUE
		_, err = db.Exec(ctx, `
        UPDATE pdp_data_set_creates
        SET data_set_created = TRUE
        WHERE create_message_hash = $1
        	AND data_set_created = FALSE;
    `, psc.CreateMessageHash)
		if err != nil {
			return false, xerrors.Errorf("failed to update data_set_creates for tx %s: %w", psc.CreateMessageHash, err)
		}
		return true, nil
	})

	if err != nil {
		return xerrors.Errorf("failed to create data set %d for tx %+v: %w", dataSetId, psc, err)
	}

	return nil
}

func extractDataSetIdFromReceipt(receipt *types.Receipt) (int64, error) {
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
			if !setIdBigInt.IsInt64() {
				return 0, xerrors.Errorf("setId is not an int64")
			}
			return setIdBigInt.Int64(), nil
		}
	}

	return 0, xerrors.Errorf("DataSetCreated event not found in receipt")
}

func getProvingPeriodChallengeWindow(ctx context.Context, ethClient *ethclient.Client, listenerAddr common.Address) (uint64, uint64, error) {
	// Get the proving schedule from the listener (handles view contract indirection)
	schedule, err := contract.GetProvingScheduleFromListener(listenerAddr, ethClient)
	if err != nil {
		return 0, 0, xerrors.Errorf("failed to get proving schedule from listener: %w", err)
	}

	config, err := schedule.GetPDPConfig(&bind.CallOpts{Context: ctx})
	if err != nil {
		return 0, 0, xerrors.Errorf("failed to GetPDPConfig: %w", err)
	}

	return config.MaxProvingPeriod, config.ChallengeWindow.Uint64(), nil
}
