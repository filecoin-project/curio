package expmgr

import (
	"context"
	"time"

	logging "github.com/ipfs/go-log/v2"
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

const ExpMgrInterval = 100 * time.Minute

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

func (e *ExpMgrTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	log.Infow("starting expiration manager task", "task_id", taskID)

	// Get the last metadata refresh time
	var lastRefresh struct {
		LastRefreshAt *time.Time `db:"last_refresh_at"`
	}
	err = e.db.QueryRow(ctx, `SELECT last_refresh_at FROM sectors_meta_updates LIMIT 1`).Scan(&lastRefresh.LastRefreshAt)
	if err != nil && err.Error() != "no rows in result set" {
		return false, xerrors.Errorf("getting last metadata refresh: %w", err)
	}

	// Query enabled SP/preset combinations
	// Only process entries where:
	// - enabled = true
	// - last_message_cid is NULL OR last_message_landed_at is NOT NULL (previous message completed)
	// - last_message_landed_at < sectors_meta_updates.last_refresh_at OR last_message_cid is NULL (metadata is fresh)
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

	if lastRefresh.LastRefreshAt != nil {
		err = e.db.Select(ctx, &assignments, `
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
			WHERE sp.enabled = true
			  AND (sp.last_message_cid IS NULL OR sp.last_message_landed_at IS NOT NULL)
			  AND (sp.last_message_landed_at IS NULL OR sp.last_message_landed_at < $1)`,
			lastRefresh.LastRefreshAt)
	} else {
		err = e.db.Select(ctx, &assignments, `
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
			WHERE sp.enabled = true
			  AND (sp.last_message_cid IS NULL OR sp.last_message_landed_at IS NOT NULL)`)
	}

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

	// Process each assignment
	for _, assignment := range assignments {
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
		switch assignment.ActionType {
		case "extend":
			if assignment.TargetExpirationDays == nil || assignment.MaxCandidateDays == nil {
				log.Errorw("extend preset missing required fields",
					"sp_id", assignment.SpID,
					"preset", assignment.PresetName)
				continue
			}

			err = e.handleExtend(ctx, extendPresetConfig{
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

			err = e.handleTopUp(ctx, topUpPresetConfig{
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
	}

	log.Infow("completed expiration manager task", "task_id", taskID)
	return true, nil
}

func (e *ExpMgrTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (e *ExpMgrTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if len(ids) > 0 {
		return &ids[0], nil
	}
	return nil, nil
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
