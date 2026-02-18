package expmgr

import (
	"context"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/tasks/message"

	"github.com/filecoin-project/lotus/api"
)

var log = logging.Logger("expmgr")

const ExpMgrInterval = 60 * time.Minute

const MaxExtendsPerMessage = 10_000

type ExpMgrTask struct {
	db     *harmonydb.DB
	chain  api.FullNode
	sender *message.Sender
}

func NewExpMgrTask(db *harmonydb.DB, chain api.FullNode, pcs *chainsched.CurioChainSched, sender *message.Sender) *ExpMgrTask {
	return &ExpMgrTask{
		db:     db,
		chain:  chain,
		sender: sender,
	}
}

func (e *ExpMgrTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {

	log.Infow("starting expiration manager task", "task_id", taskID)

	// Get the last metadata refresh time
	var lastRefresh struct {
		LastRefreshAt *time.Time `db:"last_refresh_at"`
	}
	err = e.db.QueryRow(ctx, `SELECT last_refresh_at FROM sectors_meta_updates LIMIT 1`).Scan(&lastRefresh.LastRefreshAt)
	if err != nil && err != pgx.ErrNoRows {
		return false, xerrors.Errorf("getting last metadata refresh: %w", err)
	}
	if lastRefresh.LastRefreshAt == nil {
		log.Warnw("no last refresh time, skipping expiration manager task", "task_id", taskID)
		return true, nil
	}

	// Query enabled SP/preset combinations with SP-level constraints
	// Only process entries where:
	// - enabled = true
	// - NO preset for this SP has a pending message (last_message_cid IS NOT NULL AND last_message_landed_at IS NULL)
	// - The most recent landed message for this SP is older than the last metadata refresh (or no messages sent yet)
	var assignments []struct {
		SpID       int64  `db:"sp_id"`
		PresetName string `db:"preset_name"`
		ActionType string `db:"action_type"`

		InfoBucketAboveDays     int    `db:"info_bucket_above_days"`
		InfoBucketBelowDays     int    `db:"info_bucket_below_days"`
		TargetExpirationDays    *int64 `db:"target_expiration_days"`
		MaxCandidateDays        *int64 `db:"max_candidate_days"`
		TopUpCountLowWaterMark  *int64 `db:"top_up_count_low_water_mark"`
		TopUpCountHighWaterMark *int64 `db:"top_up_count_high_water_mark"`
		CC                      *bool  `db:"cc"`
		DropClaims              bool   `db:"drop_claims"`
	}

	err = e.db.Select(ctx, &assignments, `
		WITH sp_message_status AS (
			SELECT 
				sp_id,
				MAX(last_message_landed_at) as max_landed_at,
				BOOL_OR(last_message_cid IS NOT NULL AND last_message_landed_at IS NULL) as has_pending_message
			FROM sectors_exp_manager_sp
			WHERE enabled = true
			GROUP BY sp_id
		)
		SELECT 
			sp.sp_id,
			sp.preset_name,
			p.action_type,
			p.info_bucket_above_days,
			p.info_bucket_below_days,
			p.target_expiration_days,
			p.max_candidate_days,
			p.top_up_count_low_water_mark,
			p.top_up_count_high_water_mark,
			p.cc,
			p.drop_claims
		FROM sectors_exp_manager_sp sp
		INNER JOIN sectors_exp_manager_presets p ON sp.preset_name = p.name
		INNER JOIN sp_message_status sms ON sp.sp_id = sms.sp_id
		WHERE sp.enabled = true
		  AND NOT sms.has_pending_message
		  AND (sms.max_landed_at IS NULL OR sms.max_landed_at < $1)
		ORDER BY sp.sp_id, sp.preset_name`,
		lastRefresh.LastRefreshAt)

	if err != nil {
		return false, xerrors.Errorf("querying enabled assignments: %w", err)
	}

	log.Infow("found enabled assignments", "count", len(assignments))

	if len(assignments) == 0 {
		log.Info("no enabled assignments to process")
		return true, nil
	}

	// Get current chain head for condition evaluation
	head, err := e.chain.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting chain head: %w", err)
	}

	// Track which SPs we've processed to ensure we only process one assignment per SP
	// This prevents sending multiple extension messages for the same SP in a single run
	processedSPs := make(map[int64]bool)

	// Process each assignment (already ordered by sp_id, preset_name)
	for _, assignment := range assignments {
		// Skip if we've already processed an assignment for this SP in this run
		if processedSPs[assignment.SpID] {
			log.Infow("skipping assignment, SP already processed in this run",
				"sp_id", assignment.SpID,
				"preset", assignment.PresetName)
			continue
		}

		log.Infow("processing assignment",
			"sp_id", assignment.SpID,
			"preset", assignment.PresetName,
			"action_type", assignment.ActionType)

		// Evaluate the condition using the SQL function
		var needsAction bool
		err = e.db.QueryRow(ctx, `SELECT eval_ext_mgr_sp_condition($1, $2, $3, 2880)`,
			assignment.SpID, assignment.PresetName, int64(head.Height())).Scan(&needsAction)
		if err != nil {
			log.Errorw("failed to evaluate condition",
				"sp_id", assignment.SpID,
				"preset", assignment.PresetName,
				"error", err)
			continue
		}

		if !needsAction {
			log.Infow("condition not met, skipping",
				"sp_id", assignment.SpID,
				"preset", assignment.PresetName,
				"action_type", assignment.ActionType)
			continue
		}

		log.Infow("condition met, processing",
			"sp_id", assignment.SpID,
			"preset", assignment.PresetName,
			"action_type", assignment.ActionType)

		// Dispatch to appropriate handler based on action type
		var processed bool
		switch assignment.ActionType {
		case "extend":
			if assignment.TargetExpirationDays == nil || assignment.MaxCandidateDays == nil {
				log.Errorw("extend preset missing required fields",
					"sp_id", assignment.SpID,
					"preset", assignment.PresetName)
				continue
			}

			processed, err = e.handleExtend(ctx, extendPresetConfig{
				Name:                 assignment.PresetName,
				SpID:                 assignment.SpID,
				InfoBucketAboveDays:  assignment.InfoBucketAboveDays,
				InfoBucketBelowDays:  assignment.InfoBucketBelowDays,
				TargetExpirationDays: *assignment.TargetExpirationDays,
				MaxCandidateDays:     *assignment.MaxCandidateDays,
				CC:                   assignment.CC,
				DropClaims:           assignment.DropClaims,
			})

		case "top_up":
			if assignment.TopUpCountLowWaterMark == nil || assignment.TopUpCountHighWaterMark == nil {
				log.Errorw("top_up preset missing required fields",
					"sp_id", assignment.SpID,
					"preset", assignment.PresetName)
				continue
			}

			processed, err = e.handleTopUp(ctx, topUpPresetConfig{
				Name:                    assignment.PresetName,
				SpID:                    assignment.SpID,
				InfoBucketAboveDays:     assignment.InfoBucketAboveDays,
				InfoBucketBelowDays:     assignment.InfoBucketBelowDays,
				TopUpCountLowWaterMark:  *assignment.TopUpCountLowWaterMark,
				TopUpCountHighWaterMark: *assignment.TopUpCountHighWaterMark,
				CC:                      assignment.CC,
				DropClaims:              assignment.DropClaims,
			})

		default:
			log.Errorw("unknown action type",
				"sp_id", assignment.SpID,
				"preset", assignment.PresetName,
				"action_type", assignment.ActionType)
			continue
		}

		if err != nil {
			log.Errorw("error handling assignment",
				"sp_id", assignment.SpID,
				"preset", assignment.PresetName,
				"action_type", assignment.ActionType,
				"error", err)
			// Continue processing other assignments
			continue
		}

		// Mark this SP as processed to skip any further assignments for it in this run
		processedSPs[assignment.SpID] = processed
		log.Infow("marked SP as processed",
			"sp_id", assignment.SpID,
			"preset", assignment.PresetName)
	}

	log.Infow("completed expiration manager task", "task_id", taskID)
	return true, nil
}

func (e *ExpMgrTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (e *ExpMgrTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (e *ExpMgrTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "ExpMgr",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 10 << 20,
		},
		MaxFailures: 2,
		IAmBored:    harmonytask.SingletonTaskAdder(ExpMgrInterval, e),
	}
}

var _ harmonytask.TaskInterface = &ExpMgrTask{}
var _ = harmonytask.Reg(&ExpMgrTask{})
