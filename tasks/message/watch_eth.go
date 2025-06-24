package message

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/chainsched"

	types2 "github.com/filecoin-project/lotus/chain/types"
)

type MessageWatcherEth struct {
	db  *harmonydb.DB
	ht  *harmonytask.TaskEngine
	api *ethclient.Client

	stopping, stopped chan struct{}

	updateCh        chan struct{}
	bestBlockNumber atomic.Pointer[big.Int]
}

func NewMessageWatcherEth(db *harmonydb.DB, ht *harmonytask.TaskEngine, pcs *chainsched.CurioChainSched, api *ethclient.Client) (*MessageWatcherEth, error) {
	mw := &MessageWatcherEth{
		db:       db,
		ht:       ht,
		api:      api,
		stopping: make(chan struct{}),
		stopped:  make(chan struct{}),
		updateCh: make(chan struct{}, 1),
	}
	go mw.run()
	if err := pcs.AddHandler(mw.processHeadChange); err != nil {
		return nil, err
	}
	return mw, nil
}

func (mw *MessageWatcherEth) run() {
	defer close(mw.stopped)
	log.Infow("MessageWatcherEth started")

	for {
		select {
		case <-mw.stopping:
			log.Infow("MessageWatcherEth stopping")
			// TODO: cleanup assignments
			return
		case <-mw.updateCh:
			log.Debugw("MessageWatcherEth update triggered")
			mw.update()
		}
	}
}

func (mw *MessageWatcherEth) update() {
	ctx := context.Background()

	bestBlockNumber := mw.bestBlockNumber.Load()
	log.Debugw("MessageWatcherEth update starting", "bestBlockNumber", bestBlockNumber)

	confirmedBlockNumber := new(big.Int).Sub(bestBlockNumber, big.NewInt(MinConfidence))
	if confirmedBlockNumber.Sign() < 0 {
		// Not enough blocks yet
		log.Debugw("Not enough blocks for confirmations", "bestBlockNumber", bestBlockNumber, "minConfidence", MinConfidence)
		return
	}

	machineID := mw.ht.ResourcesAvailable().MachineID

	// Assign pending transactions with null owner to ourselves
	{
		n, err := mw.db.Exec(ctx, `UPDATE message_waits_eth SET waiter_machine_id = $1 WHERE waiter_machine_id IS NULL AND tx_status = 'pending'`, machineID)
		if err != nil {
			log.Errorf("failed to assign pending transactions: %+v", err)
			return
		}
		if n > 0 {
			log.Infow("Assigned orphaned pending transactions", "count", n, "machineID", machineID)
		}
	}

	// Get transactions assigned to us
	var txs []struct {
		TxHash string `db:"signed_tx_hash"`
	}

	err := mw.db.Select(ctx, &txs, `SELECT signed_tx_hash FROM message_waits_eth WHERE waiter_machine_id = $1 AND tx_status = 'pending' LIMIT 10000`, machineID)
	if err != nil {
		log.Errorf("failed to get assigned transactions: %+v", err)
		return
	}

	log.Infow("Processing pending transactions", "count", len(txs), "machineID", machineID)

	// Track statistics
	var processed, stillPending, waitingConfirmations, confirmed, errorCount int

	// Check if any of the transactions we have assigned are now confirmed
	for _, tx := range txs {
		txHash := common.HexToHash(tx.TxHash)
		log.Debugw("Checking transaction", "txHash", txHash.Hex())

		receipt, err := mw.api.TransactionReceipt(ctx, txHash)
		if err != nil {
			if errors.Is(err, ethereum.NotFound) {
				// Transaction is still pending
				stillPending++
				log.Debugw("Transaction still pending", "txHash", txHash.Hex())
				continue
			}
			errorCount++
			log.Errorw("Failed to get transaction receipt - continuing with next tx",
				"txHash", txHash.Hex(),
				"error", err,
				"errorType", fmt.Sprintf("%T", err))
			// Continue processing other transactions instead of returning
			continue
		}

		// Transaction receipt found
		log.Debugw("Transaction receipt found",
			"txHash", txHash.Hex(),
			"blockNumber", receipt.BlockNumber,
			"status", receipt.Status)

		// Check if the transaction has enough confirmations
		confirmations := new(big.Int).Sub(bestBlockNumber, receipt.BlockNumber)
		if confirmations.Cmp(big.NewInt(MinConfidence)) < 0 {
			// Not enough confirmations yet
			waitingConfirmations++
			log.Debugw("Transaction waiting for confirmations",
				"txHash", txHash.Hex(),
				"confirmations", confirmations,
				"required", MinConfidence,
				"blockNumber", receipt.BlockNumber)
			continue
		}

		// Get the transaction data
		txData, _, err := mw.api.TransactionByHash(ctx, txHash)
		if err != nil {
			if errors.Is(err, ethereum.NotFound) {
				errorCount++
				log.Errorw("Transaction data not found - continuing", "txHash", txHash.Hex())
				continue
			}
			errorCount++
			log.Errorw("Failed to get transaction by hash - continuing",
				"txHash", txHash.Hex(),
				"error", err)
			continue
		}

		txDataJSON, err := json.Marshal(txData)
		if err != nil {
			errorCount++
			log.Errorw("Failed to marshal transaction data - continuing",
				"txHash", txHash.Hex(),
				"error", err)
			continue
		}

		receiptJSON, err := json.Marshal(receipt)
		if err != nil {
			errorCount++
			log.Errorw("Failed to marshal receipt data - continuing",
				"txHash", txHash.Hex(),
				"error", err)
			continue
		}

		txStatus := "confirmed"
		txSuccess := receipt.Status == 1

		log.Infow("Updating transaction to confirmed",
			"txHash", txHash.Hex(),
			"success", txSuccess,
			"blockNumber", receipt.BlockNumber,
			"confirmations", confirmations)

		// Update the database
		_, err = mw.db.Exec(ctx, `UPDATE message_waits_eth SET
                waiter_machine_id = NULL,
                confirmed_block_number = $1,
                confirmed_tx_hash = $2,
                confirmed_tx_data = $3,
                tx_status = $4,
                tx_receipt = $5,
                tx_success = $6
                WHERE signed_tx_hash = $7`,
			receipt.BlockNumber.Int64(),
			receipt.TxHash.Hex(),
			txDataJSON,
			txStatus,
			receiptJSON,
			txSuccess,
			tx.TxHash,
		)
		if err != nil {
			errorCount++
			log.Errorw("Failed to update message wait - continuing",
				"txHash", txHash.Hex(),
				"error", err)
			continue
		}

		confirmed++
		processed++
		log.Infow("Successfully confirmed transaction", "txHash", txHash.Hex())
	}

	log.Infow("MessageWatcherEth update completed",
		"processed", processed,
		"confirmed", confirmed,
		"stillPending", stillPending,
		"waitingConfirmations", waitingConfirmations,
		"errors", errorCount,
		"total", len(txs))
}

func (mw *MessageWatcherEth) Stop(ctx context.Context) error {
	close(mw.stopping)
	select {
	case <-mw.stopped:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (mw *MessageWatcherEth) processHeadChange(ctx context.Context, revert, apply *types2.TipSet) error {
	if apply != nil {
		height := apply.Height()
		mw.bestBlockNumber.Store(big.NewInt(int64(height)))
		log.Debugw("Head change received", "newHeight", height)
		select {
		case mw.updateCh <- struct{}{}:
			log.Debugw("Update triggered by head change", "height", height)
		default:
			log.Debugw("Update channel full, skipping", "height", height)
		}
	}
	return nil
}
