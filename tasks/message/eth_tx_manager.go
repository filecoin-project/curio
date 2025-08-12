package message

import (
	"context"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// EthTransactionManager provides a simple interface for managing Ethereum transactions in the database
type EthTransactionManager interface {
	// AssignPendingToMachine assigns pending transactions to a machine for processing
	AssignPendingToMachine(ctx context.Context, machineID int64) (int, error)

	// GetPendingForMachine gets all pending transactions assigned to a machine
	GetPendingForMachine(ctx context.Context, machineID int64) ([]string, error)

	// UpdateToConfirmed updates a transaction to confirmed status with all the details
	UpdateToConfirmed(ctx context.Context, signedTxHash string, blockNumber int64, confirmedTxHash string, txData []byte, receipt []byte, success bool) error
}

// HarmonyEthTxManager is the real implementation using HarmonyDB
type HarmonyEthTxManager struct {
	db *harmonydb.DB
}

// NewHarmonyEthTxManager creates a new HarmonyEthTxManager
func NewHarmonyEthTxManager(db *harmonydb.DB) *HarmonyEthTxManager {
	return &HarmonyEthTxManager{db: db}
}

// AssignPendingToMachine assigns pending transactions to a machine for processing
func (h *HarmonyEthTxManager) AssignPendingToMachine(ctx context.Context, machineID int64) (int, error) {
	return h.db.Exec(ctx, `UPDATE message_waits_eth SET waiter_machine_id = $1 WHERE waiter_machine_id IS NULL AND tx_status = 'pending'`, machineID)
}

// GetPendingForMachine gets all pending transactions assigned to a machine
func (h *HarmonyEthTxManager) GetPendingForMachine(ctx context.Context, machineID int64) ([]string, error) {
	var txs []struct {
		TxHash string `db:"signed_tx_hash"`
	}

	err := h.db.Select(ctx, &txs, `SELECT signed_tx_hash FROM message_waits_eth WHERE waiter_machine_id = $1 AND tx_status = 'pending' LIMIT 10000`, machineID)
	if err != nil {
		return nil, err
	}

	// Convert to simple string slice
	result := make([]string, len(txs))
	for i, tx := range txs {
		result[i] = tx.TxHash
	}

	return result, nil
}

// UpdateToConfirmed updates a transaction to confirmed status with all the details
func (h *HarmonyEthTxManager) UpdateToConfirmed(ctx context.Context, signedTxHash string, blockNumber int64, confirmedTxHash string, txData []byte, receipt []byte, success bool) error {
	_, err := h.db.Exec(ctx, `UPDATE message_waits_eth SET
		waiter_machine_id = NULL,
		confirmed_block_number = $1,
		confirmed_tx_hash = $2,
		confirmed_tx_data = $3,
		tx_status = 'confirmed',
		tx_receipt = $4,
		tx_success = $5
		WHERE signed_tx_hash = $6`,
		blockNumber,
		confirmedTxHash,
		txData,
		receipt,
		success,
		signedTxHash,
	)
	return err
}
