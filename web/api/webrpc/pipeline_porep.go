package webrpc

import (
	"context"
	"time"

	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

type PipelineTask struct {
	// Cache line 1 (bytes 0-64): Hot path - identification
	SpID         int64     `db:"sp_id"`         // 8 bytes (0-8)
	SectorNumber int64     `db:"sector_number"` // 8 bytes (8-16)
	CreateTime   time.Time `db:"create_time"`   // 24 bytes (16-40)
	Failed       bool      `db:"failed"`        // 1 byte (40-41) - checked early
	// Early stage task IDs (checked together)
	TaskSDR    NullInt64 `db:"task_id_sdr"` // 16 bytes (41-57, with padding)
	AfterSDR   bool      `db:"after_sdr"`   // 1 byte
	StartedSDR bool      `db:"started_sdr"` // 1 byte
	// Cache line 2 (bytes 64-128): Tree stages (accessed together)
	TaskTreeD     NullInt64  `db:"task_id_tree_d"`  // 16 bytes
	TreeD         NullString `db:"tree_d_cid"`      // 24 bytes
	TaskTreeC     NullInt64  `db:"task_id_tree_c"`  // 16 bytes
	AfterTreeD    bool       `db:"after_tree_d"`    // 1 byte
	StartedTreeD  bool       `db:"started_tree_d"`  // 1 byte
	AfterTreeC    bool       `db:"after_tree_c"`    // 1 byte
	StartedTreeRC bool       `db:"started_tree_rc"` // 1 byte
	// Cache line 3 (bytes 128-192): TreeR and Synthetic stages
	TaskTreeR        NullInt64  `db:"task_id_tree_r"`    // 16 bytes
	TreeR            NullString `db:"tree_r_cid"`        // 24 bytes
	TaskSynthetic    NullInt64  `db:"task_id_synth"`     // 16 bytes
	AfterTreeR       bool       `db:"after_tree_r"`      // 1 byte
	AfterSynthetic   bool       `db:"after_synth"`       // 1 byte
	StartedSynthetic bool       `db:"started_synthetic"` // 1 byte
	// Cache line 4 (bytes 192-256): PreCommit stage
	PreCommitReadyAt         NullTime  `db:"precommit_ready_at"`          // 32 bytes
	TaskPrecommitMsg         NullInt64 `db:"task_id_precommit_msg"`       // 16 bytes
	AfterPrecommitMsg        bool      `db:"after_precommit_msg"`         // 1 byte
	StartedPrecommitMsg      bool      `db:"started_precommit_msg"`       // 1 byte
	AfterPrecommitMsgSuccess bool      `db:"after_precommit_msg_success"` // 1 byte
	// Cache line 5 (bytes 256-320): PreCommit CID and SeedEpoch
	PreCommitMsgCid NullString `db:"precommit_msg_cid"` // 24 bytes
	SeedEpoch       NullInt64  `db:"seed_epoch"`        // 16 bytes
	// PoRep stage (accessed together)
	TaskPoRep    NullInt64 `db:"task_id_porep"` // 16 bytes
	AfterPoRep   bool      `db:"after_porep"`   // 1 byte
	StartedPoRep bool      `db:"started_porep"` // 1 byte
	// Cache line 6 (bytes 320-384): Finalize and MoveStorage stages
	TaskFinalize       NullInt64 `db:"task_id_finalize"`     // 16 bytes
	TaskMoveStorage    NullInt64 `db:"task_id_move_storage"` // 16 bytes
	AfterFinalize      bool      `db:"after_finalize"`       // 1 byte
	StartedFinalize    bool      `db:"started_finalize"`     // 1 byte
	AfterMoveStorage   bool      `db:"after_move_storage"`   // 1 byte
	StartedMoveStorage bool      `db:"started_move_storage"` // 1 byte
	// Commit stage (accessed together)
	CommitReadyAt NullTime `db:"commit_ready_at"` // 32 bytes
	// Cache line 7 (bytes 384-448): Commit message stage
	TaskCommitMsg         NullInt64  `db:"task_id_commit_msg"`       // 16 bytes
	CommitMsgCid          NullString `db:"commit_msg_cid"`           // 24 bytes
	AfterCommitMsg        bool       `db:"after_commit_msg"`         // 1 byte
	StartedCommitMsg      bool       `db:"started_commit_msg"`       // 1 byte
	AfterCommitMsgSuccess bool       `db:"after_commit_msg_success"` // 1 byte
	// Larger fields at end (rarely accessed or only when needed)
	FailedReason string  `db:"failed_reason"` // 16 bytes - only used when Failed=true
	PoRepProof   []byte  `db:"porep_proof"`   // 24 bytes - only used in PoRep stage
	MissingTasks []int64 `db:"missing_tasks"` // 24 bytes - computed field
	AllTasks     []int64 `db:"all_tasks"`     // 24 bytes - computed field
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
												sp.sp_id, 
												sp.sector_number,
												sp.create_time,
												sp.task_id_sdr, 
												sp.after_sdr,
												sp.task_id_tree_d, 
												sp.after_tree_d,
												sp.task_id_tree_c, 
												sp.after_tree_c,
												sp.task_id_tree_r, 
												sp.after_tree_r,
												sp.task_id_synth, 
												sp.after_synth,
												sp.precommit_ready_at,
												sp.task_id_precommit_msg, 
												sp.after_precommit_msg,
												sp.after_precommit_msg_success, 
												sp.seed_epoch,
												sp.task_id_porep, 
												sp.porep_proof, 
												sp.after_porep,
												sp.task_id_finalize, 
												sp.after_finalize,
												sp.task_id_move_storage, 
												sp.after_move_storage,
												sp.commit_ready_at,
												sp.task_id_commit_msg, 
												sp.after_commit_msg,
												sp.after_commit_msg_success,
												sp.failed, 
												sp.failed_reason,
											
												-- Compute StartedSDR
												CASE 
													WHEN NOT after_tree_d AND task_id_sdr IS NOT NULL THEN
														EXISTS (
															SELECT 1 
															FROM harmony_task 
															WHERE id = task_id_sdr AND owner_id > 0
														)
													ELSE FALSE 
												END AS started_sdr,
											
												-- Compute StartedTreeD
												CASE 
													WHEN after_sdr AND NOT after_tree_d AND task_id_tree_d IS NOT NULL THEN
														EXISTS (
															SELECT 1 
															FROM harmony_task 
															WHERE id = task_id_tree_d AND owner_id > 0
														)
													ELSE FALSE 
												END AS started_tree_d,
											
												-- Compute StartedTreeRC
												CASE 
													WHEN after_tree_d AND NOT after_tree_c AND task_id_tree_c IS NOT NULL THEN
														EXISTS (
															SELECT 1 
															FROM harmony_task 
															WHERE id = task_id_tree_c AND owner_id > 0
														)
													ELSE FALSE 
												END AS started_tree_rc,
											
												-- Compute StartedSynthetic
												CASE 
													WHEN after_tree_c AND NOT after_synth AND task_id_synth IS NOT NULL THEN
														EXISTS (
															SELECT 1 
															FROM harmony_task 
															WHERE id = task_id_synth AND owner_id > 0
														)
													ELSE FALSE 
												END AS started_synthetic,
											
												-- Compute StartedPrecommitMsg
												CASE 
													WHEN after_synth AND NOT after_precommit_msg AND task_id_precommit_msg IS NOT NULL THEN
														EXISTS (
															SELECT 1 
															FROM harmony_task 
															WHERE id = task_id_precommit_msg AND owner_id > 0
														)
													ELSE FALSE 
												END AS started_precommit_msg,
											
												-- Compute StartedPoRep
												CASE 
													WHEN after_precommit_msg AND NOT after_porep AND task_id_porep IS NOT NULL THEN
														EXISTS (
															SELECT 1 
															FROM harmony_task 
															WHERE id = task_id_porep AND owner_id > 0
														)
													ELSE FALSE 
												END AS started_porep,
											
												-- Compute StartedFinalize
												CASE 
													WHEN after_porep AND NOT after_finalize AND task_id_finalize IS NOT NULL THEN
														EXISTS (
															SELECT 1 
															FROM harmony_task 
															WHERE id = task_id_finalize AND owner_id > 0
														)
													ELSE FALSE 
												END AS started_finalize,
											
												-- Compute StartedCommitMsg
												CASE 
													WHEN after_porep AND NOT after_commit_msg AND task_id_commit_msg IS NOT NULL THEN
														EXISTS (
															SELECT 1 
															FROM harmony_task 
															WHERE id = task_id_commit_msg AND owner_id > 0
														)
													ELSE FALSE 
												END AS started_commit_msg,
											
												-- Compute StartedMoveStorage
												CASE 
													WHEN after_finalize AND NOT after_move_storage AND task_id_move_storage IS NOT NULL THEN
														EXISTS (
															SELECT 1 
															FROM harmony_task 
															WHERE id = task_id_move_storage AND owner_id > 0
														)
													ELSE FALSE 
												END AS started_move_storage,
    
												-- Collect all task IDs into an array without NULLs
												(
													SELECT array_agg(task_id)
													FROM (
														VALUES
															(sp.task_id_sdr),
															(sp.task_id_tree_d),
															(sp.task_id_tree_c),
															(sp.task_id_tree_r),
															(sp.task_id_synth),
															(sp.task_id_precommit_msg),
															(sp.task_id_porep),
															(sp.task_id_finalize),
															(sp.task_id_move_storage),
															(sp.task_id_commit_msg)
													) AS t(task_id)
													WHERE task_id IS NOT NULL
												) AS all_tasks,
											
												-- Compute missing tasks without NULLs
												(
													SELECT array_agg(task_id)
													FROM (
														SELECT task_id
														FROM unnest(ARRAY[
															sp.task_id_sdr,
															sp.task_id_tree_d,
															sp.task_id_tree_c,
															sp.task_id_tree_r,
															sp.task_id_synth,
															sp.task_id_precommit_msg,
															sp.task_id_porep,
															sp.task_id_finalize,
															sp.task_id_move_storage,
															sp.task_id_commit_msg
														]) AS task_id
														LEFT JOIN harmony_task ht ON ht.id = task_id
														WHERE task_id IS NOT NULL AND ht.id IS NULL
													) AS missing
												) AS missing_tasks
											
											FROM sectors_sdr_pipeline sp
											ORDER BY sp_id, sector_number;
											`) // todo where constrain list
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

		afterSeed := task.SeedEpoch.Valid && task.SeedEpoch.Int64 <= int64(epoch)

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
		COUNT(*) FILTER (WHERE after_porep = false AND after_precommit_msg_success = true AND seed_epoch < $1) as CountPoRep,
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

func (a *WebRPC) PipelinePorepRestartAll(ctx context.Context) error {
	missing, err := a.pipelinePorepMissingTasks(ctx)
	if err != nil {
		return err
	}

	for _, mt := range missing {
		if len(mt.AllTaskIDs) != len(mt.MissingTaskIDs) || len(mt.MissingTaskIDs) == 0 {
			continue
		}

		log.Infow("Restarting sector", "sector", mt.sectorID(), "missing_tasks", mt.MissingTasksCount)

		if err := a.SectorResume(ctx, mt.SpID, mt.SectorNumber); err != nil {
			return err
		}
	}
	return nil
}

type porepMissingTask struct {
	SpID         int64 `db:"sp_id"`
	SectorNumber int64 `db:"sector_number"`

	AllTaskIDs        []int64 `db:"all_task_ids"`
	MissingTaskIDs    []int64 `db:"missing_task_ids"`
	TotalTasks        int     `db:"total_tasks"`
	MissingTasksCount int     `db:"missing_tasks_count"`
	RestartStatus     string  `db:"restart_status"`
}

func (pmt porepMissingTask) sectorID() abi.SectorID {
	return abi.SectorID{Miner: abi.ActorID(pmt.SpID), Number: abi.SectorNumber(pmt.SectorNumber)}
}

func (a *WebRPC) pipelinePorepMissingTasks(ctx context.Context) ([]porepMissingTask, error) {
	var tasks []porepMissingTask
	err := a.deps.DB.Select(ctx, &tasks, `
		WITH sector_tasks AS (
			SELECT
				sp.sp_id,
				sp.sector_number,
				get_sdr_pipeline_tasks(sp.sp_id, sp.sector_number) AS task_ids
			FROM
				sectors_sdr_pipeline sp
		),
			 missing_tasks AS (
				 SELECT
					 st.sp_id,
					 st.sector_number,
					 st.task_ids,
					 array_agg(CASE WHEN ht.id IS NULL THEN task_id ELSE NULL END) AS missing_task_ids
				 FROM
					 sector_tasks st
						 CROSS JOIN UNNEST(st.task_ids) WITH ORDINALITY AS t(task_id, task_order)
						 LEFT JOIN harmony_task ht ON ht.id = task_id
				 GROUP BY
					 st.sp_id, st.sector_number, st.task_ids
			 )
		SELECT
			mt.sp_id,
			mt.sector_number,
			mt.task_ids AS all_task_ids,
			mt.missing_task_ids,
			array_length(mt.task_ids, 1) AS total_tasks,
			array_length(mt.missing_task_ids, 1) AS missing_tasks_count,
			CASE
				WHEN array_length(mt.task_ids, 1) = array_length(mt.missing_task_ids, 1) THEN 'All tasks missing'
				ELSE 'Some tasks missing'
				END AS restart_status
		FROM
			missing_tasks mt
		WHERE
			array_length(mt.task_ids, 1) > 0 -- Has at least one task
		  AND array_length(array_remove(mt.missing_task_ids, NULL), 1) > 0 -- At least one task is missing
		ORDER BY
			mt.sp_id, mt.sector_number;`)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch missing tasks: %w", err)
	}

	return tasks, nil
}
