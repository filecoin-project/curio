package harmonytask

import (
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/filecoin-project/curio/harmony/resources"
)

const COST_TIMEOUT = 200 * time.Millisecond
const PREEMPT_TASK_KILL_TIMEOUT = 500 * time.Millisecond

type preemptCandidate struct {
	handler *taskTypeHandler
	taskID  TaskID
	runtime time.Duration
}

type preemptCostResponse struct {
	PeerID int64
	Cost   time.Duration
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
		h.runningLk.Lock()
		for id, info := range h.runningTasks {
			allCandidates = append(allCandidates, preemptCandidate{
				handler: h,
				taskID:  id,
				runtime: now.Sub(info.startTime),
			})
		}
		h.runningLk.Unlock()
	}

	if len(allCandidates) == 0 {
		return nil
	}

	// Step 1: sort youngest first (cheapest runtime)
	sort.Slice(allCandidates, func(i, j int) bool {
		return allCandidates[i].runtime < allCandidates[j].runtime
	})

	// Step 2: find the cutoff — subtract from deficit until all dimensions <= 0
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

	// Step 3: walk oldest→youngest through allCandidates[:cutoff].
	// Deficit is now negative (surplus). Try adding each candidate's cost
	// back — if deficit stays <= 0 in all dimensions, we can skip it.
	var selected []preemptCandidate
	var totalCost time.Duration

	for i := cutoff - 1; i >= 0; i-- {
		candidate := allCandidates[i]
		c := candidate.handler.Cost
		newCpu := cpuDeficit + c.Cpu
		newRam := ramDeficit + int64(c.Ram)
		newGpu := gpuDeficit + c.Gpu
		if newCpu <= 0 && newRam <= 0 && newGpu <= 0 { // if we over-claimed, give back.
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
	for _, c := range plan.candidates {
		c.handler.runningLk.Lock()
		info, ok := c.handler.runningTasks[c.taskID]
		c.handler.runningLk.Unlock()
		if !ok {
			continue
		}

		log.Infow("preempting task", "task", c.handler.Name, "id", c.taskID, "runtime", c.runtime)

		info.preempted.Store(true)
		info.cancel()
	}

	deadline := time.After(PREEMPT_TASK_KILL_TIMEOUT)
	for _, c := range plan.candidates {
		c.handler.runningLk.Lock()
		info, ok := c.handler.runningTasks[c.taskID]
		c.handler.runningLk.Unlock()
		if !ok {
			continue
		}
		select {
		case <-info.done:
		case <-deadline:
			log.Warnw("preempted task did not exit in time", "task", c.handler.Name, "id", c.taskID)
		}
	}
}

// preemptForTimeSensitive runs in its own goroutine, off the scheduler thread.
// All state it touches is either atomic, mutex-protected, or channel-based.
// After preemption, it signals the scheduler to re-run waterfall.
func (e *TaskEngine) preemptForTimeSensitive(h *taskTypeHandler, tID TaskID) {
	plan := e.computePreemptionPlan(h.Cost)
	if plan == nil {
		log.Debugw("no viable preemption plan", "task", h.Name, "taskID", tID)
		return
	}

	// Register our cost channel and drain any messages that arrived early
	e.preemptCostMu.Lock()
	if e.preemptCostChs == nil {
		e.preemptCostChs = make(map[TaskID]chan preemptCostResponse)
	}
	costCh := make(chan preemptCostResponse, len(e.peering.peers)+len(e.preemptCostPending[tID]))
	for _, pending := range e.preemptCostPending[tID] {
		costCh <- pending
	}
	delete(e.preemptCostPending, tID)
	e.preemptCostChs[tID] = costCh
	e.preemptCostMu.Unlock()

	defer func() {
		e.preemptCostMu.Lock()
		delete(e.preemptCostChs, tID)
		delete(e.preemptCostPending, tID)
		e.preemptCostMu.Unlock()
	}()

	e.peering.peersLock.RLock()
	peerCount := len(e.peering.m[h.Name])
	e.peering.peersLock.RUnlock()

	e.peering.TellOthersMessage(h.Name, messageRenderPreemptCost{
		TaskType: h.Name,
		TaskID:   tID,
		Cost:     plan.totalCost,
	})

	if peerCount == 0 {
		log.Infow("only worker for time-sensitive task, preempting", "task", h.Name, "taskID", tID, "cost", plan.totalCost)
		e.schedulerChannel <- schedulerEvent{TaskID: tID, TaskType: h.Name, Source: schedulerSourceStartTimeSensitive}
		return
	}

	timer := time.NewTimer(COST_TIMEOUT)
	defer timer.Stop()

	weAreCheapest := true
	responsesReceived := 0

	for responsesReceived < peerCount {
		select {
		case resp := <-costCh:
			responsesReceived++
			if resp.Cost < plan.totalCost || (resp.Cost == plan.totalCost && resp.PeerID < int64(e.ownerID)) {
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

type messageRenderPreemptCost struct {
	TaskType string
	TaskID   TaskID
	Cost     time.Duration
}

func (m messageRenderPreemptCost) Render() []byte {
	return binary.BigEndian.AppendUint64(
		binary.BigEndian.AppendUint64(
			[]byte(fmt.Sprintf("%c:%s:", messageTypePreemptCost, m.TaskType)),
			uint64(m.TaskID)),
		uint64(m.Cost))
}
