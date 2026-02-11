package balancemgr

import (
	"context"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/tasks/message"

	"github.com/filecoin-project/lotus/api"
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
			case "f05":
				err = t.adderF05(ctx, taskFunc, addr)
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

// Adder implements harmonytask.TaskInterface.
func (b *BalanceMgrTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	b.adder.Set(taskFunc)
}

// CanAccept implements harmonytask.TaskInterface.
func (b *BalanceMgrTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	preferred := []harmonytask.TaskID{}
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

		switch subjectType {
		case "wallet", "proofshare", "f05":
			preferred = append(preferred, id)
		}
	}

	return preferred, nil
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
	case "f05":
		return b.doF05(ctx, taskID, addr)
	default:
		return false, xerrors.Errorf("unknown subject type: %s", addr.SubjectType)
	}
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
