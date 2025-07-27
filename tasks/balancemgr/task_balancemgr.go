package balancemgr

import (
	"context"
	"fmt"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/types"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

var log = logging.Logger("balancemgr")

var MaxSendFee = types.MustParseFIL("0.05 FIL")

type BalanceMgrTask struct {
	db     *harmonydb.DB
	chain  api.FullNode
	sender *message.Sender
	adder  promise.Promise[harmonytask.AddTaskFunc]
}

func NewBalanceMgrTask(db *harmonydb.DB, chain api.FullNode, pcs *chainsched.CurioChainSched, sender *message.Sender) *BalanceMgrTask {
	t := &BalanceMgrTask{
		db:     db,
		chain:  chain,
		sender: sender,
	}

	pcs.AddHandler(func(ctx context.Context, revert, apply *types.TipSet) error {
		if !t.adder.IsSet() {
			return nil
		}

		taskFunc := t.adder.Val(ctx)

		// select all addrs, where active_task_id = null, last_msg_cid is null OR last_msg_landed_at is NOT null
		rows, err := t.db.Query(ctx, `
			SELECT id, subject_address, second_address, action_type, low_watermark_fil_balance, high_watermark_fil_balance
			FROM balance_manager_addresses
			WHERE active_task_id IS NULL AND (last_msg_cid IS NULL OR last_msg_landed_at IS NOT NULL)
		`)
		if err != nil {
			return err
		}
		defer rows.Close()

		type balanceManagerAddress struct {
			ID                      int64
			SubjectAddress          address.Address
			SecondAddress           address.Address
			ActionType              string
			LowWatermarkFilBalance  types.BigInt
			HighWatermarkFilBalance types.BigInt

			// chain data
			SubjectBalance types.BigInt
			SecondBalance  types.BigInt
		}
		addresses := make([]*balanceManagerAddress, 0)

		for rows.Next() {
			var id int64
			var subjectAddressStr string
			var secondAddressStr string
			var actionType string
			var lowWatermarkFilBalanceStr string
			var highWatermarkFilBalanceStr string

			err = rows.Scan(&id, &subjectAddressStr, &secondAddressStr, &actionType, &lowWatermarkFilBalanceStr, &highWatermarkFilBalanceStr)
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
				LowWatermarkFilBalance:  abi.TokenAmount(lowWatermark),
				HighWatermarkFilBalance: abi.TokenAmount(highWatermark),
			}

			addresses = append(addresses, addr)
		}

		for _, addr := range addresses {
			// get balances
			subjectBalance, err := t.chain.WalletBalance(ctx, addr.SubjectAddress)
			if err != nil {
				return xerrors.Errorf("getting subject balance: %w", err)
			}

			secondBalance, err := t.chain.WalletBalance(ctx, addr.SecondAddress)
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
		}
		return nil
	})

	return t
}

// Adder implements harmonytask.TaskInterface.
func (b *BalanceMgrTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	b.adder.Set(taskFunc)
}

// CanAccept implements harmonytask.TaskInterface.
func (b *BalanceMgrTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

// Do implements harmonytask.TaskInterface.
func (b *BalanceMgrTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// select task info
	var id int64
	var subjectAddressStr string
	var secondAddressStr string
	var actionType string
	var lowWatermarkStr string
	var highWatermarkStr string

	err = b.db.QueryRow(ctx, `
		SELECT id, subject_address, second_address, action_type, 
		       low_watermark_fil_balance, high_watermark_fil_balance
		FROM balance_manager_addresses
		WHERE active_task_id = $1
	`, taskID).Scan(&id, &subjectAddressStr, &secondAddressStr, &actionType,
		&lowWatermarkStr, &highWatermarkStr)
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

	// Get current balances
	subjectBalance, err := b.chain.WalletBalance(ctx, subjectAddr)
	if err != nil {
		return false, xerrors.Errorf("getting subject balance: %w", err)
	}

	secondBalance, err := b.chain.WalletBalance(ctx, secondAddr)
	if err != nil {
		return false, xerrors.Errorf("getting second balance: %w", err)
	}

	// calculate amount to send (based on latest chain balance)
	var amount types.BigInt
	var from, to address.Address
	var shouldSend bool

	switch actionType {
	case "requester":
		// If subject below low watermark, send from second to subject up to high watermark
		if subjectBalance.LessThan(abi.TokenAmount(lowWatermark)) {
			targetAmount := types.BigSub(abi.TokenAmount(highWatermark), subjectBalance)
			// Make sure we don't send more than second address has
			if targetAmount.GreaterThan(secondBalance) {
				log.Warnw("second address has insufficient balance",
					"needed", types.FIL(targetAmount),
					"available", types.FIL(secondBalance))

				// clear the task
				_, err = b.db.Exec(ctx, `
					UPDATE balance_manager_addresses 
					SET active_task_id = NULL, last_action = NOW()
					WHERE id = $1
				`, id)
				if err != nil {
					return false, xerrors.Errorf("clearing task id: %w", err)
				}

				return true, nil
			}
			amount = targetAmount
			from = secondAddr
			to = subjectAddr
			shouldSend = true
		}

	case "active-provider":
		// If subject above high watermark, send from subject to second down to low watermark
		if subjectBalance.GreaterThan(abi.TokenAmount(highWatermark)) {
			amount = types.BigSub(subjectBalance, abi.TokenAmount(lowWatermark))
			from = subjectAddr
			to = secondAddr
			shouldSend = true
		}

	default:
		return false, xerrors.Errorf("unknown action type: %s", actionType)
	}

	// If no need to send, clear the task and return
	if !shouldSend {
		log.Infow("balance within watermarks, no action needed",
			"subject", subjectAddr,
			"balance", types.FIL(subjectBalance),
			"low", types.FIL(lowWatermark),
			"high", types.FIL(highWatermark))

		_, err = b.db.Exec(ctx, `
			UPDATE balance_manager_addresses 
			SET active_task_id = NULL, last_action = NOW()
			WHERE id = $1
		`, id)
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
		MaxFee: abi.TokenAmount(MaxSendFee),
		MaximizeFeeCap: true,
	}

	// Send the message - sender will handle message_wait insertion
	msgCid, err := b.sender.Send(ctx, msg, mss, fmt.Sprintf("balancemgr-%s", actionType))
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
	`, id, msgCid.String())
	if err != nil {
		return false, xerrors.Errorf("updating message cid: %w", err)
	}

	log.Infow("sent balance management message",
		"from", from,
		"to", to,
		"amount", types.FIL(amount),
		"msgCid", msgCid,
		"actionType", actionType)

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
