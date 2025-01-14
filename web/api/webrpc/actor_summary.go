package webrpc

import (
	"bytes"
	"context"
	"sort"

	"github.com/filecoin-project/go-state-types/abi"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/lib/curiochain"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
)

type ActorSummary struct {
	Address string
	CLayers []string

	QualityAdjustedPower string
	RawBytePower         string

	ActorBalance, ActorAvailable, VestingFunds, InitialPledgeRequirement, PreCommitDeposits string

	Win1, Win7, Win30 int64

	Deadlines []ActorDeadline
}

type ActorDeadline struct {
	Empty      bool
	Current    bool
	Proven     bool
	PartFaulty bool
	Faulty     bool
	Count      *DeadlineCount
}

type DeadlineCount struct {
	Total      uint64
	Active     uint64
	Live       uint64
	Fault      uint64
	Recovering uint64
}

type minimalActorInfo struct {
	Addresses []config.CurioAddresses
}

type WalletInfo struct {
	Type    string
	Address string
	Balance string
}
type ActorDetail struct {
	Summary                ActorSummary
	OwnerAddress           string
	Beneficiary            string
	WorkerAddress          string
	WorkerBalance          string
	PeerID                 string
	Address                []string
	SectorSize             abi.SectorSize
	PendingOwnerAddress    *address.Address
	BeneficiaryTerm        *miner.BeneficiaryTerm
	PendingBeneficiaryTerm *miner.PendingBeneficiaryChange
	Wallets                []WalletInfo
}

func (a *WebRPC) ActorInfo(ctx context.Context, ActorIDstr string) (*ActorDetail, error) {
	maddr, err := address.NewFromString(ActorIDstr)
	if err != nil {
		return nil, xerrors.Errorf("parsing address: %w", err)
	}
	confNameToAddr := map[address.Address][]string{}
	minerWallets := map[string][]address.Address{}
	err = a.visitAddresses(func(layer string, cAddrs config.CurioAddresses, madr address.Address) {
		if !bytes.Equal(maddr.Bytes(), madr.Bytes()) {
			return
		}
		for name, aset := range map[string][]string{
			layer + ":Commit":    cAddrs.PreCommitControl,
			layer + ":Control":   cAddrs.CommitControl,
			layer + ":Terminate": cAddrs.TerminateControl,
		} {
			for _, addrStr := range aset {
				addr, err := address.NewFromString(addrStr)
				if err != nil {
					log.Errorf("parsing address: %w", err)
					continue
				}
				minerWallets[name] = append(minerWallets[name], addr)
			}
		}
		confNameToAddr[madr] = append(confNameToAddr[madr], layer)
	})
	if err != nil {
		return nil, xerrors.Errorf("visiting addresses: %w", err)
	}

	summaries, err := a.getActorSummary(ctx, confNameToAddr)
	if err != nil {
		return nil, xerrors.Errorf("getting actor summary: %w", err)
	}

	summary := summaries[0]

	info, err := a.deps.Chain.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting miner info: %w", err)
	}

	balanceCache := map[address.Address]big.Int{}
	wbal, ok := balanceCache[info.Worker]
	if !ok {
		wbal, err = a.deps.Chain.WalletBalance(ctx, info.Worker)
		if err != nil {
			return nil, xerrors.Errorf("getting worker balance: %w", err)
		}
		balanceCache[info.Worker] = wbal
	}

	var addresses []string
	for _, addr := range info.Multiaddrs {
		addresses = append(addresses, string(addr))
	}

	ad := &ActorDetail{
		Summary:                summary,
		OwnerAddress:           info.Owner.String(),
		Beneficiary:            info.Beneficiary.String(),
		WorkerAddress:          info.Worker.String(),
		WorkerBalance:          types.FIL(wbal).Short(),
		PeerID:                 info.PeerId.String(),
		Address:                addresses,
		SectorSize:             info.SectorSize,
		PendingOwnerAddress:    info.PendingOwnerAddress,
		BeneficiaryTerm:        info.BeneficiaryTerm,
		PendingBeneficiaryTerm: info.PendingBeneficiaryTerm,
	}

	var wallets []WalletInfo

	found := make(map[address.Address]struct{})
	for name, addrs := range minerWallets {
		for _, addr := range addrs {
			wb, ok := balanceCache[addr]
			if !ok {
				wb, err = a.deps.Chain.WalletBalance(ctx, addr)
				if err != nil {
					return nil, xerrors.Errorf("getting wallet balance: %w", err)
				}
				balanceCache[addr] = wb
			}
			ad.Wallets = append(ad.Wallets, WalletInfo{
				Type:    name,
				Address: addr.String(),
				Balance: types.FIL(wb).Short(),
			})
			found[addr] = struct{}{}
		}
	}

	for _, addr := range info.ControlAddresses {
		_, ok := found[addr]
		if ok {
			continue
		}
		bal, ok := balanceCache[addr]
		if !ok {
			bal, err = a.deps.Chain.WalletBalance(ctx, info.Worker)
			if err != nil {
				return nil, xerrors.Errorf("getting control address balance: %w", err)
			}
			balanceCache[addr] = bal
		}
		ad.Wallets = append(ad.Wallets, WalletInfo{
			Type:    "Control",
			Address: addr.String(),
			Balance: types.FIL(bal).Short(),
		})
	}

	ad.Wallets = wallets
	return ad, nil
}

func (a *WebRPC) ActorSummary(ctx context.Context) ([]ActorSummary, error) {
	confNameToAddr := map[address.Address][]string{}
	err := a.visitAddresses(func(name string, _ config.CurioAddresses, a address.Address) {
		confNameToAddr[a] = append(confNameToAddr[a], name)
	})
	if err != nil {
		return nil, err
	}
	as, err := a.getActorSummary(ctx, confNameToAddr)
	return as, err
}

func (a *WebRPC) visitAddresses(cb func(string, config.CurioAddresses, address.Address)) error {
	err := forEachConfig(a, func(name string, info minimalActorInfo) error {
		for _, aset := range info.Addresses {
			for _, addr := range aset.MinerAddresses {
				a, err := address.NewFromString(addr)
				if err != nil {
					return xerrors.Errorf("parsing address: %w", err)
				}
				cb(name, aset, a)
			}
		}
		return nil
	})
	if err != nil {
		return nil
	}
	return nil
}

func (a *WebRPC) getActorSummary(ctx context.Context, confNameToAddr map[address.Address][]string) (as []ActorSummary, err error) {
	wins, err := a.spWins(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting sp wins: %w", err)
	}

	stor := store.ActorStore(ctx,
		blockstore.NewReadCachedBlockstore(blockstore.NewAPIBlockstore(a.deps.Chain), curiochain.ChainBlockCache))
	var actorInfos []ActorSummary

	for addr, cnames := range confNameToAddr {
		p, err := a.deps.Chain.StateMinerPower(ctx, addr, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("getting miner power: %w", err)
		}

		mact, err := a.deps.Chain.StateGetActor(ctx, addr, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("getting actor: %w", err)
		}

		mas, err := miner.Load(stor, mact)
		if err != nil {
			return nil, err
		}

		avail, err := mas.AvailableBalance(mact.Balance)
		if err != nil {
			return nil, xerrors.Errorf("getting available balance: %w", err)
		}

		locked, err := mas.LockedFunds()
		if err != nil {
			return nil, xerrors.Errorf("getting locked funds: %w", err)
		}

		deadlines, err := a.getDeadlines(ctx, addr)
		if err != nil {
			return nil, xerrors.Errorf("getting deadlines: %w", err)
		}

		sort.Strings(cnames)
		as := ActorSummary{
			Address:                  addr.String(),
			CLayers:                  cnames,
			QualityAdjustedPower:     types.DeciStr(p.MinerPower.QualityAdjPower),
			RawBytePower:             types.DeciStr(p.MinerPower.RawBytePower),
			ActorBalance:             types.FIL(mact.Balance).Short(),
			ActorAvailable:           types.FIL(avail).Short(),
			VestingFunds:             types.FIL(locked.VestingFunds).Short(),
			InitialPledgeRequirement: types.FIL(locked.InitialPledgeRequirement).Short(),
			PreCommitDeposits:        types.FIL(locked.PreCommitDeposits).Short(),
			Win1:                     wins[addr].Win1,
			Win7:                     wins[addr].Win7,
			Win30:                    wins[addr].Win30,
			Deadlines:                deadlines,
		}
		actorInfos = append(actorInfos, as)
	}

	sort.Slice(actorInfos, func(i, j int) bool {
		return actorInfos[i].Address < actorInfos[j].Address
	})

	return actorInfos, nil
}

func (a *WebRPC) getDeadlines(ctx context.Context, addr address.Address) ([]ActorDeadline, error) {
	dls, err := a.deps.Chain.StateMinerDeadlines(ctx, addr, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting deadlines: %w", err)
	}

	outDls := make([]ActorDeadline, 48)

	for dlidx := range dls {
		p, err := a.deps.Chain.StateMinerPartitions(ctx, addr, uint64(dlidx), types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("getting partition: %w", err)
		}

		dl := ActorDeadline{}

		var live, faulty, total, active, recovering uint64

		for _, part := range p {
			al, err := part.AllSectors.Count()
			if err != nil {
				return nil, xerrors.Errorf("getting all sectors: %w", err)
			}
			total += al

			ac, err := part.ActiveSectors.Count()
			if err != nil {
				return nil, xerrors.Errorf("getting active sectors: %w", err)
			}
			active += ac

			re, err := part.RecoveringSectors.Count()
			if err != nil {
				return nil, xerrors.Errorf("getting recovering sectors: %w", err)
			}
			recovering += re

			l, err := part.LiveSectors.Count()
			if err != nil {
				return nil, xerrors.Errorf("getting live sectors: %w", err)
			}
			live += l

			f, err := part.FaultySectors.Count()
			if err != nil {
				return nil, xerrors.Errorf("getting faulty sectors: %w", err)
			}
			faulty += f
		}

		dl.Empty = live == 0
		dl.Proven = live > 0 && faulty == 0
		dl.PartFaulty = faulty > 0
		dl.Faulty = faulty > 0 && faulty == live
		dl.Count = &DeadlineCount{
			Total:      total,
			Active:     active,
			Live:       live,
			Fault:      faulty,
			Recovering: recovering,
		}

		outDls[dlidx] = dl
	}

	pd, err := a.deps.Chain.StateMinerProvingDeadline(ctx, addr, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting proving deadline: %w", err)
	}

	outDls[pd.Index].Current = true

	return outDls, nil
}

type wins struct {
	SpID  int64 `db:"sp_id"`
	Win1  int64 `db:"win1"`
	Win7  int64 `db:"win7"`
	Win30 int64 `db:"win30"`
}

func (a *WebRPC) spWins(ctx context.Context) (map[address.Address]wins, error) {
	var w []wins

	// note: this query uses mining_tasks_won_sp_id_base_compute_time_index
	err := a.deps.DB.Select(ctx, &w, `WITH wins AS (
	    SELECT
	        sp_id,
	        base_compute_time,
	        won
	    FROM
	        mining_tasks
	    WHERE
	        won = true
	      AND base_compute_time > NOW() - INTERVAL '30 days'
	)

	SELECT
	    sp_id,
	    COUNT(*) FILTER (WHERE base_compute_time > NOW() - INTERVAL '1 day') AS "win1",
	    COUNT(*) FILTER (WHERE base_compute_time > NOW() - INTERVAL '7 days') AS "win7",
	    COUNT(*) FILTER (WHERE base_compute_time > NOW() - INTERVAL '30 days') AS "win30"
	FROM
	    wins
	GROUP BY
	    sp_id
	ORDER BY
	    sp_id`)
	if err != nil {
		return nil, xerrors.Errorf("query win counts: %w", err)
	}

	wm := make(map[address.Address]wins)
	for _, wi := range w {
		ma, err := address.NewIDAddress(uint64(wi.SpID))
		if err != nil {
			return nil, xerrors.Errorf("parsing miner address: %w", err)
		}

		wm[ma] = wi
	}

	return wm, nil
}

func (a *WebRPC) ActorList(ctx context.Context) ([]string, error) {
	confNameToAddr := map[address.Address][]string{}

	err := forEachConfig(a, func(name string, info minimalActorInfo) error {
		for _, aset := range info.Addresses {
			for _, addr := range aset.MinerAddresses {
				a, err := address.NewFromString(addr)
				if err != nil {
					return xerrors.Errorf("parsing address: %w", err)
				}
				confNameToAddr[a] = append(confNameToAddr[a], name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var ret []string

	for m := range confNameToAddr {
		ret = append(ret, m.String())
	}

	return ret, nil
}
