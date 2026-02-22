package harmonytask

import (
	"context"
	"sync"
	"time"

	"github.com/samber/lo"

	"github.com/filecoin-project/curio/harmony/resources"
)

type schedulerEvent struct {
	TaskID
	TaskType string
	Source   schedulerSource
	PeerID   int64
	Retries  int
	DBTasks  map[string][]task // only set for schedulerSourceDBPoll
}

type schedulerSource byte

const (
	schedulerSourceAdded schedulerSource = iota
	schedulerSourcePeerNewTask
	schedulerSourcePeerStarted
	schedulerSourcePeerReserved // FUTURE PR: schedulerSourcePeerReserved
	schedulerSourceTaskCompleted
	schedulerSourceTaskStarted
	schedulerSourceDBPoll
)

const chokePoint = 1000

// taskSchedule is a collection of available tasks of one type that are to be scheduled.
type taskSchedule struct {
	hasID  map[TaskID]task
	choked bool // FUTURE PR: we choked the adding of TaskIDs for mem savings.
	// In this state, we try what we have, and go to DB if we need more.
	reservedTask TaskID // This will be ran when resources are available. 0 for none.
}

func (sched *taskSchedule) ReserveNext(reserveTask func(TaskID)) {
	sched.reservedTask = 0
	if len(sched.hasID) > 0 {
		for _, t := range sched.hasID {
			if !t.ReservedElsewhere {
				sched.reservedTask = t.ID
				reserveTask(t.ID)
				break
			}
		}
	}
}

func (e *TaskEngine) startScheduler() {
	go func() {
		for {
			select {
			case <-e.ctx.Done():
				log.Infof("scheduler stopped")
				return
			case <-time.After(CLEANUP_FREQUENCY):
				resources.CleanupMachines(e.ctx, e.db)
			}
		}
	}()

	// Background DB poller: queries all task types off the scheduler thread,
	// then signals the scheduler if anything is new.
	go func() {
		timer := time.NewTimer(0) // fire immediately for initial poll
		defer timer.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-timer.C:
				dbTasks := e.pollAllTaskTypes()
				e.schedulerChannel <- schedulerEvent{
					Source:  schedulerSourceDBPoll,
					DBTasks: dbTasks,
				}
				timer.Reset(e.pollDuration.Load().(time.Duration))
			}
		}
	}()

	go func() {
		bundleCollector, bundleSleep := bundler()
		availableTasks := map[string]*taskSchedule{} // TaskType -> TaskID -> bool FUTURE PR: mem savings.
		for _, h := range e.handlers {
			availableTasks[h.Name] = &taskSchedule{hasID: make(map[TaskID]task)}
		}
		ts := taskSourceLocal{availableTasks, e.peering}
		ee := eventEmitter{e.schedulerChannel}
		for {
			select {
			case <-e.ctx.Done():
				log.Infof("scheduler stopped")
				return
			case event := <-e.schedulerChannel:
				switch event.Source {
				case schedulerSourceDBPoll:
					hasNew := false
					for taskName, tasks := range event.DBTasks {
						sched := availableTasks[taskName]
						if sched == nil {
							continue
						}
						prevReserved := sched.reservedTask
						newHas := lo.Associate(tasks, func(t task) (TaskID, task) {
							return t.ID, t
						})
						for id := range newHas {
							if _, exists := sched.hasID[id]; !exists {
								hasNew = true
							}
						}
						newSched := &taskSchedule{
							hasID:        newHas,
							choked:       len(tasks) > chokePoint,
							reservedTask: prevReserved,
						}
						if prevReserved != 0 {
							if _, ok := newHas[prevReserved]; !ok {
								newSched.ReserveNext(func(taskID TaskID) {
									e.peering.TellOthers(messageTypeReserve, taskName, taskID)
								})
							}
						}
						availableTasks[taskName] = newSched
					}
					if hasNew {
						if err := e.pollerTryAllWork(ts, ee); err != nil {
							log.Errorw("failed waterfall", "error", err)
						}
					}
				case schedulerSourceAdded:
					if _, ok := availableTasks[event.TaskType]; ok {
						availableTasks[event.TaskType].hasID[event.TaskID] = task{ID: event.TaskID, UpdateTime: time.Now(), Retries: event.Retries}
						if h := e.taskMap[event.TaskType]; h != nil && h.TimeSensitive {
							if err := e.pollerTryAllWork(ts, ee); err != nil {
								log.Errorw("failed waterfall", "error", err)
							}
						} else {
							bundleCollector(event.TaskType)
						}
					}
					e.peering.TellOthers(messageTypeNewTask, event.TaskType, event.TaskID)
				case schedulerSourcePeerNewTask:
					t, ok := availableTasks[event.TaskType]
					if !ok {
						continue // we don't handle this task type
					}
					if len(t.hasID) > chokePoint {
						t.choked = true
						continue
					}
					t.hasID[event.TaskID] = task{ID: event.TaskID, UpdateTime: time.Now(), Retries: event.Retries}
					if h := e.taskMap[event.TaskType]; h != nil && h.TimeSensitive {
						if err := e.pollerTryAllWork(ts, ee); err != nil {
							log.Errorw("failed waterfall", "error", err)
						}
					} else {
						bundleCollector(event.TaskType)
					}
				case schedulerSourceTaskStarted:
					avail := availableTasks[event.TaskType]
					delete(avail.hasID, event.TaskID)
					if avail.reservedTask == event.TaskID && e.taskMap[event.TaskType].TimeSensitive { // FUTURE: "stress" reservations will not reserve the next task.
						avail.ReserveNext(func(taskID TaskID) {
							e.peering.TellOthers(messageTypeReserve, event.TaskType, taskID)
						})
					}
					e.peering.TellOthers(messageTypeStarted, event.TaskType, event.TaskID)
				case schedulerSourcePeerStarted:
					avail, ok := availableTasks[event.TaskType]
					if !ok {
						continue
					}
					delete(avail.hasID, event.TaskID)
					if avail.reservedTask == event.TaskID {
						avail.ReserveNext(func(taskID TaskID) {
							e.peering.TellOthers(messageTypeReserve, event.TaskType, taskID)
						})
					}
				case schedulerSourceTaskCompleted:
					if err := e.pollerTryAllWork(ts, ee); err != nil {
						log.Errorw("failed waterfall", "error", err)
					}
				case schedulerSourcePeerReserved: // FUTURE: apply and respect reservations for anti-starve common tasks.
					avail, ok := availableTasks[event.TaskType]
					if !ok {
						continue
					}
					if event.PeerID > int64(e.ownerID) && avail.reservedTask == event.TaskID {
						t := avail.hasID[event.TaskID]
						t.ReservedElsewhere = true
						avail.hasID[event.TaskID] = t
						avail.ReserveNext(func(taskID TaskID) {
							e.peering.TellOthers(messageTypeReserve, event.TaskType, taskID)
						})
					}
				default:
					log.Errorw("unknown scheduler source", "source", event.Source)
				}
			case <-bundleSleep:
				if err := e.pollerTryAllWork(ts, ee); err != nil {
					log.Errorw("failed waterfall", "error", err)
				}
			}
			// FUTURE: RetryWait could start timers.
		}
	}()
} // FUTURE Move all harmony_task writers to taskEngine.AddTask() to transmit over the RPC.

type taskSource interface {
	GetTasks(taskName string) []task
	ReserveTask(taskName string, taskID TaskID)
}

func (e *TaskEngine) tryStartTask(taskName string, taskSource taskSource, eventEmitter eventEmitter) (TaskID, error) {
	_ = taskName // later: for a fast-path.

	err := e.pollerTryAllWork(taskSource, eventEmitter)
	if err != nil {
		log.Errorw("failed to try waterfall", "error", err)
		return 0, err
	}

	return 0, nil
}

type taskSourceLocal struct {
	availableTasks map[string]*taskSchedule
	peering        *peering
}

func (t taskSourceLocal) GetTasks(taskName string) []task {
	taskObject := t.availableTasks[taskName]
	tasks := []task{}
	if taskObject.reservedTask != 0 {
		tasks = append(tasks, taskObject.hasID[taskObject.reservedTask])
	}
	for taskID, task := range taskObject.hasID {
		if taskObject.reservedTask == taskID {
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func (t taskSourceLocal) ReserveTask(taskName string, taskID TaskID) {
	t.availableTasks[taskName].reservedTask = taskID
	t.peering.TellOthers(messageTypeReserve, taskName, taskID)
}

// Emits are called from other threads, so we cannot change t.availableTasks.
type eventEmitter struct {
	schedulerChannel chan schedulerEvent
}

func (ee eventEmitter) EmitTaskStarted(taskName string, taskID TaskID) {
	ee.schedulerChannel <- schedulerEvent{
		TaskID:   taskID,
		TaskType: taskName,
		Source:   schedulerSourceTaskStarted,
	}
}

func (ee eventEmitter) EmitTaskNew(taskName string, task task) {
	ee.schedulerChannel <- schedulerEvent{
		TaskID:   task.ID,
		TaskType: taskName,
		Source:   schedulerSourceAdded,
		Retries:  task.Retries,
	}
}

func (ee eventEmitter) EmitTaskCompleted(taskName string) {
	ee.schedulerChannel <- schedulerEvent{
		TaskType: taskName,
		Source:   schedulerSourceTaskCompleted,
	}
}

// pollAllTaskTypes queries the DB for all unowned tasks in a single round-trip.
// Runs off the scheduler thread on the background poller goroutine.
// Returns nil on error so the scheduler preserves existing state and reservations.
func (e *TaskEngine) pollAllTaskTypes() map[string][]task {
	var rows []struct {
		ID         TaskID    `db:"id"`
		Name       string    `db:"name"`
		UpdateTime time.Time `db:"update_time"`
		Retries    int       `db:"retries"`
	}
	names := make([]string, len(e.handlers))
	for i, h := range e.handlers {
		names[i] = h.Name
	}
	err := e.db.Select(context.Background(), &rows,
		`SELECT id, name, update_time, retries FROM harmony_task WHERE owner_id IS NULL AND name = ANY($1)`, names)
	if err != nil {
		log.Errorw("failed to poll tasks from db", "error", err)
		return nil
	}

	result := make(map[string][]task, len(e.handlers))
	for _, h := range e.handlers {
		result[h.Name] = nil
	}
	for _, r := range rows {
		if _, ok := result[r.Name]; !ok {
			continue
		}
		if len(result[r.Name]) > chokePoint {
			continue
		}
		result[r.Name] = append(result[r.Name], task{
			ID:         r.ID,
			UpdateTime: r.UpdateTime,
			Retries:    r.Retries,
		})
	}
	return result
}

const bundleCollectionTimeout = time.Millisecond * 10

// expects single-threaded caller of
func bundler() (bundler func(string), bundleSleep <-chan string) {
	timers := make(map[string]*time.Timer)
	timerMx := sync.Mutex{}
	output := make(chan string)
	return func(taskType string) {
		timerMx.Lock()
		t, ok := timers[taskType]
		timerMx.Unlock()
		if !ok {
			t = time.NewTimer(bundleCollectionTimeout)
			timerMx.Lock()
			timers[taskType] = t
			timerMx.Unlock()
			go func() {
				<-t.C
				timerMx.Lock()
				delete(timers, taskType)
				timerMx.Unlock()
				output <- taskType
			}()
		} else {
			t.Reset(bundleCollectionTimeout)
		}
	}, output
}
