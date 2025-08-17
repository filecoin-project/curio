package balancemgr

import (
	"context"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

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

	svc := common.NewServiceCustomSend(b.chain, func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error) {
		mss.MaximizeFeeCap = true
		return b.sender.Send(ctx, msg, mss, "balancemgr-proofshare")
	})
	idAddr, err := b.chain.StateLookupID(ctx, addr.SubjectAddress, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting address ID: %w", err)
	}

	addrID, err := address.IDFromAddress(idAddr)
	if err != nil {
		return false, xerrors.Errorf("getting address ID: %w", err)
	}

	clientState, err := svc.GetClientState(ctx, addrID)
	if err != nil {
		return false, xerrors.Errorf("PSClientWallets: failed to get client state: %w", err)
	}

	sourceBalance, err := b.chain.StateGetActor(ctx, addr.SubjectAddress, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("getting source balance: %w", err)
	}

	addr.SubjectBalance = types.BigInt(clientState.Balance)
	addr.SecondBalance = types.BigInt(sourceBalance.Balance)

	// calculate amount to send (based on latest chain balance)
	var amount types.BigInt
	var from, to address.Address
	var shouldSend bool

	if addr.ActionType != "requester" {
		return false, xerrors.Errorf("action type is not requester: %s", addr.ActionType)
	}

	if addr.SubjectBalance.LessThan(addr.LowWatermarkFilBalance) {
		amount = types.BigSub(addr.HighWatermarkFilBalance, addr.SubjectBalance)
		from = addr.SecondAddress
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

	msgCid, err := svc.ClientDeposit(ctx, addr.SubjectAddress, amount)
	if err != nil {
		return false, xerrors.Errorf("ClientDeposit: %w", err)
	}

	_, err = b.db.Exec(ctx, `INSERT INTO message_waits (signed_message_cid) VALUES ($1)`, msgCid)
	if err != nil {
		return false, xerrors.Errorf("inserting into message_waits: %w", err)
	}

	_, err = b.db.Exec(ctx, `
		UPDATE balance_manager_addresses 
		SET last_msg_cid = $2, 
		    last_msg_sent_at = NOW(), 
		    last_msg_landed_at = NULL
		WHERE id = $1
	`, addr.ID, msgCid.String())
	if err != nil {
		return false, xerrors.Errorf("updating message cid: %w", err)
	}

	_, err = b.db.Exec(ctx, `
			INSERT INTO proofshare_client_messages (signed_cid, wallet, action)
			VALUES ($1, $2, $3)
		`, msgCid, idAddr, "deposit-bmgr")
	if err != nil {
		return false, xerrors.Errorf("addMessageTracking: failed to insert proofshare_client_messages: %w", err)
	}

	log.Infow("sent balance management message",
		"from", from,
		"to", to,
		"subjectType", "proofshare",
		"amount", types.FIL(amount),
		"msgCid", msgCid,
		"actionType", addr.ActionType)

	return true, nil
}
