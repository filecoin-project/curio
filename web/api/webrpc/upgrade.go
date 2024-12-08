package webrpc

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/tasks/snap"
)

type UpgradeSector struct {
	StartTime time.Time `db:"start_time"`

	SpID      uint64 `db:"sp_id"`
	SectorNum uint64 `db:"sector_number"`

	TaskIDEncode *uint64 `db:"task_id_encode"`
	AfterEncode  bool    `db:"after_encode"`

	TaskIDProve *uint64 `db:"task_id_prove"`
	AfterProve  bool    `db:"after_prove"`

	TaskIDSubmit *uint64 `db:"task_id_submit"`
	AfterSubmit  bool    `db:"after_submit"`

	AfterProveSuccess bool `db:"after_prove_msg_success"`

	TaskIDMoveStorage *uint64 `db:"task_id_move_storage"`
	AfterMoveStorage  bool    `db:"after_move_storage"`

	Failed       bool   `db:"failed"`
	FailedReason string `db:"failed_reason"`
	FailedMsg    string `db:"failed_reason_msg"`

	MissingTasks []int64 `db:"-"`
	AllTasks     []int64 `db:"-"`

	Miner string
}

func (a *WebRPC) UpgradeSectors(ctx context.Context) ([]*UpgradeSector, error) {
	sectors := []*UpgradeSector{}
	err := a.deps.DB.Select(ctx, &sectors, `SELECT start_time, sp_id, sector_number, task_id_encode, after_encode, task_id_prove, after_prove, task_id_submit, after_submit, after_prove_msg_success, task_id_move_storage, after_move_storage, failed, failed_reason, failed_reason_msg FROM sectors_snap_pipeline`)
	if err != nil {
		return nil, err
	}

	smt, err := a.pipelineSnapMissingTasks(ctx)
	if err != nil {
		return nil, err
	}

	for _, s := range sectors {
		maddr, err := address.NewIDAddress(s.SpID)
		if err != nil {
			return nil, err
		}
		s.Miner = maddr.String()

		for _, mt := range smt {
			if mt.SpID == int64(s.SpID) && mt.SectorNumber == int64(s.SectorNum) {
				s.MissingTasks = mt.MissingTaskIDs
				s.AllTasks = mt.AllTaskIDs
				break
			}
		}
	}

	return sectors, nil
}

func (a *WebRPC) UpgradeResetTaskIDs(ctx context.Context, spid, sectorNum int64) error {
	_, err := a.deps.DB.Exec(ctx, `SELECT unset_task_id_snap($1, $2)`, spid, sectorNum)
	return err
}

func (a *WebRPC) UpgradeDelete(ctx context.Context, spid, sectorNum uint64) error {
	if err := snap.DropSectorPieceRefsSnap(ctx, a.deps.DB, abi.SectorID{Miner: abi.ActorID(spid), Number: abi.SectorNumber(sectorNum)}); err != nil {
		// bad, but still do best we can and continue
		log.Errorw("failed to drop sector piece refs", "error", err)
	}

	_, err := a.deps.DB.Exec(ctx, `DELETE FROM sectors_snap_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, sectorNum)
	return err
}

type SnapMissingTask struct {
	SpID              int64   `db:"sp_id"`
	SectorNumber      int64   `db:"sector_number"`
	AllTaskIDs        []int64 `db:"all_task_ids"`
	MissingTaskIDs    []int64 `db:"missing_task_ids"`
	TotalTasks        int     `db:"total_tasks"`
	MissingTasksCount int     `db:"missing_tasks_count"`
	RestartStatus     string  `db:"restart_status"`
}

func (smt SnapMissingTask) sectorID() abi.SectorID {
	return abi.SectorID{Miner: abi.ActorID(smt.SpID), Number: abi.SectorNumber(smt.SectorNumber)}
}

func (a *WebRPC) pipelineSnapMissingTasks(ctx context.Context) ([]SnapMissingTask, error) {
	var tasks []SnapMissingTask
	err := a.deps.DB.Select(ctx, &tasks, `
        WITH sector_tasks AS (
            SELECT
                sp.sp_id,
                sp.sector_number,
                get_snap_pipeline_tasks(sp.sp_id, sp.sector_number) AS task_ids
            FROM
                sectors_snap_pipeline sp
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
		return nil, xerrors.Errorf("failed to fetch missing SNAP tasks: %w", err)
	}

	return tasks, nil
}

func (a *WebRPC) PipelineSnapRestartAll(ctx context.Context) error {
	missing, err := a.pipelineSnapMissingTasks(ctx)
	if err != nil {
		return err
	}

	for _, mt := range missing {
		if len(mt.AllTaskIDs) != len(mt.MissingTaskIDs) || len(mt.MissingTaskIDs) == 0 {
			continue
		}

		log.Infow("Restarting SNAP sector", "sector", mt.sectorID(), "missing_tasks", mt.MissingTasksCount)

		if err := a.UpgradeResetTaskIDs(ctx, mt.SpID, mt.SectorNumber); err != nil {
			return err
		}
	}
	return nil
}
