package message

import (
	"context"
	"encoding/json"
	types2 "github.com/filecoin-project/lotus/chain/types"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/chainsched"
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

	for {
		select {
		case <-mw.stopping:
			// TODO: cleanup assignments
			return
		case <-mw.updateCh:
			mw.update()
		}
	}
}

func (mw *MessageWatcherEth) update() {
	ctx := context.Background()

	bestBlockNumber := mw.bestBlockNumber.Load()

	confirmedBlockNumber := new(big.Int).Sub(bestBlockNumber, big.NewInt(MinConfidence))
	if confirmedBlockNumber.Sign() < 0 {
		// Not enough blocks yet
		return
	}

	machineID := mw.ht.ResourcesAvailable().MachineID

	// Assign pending messages with null owner to ourselves
	{
		n, err := mw.db.Exec(ctx, `UPDATE message_waits_eth SET waiter_machine_id = $1 WHERE waiter_machine_id IS NULL AND tx_status = 'pending'`, machineID)
		if err != nil {
			log.Errorf("failed to assign pending transactions: %+v", err)
			return
		}
		if n > 0 {
			log.Debugw("assigned pending transactions to ourselves", "assigned", n)
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

	// Check if any of the transactions we have assigned are now confirmed
	for _, tx := range txs {
		txHash := common.HexToHash(tx.TxHash)

		receipt, err := mw.api.TransactionReceipt(ctx, txHash)
		if err != nil {
			if err.Error() == "not found" {
				// Transaction is still pending
				continue
			}
			log.Errorf("failed to get transaction receipt: %+v", err)
			return
		}

		if receipt == nil {
			// Transaction is still pending
			continue
		}

		// Check if the transaction has enough confirmations
		confirmations := new(big.Int).Sub(bestBlockNumber, receipt.BlockNumber)
		if confirmations.Cmp(big.NewInt(MinConfidence)) < 0 {
			// Not enough confirmations yet
			continue
		}

		// Get the transaction data
		txData, _, err := mw.api.TransactionByHash(ctx, txHash)
		if err != nil {
			log.Errorf("failed to get transaction by hash: %+v", err)
			return
		}

		txDataJSON, err := json.Marshal(txData)
		if err != nil {
			log.Errorf("failed to marshal transaction data: %+v", err)
			return
		}

		receiptJSON, err := json.Marshal(receipt)
		if err != nil {
			log.Errorf("failed to marshal receipt data: %+v", err)
			return
		}

		// Update the database
		_, err = mw.db.Exec(ctx, `UPDATE message_waits_eth SET
            waiter_machine_id = NULL,
            confirmed_block_number = $1,
            confirmed_tx_hash = $2,
            confirmed_tx_data = $3,
            tx_status = $4,
            tx_receipt = $5
            WHERE signed_tx_hash = $6`,
			receipt.BlockNumber.Int64(),
			receipt.TxHash.Hex(),
			txDataJSON,
			"confirmed",
			receiptJSON,
			tx.TxHash,
		)
		if err != nil {
			log.Errorf("failed to update message wait: %+v", err)
			return
		}
	}
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
		mw.bestBlockNumber.Store(big.NewInt(int64(apply.Height())))
		select {
		case mw.updateCh <- struct{}{}:
		default:
		}
	}
	return nil
}
