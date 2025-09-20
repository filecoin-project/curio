package storage_market

import (
	"context"
	"time"

	logging "github.com/ipfs/go-log/v2"
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

const BalanceCheckInterval = 5 * time.Minute

var blog = logging.Logger("balancemgr")

type mbalanceApi interface {
	ChainHead(ctx context.Context) (*types.TipSet, error)
	StateMarketBalance(context.Context, address.Address, types.TipSetKey) (lapi.MarketBalance, error)
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
}

type BalanceManager struct {
	api    mbalanceApi
	miners map[string][]address.Address
	cfg    *config.CurioConfig
	sender *message.Sender
	bmcfg  map[address.Address]config.BalanceManagerConfig
}

func NewBalanceManager(api mbalanceApi, miners []address.Address, cfg *config.CurioConfig, sender *message.Sender) (*BalanceManager, error) {
	var mk12disabledMiners []address.Address

	for _, m := range cfg.Market.StorageMarketConfig.MK12.DisabledMiners {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return nil, xerrors.Errorf("failed to parse miner string: %s", err)
		}
		mk12disabledMiners = append(mk12disabledMiners, maddr)
	}

	mk12enabled, _ := lo.Difference(miners, mk12disabledMiners)

	var mk20disabledMiners []address.Address
	for _, m := range cfg.Market.StorageMarketConfig.MK20.DisabledMiners {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return nil, xerrors.Errorf("failed to parse miner string: %s", err)
		}
		mk20disabledMiners = append(mk20disabledMiners, maddr)
	}
	mk20enabled, _ := lo.Difference(miners, mk20disabledMiners)

	mmap := make(map[string][]address.Address)
	mmap[mk12Str] = mk12enabled
	mmap[mk20Str] = mk20enabled
	bmcfg := make(map[address.Address]config.BalanceManagerConfig)
	for _, a := range cfg.Addresses {
		if len(a.MinerAddresses) > 0 {
			for _, m := range a.MinerAddresses {
				maddr, err := address.NewFromString(m)
				if err != nil {
					return nil, xerrors.Errorf("failed to parse miner string: %s", err)
				}
				bmcfg[maddr] = a.BalanceManager
			}
		}
	}

	return &BalanceManager{
		api:    api,
		cfg:    cfg,
		miners: mmap,
		sender: sender,
		bmcfg:  bmcfg,
	}, nil
}

func (m *BalanceManager) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	err = m.dealMarketBalance(ctx)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (m *BalanceManager) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (m *BalanceManager) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "BalanceManager",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(BalanceCheckInterval, m),
	}
}

func (m *BalanceManager) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &BalanceManager{}
var _ = harmonytask.Reg(&BalanceManager{})

func (m *BalanceManager) dealMarketBalance(ctx context.Context) error {

	for module, miners := range m.miners {
		if module != mk12Str {
			continue
		}
		for _, miner := range miners {
			miner := miner

			lowthreshold := abi.TokenAmount(m.bmcfg[miner].MK12Collateral.CollateralLowThreshold)
			highthreshold := abi.TokenAmount(m.bmcfg[miner].MK12Collateral.CollateralHighThreshold)

			if m.bmcfg[miner].MK12Collateral.DealCollateralWallet == "" {
				blog.Errorf("Deal collateral wallet is not set for miner %s", miner.String())
				continue
			}

			wallet, err := address.NewFromString(m.bmcfg[miner].MK12Collateral.DealCollateralWallet)
			if err != nil {
				return xerrors.Errorf("failed to parse deal collateral wallet: %w", err)
			}

			w, err := m.api.StateGetActor(ctx, wallet, types.EmptyTSK)
			if err != nil {
				return xerrors.Errorf("failed to get wallet actor: %w", err)
			}

			wbal := w.Balance

			// Check head in loop in case it changes and wallet balance changes
			head, err := m.api.ChainHead(ctx)
			if err != nil {
				return xerrors.Errorf("failed to get chain head: %w", err)
			}

			bal, err := m.api.StateMarketBalance(ctx, miner, head.Key())
			if err != nil {
				return xerrors.Errorf("failed to get market balance: %w", err)
			}

			avail := big.Sub(bal.Escrow, bal.Locked)

			if avail.GreaterThan(lowthreshold) {
				blog.Debugf("Skipping add balance for miner %s, available balance is %s, threshold is %s", miner.String(), avail.String(), lowthreshold.String())
				continue
			}

			amount := big.Sub(highthreshold, avail)

			if wbal.LessThan(amount) {
				return xerrors.Errorf("Worker wallet balance %s is lower than specified amount %s", wbal.String(), amount.String())
			}

			params, err := actors.SerializeParams(&miner)
			if err != nil {
				return xerrors.Errorf("failed to serialize miner address: %w", err)
			}

			maxfee, err := types.ParseFIL("0.05 FIL")
			if err != nil {
				return xerrors.Errorf("failed to parse max fee: %w", err)
			}

			msp := &lapi.MessageSendSpec{
				MaxFee: abi.TokenAmount(maxfee),
			}

			msg := &types.Message{
				To:     marketActor.Address,
				From:   wallet,
				Value:  amount,
				Method: marketActor.Methods.AddBalance,
				Params: params,
			}

			mcid, err := m.sender.Send(ctx, msg, msp, "add-market-collateral")
			if err != nil {
				return xerrors.Errorf("failed to send message: %w", err)
			}

			blog.Debugf("sent message %s to add collateral to miner %s", mcid.String(), miner.String())
		}
	}

	return nil
}
