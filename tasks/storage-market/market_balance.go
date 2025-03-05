package storage_market

import (
	"context"
	"time"

	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/tasks/message"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	marketActor "github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/types"
)

const MarketBalanceCheckInterval = 5 * time.Minute

type mbalanceApi interface {
	ChainHead(ctx context.Context) (*types.TipSet, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (lapi.MinerInfo, error)
	StateMarketBalance(context.Context, address.Address, types.TipSetKey) (lapi.MarketBalance, error)
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
}

type MarketBalanceManager struct {
	api    mbalanceApi
	miners map[string][]address.Address
	cfg    *config.MK12Config
	sender *message.Sender
}

func NewMarketBalanceManager(api mbalanceApi, miners []address.Address, cfg *config.CurioConfig, sender *message.Sender) (*MarketBalanceManager, error) {
	mk12Cfg := cfg.Market.StorageMarketConfig.MK12
	var disabledMiners []address.Address

	for _, m := range mk12Cfg.DisabledMiners {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return nil, xerrors.Errorf("failed to parse miner string: %s", err)
		}
		disabledMiners = append(disabledMiners, maddr)
	}

	enabled, _ := lo.Difference(miners, disabledMiners)

	mmap := make(map[string][]address.Address)
	mmap[mk12Str] = enabled

	return &MarketBalanceManager{
		api:    api,
		cfg:    &mk12Cfg,
		miners: mmap,
		sender: sender,
	}, nil
}

func (m *MarketBalanceManager) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	llog := log.With("Task", "MarketBalanceManager")

	ctx := context.Background()

	threshold := abi.TokenAmount(m.cfg.CollateralAddThreshold)
	amount := abi.TokenAmount(m.cfg.CollateralAddAmount)

	for module, miners := range m.miners {
		if module != mk12Str {
			continue
		}
		for _, miner := range miners {
			miner := miner

			// Check head in loop in case it changes and wallet balance changes
			head, err := m.api.ChainHead(ctx)
			if err != nil {
				return false, xerrors.Errorf("failed to get chain head: %w", err)
			}

			bal, err := m.api.StateMarketBalance(ctx, miner, head.Key())
			if err != nil {
				return false, xerrors.Errorf("failed to get market balance: %w", err)
			}

			avail := big.Sub(bal.Escrow, bal.Locked)

			if avail.GreaterThan(threshold) {
				llog.Debugf("Skipping add balance for miner %s, available balance is %s, threshold is %s", miner.String(), avail.String(), threshold.String())
				continue
			}

			var sender address.Address

			if m.cfg.DealCollateralWallet != "" {
				wallet, err := address.NewFromString(m.cfg.DealCollateralWallet)
				if err != nil {
					return false, xerrors.Errorf("failed to parse deal collateral wallet: %w", err)
				}

				w, err := m.api.StateGetActor(ctx, wallet, head.Key())
				if err != nil {
					return false, xerrors.Errorf("failed to get wallet actor: %w", err)
				}

				if w.Balance.LessThan(amount) {
					return false, xerrors.Errorf("Collateral wallet balance %s is lower than specified amount %s", w.Balance.String(), amount.String())
				}
				sender = wallet
			} else {
				mi, err := m.api.StateMinerInfo(ctx, miner, head.Key())
				if err != nil {
					return false, xerrors.Errorf("failed to get miner info: %w", err)
				}

				w, err := m.api.StateGetActor(ctx, mi.Worker, head.Key())
				if err != nil {
					return false, xerrors.Errorf("failed to get wallet actor: %w", err)
				}

				if w.Balance.LessThan(amount) {
					return false, xerrors.Errorf("Worker wallet balance %s is lower than specified amount %s", w.Balance.String(), amount.String())
				}
				sender = mi.Worker
			}

			params, err := actors.SerializeParams(&miner)
			if err != nil {
				return false, xerrors.Errorf("failed to serialize miner address: %w", err)
			}

			maxfee, err := types.ParseFIL("0.05 FIL")
			if err != nil {
				return false, xerrors.Errorf("failed to parse max fee: %w", err)
			}

			msp := &lapi.MessageSendSpec{
				MaxFee: abi.TokenAmount(maxfee),
			}

			msg := &types.Message{
				To:     marketActor.Address,
				From:   sender,
				Value:  amount,
				Method: marketActor.Methods.AddBalance,
				Params: params,
			}

			mcid, err := m.sender.Send(ctx, msg, msp, "Add Market Collateral")
			if err != nil {
				return false, xerrors.Errorf("failed to send message: %w", err)
			}

			llog.Debugf("sent message %s to add collateral to miner %s", mcid.String(), miner.String())
		}
	}

	return true, nil
}

func (m *MarketBalanceManager) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (m *MarketBalanceManager) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "MarketBalanceMgr",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(MarketBalanceCheckInterval, m),
	}
}

func (m *MarketBalanceManager) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &MarketBalanceManager{}
var _ = harmonytask.Reg(&MarketBalanceManager{})
