package harmonytask

import (
	"sort"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
)

const costTimeout = 200 * time.Millisecond
const preemptTaskKillTimeout = 500 * time.Millisecond

type preemptCandidate struct {
	handler *taskTypeHandler
	taskID  TaskID
	runtime time.Duration
}

type preemptionPlan struct {
	totalCost  time.Duration
	candidates []preemptCandidate
}

// computePreemptionPlan finds the minimum-cost set of non-time-sensitive running
// tasks to kill in order to free enough resources for `needed`.
//
// Algorithm:
//  1. Sort candidates youngest-first (shortest runtime = cheapest).
//  2. Accumulate from youngest until all resource deficits are covered.
//  3. Walk backwards through the selected set (oldest to youngest) and remove
//     any candidate whose resources are not required to cover the deficit.
func (e *TaskEngine) computePreemptionPlan(needed resources.Resources) *preemptionPlan {
	available := e.ResourcesAvailable()

	cpuDeficit := needed.Cpu - available.Cpu
	ramDeficit := int64(needed.Ram) - int64(available.Ram)
	gpuDeficit := needed.Gpu - available.Gpu

	if cpuDeficit <= 0 && ramDeficit <= 0 && gpuDeficit <= 0 {
		return nil
	}

	var allCandidates []preemptCandidate
	now := time.Now()
	for _, h := range e.handlers {
		if h.TimeSensitive {
			continue
		}
		for _, entry := range h.running.Snapshot() {
			allCandidates = append(allCandidates, preemptCandidate{
				handler: h,
				taskID:  TaskID(entry.ID),
				runtime: now.Sub(entry.StartTime),
			})
		}
	}

	if len(allCandidates) == 0 {
		return nil
	}

	sort.Slice(allCandidates, func(i, j int) bool {
		return allCandidates[i].runtime < allCandidates[j].runtime
	})

	cutoff := 0
	for i, c := range allCandidates {
		if cpuDeficit <= 0 && ramDeficit <= 0 && gpuDeficit <= 0 {
			break
		}
		cpuDeficit -= c.handler.Cost.Cpu
		ramDeficit -= int64(c.handler.Cost.Ram)
		gpuDeficit -= c.handler.Cost.Gpu
		cutoff = i + 1
	}

	if cpuDeficit > 0 || ramDeficit > 0 || gpuDeficit > 0 {
		return nil
	}

	// Walk oldest->youngest through allCandidates[:cutoff]. Deficit is now
	// negative (surplus). Try adding each candidate's cost back — if deficit
	// stays <= 0 in all dimensions, we can skip it.
	var selected []preemptCandidate
	var totalCost time.Duration

	for i := cutoff - 1; i >= 0; i-- {
		candidate := allCandidates[i]
		c := candidate.handler.Cost
		newCpu := cpuDeficit + c.Cpu
		newRam := ramDeficit + int64(c.Ram)
		newGpu := gpuDeficit + c.Gpu
		if newCpu <= 0 && newRam <= 0 && newGpu <= 0 {
			cpuDeficit = newCpu
			ramDeficit = newRam
			gpuDeficit = newGpu
		} else {
			selected = append(selected, candidate)
			totalCost += candidate.runtime
		}
	}

	return &preemptionPlan{
		totalCost:  totalCost,
		candidates: selected,
	}
}

func (e *TaskEngine) executePreemption(plan *preemptionPlan) {
	handles := make([]*runregistry.Handle, 0, len(plan.candidates))
	for _, c := range plan.candidates {
		handle, ok := c.handler.running.Get(int64(c.taskID))
		if !ok {
			continue
		}
		log.Infow("preempting task", "task", c.handler.Name, "id", c.taskID, "runtime", c.runtime)
		handle.Preempt()
		handles = append(handles, handle)
	}

	deadline := time.After(preemptTaskKillTimeout)
	for _, handle := range handles {
		if exited := handle.WaitDone(deadline); !exited {
			log.Warnw("preempted task did not exit in time")
		}
	}
}

// preemptForTimeSensitive runs in its own goroutine, off the scheduler thread.
// All state it touches is either atomic, mutex-protected (inside internal
// sub-packages), or channel-based. After preemption, it signals the
// scheduler to re-run waterfall.
func (e *TaskEngine) preemptForTimeSensitive(h *taskTypeHandler, tID TaskID) {
	plan := e.computePreemptionPlan(h.Cost)
	if plan == nil {
		log.Debugw("no viable preemption plan", "task", h.Name, "taskID", tID)
		return
	}

	peerCount := e.peering.peers.CountFor(h.Name)

	// Register to receive peer cost responses. The registry pre-loads any
	// responses that arrived before we registered, so we never miss one.
	costCh, cancel := e.state.preemptBids.Register(int64(tID), peerCount+1)
	defer cancel()

	bytes, err := marshalPeerMessage(messageTypePreemptCost, tID, taskOther{TaskType: h.Name, Cost: plan.totalCost})
	if err != nil {
		log.Errorw("failed to marshal preempt cost message", "error", err)
		return
	}
	e.peering.broadcast(h.Name, bytes)

	if peerCount == 0 {
		log.Infow("only worker for time-sensitive task, preempting", "task", h.Name, "taskID", tID, "cost", plan.totalCost)
		e.schedulerChannel <- schedulerEvent{TaskID: tID, TaskType: h.Name, Source: schedulerSourceStartTimeSensitive}
		return
	}

	timer := time.NewTimer(costTimeout)
	defer timer.Stop()

	weAreCheapest := true
	responsesReceived := 0

	for responsesReceived < peerCount {
		select {
		case resp := <-costCh:
			responsesReceived++
			if resp.Cost < plan.totalCost || (resp.Cost == plan.totalCost && resp.PeerID < int64(e.cfg.ownerID)) {
				weAreCheapest = false
			}
		case <-timer.C:
			goto decide
		}
	}

decide:
	if weAreCheapest {
		log.Infow("we are cheapest, preempting for time-sensitive task", "task", h.Name, "taskID", tID, "cost", plan.totalCost)
		e.schedulerChannel <- schedulerEvent{TaskID: tID, TaskType: h.Name, Source: schedulerSourceStartTimeSensitive}
	} else {
		log.Infow("another machine is cheaper, deferring preemption", "task", h.Name, "taskID", tID)
	}
}
