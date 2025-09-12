package proofshare

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// List of tasks that are considered "priority"
// When all machines in the cluster are responsible for PSProve AND those tasks
// ProofShare asks will be suspended until the priority tasks get assigned to machines
var PrioTasks = []string{
	"PoRep",
	"UpdateProve",
	"WdPost",
	"WinPost",
	"TreeRC",
}

const PSProveTask = "PSProve"

// GetMinimumConflictSet returns a slice of task names from PrioTasks which are NOT supported by any
// machine that can run PSProve. The intention is to identify which task types might starve when
// every PSProve-capable machine is busy proving, so that scheduling logic can decide if it should
// temporarily pause ProofShare related work until those task types are picked up.
//
// The implementation looks at harmony_machine_details.tasks – a list of task
// names supported by each machine. If at least one PSProve-capable machine advertises support for
// a priority task, that task is *removed* from the returned conflict set because it can already be
// handled by the same pool of machines that do PSProve.
func GetMinimumConflictSet(ctx context.Context, db *harmonydb.DB) []string {
	// Fetch task-lists for all machines
	type machineTaskRow struct {
		Tasks string `db:"tasks"`
	}

	var rows []machineTaskRow
	err := db.Select(ctx, &rows, `SELECT tasks FROM harmony_machine_details WHERE tasks ILIKE '%' || $1 || '%'`, PSProveTask)
	if err != nil {
		log.Errorw("GetMinimumConflictSet failed to query machine details", "error", err)
		return PrioTasks // return all priority tasks as conflicts on error
	}

	// Build support counters for each priority task
	supportCount := make(map[string]int, len(PrioTasks))
	totalPSMachines := 0

	for i, r := range rows {
		tasks := strings.Split(r.Tasks, ",")

		if !slices.Contains(tasks, PSProveTask) {
			// not supposed to happen
			log.Warnw("GetMinimumConflictSet skipping non-PSProve machine", "machineIndex", i, "tasks", tasks)
			continue // not PSProve-capable, ignore
		}
		totalPSMachines++

		for _, pt := range PrioTasks {
			if slices.Contains(tasks, pt) {
				supportCount[pt]++
			}
		}
	}

	// Tasks to keep are those supported by ALL PSProve machines.
	var remaining []string
	for _, pt := range PrioTasks {
		if supportCount[pt] == totalPSMachines { // supported everywhere
			remaining = append(remaining, pt)
		}
	}

	log.Infow("GetMinimumConflictSet result", "conflictSet", remaining, "conflictSetSize", len(remaining), "totalPSMachines", totalPSMachines, "supportCount", supportCount)
	return remaining
}

// ShouldHoldProofShare decides whether ProofShare (PSProve) work should be held off. It returns
// true when there are still unassigned tasks in the minimum conflict set calculated above.
// Unassigned means a row in harmony_task with owner_id IS NULL.
func ShouldHoldProofShare(ctx context.Context, db *harmonydb.DB) bool {
	conflictSet := GetMinimumConflictSet(ctx, db)

	if len(conflictSet) == 0 {
		// No conflicting task types – safe to proceed
		return false
	}

	// Check if any task in the conflict set is currently waiting for an owner
	for _, taskName := range conflictSet {
		var dummy int
		err := db.QueryRow(ctx, `SELECT 1 FROM harmony_task WHERE name = $1 AND owner_id IS NULL LIMIT 1`, taskName).Scan(&dummy)
		if err == nil {
			log.Infow("ShouldHoldProofShare", "taskName", taskName, "dummy", dummy)
			return true // at least one unassigned task
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Errorw("ShouldHoldProofShare", "taskName", taskName, "err", err)
			// On query failure play safe and hold ProofShare
			return true
		}
	}

	return false
}
