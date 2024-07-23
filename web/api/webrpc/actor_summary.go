package webrpc

import (
	"context"
	"sort"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

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

	ActorBalance, ActorAvailable, WorkerBalance string

	Win1, Win7, Win30 int64

	Deadlines []ActorDeadline
}

type ActorDeadline struct {
	Empty      bool
	Current    bool
	Proven     bool
	PartFaulty bool
	Faulty     bool
}

type minimalActorInfo struct {
	Addresses []struct {
		MinerAddresses []string
	}
}

func (a *WebRPC) ActorSummary(ctx context.Context) ([]ActorSummary, error) {
	stor := store.ActorStore(ctx, blockstore.NewReadCachedBlockstore(blockstore.NewAPIBlockstore(a.deps.Chain), curiochain.ChainBlockCache))

	var actorInfos []ActorSummary

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

	wins, err := a.spWins(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting sp wins: %w", err)
	}

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

		deadlines, err := a.getDeadlines(ctx, addr)
		if err != nil {
			return nil, err
		}

		avail, err := mas.AvailableBalance(mact.Balance)
		if err != nil {
			return nil, xerrors.Errorf("getting available balance: %w", err)
		}

		mi, err := mas.Info()
		if err != nil {
			return nil, xerrors.Errorf("getting miner info: %w", err)
		}

		wbal, err := a.deps.Chain.WalletBalance(ctx, mi.Worker)
		if err != nil {
			return nil, xerrors.Errorf("getting worker balance: %w", err)
		}

		sort.Strings(cnames)

		actorInfos = append(actorInfos, ActorSummary{
			Address:              addr.String(),
			CLayers:              cnames,
			QualityAdjustedPower: types.DeciStr(p.MinerPower.QualityAdjPower),
			RawBytePower:         types.DeciStr(p.MinerPower.RawBytePower),
			Deadlines:            deadlines,
			ActorBalance:         types.FIL(mact.Balance).Short(),
			ActorAvailable:       types.FIL(avail).Short(),
			WorkerBalance:        types.FIL(wbal).Short(),
			Win1:                 wins[addr].Win1,
			Win7:                 wins[addr].Win7,
			Win30:                wins[addr].Win30,
		})
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

		var live, faulty uint64

		for _, part := range p {
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
