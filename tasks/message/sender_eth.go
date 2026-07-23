package message

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/multierr"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/promise"

	"github.com/filecoin-project/lotus/build/buildconstants"
)

type SenderETH struct {
	client ethchain.EthClient

	sendTask *SendTaskETH

	db *harmonydb.DB
}

type SendTaskETH struct {
	sendTF promise.Promise[harmonytask.AddTaskFunc]

	client ethchain.EthClient

	db *harmonydb.DB
}

func (s *SendTaskETH) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

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

	if dbTx.SendSuccess.Valid {
		return true, nil
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
		log.Debugw("waiting for send lock", "task_id", taskID, "from", dbTx.FromAddress)
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

	// Set a timeout on the eth transaction
	ethCtx, cancel := context.WithTimeout(ctx, defaultEthCallTimeout)
	defer cancel()

	var signedTx *types.Transaction

	if !dbTx.Nonce.Valid {
		// Get the latest nonce
		pendingNonce, err := s.client.PendingNonceAt(ethCtx, fromAddress)
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
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:    tx.ChainId(),
			Nonce:      assignedNonce,
			GasTipCap:  tx.GasTipCap(),
			GasFeeCap:  tx.GasFeeCap(),
			Gas:        tx.Gas(),
			To:         tx.To(),
			Value:      tx.Value(),
			Data:       tx.Data(),
			AccessList: tx.AccessList(),
		})

		// Sign the transaction
		signedTx, err = s.signTransaction(ethCtx, fromAddress, tx)
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
	sendErr := s.client.SendTransaction(ethCtx, signedTx)
	sendResult := classifyEthSendError(sendErr)
	if sendResult == ethSendUnknown {
		known, lookupErr := s.checkInTransactionPool(signedTx.Hash())
		if lookupErr != nil {
			log.Warnw("eth transaction send state unknown; failed to look up transaction before retry",
				"task_id", taskID,
				"from", dbTx.FromAddress,
				"nonce", signedTx.Nonce(),
				"hash", signedTx.Hash().Hex(),
				"send_error", sendErr,
				"lookup_error", lookupErr)
			return false, multierr.Combine(
				xerrors.Errorf("eth transaction send state unknown for %s: %w", signedTx.Hash().Hex(), sendErr),
				xerrors.Errorf("looking up eth transaction %s: %w", signedTx.Hash().Hex(), lookupErr),
			)
		}
		if !known {
			log.Warnw("eth transaction send state unknown; leaving signed transaction for retry",
				"task_id", taskID,
				"from", dbTx.FromAddress,
				"nonce", signedTx.Nonce(),
				"hash", signedTx.Hash().Hex(),
				"error", sendErr)
			return false, xerrors.Errorf("eth transaction send state unknown for %s: %w", signedTx.Hash().Hex(), sendErr)
		}

		log.Infow("eth transaction found after ambiguous send error",
			"task_id", taskID,
			"from", dbTx.FromAddress,
			"nonce", signedTx.Nonce(),
			"hash", signedTx.Hash().Hex(),
			"error", sendErr)
		sendResult = ethSendAccepted
	}

	sendSuccess := sendResult == ethSendAccepted
	sendError := ""
	if sendResult == ethSendDefinitiveError {
		sendError = sendErr.Error()
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

// checkInTransactionPool returns true when Lotus can resolve the exact signed
// tx hash, either as pending or already mined.
func (s *SendTaskETH) checkInTransactionPool(hash common.Hash) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultEthCallTimeout)
	defer cancel()

	tx, pending, err := s.client.TransactionByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, nil
		}
		return false, err
	}

	if pending || tx != nil {
		return true, nil
	}

	return false, nil
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

func (s *SendTaskETH) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if len(ids) == 0 {
		// Should not happen
		return nil, nil
	}

	return ids, nil
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
		RetryWait:   taskhelp.RetryWaitLinear(time.Second, 2*time.Second),
	}
}

func (s *SendTaskETH) Adder(taskFunc harmonytask.AddTaskFunc) {
	s.sendTF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &SendTaskETH{}
var _ = harmonytask.Reg(&SendTaskETH{})

// NewSenderETH creates a new SenderETH.
func NewSenderETH(client ethchain.EthClient, db *harmonydb.DB) (*SenderETH, *SendTaskETH) {
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
	return s.send(ctx, fromAddress, tx, reason, 1.0)
}

// SendWithGasOverestimate is like Send, but multiplies the estimated gas
// limit by overestimate before submission (capped at the block gas limit).
func (s *SenderETH) SendWithGasOverestimate(ctx context.Context, fromAddress common.Address, tx *types.Transaction, reason string, overestimate float64) (common.Hash, error) {
	return s.send(ctx, fromAddress, tx, reason, overestimate)
}

func (s *SenderETH) send(ctx context.Context, fromAddress common.Address, tx *types.Transaction, reason string, overestimate float64) (common.Hash, error) {
	// Ensure the transaction has zero nonce; it will be assigned during send task
	if tx.Nonce() != 0 {
		return common.Hash{}, xerrors.Errorf("Send expects transaction nonce to be 0, was %d", tx.Nonce())
	}

	if tx.Value() == nil {
		return common.Hash{}, xerrors.Errorf("Send expects transaction value to be non-nil")
	}

	if tx.Gas() == 0 {
		// Estimate gas limit
		msg := ethereum.CallMsg{
			From:  fromAddress,
			To:    tx.To(),
			Value: tx.Value(),
			Data:  tx.Data(),
		}

		ethCtx, cancel := context.WithTimeout(ctx, defaultEthCallTimeout)
		gasLimit, err := s.client.EstimateGas(ethCtx, msg)
		cancel()
		if err != nil {
			return common.Hash{}, fmt.Errorf("failed to estimate gas: %w", err)
		}
		if gasLimit == 0 {
			return common.Hash{}, fmt.Errorf("estimated gas limit is zero")
		}

		gasLimit = min(uint64(float64(gasLimit)*overestimate), uint64(buildconstants.BlockGasLimit))

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

type ethSendResult int

const (
	ethSendAccepted ethSendResult = iota
	ethSendDefinitiveError
	ethSendUnknown
)

// classifyEthSendError classifies only source-backed outcomes.
//
// Sources:
//   - Lotus node/impl/eth/send.go, ethSendRawTransaction: raw tx parse, tx hash,
//     and Filecoin message conversion all happen before MpoolPush/MpoolPushUntrusted.
//   - Lotus chain/messagepool/messagepool.go, MessagePool.Push/addTs/addLocked:
//     validation happens before addLocked, while duplicate-same-message is reported
//     from addLocked as ErrExistingNonce.
//
// Transport, context, JSON-RPC, storage, publish, soft-validation, and nonce-low
// errors are left unknown unless listed below. Those errors can happen after a
// previous send attempt may already have been accepted or landed.
func classifyEthSendError(err error) ethSendResult {
	if err == nil {
		return ethSendAccepted
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ethSendUnknown
	}

	msg := strings.ToLower(err.Error())
	if ethSendErrorContainsAny(msg, ethSendAmbiguousErrors) {
		return ethSendUnknown
	}
	if ethSendErrorContainsAny(msg, ethSendDefinitiveErrors) {
		return ethSendDefinitiveError
	}

	return ethSendUnknown
}

func ethSendErrorContainsAny(msg string, parts []string) bool {
	for _, part := range parts {
		if strings.Contains(msg, part) {
			return true
		}
	}
	return false
}

// Source: Lotus chain/messagepool/messagepool.go, ErrExistingNonce.
// The same signed Filecoin message is already in the local mpool.
var errMessageWithNonceExists = "message with nonce already exists"

var ethSendAmbiguousErrors = []string{
	errMessageWithNonceExists,
}

var ethSendDefinitiveErrors = []string{
	// Source: Lotus chain/types/ethtypes/eth_transactions.go, ParseEthTransaction.
	// ethSendRawTransaction returns these before calling MpoolPush.
	"empty data",
	"eip-2930 transaction is not supported",
	"unsupported transaction type",
	"failed to parse legacy transaction",

	// Source: Lotus chain/types/ethtypes/eth_1559_transactions.go, parseEip1559Tx,
	// and chain/types/ethtypes/rlp.go, DecodeRLP.
	"invalid rlp data",
	"not an eip-1559 transaction",
	"access list should be an empty list",
	"eip-1559 transactions only support 0 or 1 for v",
	"cannot parse interface to int",
	"cannot parse interface to ethuint64",
	"cannot parse interface to big.int",
	"cannot parse interface into bytes",
	"cannot parse bytes into an ethaddress",

	// Source: Lotus chain/types/ethtypes/eth_transactions.go,
	// ToSignedFilecoinMessage. These happen before MpoolPush.
	"failed to calculate sender",
	"failed to convert to unsigned msg",
	"failed to calculate signature",
	"signature is not 65 bytes",

	// Source: Lotus chain/types/ethtypes/eth_1559_transactions.go,
	// Eth1559TxArgs.ToUnsignedFilecoinMessage, and
	// chain/types/ethtypes/eth_types.go, EthAddress.ToFilecoinAddress.
	"invalid chain id",
	"failed to write input args",
	"failed to convert ethaddress to filecoin address",
	"cannot get filecoin address",
	"expected a class 4 address",

	// Source: Lotus chain/types/ethtypes/eth_transactions.go and
	// eth_legacy_155_transactions.go, legacy transaction parsing.
	"unsupported legacy transaction",
	"not a legacy eth transaction",
	"not a legacy transaction",
	"legacy homestead transactions only support",
	"failed to validate eip155 chain id",

	// Source: Lotus node/impl/full/mpool.go, sanityCheckOutgoingMessage.
	// MpoolPush/MpoolPushUntrusted run this before MessagePool.Push.
	"delegated address but not a valid eth address",

	// Source: Lotus chain/messagepool/messagepool.go, checkMessage.
	// MessagePool.Push runs this before addTs/addLocked.
	"message too big",
	"mpool message too large",
	"message not valid for block inclusion",
	"message had invalid to address",
	"cannot send more filecoin than will ever exist",
	"gas fee cap too low",
	"signature verification failed",
	"failed to validate signature",

	// Source: Lotus chain/messagepool/messagepool.go, addTs/checkBalance.
	// These are hard rejects before addLocked inserts the message into the mpool.
	"not enough funds (required:",
	"not enough funds to execute transaction",
	"is not a valid top-level sender",
	"network version should be at least",

	// Source: Lotus chain/messagepool/messagepool.go, msgSet.add.
	// These reject the candidate instead of adding it as pending.
	"replace by fee has too low gaspremium",
	"too many pending messages for actor",
	"unfulfilled nonce gap",

	// Source: Lotus node/impl/eth/send.go, EthSendDisabled, and
	// node/modules/eth.go, GatewayEthSend.
	"ethsendrawtransactionuntrusted is not supported",
	"module disabled",
}
