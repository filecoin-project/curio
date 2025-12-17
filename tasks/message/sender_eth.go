package message

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/multierr"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/promise"
)

type SenderETH struct {
	client api.EthClientInterface

	sendTask *SendTaskETH

	db *harmonydb.DB
}

type SendTaskETH struct {
	sendTF promise.Promise[harmonytask.AddTaskFunc]

	client api.EthClientInterface

	db *harmonydb.DB
}

func (s *SendTaskETH) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.TODO()

	// Get transaction from the database
	var dbTx struct {
		FromAddress  string         `db:"from_address"`
		ToAddress    string         `db:"to_address"`
		UnsignedTx   []byte         `db:"unsigned_tx"`
		UnsignedHash string         `db:"unsigned_hash"`
		Nonce        sql.NullInt64  `db:"nonce"`
		SignedTx     []byte         `db:"signed_tx"`
		SendSuccess  sql.NullBool   `db:"send_success"`
		SendError    sql.NullString `db:"send_error"`
	}

	err = s.db.QueryRow(ctx,
		`SELECT from_address, to_address, unsigned_tx, unsigned_hash, nonce, signed_tx, send_success, send_error
         FROM message_sends_eth
         WHERE send_task_id = $1`, taskID).Scan(
		&dbTx.FromAddress, &dbTx.ToAddress, &dbTx.UnsignedTx, &dbTx.UnsignedHash, &dbTx.Nonce, &dbTx.SignedTx, &dbTx.SendSuccess, &dbTx.SendError)
	if err != nil {
		return false, xerrors.Errorf("getting transaction from db: %w", err)
	}

	// Deserialize the unsigned transaction
	tx := new(types.Transaction)
	err = tx.UnmarshalBinary(dbTx.UnsignedTx)
	if err != nil {
		return false, xerrors.Errorf("unmarshaling unsigned transaction: %w", err)
	}

	fromAddress := common.HexToAddress(dbTx.FromAddress)

	// Acquire lock on from_address
	for {
		if !stillOwned() {
			return false, xerrors.Errorf("lost ownership of task")
		}

		// Try to acquire lock
		cn, err := s.db.Exec(ctx,
			`INSERT INTO message_send_eth_locks (from_address, task_id, claimed_at)
             VALUES ($1, $2, CURRENT_TIMESTAMP)
             ON CONFLICT (from_address) DO UPDATE
             SET task_id = EXCLUDED.task_id, claimed_at = CURRENT_TIMESTAMP
             WHERE message_send_eth_locks.task_id = $2`, dbTx.FromAddress, taskID)
		if err != nil {
			return false, xerrors.Errorf("acquiring send lock: %w", err)
		}

		if cn == 1 {
			// Acquired the lock
			break
		}

		// Wait and retry
		log.Infow("waiting for send lock", "task_id", taskID, "from", dbTx.FromAddress)
		time.Sleep(SendLockedWait)
	}

	// Defer release of the lock
	defer func() {
		_, err2 := s.db.Exec(ctx,
			`DELETE FROM message_send_eth_locks WHERE from_address = $1 AND task_id = $2`, dbTx.FromAddress, taskID)
		if err2 != nil {
			log.Errorw("releasing send lock", "task_id", taskID, "from", dbTx.FromAddress, "error", err2)

			// Ensure the task is retried
			done = false
			err = multierr.Append(err, xerrors.Errorf("releasing send lock: %w", err2))
		}
	}()

	var signedTx *types.Transaction

	if !dbTx.Nonce.Valid {
		// Get the latest nonce
		pendingNonce, err := s.client.PendingNonceAt(ctx, fromAddress)
		if err != nil {
			return false, xerrors.Errorf("getting pending nonce: %w", err)
		}

		// Get max nonce from successful transactions in DB
		var dbNonce *uint64
		err = s.db.QueryRow(ctx,
			`SELECT MAX(nonce) FROM message_sends_eth WHERE from_address = $1 AND send_success = TRUE`, dbTx.FromAddress).Scan(&dbNonce)
		if err != nil {
			return false, xerrors.Errorf("getting max nonce from db: %w", err)
		}

		assignedNonce := pendingNonce
		if dbNonce != nil && *dbNonce+1 > pendingNonce {
			assignedNonce = *dbNonce + 1
		}

		// Update the transaction with the assigned nonce
		tx = types.NewTransaction(assignedNonce, *tx.To(), tx.Value(), tx.Gas(), tx.GasPrice(), tx.Data())

		// Sign the transaction
		signedTx, err = s.signTransaction(ctx, fromAddress, tx)
		if err != nil {
			return false, xerrors.Errorf("signing transaction: %w", err)
		}

		// Serialize the signed transaction
		signedTxData, err := signedTx.MarshalBinary()
		if err != nil {
			return false, xerrors.Errorf("serializing signed transaction: %w", err)
		}

		// Update the database with nonce and signed transaction
		n, err := s.db.Exec(ctx,
			`UPDATE message_sends_eth
             SET nonce = $1, signed_tx = $2, signed_hash = $3
             WHERE send_task_id = $4`, assignedNonce, signedTxData, signedTx.Hash().Hex(), taskID)
		if err != nil {
			return false, xerrors.Errorf("updating db record: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 row, updated %d", n)
		}
	} else {
		// Transaction was previously signed but possibly failed to send
		// Deserialize the signed transaction
		signedTx = new(types.Transaction)
		err = signedTx.UnmarshalBinary(dbTx.SignedTx)
		if err != nil {
			return false, xerrors.Errorf("unmarshaling signed transaction: %w", err)
		}
	}

	// Send the transaction
	err = s.client.SendTransaction(ctx, signedTx)

	// Persist send result
	var sendSuccess = err == nil
	var sendError string
	if err != nil {
		sendError = err.Error()
	}

	_, err = s.db.Exec(ctx,
		`UPDATE message_sends_eth
         SET send_success = $1, send_error = $2, send_time = CURRENT_TIMESTAMP
         WHERE send_task_id = $3`, sendSuccess, sendError, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating db record: %w", err)
	}

	return true, nil
}

func (s *SendTaskETH) signTransaction(ctx context.Context, fromAddress common.Address, tx *types.Transaction) (*types.Transaction, error) {
	// Fetch the private key from the database
	var privateKeyData []byte
	err := s.db.QueryRow(ctx,
		`SELECT private_key FROM eth_keys WHERE address = $1`, fromAddress.Hex()).Scan(&privateKeyData)
	if err != nil {
		return nil, xerrors.Errorf("fetching private key from db: %w", err)
	}

	privateKey, err := crypto.ToECDSA(privateKeyData)
	if err != nil {
		return nil, xerrors.Errorf("converting private key: %w", err)
	}

	// Get the chain ID
	chainID, err := s.client.NetworkID(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting network ID: %w", err)
	}

	// Sign the transaction
	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, privateKey)
	if err != nil {
		return nil, xerrors.Errorf("signing transaction: %w", err)
	}

	return signedTx, nil
}

func (s *SendTaskETH) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if len(ids) == 0 {
		// Should not happen
		return nil, nil
	}

	return &ids[0], nil
}

func (s *SendTaskETH) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1024),
		Name: "SendTransaction",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 1 << 20,
		},
		MaxFailures: 1000,
		Follows:     nil,
	}
}

func (s *SendTaskETH) Adder(taskFunc harmonytask.AddTaskFunc) {
	s.sendTF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &SendTaskETH{}
var _ = harmonytask.Reg(&SendTaskETH{})

// NewSenderETH creates a new SenderETH.
func NewSenderETH(client api.EthClientInterface, db *harmonydb.DB) (*SenderETH, *SendTaskETH) {
	st := &SendTaskETH{
		client: client,
		db:     db,
	}

	return &SenderETH{
		client:   client,
		db:       db,
		sendTask: st,
	}, st
}

// Send sends an Ethereum transaction, coordinating nonce assignment, signing, and broadcasting.
func (s *SenderETH) Send(ctx context.Context, fromAddress common.Address, tx *types.Transaction, reason string) (common.Hash, error) {
	// Ensure the transaction has zero nonce; it will be assigned during send task
	if tx.Nonce() != 0 {
		return common.Hash{}, xerrors.Errorf("Send expects transaction nonce to be 0, was %d", tx.Nonce())
	}

	if tx.Gas() == 0 {
		// Estimate gas limit
		msg := ethereum.CallMsg{
			From:  fromAddress,
			To:    tx.To(),
			Value: tx.Value(),
			Data:  tx.Data(),
		}

		gasLimit, err := s.client.EstimateGas(ctx, msg)
		if err != nil {
			return common.Hash{}, fmt.Errorf("failed to estimate gas: %w", err)
		}
		if gasLimit == 0 {
			return common.Hash{}, fmt.Errorf("estimated gas limit is zero")
		}

		// Fetch current base fee
		header, err := s.client.HeaderByNumber(ctx, nil)
		if err != nil {
			return common.Hash{}, fmt.Errorf("failed to get latest block header: %w", err)
		}

		baseFee := header.BaseFee
		if baseFee == nil {
			return common.Hash{}, fmt.Errorf("base fee not available; network might not support EIP-1559")
		}

		// Set GasTipCap (maxPriorityFeePerGas)
		gasTipCap, err := s.client.SuggestGasTipCap(ctx)
		if err != nil {
			return common.Hash{}, xerrors.Errorf("estimating gas premium: %w", err)
		}

		// Calculate GasFeeCap (maxFeePerGas)
		gasFeeCap := new(big.Int).Add(baseFee, gasTipCap)

		chainID, err := s.client.NetworkID(ctx)
		if err != nil {
			return common.Hash{}, xerrors.Errorf("getting network ID: %w", err)
		}

		// Create a new transaction with estimated gas limit and fee caps
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     0, // nonce will be set later
			GasFeeCap: gasFeeCap,
			GasTipCap: gasTipCap,
			Gas:       gasLimit,
			To:        tx.To(),
			Value:     tx.Value(),
			Data:      tx.Data(),
		})
	}

	// Serialize the unsigned transaction
	unsignedTxData, err := tx.MarshalBinary()
	if err != nil {
		return common.Hash{}, xerrors.Errorf("marshaling unsigned transaction: %w", err)
	}

	unsignedHash := tx.Hash().Hex()

	// Push the task
	taskAdder := s.sendTask.sendTF.Val(ctx)

	var sendTaskID *harmonytask.TaskID
	taskAdder(func(id harmonytask.TaskID, txdb *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		_, err := txdb.Exec(`INSERT INTO message_sends_eth (from_address, to_address, send_reason, unsigned_tx, unsigned_hash, send_task_id)
                             VALUES ($1, $2, $3, $4, $5, $6)`,
			fromAddress.Hex(), tx.To().Hex(), reason, unsignedTxData, unsignedHash, id)
		if err != nil {
			return false, xerrors.Errorf("inserting transaction into db: %w", err)
		}

		sendTaskID = &id

		return true, nil
	})

	if sendTaskID == nil {
		return common.Hash{}, xerrors.Errorf("failed to add task")
	}

	// Wait for execution
	var (
		pollInterval    = 50 * time.Millisecond
		pollIntervalMul = 2
		maxPollInterval = 5 * time.Second
		pollLoops       = 0

		signedHash common.Hash
		sendErr    error
	)

	for {
		var dbTx struct {
			SignedHash  sql.NullString `db:"signed_hash"`
			SendSuccess sql.NullBool   `db:"send_success"`
			SendError   sql.NullString `db:"send_error"`
		}

		err := s.db.QueryRow(ctx,
			`SELECT signed_hash, send_success, send_error FROM message_sends_eth WHERE send_task_id = $1`, sendTaskID).Scan(
			&dbTx.SignedHash, &dbTx.SendSuccess, &dbTx.SendError)
		if err != nil {
			return common.Hash{}, xerrors.Errorf("getting send status for task: %w", err)
		}

		if !dbTx.SendSuccess.Valid {
			time.Sleep(pollInterval)
			pollLoops++
			pollInterval *= time.Duration(pollIntervalMul)
			if pollInterval > maxPollInterval {
				pollInterval = maxPollInterval
			}
			continue
		}

		if !dbTx.SignedHash.Valid || !dbTx.SendError.Valid {
			return common.Hash{}, xerrors.Errorf("unexpected null values in send status")
		}

		if !dbTx.SendSuccess.Bool {
			sendErr = xerrors.Errorf("send error: %s", dbTx.SendError.String)
		} else {
			signedHash = common.HexToHash(dbTx.SignedHash.String)
		}

		break
	}

	log.Infow("sent transaction", "hash", signedHash, "task_id", sendTaskID, "send_error", sendErr, "poll_loops", pollLoops)

	return signedHash, sendErr
}
