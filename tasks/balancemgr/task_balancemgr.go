package balancemgr

import (
	"context"
	"fmt"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/curio/tasks/message"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("balancemgr")

var MaxSendFee = types.MustParseFIL("0.05 FIL")

type BalanceMgrTask struct {
	db     *harmonydb.DB
	chain  api.FullNode
	sender *message.Sender
	adder  promise.Promise[harmonytask.AddTaskFunc]
}

type balanceManagerAddress struct {
	ID                      int64
	SubjectAddress          address.Address
	SecondAddress           address.Address
	ActionType              string
	SubjectType             string
	LowWatermarkFilBalance  types.BigInt
	HighWatermarkFilBalance types.BigInt

	// chain data
	SubjectBalance types.BigInt
	SecondBalance  types.BigInt
}

func NewBalanceMgrTask(db *harmonydb.DB, chain api.FullNode, pcs *chainsched.CurioChainSched, sender *message.Sender) *BalanceMgrTask {
	t := &BalanceMgrTask{
		db:     db,
		chain:  chain,
		sender: sender,
	}

	err := pcs.AddHandler(func(ctx context.Context, revert, apply *types.TipSet) error {
		if !t.adder.IsSet() {
			return nil
		}

		taskFunc := t.adder.Val(ctx)

		// select all addrs, where active_task_id = null, last_msg_cid is null OR last_msg_landed_at is NOT null
		rows, err := t.db.Query(ctx, `
			SELECT id, subject_address, second_address, action_type, low_watermark_fil_balance, high_watermark_fil_balance, subject_type
			FROM balance_manager_addresses
			WHERE active_task_id IS NULL AND (last_msg_cid IS NULL OR last_msg_landed_at IS NOT NULL)
		`)
		if err != nil {
			return err
		}
		defer rows.Close()

		addresses := make([]*balanceManagerAddress, 0)

		for rows.Next() {
			var id int64
			var subjectAddressStr string
			var secondAddressStr string
			var actionType string
			var subjectType string
			var lowWatermarkFilBalanceStr string
			var highWatermarkFilBalanceStr string

			err = rows.Scan(&id, &subjectAddressStr, &secondAddressStr, &actionType, &lowWatermarkFilBalanceStr, &highWatermarkFilBalanceStr, &subjectType)
			if err != nil {
				return xerrors.Errorf("scanning balance manager address: %w", err)
			}

			subjectAddr, err := address.NewFromString(subjectAddressStr)
			if err != nil {
				return xerrors.Errorf("parsing subject address: %w", err)
			}

			secondAddr, err := address.NewFromString(secondAddressStr)
			if err != nil {
				return xerrors.Errorf("parsing second address: %w", err)
			}

			lowWatermark, err := types.ParseFIL(lowWatermarkFilBalanceStr)
			if err != nil {
				return xerrors.Errorf("parsing low watermark: %w", err)
			}

			highWatermark, err := types.ParseFIL(highWatermarkFilBalanceStr)
			if err != nil {
				return xerrors.Errorf("parsing high watermark: %w", err)
			}

			addr := &balanceManagerAddress{
				ID:                      id,
				SubjectAddress:          subjectAddr,
				SecondAddress:           secondAddr,
				ActionType:              actionType,
				SubjectType:             subjectType,
				LowWatermarkFilBalance:  abi.TokenAmount(lowWatermark),
				HighWatermarkFilBalance: abi.TokenAmount(highWatermark),
			}

			addresses = append(addresses, addr)
		}

		for _, addr := range addresses {
			// Only wallet-type rules are handled by on-chain sends here.
			switch addr.SubjectType {
			case "wallet":
				err = t.adderWallet(ctx, taskFunc, addr)
			case "proofshare":
				err = t.adderProofshare(ctx, taskFunc, addr)
			default:
				log.Warnw("unknown subject type", "type", addr.SubjectType)
				continue
			}

			if err != nil {
				log.Errorw("error considering task add", "error", err, "addr", addr)
			}
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	return t
}

func (b *BalanceMgrTask) adderProofshare(ctx context.Context, taskFunc harmonytask.AddTaskFunc, addr *balanceManagerAddress) error {
	svc := common.NewService(b.chain)
	idAddr, err := b.chain.StateLookupID(ctx, addr.SubjectAddress, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting address ID: %w", err)
	}

	addrID, err := address.IDFromAddress(idAddr)
	if err != nil {
		return xerrors.Errorf("getting address ID: %w", err)
	}

	clientState, err := svc.GetClientState(ctx, addrID)
	if err != nil {
		return xerrors.Errorf("PSClientWallets: failed to get client state: %w", err)
	}

	sourceBalance, err := b.chain.StateGetActor(ctx, addr.SubjectAddress, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting source balance: %w", err)
	}

	addr.SubjectBalance = types.BigInt(clientState.Balance)
	addr.SecondBalance = types.BigInt(sourceBalance.Balance)

	var shouldCreateTask bool
	switch addr.ActionType {
	case "requester":
		shouldCreateTask = addr.SubjectBalance.LessThan(addr.LowWatermarkFilBalance)
	}

	if shouldCreateTask {
		taskFunc(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			// check that address.ID has active_task_id = null, set the task ID, set last_ to null
			n, err := tx.Exec(`
				UPDATE balance_manager_addresses
				SET active_task_id = $1, last_msg_cid = NULL, last_msg_sent_at = NULL, last_msg_landed_at = NULL
				WHERE id = $2 AND active_task_id IS NULL AND (last_msg_cid IS NULL OR last_msg_landed_at IS NOT NULL)
			`, taskID, addr.ID)
			if err != nil {
				return false, xerrors.Errorf("updating balance manager address: %w", err)
			}

			return n > 0, nil
		})
	}
	return nil
}

func (b *BalanceMgrTask) adderWallet(ctx context.Context, taskFunc harmonytask.AddTaskFunc, addr *balanceManagerAddress) error {
	// get balances
	subjectBalance, err := b.chain.WalletBalance(ctx, addr.SubjectAddress)
	if err != nil {
		return xerrors.Errorf("getting subject balance: %w", err)
	}

	secondBalance, err := b.chain.WalletBalance(ctx, addr.SecondAddress)
	if err != nil {
		return xerrors.Errorf("getting second balance: %w", err)
	}

	addr.SubjectBalance = subjectBalance
	addr.SecondBalance = secondBalance

	var shouldCreateTask bool
	switch addr.ActionType {
	case "requester":
		shouldCreateTask = addr.SubjectBalance.LessThan(addr.LowWatermarkFilBalance)
	case "active-provider":
		shouldCreateTask = addr.SubjectBalance.GreaterThan(addr.HighWatermarkFilBalance)
	}

	if shouldCreateTask {
		taskFunc(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			// check that address.ID has active_task_id = null, set the task ID, set last_ to null
			n, err := tx.Exec(`
				UPDATE balance_manager_addresses
				SET active_task_id = $1, last_msg_cid = NULL, last_msg_sent_at = NULL, last_msg_landed_at = NULL
				WHERE id = $2 AND active_task_id IS NULL AND (last_msg_cid IS NULL OR last_msg_landed_at IS NOT NULL)
			`, taskID, addr.ID)
			if err != nil {
				return false, xerrors.Errorf("updating balance manager address: %w", err)
			}

			return n > 0, nil
		})
	}

	return nil
}

// Adder implements harmonytask.TaskInterface.
func (b *BalanceMgrTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	b.adder.Set(taskFunc)
}

// CanAccept implements harmonytask.TaskInterface.
func (b *BalanceMgrTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	for _, id := range ids {
		var subjectType string

		err := b.db.QueryRow(context.Background(), `
			SELECT subject_type
			FROM balance_manager_addresses
			WHERE active_task_id = $1
		`, id).Scan(&subjectType)
		if err != nil {
			return nil, xerrors.Errorf("getting subject type: %w", err)
		}

		if subjectType == "wallet" {
			return &id, nil
		}
	}

	return nil, nil
}

// Do implements harmonytask.TaskInterface.
func (b *BalanceMgrTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// select task info
	var id int64
	var subjectAddressStr string
	var secondAddressStr string
	var actionType string
	var subjectType string
	var lowWatermarkStr string
	var highWatermarkStr string

	err = b.db.QueryRow(ctx, `
		SELECT id, subject_address, second_address, action_type, 
		       low_watermark_fil_balance, high_watermark_fil_balance, subject_type
		FROM balance_manager_addresses
		WHERE active_task_id = $1
	`, taskID).Scan(&id, &subjectAddressStr, &secondAddressStr, &actionType,
		&lowWatermarkStr, &highWatermarkStr, &subjectType)
	if err != nil {
		return false, xerrors.Errorf("failed to get balance manager address: %w", err)
	}

	// Parse addresses and watermarks
	subjectAddr, err := address.NewFromString(subjectAddressStr)
	if err != nil {
		return false, xerrors.Errorf("parsing subject address: %w", err)
	}

	secondAddr, err := address.NewFromString(secondAddressStr)
	if err != nil {
		return false, xerrors.Errorf("parsing second address: %w", err)
	}

	lowWatermark, err := types.ParseFIL(lowWatermarkStr)
	if err != nil {
		return false, xerrors.Errorf("parsing low watermark: %w", err)
	}

	highWatermark, err := types.ParseFIL(highWatermarkStr)
	if err != nil {
		return false, xerrors.Errorf("parsing high watermark: %w", err)
	}

	addr := &balanceManagerAddress{
		ID:                      id,
		SubjectAddress:          subjectAddr,
		SecondAddress:           secondAddr,
		ActionType:              actionType,
		SubjectType:             subjectType,
		LowWatermarkFilBalance:  abi.TokenAmount(lowWatermark),
		HighWatermarkFilBalance: abi.TokenAmount(highWatermark),
	}

	switch addr.SubjectType {
	case "wallet":
		return b.doWallet(ctx, taskID, addr)
	case "proofshare":
		return b.doProofshare(ctx, taskID, addr)
	default:
		return false, xerrors.Errorf("unknown subject type: %s", addr.SubjectType)
	}
}

// doProofshare is a stub for proofshare subject rules.
func (b *BalanceMgrTask) doProofshare(ctx context.Context, taskID harmonytask.TaskID, addr *balanceManagerAddress) (bool, error) {
	log.Infow("balancemgr proofshare Do stub",
		"id", addr.ID,
		"subject", addr.SubjectAddress,
		"low", types.FIL(addr.LowWatermarkFilBalance),
		"high", types.FIL(addr.HighWatermarkFilBalance))

	// Clear the task and return done.
	_, err := b.db.Exec(ctx, `
		UPDATE balance_manager_addresses
		SET active_task_id = NULL, last_action = NOW()
		WHERE id = $1
	`, addr.ID)
	if err != nil {
		return false, xerrors.Errorf("proofshare Do: clearing task id: %w", err)
	}
	return true, nil
}

// doWallet handles wallet subject rules.
func (b *BalanceMgrTask) doWallet(ctx context.Context, taskID harmonytask.TaskID, addr *balanceManagerAddress) (bool, error) {
	// Get current balances
	subjectBalance, err := b.chain.WalletBalance(ctx, addr.SubjectAddress)
	if err != nil {
		return false, xerrors.Errorf("getting subject balance: %w", err)
	}

	secondBalance, err := b.chain.WalletBalance(ctx, addr.SecondAddress)
	if err != nil {
		return false, xerrors.Errorf("getting second balance: %w", err)
	}

	addr.SubjectBalance = subjectBalance
	addr.SecondBalance = secondBalance

	// calculate amount to send (based on latest chain balance)
	var amount types.BigInt
	var from, to address.Address
	var shouldSend bool

	switch addr.ActionType {
	case "requester":
		// If subject below low watermark, send from second to subject up to high watermark
		if addr.SubjectBalance.LessThan(addr.LowWatermarkFilBalance) {
			targetAmount := types.BigSub(addr.HighWatermarkFilBalance, addr.SubjectBalance)
			// Make sure we don't send more than second address has
			if targetAmount.GreaterThan(addr.SecondBalance) {
				log.Warnw("second address has insufficient balance",
					"needed", types.FIL(targetAmount),
					"available", types.FIL(addr.SecondBalance))

				// clear the task
				_, err = b.db.Exec(ctx, `
					UPDATE balance_manager_addresses 
					SET active_task_id = NULL, last_action = NOW()
					WHERE id = $1
				`, addr.ID)
				if err != nil {
					return false, xerrors.Errorf("clearing task id: %w", err)
				}

				return true, nil
			}
			amount = targetAmount
			from = addr.SecondAddress
			to = addr.SubjectAddress
			shouldSend = true
		}

	case "active-provider":
		// If subject above high watermark, send from subject to second down to low watermark
		if addr.SubjectBalance.GreaterThan(addr.HighWatermarkFilBalance) {
			amount = types.BigSub(addr.SubjectBalance, addr.LowWatermarkFilBalance)
			from = addr.SubjectAddress
			to = addr.SecondAddress
			shouldSend = true
		}

	default:
		return false, xerrors.Errorf("unknown action type: %s", addr.ActionType)
	}

	// If no need to send, clear the task and return
	if !shouldSend {
		log.Infow("balance within watermarks, no action needed",
			"subject", addr.SubjectAddress,
			"balance", types.FIL(addr.SubjectBalance),
			"low", types.FIL(addr.LowWatermarkFilBalance),
			"high", types.FIL(addr.HighWatermarkFilBalance))

		_, err = b.db.Exec(ctx, `
			UPDATE balance_manager_addresses 
			SET active_task_id = NULL, last_action = NOW()
			WHERE id = $1
		`, addr.ID)
		if err != nil {
			return false, xerrors.Errorf("clearing task id: %w", err)
		}
		return true, nil
	}

	// send msg
	msg := &types.Message{
		From:   from,
		To:     to,
		Value:  amount,
		Method: builtin.MethodSend,
	}

	mss := &api.MessageSendSpec{
		MaxFee:         abi.TokenAmount(MaxSendFee),
		MaximizeFeeCap: true,
	}

	// Send the message - sender will handle message_wait insertion
	msgCid, err := b.sender.Send(ctx, msg, mss, fmt.Sprintf("balancemgr-%s", addr.ActionType))
	if err != nil {
		return false, xerrors.Errorf("sending message: %w", err)
	}

	_, err = b.db.Exec(ctx, `INSERT INTO message_waits (signed_message_cid) VALUES ($1)`, msgCid)
	if err != nil {
		return false, xerrors.Errorf("inserting into message_waits: %w", err)
	}

	// Update the database with message info
	_, err = b.db.Exec(ctx, `
		UPDATE balance_manager_addresses 
		SET last_msg_cid = $2, 
		    last_msg_sent_at = NOW(), 
		    last_msg_landed_at = NULL,
			active_task_id = NULL
		WHERE id = $1
	`, addr.ID, msgCid.String())
	if err != nil {
		return false, xerrors.Errorf("updating message cid: %w", err)
	}

	log.Infow("sent balance management message",
		"from", from,
		"to", to,
		"amount", types.FIL(amount),
		"msgCid", msgCid,
		"actionType", addr.ActionType)

	// Task complete - chain handler will clear active_task_id when message lands
	return true, nil
}

// TypeDetails implements harmonytask.TaskInterface.
func (b *BalanceMgrTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "BalanceMgr",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 10 << 20,
		},
		MaxFailures: 2,
	}
}

var _ = harmonytask.Reg(&BalanceMgrTask{})
