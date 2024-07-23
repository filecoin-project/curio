package webrpc

import (
	"context"
	"time"

	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"

	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

type PipelineTask struct {
	SpID         int64 `db:"sp_id"`
	SectorNumber int64 `db:"sector_number"`

	CreateTime time.Time `db:"create_time"`

	TaskSDR  *int64 `db:"task_id_sdr"`
	AfterSDR bool   `db:"after_sdr"`

	TaskTreeD  *int64 `db:"task_id_tree_d"`
	AfterTreeD bool   `db:"after_tree_d"`

	TaskTreeC  *int64 `db:"task_id_tree_c"`
	AfterTreeC bool   `db:"after_tree_c"`

	TaskTreeR  *int64 `db:"task_id_tree_r"`
	AfterTreeR bool   `db:"after_tree_r"`

	TaskPrecommitMsg  *int64 `db:"task_id_precommit_msg"`
	AfterPrecommitMsg bool   `db:"after_precommit_msg"`

	AfterPrecommitMsgSuccess bool   `db:"after_precommit_msg_success"`
	SeedEpoch                *int64 `db:"seed_epoch"`

	TaskPoRep  *int64 `db:"task_id_porep"`
	PoRepProof []byte `db:"porep_proof"`
	AfterPoRep bool   `db:"after_porep"`

	TaskFinalize  *int64 `db:"task_id_finalize"`
	AfterFinalize bool   `db:"after_finalize"`

	TaskMoveStorage  *int64 `db:"task_id_move_storage"`
	AfterMoveStorage bool   `db:"after_move_storage"`

	TaskCommitMsg  *int64 `db:"task_id_commit_msg"`
	AfterCommitMsg bool   `db:"after_commit_msg"`

	AfterCommitMsgSuccess bool `db:"after_commit_msg_success"`

	Failed       bool   `db:"failed"`
	FailedReason string `db:"failed_reason"`
}

type sectorListEntry struct {
	PipelineTask

	Address    address.Address
	CreateTime string
	AfterSeed  bool

	ChainAlloc, ChainSector, ChainActive, ChainUnproven, ChainFaulty bool
}

type minerBitfields struct {
	alloc, sectorSet, active, unproven, faulty bitfield.BitField
}

func (a *WebRPC) PipelinePorepSectors(ctx context.Context) ([]sectorListEntry, error) {
	var tasks []PipelineTask

	err := a.deps.DB.Select(ctx, &tasks, `SELECT 
       sp_id, sector_number,
       create_time,
       task_id_sdr, after_sdr,
       task_id_tree_d, after_tree_d,
       task_id_tree_c, after_tree_c,
       task_id_tree_r, after_tree_r,
       task_id_precommit_msg, after_precommit_msg,
       after_precommit_msg_success, seed_epoch,
       task_id_porep, porep_proof, after_porep,
       task_id_finalize, after_finalize,
       task_id_move_storage, after_move_storage,
       task_id_commit_msg, after_commit_msg,
       after_commit_msg_success,
       failed, failed_reason
    FROM sectors_sdr_pipeline order by sp_id, sector_number`) // todo where constrain list
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch pipeline tasks: %w", err)
	}

	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch chain head: %w", err)
	}
	epoch := head.Height()

	minerBitfieldCache := map[address.Address]minerBitfields{}

	sectorList := make([]sectorListEntry, 0, len(tasks))
	for _, task := range tasks {
		task := task

		task.CreateTime = task.CreateTime.Local()

		addr, err := address.NewIDAddress(uint64(task.SpID))
		if err != nil {
			return nil, xerrors.Errorf("failed to create actor address: %w", err)
		}

		mbf, ok := minerBitfieldCache[addr]
		if !ok {
			mbf, err = a.getMinerBitfields(ctx, addr, a.stor)
			if err != nil {
				return nil, xerrors.Errorf("failed to load miner bitfields: %w", err)
			}
			minerBitfieldCache[addr] = mbf
		}

		afterSeed := task.SeedEpoch != nil && *task.SeedEpoch <= int64(epoch)

		sectorList = append(sectorList, sectorListEntry{
			PipelineTask: task,
			Address:      addr,
			CreateTime:   task.CreateTime.Format(time.DateTime),
			AfterSeed:    afterSeed,

			ChainAlloc:    must.One(mbf.alloc.IsSet(uint64(task.SectorNumber))),
			ChainSector:   must.One(mbf.sectorSet.IsSet(uint64(task.SectorNumber))),
			ChainActive:   must.One(mbf.active.IsSet(uint64(task.SectorNumber))),
			ChainUnproven: must.One(mbf.unproven.IsSet(uint64(task.SectorNumber))),
			ChainFaulty:   must.One(mbf.faulty.IsSet(uint64(task.SectorNumber))),
		})
	}

	return sectorList, nil
}

func (a *WebRPC) getMinerBitfields(ctx context.Context, addr address.Address, stor adt.Store) (minerBitfields, error) {
	act, err := a.deps.Chain.StateGetActor(ctx, addr, types.EmptyTSK)
	if err != nil {
		return minerBitfields{}, xerrors.Errorf("failed to load actor: %w", err)
	}

	mas, err := miner.Load(stor, act)
	if err != nil {
		return minerBitfields{}, xerrors.Errorf("failed to load miner actor: %w", err)
	}

	activeSectors, err := miner.AllPartSectors(mas, miner.Partition.ActiveSectors)
	if err != nil {
		return minerBitfields{}, xerrors.Errorf("failed to load active sectors: %w", err)
	}

	allSectors, err := miner.AllPartSectors(mas, miner.Partition.AllSectors)
	if err != nil {
		return minerBitfields{}, xerrors.Errorf("failed to load all sectors: %w", err)
	}

	unproved, err := miner.AllPartSectors(mas, miner.Partition.UnprovenSectors)
	if err != nil {
		return minerBitfields{}, xerrors.Errorf("failed to load unproven sectors: %w", err)
	}

	faulty, err := miner.AllPartSectors(mas, miner.Partition.FaultySectors)
	if err != nil {
		return minerBitfields{}, xerrors.Errorf("failed to load faulty sectors: %w", err)
	}

	alloc, err := mas.GetAllocatedSectors()
	if err != nil {
		return minerBitfields{}, xerrors.Errorf("failed to load allocated sectors: %w", err)
	}

	return minerBitfields{
		alloc:     *alloc,
		sectorSet: allSectors,
		active:    activeSectors,
		unproven:  unproved,
		faulty:    faulty,
	}, nil
}

type PorepPipelineSummary struct {
	Actor string

	CountSDR          int
	CountTrees        int
	CountPrecommitMsg int
	CountWaitSeed     int
	CountPoRep        int
	CountCommitMsg    int
	CountDone         int
	CountFailed       int
}

func (a *WebRPC) PorepPipelineSummary(ctx context.Context) ([]PorepPipelineSummary, error) {

	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := a.deps.DB.Query(ctx, `
	SELECT 
		sp_id,
		COUNT(*) FILTER (WHERE after_sdr = false) as CountSDR,
		COUNT(*) FILTER (WHERE (after_tree_d = false OR after_tree_c = false OR after_tree_r = false) AND after_sdr = true) as CountTrees,
		COUNT(*) FILTER (WHERE after_tree_r = true and after_precommit_msg = false) as CountPrecommitMsg,
		COUNT(*) FILTER (WHERE after_precommit_msg_success = true AND seed_epoch > $1) as CountWaitSeed,
		COUNT(*) FILTER (WHERE after_porep = false AND after_precommit_msg_success = true) as CountPoRep,
		COUNT(*) FILTER (WHERE after_commit_msg_success = false AND after_porep = true) as CountCommitMsg,
		COUNT(*) FILTER (WHERE after_commit_msg_success = true) as CountDone,
		COUNT(*) FILTER (WHERE failed = true) as CountFailed
	FROM 
		sectors_sdr_pipeline
	GROUP BY sp_id`, head.Height())
	if err != nil {
		return nil, xerrors.Errorf("query: %w", err)
	}
	defer rows.Close()

	var summaries []PorepPipelineSummary
	for rows.Next() {
		var summary PorepPipelineSummary
		var actor int64
		if err := rows.Scan(&actor, &summary.CountSDR, &summary.CountTrees, &summary.CountPrecommitMsg, &summary.CountWaitSeed, &summary.CountPoRep, &summary.CountCommitMsg, &summary.CountDone, &summary.CountFailed); err != nil {
			return nil, xerrors.Errorf("scan: %w", err)
		}

		sactor, err := address.NewIDAddress(uint64(actor))
		if err != nil {
			return nil, xerrors.Errorf("failed to create actor address: %w", err)
		}

		summary.Actor = sactor.String()

		summaries = append(summaries, summary)
	}
	return summaries, nil
}
