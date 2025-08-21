package balancemgr

import (
	"context"
	"fmt"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

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
		"subjectType", "wallet",
		"amount", types.FIL(amount),
		"msgCid", msgCid,
		"actionType", addr.ActionType)

	// Task complete - chain handler will clear active_task_id when message lands
	return true, nil
}
