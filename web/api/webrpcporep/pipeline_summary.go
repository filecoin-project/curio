package webrpcporep

import "time"

// PoRepSectorCounts counts pipeline sectors, not tasks or overlapping stages.
// Running means a current SDR Do-entry record, NOT proof of native progress.
// Complete uses pipeline GC's commit-confirmed + storage-moved boundary.
// PostSDR excludes failed and complete rows, but includes Finalize/MoveStorage
// still pending after Commit success. None of these counters query the chain.
type PoRepSectorCounts struct {
	ObservedAt       time.Time
	Total            int64
	Complete         int64
	Remaining        int64
	Failed           int64 // Remaining rows marked failed, at any stage.
	PostSDR          int64 // Non-failed, after_sdr, not complete.
	SDRTotal         int64 // All after_sdr=false rows, including exceptions below.
	SDRRunning       int64
	SDRPreparing     int64
	SDRWaitingTask   int64 // Existing, unowned SDR task (including retry wait).
	SDRWaitingCreate int64 // No task_id_sdr yet; not evidence of failure.
	SDRMissingTask   int64 // Non-NULL link to a missing task.
	SDROtherTask     int64 // Includes SDRKeyRegen and SupraSeal Batch tasks.
	SDRFailed        int64
	SDRUnknown       int64
}

// Every JOIN is to a primary key: one input/output row per (sp_id, sector_number).
// No history join, task-list limit, seed grouping, or frontend filter affects
// these counts. SDR CASE order is the classification contract (see SQL tests).
// The 2-minute heartbeat freshness display threshold matches Overview machines;
// it is not a task lease, cancellation, or native-liveness guarantee. Current
// ownership/attempt provenance is required; legacy/backfilled times stay unknown.
const porepSummaryQuery = `
WITH classified AS (
 SELECT p.sp_id, p.after_sdr, p.after_tree_d, p.after_tree_c, p.after_tree_r,
   p.after_precommit_msg, p.after_precommit_msg_success, p.seed_epoch,
   p.after_porep, p.after_commit_msg_success, p.failed,
   (after_sdr AND after_commit_msg_success AND after_move_storage) AS complete,
   CASE
    WHEN after_sdr THEN NULL
    WHEN failed THEN 'failed'
    WHEN after_tree_c OR after_tree_r OR after_precommit_msg OR after_precommit_msg_success
      OR after_porep OR after_commit_msg OR after_commit_msg_success OR after_finalize OR after_move_storage THEN 'unknown'
    WHEN task_id_sdr IS NULL THEN 'waiting_create'
    WHEN t.id IS NULL THEN 'missing_task'
    WHEN t.name <> 'SDR' THEN 'other_task'
    WHEN t.owner_id IS NULL AND t.attempt_started_at IS NULL AND t.attempt_id IS NULL
      AND (t.attempt_start_source IS NULL OR t.attempt_start_source = 'claimed') THEN 'waiting_task'
    WHEN t.owner_id IS NULL OR m.id IS NULL OR m.last_contact < statement_timestamp() - INTERVAL '2 minutes'
      OR m.last_contact > statement_timestamp()
      OR t.work_start IS NULL OR t.work_start_source IS DISTINCT FROM 'claim'
      OR t.work_start > statement_timestamp() THEN 'unknown'
    WHEN t.attempt_start_source = 'claimed' AND t.attempt_id IS NULL AND t.attempt_started_at IS NULL THEN 'preparing'
    WHEN t.attempt_start_source = 'prepared' AND NULLIF(t.attempt_id, '') IS NOT NULL AND t.attempt_started_at IS NULL THEN 'preparing'
    WHEN t.attempt_start_source = 'do_entry' AND NULLIF(t.attempt_id, '') IS NOT NULL
      AND t.attempt_started_at >= t.work_start AND t.attempt_started_at <= statement_timestamp() THEN 'running'
    ELSE 'unknown'
   END AS sdr_state
 FROM sectors_sdr_pipeline p
 LEFT JOIN harmony_task t ON t.id = p.task_id_sdr
 LEFT JOIN harmony_machines m ON m.id = t.owner_id
)
SELECT sp_id,
 COUNT(*) FILTER (WHERE after_sdr = false),
 COUNT(*) FILTER (WHERE (after_tree_d = false OR after_tree_c = false OR after_tree_r = false) AND after_sdr = true),
 COUNT(*) FILTER (WHERE after_tree_r = true AND after_precommit_msg = false),
 COUNT(*) FILTER (WHERE after_precommit_msg_success = true AND seed_epoch > $1),
 COUNT(*) FILTER (WHERE after_porep = false AND after_precommit_msg_success = true AND seed_epoch < $1),
 COUNT(*) FILTER (WHERE after_commit_msg_success = false AND after_porep = true),
 COUNT(*) FILTER (WHERE after_commit_msg_success = true),
 COUNT(*) FILTER (WHERE failed = true),
 statement_timestamp(), COUNT(*),
 COUNT(*) FILTER (WHERE complete), COUNT(*) FILTER (WHERE NOT complete),
 COUNT(*) FILTER (WHERE NOT complete AND failed),
 COUNT(*) FILTER (WHERE NOT complete AND NOT failed AND after_sdr),
 COUNT(*) FILTER (WHERE NOT after_sdr),
 COUNT(*) FILTER (WHERE sdr_state = 'running'),
 COUNT(*) FILTER (WHERE sdr_state = 'preparing'),
 COUNT(*) FILTER (WHERE sdr_state = 'waiting_task'),
 COUNT(*) FILTER (WHERE sdr_state = 'waiting_create'),
 COUNT(*) FILTER (WHERE sdr_state = 'missing_task'),
 COUNT(*) FILTER (WHERE sdr_state = 'other_task'),
 COUNT(*) FILTER (WHERE sdr_state = 'failed'),
 COUNT(*) FILTER (WHERE sdr_state = 'unknown')
FROM classified GROUP BY sp_id ORDER BY sp_id`
