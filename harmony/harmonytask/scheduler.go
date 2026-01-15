package harmonytask

import "time"

type schedulerEvent struct {
	TaskID
	TaskType string
	Source   schedulerSource
}

type schedulerSource byte

const (
	schedulerSourceAdded schedulerSource = iota
	schedulerSourcePeer
	schedulerSourcePeerStarted
	schedulerSourcePeerReserved // FUTURE PR: schedulerSourcePeerReserved
)

func (e *TaskEngine) startScheduler() {
	go func() {
		availableTasks := map[string]map[TaskID]bool{} // TaskType -> TaskID -> bool FUTURE PR: mem savings.
		for _, h := range e.handlers {
			availableTasks[h.Name] = make(map[TaskID]bool)
		}
		for {
			select {
			case <-e.ctx.Done():
				return
			case event := <-e.schedulerChannel:
				switch event.Source {
				case schedulerSourceAdded:
					if _, ok := availableTasks[event.TaskType]; ok { // we maybe not run this task.
						availableTasks[event.TaskType][event.TaskID] = true
						// Try to schedule this task or reserve it 1ms after the last one of this type arrives.
					}
					e.peering.TellOthers(event.TaskType, event.TaskID)
				case schedulerSourcePeer:
					availableTasks[event.TaskType][event.TaskID] = true
					// TODO determine if we should run after this task arrives in 5ms (later PR: or reserve.)

				case schedulerSourcePeerStarted: // TODO Emit this somewhere
					delete(availableTasks[event.TaskType], event.TaskID)
					// TODO delete reservation if any
				default:
					log.Warnw("unknown scheduler source", "source", event.Source)
				}
				// TODO reset poll duration?
			case <-time.After(e.pollDuration.Load().(time.Duration)):
				// TODO poll tasks
			}
		}
	}()
} // TODO Move all harmony_task writers to taskEngine.AddTask()
