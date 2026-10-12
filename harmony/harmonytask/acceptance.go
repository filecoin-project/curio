package harmonytask

import (
	"github.com/samber/lo"
)

const ACCEPT_WORKERS = 4

type acceptRequest struct {
	handler *taskTypeHandler
	ids     []TaskID
	reply   chan acceptResult // Used during recovery, before the scheduler starts.
}

type acceptResult struct {
	ids      []TaskID
	accepted []TaskID
	err      error
}

func (e *TaskEngine) startAcceptWorkers() {
	ti := len(lo.Filter(e.handlers, func(item *taskTypeHandler, index int) bool {
		return !item.TaskInterface.TypeDetails().TimeSensitive
	}))
	ts := len(e.handlers) - ti

	e.acceptWork = make(chan acceptRequest, ti)
	e.acceptSensitiveWork = make(chan acceptRequest, ts)
	for range ACCEPT_WORKERS {
		go func() {
			for {
				select {
				case <-e.cfg.ctx.Done():
					return
				case request := <-e.acceptWork:
					accepted, err := request.handler.CanAccept(request.ids, e)
					result := acceptResult{ids: request.ids, accepted: accepted, err: err}
					if request.reply != nil {
						select {
						case request.reply <- result:
						case <-e.cfg.ctx.Done():
							return
						}
						continue
					}
					select {
					case e.schedulerChannel <- schedulerEvent{
						Source:     schedulerSourceAcceptResult,
						TaskType:   request.handler.Name,
						Acceptance: &result,
					}:
					case <-e.cfg.ctx.Done():
						return
					}
				}
			}
		}()
	}

	if ts > 0 {
		for range ACCEPT_WORKERS {
			go func() {
				for {
					select {
					case <-e.cfg.ctx.Done():
						return
					case request := <-e.acceptSensitiveWork:
						accepted, err := request.handler.CanAccept(request.ids, e)
						result := acceptResult{ids: request.ids, accepted: accepted, err: err}
						if request.reply != nil {
							select {
							case request.reply <- result:
							case <-e.cfg.ctx.Done():
								return
							}
							continue
						}
						select {
						case e.schedulerChannel <- schedulerEvent{
							Source:     schedulerSourceAcceptResult,
							TaskType:   request.handler.Name,
							Acceptance: &result,
						}:
						case <-e.cfg.ctx.Done():
							return
						}
					}
				}
			}()
		}
	}
}

// Don't queue another check for this task type until the scheduler has handled the previous result.
func (h *taskTypeHandler) requestAcceptance(ids []TaskID) {
	if h.awaitingCanAcceptResult || len(ids) == 0 {
		return
	}
	request := acceptRequest{handler: h, ids: append([]TaskID(nil), ids...)}
	if h.TaskInterface.TypeDetails().TimeSensitive {
		select {
		case h.TaskEngine.acceptSensitiveWork <- request:
			h.awaitingCanAcceptResult = true
		default:
		}
	} else {
		select {
		case h.TaskEngine.acceptWork <- request:
			h.awaitingCanAcceptResult = true
		default:
		}
	}
}

// Check waiting tasks after the scheduler has finished trying to start this batch.
// Skip running tasks and wait for preemption negotiations to finish. If any task
// needs a check, submit the whole remaining batch: CanAccept may need several
// tasks together before it accepts any of them.
func (h *taskTypeHandler) requestUncachedAcceptance(ids []TaskID) {
	if len(h.preempting) > 0 {
		return
	}
	remaining := make([]TaskID, 0, len(ids))
	for _, id := range ids {
		if _, running := h.running.Get(int64(id)); !running {
			remaining = append(remaining, id)
		}
	}
	if _, checked := h.accept.GetMatching(toInt64s(remaining)); !checked {
		h.requestAcceptance(remaining)
	}
}

func (h *taskTypeHandler) storeAcceptance(result acceptResult) {
	if result.err != nil {
		log.Errorw("CanAccept failed", "taskType", h.Name, "error", result.err)
		result.accepted = nil
	}
	h.accept.Store(toInt64s(result.ids), toInt64s(result.accepted))
}

// checkRecoveredTask checks a task still owned by this machine after a restart.
// The scheduler has not started and the acceptance cache is empty, so startup
// waits for the worker result here. considerWork then uses that cached result
// to decide whether to resume the task.
func (e *TaskEngine) checkRecoveredTask(h *taskTypeHandler, id TaskID) error {
	if _, err := h.AssertMachineHasCapacity(); err != nil {
		return err
	}
	reply := make(chan acceptResult, 1)
	select {
	case e.acceptWork <- acceptRequest{handler: h, ids: []TaskID{id}, reply: reply}:
	case <-e.cfg.ctx.Done():
		return e.cfg.ctx.Err()
	}
	select {
	case result := <-reply:
		h.storeAcceptance(result)
		return result.err
	case <-e.cfg.ctx.Done():
		return e.cfg.ctx.Err()
	}
}
