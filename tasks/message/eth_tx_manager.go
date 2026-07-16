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
	GetPendingForMachine(ctx context.Context, machineID int64) ([]PendingEthTx, error)

	// UpdateToConfirmed updates a transaction to confirmed status with all the details
	UpdateToConfirmed(ctx context.Context, signedTxHash string, blockNumber int64, confirmedTxHash string, txData []byte, receipt []byte, success bool) error
}

type PendingEthTx struct {
	WaitHash   string `db:"wait_hash"`
	LookupHash string `db:"lookup_hash"`
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
func (h *HarmonyEthTxManager) GetPendingForMachine(ctx context.Context, machineID int64) ([]PendingEthTx, error) {
	var txs []PendingEthTx

	// Pending waits are keyed by the original caller-facing hash in message_waits_eth.
	// If that transaction was replaced, message_send_eth_replacements gives us the
	// latest successful replacement hash to query on chain while keeping the wait row
	// update anchored to the original hash.
	err := h.db.Select(ctx, &txs, `
		SELECT
			mwe.signed_tx_hash AS wait_hash,
			COALESCE(latest.signed_hash, mwe.signed_tx_hash) AS lookup_hash
		FROM message_waits_eth mwe
		LEFT JOIN LATERAL (
			SELECT r.signed_hash
			FROM message_send_eth_replacements r
			WHERE r.original_signed_hash = mwe.signed_tx_hash
				AND r.send_success = TRUE
				AND r.signed_hash IS NOT NULL
				AND NOT EXISTS (
					SELECT 1
					FROM message_send_eth_replacements next
					WHERE next.original_signed_hash = r.original_signed_hash
						AND next.send_success = TRUE
						AND next.replaces_signed_hash = r.signed_hash
				)
			LIMIT 1
		) latest ON TRUE
		WHERE mwe.waiter_machine_id = $1
			AND mwe.tx_status = 'pending'
		LIMIT 10000`, machineID)
	return txs, err
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
