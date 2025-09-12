package balancemgr

import (
	"context"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

func (b *BalanceMgrTask) adderF05(ctx context.Context, taskFunc harmonytask.AddTaskFunc, addr *balanceManagerAddress) error {
	marketBalance, err := b.chain.StateMarketBalance(ctx, addr.SubjectAddress, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting market balance: %w", err)
	}

	sourceBalance, err := b.chain.StateGetActor(ctx, addr.SubjectAddress, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting source balance: %w", err)
	}

	addr.SubjectBalance = big.Sub(marketBalance.Escrow, marketBalance.Locked)
	addr.SecondBalance = sourceBalance.Balance

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

func (b *BalanceMgrTask) doF05(ctx context.Context, taskID harmonytask.TaskID, addr *balanceManagerAddress) (bool, error) {
	log.Infow("balancemgr f05 Do",
		"id", addr.ID,
		"subject", addr.SubjectAddress,
		"low", types.FIL(addr.LowWatermarkFilBalance),
		"high", types.FIL(addr.HighWatermarkFilBalance))

	marketBalance, err := b.chain.StateMarketBalance(ctx, addr.SubjectAddress, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting market balance: %w", err)
	}
	
	sourceBalance, err := b.chain.StateGetActor(ctx, addr.SubjectAddress, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting source balance: %w", err)
	}

	idAddr, err := b.chain.StateLookupID(ctx, addr.SubjectAddress, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting address ID: %w", err)
	}

	addrID, err := address.IDFromAddress(idAddr)
	if err != nil {
		return false, xerrors.Errorf("getting address ID: %w", err)
	}

	addr.SubjectBalance = big.Sub(marketBalance.Escrow, marketBalance.Locked)
	addr.SecondBalance = types.BigInt(sourceBalance.Balance)

	// calculate amount to send (based on latest chain balance)
	var amount types.BigInt
	var to address.Address
	var shouldSend bool

	if addr.ActionType != "requester" {
		return false, xerrors.Errorf("action type is not requester: %s", addr.ActionType)
	}

	if addr.SubjectBalance.LessThan(addr.LowWatermarkFilBalance) {
		amount = types.BigSub(addr.HighWatermarkFilBalance, addr.SubjectBalance)
		to = addr.SubjectAddress
		shouldSend = true
	}

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

	params, err := actors.SerializeParams(&addr.SubjectAddress)
	if err != nil {
		return false, xerrors.Errorf("failed to serialize miner address: %w", err)
	}

	maxfee, err := types.ParseFIL("0.05 FIL")
	if err != nil {
		return false, xerrors.Errorf("failed to parse max fee: %w", err)
	}

	msp := &api.MessageSendSpec{
		MaxFee: abi.TokenAmount(maxfee),
	}

	msg := &types.Message{
		To:     market.Address,
		From:   addr.SecondAddress,
		Value:  amount,
		Method: market.Methods.AddBalance,
		Params: params,
	}

	msgCid, err := b.sender.Send(ctx, msg, msp, "add-market-collateral")
	if err != nil {
		return false, xerrors.Errorf("failed to send message: %w", err)
	}

	_, err = b.db.Exec(ctx, `INSERT INTO message_waits (signed_message_cid) VALUES ($1)`, msgCid)
	if err != nil {
		return false, xerrors.Errorf("inserting into message_waits: %w", err)
	}

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

	_, err = b.db.Exec(ctx, `
			INSERT INTO proofshare_client_messages (signed_cid, wallet, action)
			VALUES ($1, $2, $3)
		`, msgCid, addrID, "deposit-bmgr")
	if err != nil {
		return false, xerrors.Errorf("addMessageTracking: failed to insert proofshare_client_messages: %w", err)
	}

	log.Infow("sent balance management message",
		"from", addr.SecondAddress,
		"to", to,
		"subjectType", "proofshare",
		"amount", types.FIL(amount),
		"msgCid", msgCid,
		"actionType", addr.ActionType)

	return true, nil
}
