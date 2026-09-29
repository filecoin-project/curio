package harmonytask

import (
	"context"
	"time"
)

// taskCandidateFilter excludes tasks whose backing work is no longer eligible.
// This read-only, bulk check runs before the poll snapshot is capped and again
// before admission (including cache hits and recovery). It is advisory: claim
// and the task's storage/entry checks remain authoritative.
type taskCandidateFilter interface {
	FilterCandidates(context.Context, []TaskID) ([]TaskID, error)
}

const candidateFilterTimeout = 5 * time.Second

func filterTaskCandidates(ctx context.Context, impl TaskInterface, ids []TaskID) ([]TaskID, error) {
	filter, ok := impl.(taskCandidateFilter)
	if !ok || len(ids) == 0 {
		return ids, nil
	}
	ctx, cancel := context.WithTimeout(ctx, candidateFilterTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	eligible, err := filter.FilterCandidates(ctx, ids)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := make(map[TaskID]bool, len(eligible))
	for _, id := range eligible {
		wanted[id] = true
	}
	ordered := make([]TaskID, 0, len(eligible))
	for _, id := range ids {
		if wanted[id] {
			ordered = append(ordered, id)
			delete(wanted, id)
		}
	}
	return ordered, nil
}

// Only eligible tasks consume the snapshot bound. In particular, a prefix of
// more than chokePoint invalid tasks must not hide valid work on every poll.
func filterPolledTasks(ctx context.Context, h *taskTypeHandler, tasks []task) ([]task, error) {
	ids := make([]TaskID, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	eligible, err := filterTaskCandidates(ctx, h.TaskInterface, ids)
	if err != nil {
		return nil, err
	}
	wanted := make(map[TaskID]bool, len(eligible))
	for _, id := range eligible {
		wanted[id] = true
	}
	selected := make([]task, 0, min(chokePoint, len(eligible)))
	for _, t := range tasks {
		if wanted[t.ID] {
			selected = append(selected, t)
			delete(wanted, t.ID)
			if len(selected) == chokePoint {
				break
			}
		}
	}
	return selected, nil
}
