package harmonytask

import (
	"context"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/samber/lo"
)

type schedulerEvent struct {
	TaskID
	TaskType string
	Source   schedulerSource
	PeerID   int64
}

type schedulerSource byte

const (
	schedulerSourceAdded schedulerSource = iota
	schedulerSourcePeerNewTask
	schedulerSourcePeerStarted
	schedulerSourcePeerReserved // FUTURE PR: schedulerSourcePeerReserved
	schedulerSourceTaskCompleted
)

const chokePoint = 1000

type taskSchedule struct {
	hasID  map[TaskID]bool
	choked bool // FUTURE PR: we choked the adding of TaskIDs for mem savings.
	// In this state, we try what we have, and go to DB if we need more.
	reservedTask TaskID // This will be ran when resources are available. 0 for none.
}

func (e *TaskEngine) startScheduler() {

	go func() {
		bundleCollector, bundleSleep := bundler()
		availableTasks := map[string]*taskSchedule{} // TaskType -> TaskID -> bool FUTURE PR: mem savings.
		for _, h := range e.handlers {
			availableTasks[h.Name] = &taskSchedule{hasID: make(map[TaskID]bool)}
		}
		for {
			select {
			case <-e.ctx.Done():
				log.Infof("scheduler stopped")
				return
			case event := <-e.schedulerChannel:
				switch event.Source {
				case schedulerSourceAdded:
					if _, ok := availableTasks[event.TaskType]; ok { // we maybe not run this task.
						availableTasks[event.TaskType].hasID[event.TaskID] = true
						bundleCollector(event.TaskType)
					}
					e.peering.TellOthers(messageTypeNewTask, event.TaskType, event.TaskID)
				case schedulerSourcePeerNewTask:
					t := availableTasks[event.TaskType]
					if len(t.hasID) > chokePoint {
						t.choked = true
						continue
					}
					availableTasks[event.TaskType].hasID[event.TaskID] = true
					bundleCollector(event.TaskType)
				case schedulerSourcePeerStarted: // TODO Emit this from waterfall
					t := availableTasks[event.TaskType]
					delete(t.hasID, event.TaskID)
					if t.reservedTask == event.TaskID {
						t.reservedTask = 0
					}
				case schedulerSourceTaskCompleted:
					err := e.waterfall(taskSourceLocal{availableTasks, e.peering})
					if err != nil {
						log.Errorw("failed to full waterfall", "error", err)
						continue
					}
				case schedulerSourcePeerReserved:
					if event.PeerID > int64(e.ownerID) && availableTasks[event.TaskType].reservedTask == event.TaskID {
						availableTasks[event.TaskType].reservedTask = 0
					}
				default:
					log.Warnw("unknown scheduler source", "source", event.Source)
				}
			case taskName := <-bundleSleep:
				shouldReserve, err := tryStart(taskName)
				if err != nil {
					log.Errorw("failed to try waterfall", "error", err)
					continue
				}
				if shouldReserve != 0 {
					availableTasks[taskName].reservedTask = shouldReserve
					e.peering.TellOthers(messageTypeReserve, taskName, shouldReserve)
				}
			case <-time.After(e.pollDuration.Load().(time.Duration)): // fast life & early-gather at Go_1.26
				err := e.waterfall(taskSourceDb{e.db, availableTasks, e.peering, taskSourceLocal{availableTasks, e.peering}}) // TODO Reserve tasks as we go.
				if err != nil {
					log.Errorw("failed to full waterfall", "error", err)
					continue
				}
			}
		}
	}()
} // TODO Move all harmony_task writers to taskEngine.AddTask()

type taskSource interface {
	GetTasks(taskName string) []TaskID
	ReserveTask(taskName string, taskID TaskID)
	EventTaskStarted(taskName string, taskID TaskID)
}

// Waterfall is the main function that will start tasks.
// It will start tasks from the taskSource and reserve tasks as we go.
// It must be called only by the scheduler.
func (e *TaskEngine) waterfall(taskSource taskSource) error {
	// TODO Implement this. It should do whatever poller did too.
	return nil
}

type taskSourceLocal struct {
	availableTasks map[string]*taskSchedule
	peering        *peering
}

func (t taskSourceLocal) GetTasks(taskName string) []TaskID {
	taskObject := t.availableTasks[taskName]
	tasks := []TaskID{}
	if taskObject.reservedTask != 0 {
		tasks = append(tasks, taskObject.reservedTask)
	}
	for taskID, _ := range taskObject.hasID {
		if taskObject.reservedTask == taskID {
			continue
		}
		tasks = append(tasks, taskID)
	}
	return tasks
}

func (t taskSourceLocal) ReserveTask(taskName string, taskID TaskID) {
	t.availableTasks[taskName].reservedTask = taskID
	t.peering.TellOthers(messageTypeReserve, taskName, taskID)
}

func (t taskSourceLocal) EventTaskStarted(taskName string, taskID TaskID) {
	t.availableTasks[taskName].hasID[taskID] = false
	if t.availableTasks[taskName].reservedTask == taskID {
		t.availableTasks[taskName].reservedTask = 0
	}
	t.peering.TellOthers(messageTypeStarted, taskName, taskID)
}

type taskSourceDb struct {
	db             *harmonydb.DB
	availableTasks map[string]*taskSchedule
	peering        *peering
	taskSourceLocal
}

func (t taskSourceDb) GetTasks(taskName string) []TaskID {
	tasks := []TaskID{}
	err := t.db.Select(context.Background(), &tasks, `SELECT id FROM harmony_task WHERE task_type = $1 LIMIT $2`, taskName, chokePoint+1)
	if err != nil {
		log.Errorw("failed to get tasks from db", "error", err)
		return nil
	}
	previousReservedTask := t.availableTasks[taskName].reservedTask
	newHas := lo.Associate(tasks, func(t TaskID) (TaskID, bool) {
		return t, true
	})
	if _, ok := newHas[previousReservedTask]; !ok {
		previousReservedTask = 0
	}
	t.availableTasks[taskName] = &taskSchedule{
		hasID:        newHas,
		choked:       len(tasks) > chokePoint,
		reservedTask: previousReservedTask,
	}

	return tasks
}

func (t taskSourceDb) ReserveTask(taskName string, taskID TaskID) {
	t.availableTasks[taskName].reservedTask = taskID
	t.peering.TellOthers(messageTypeReserve, taskName, taskID)
}

const bundleCollectionTimeout = time.Millisecond * 10

// expects single-threaded caller of
func bundler() (bundler func(string), bundleSleep <-chan string) {
	timers := make(map[string]*time.Timer)
	output := make(chan string)
	return func(taskType string) {
		t, ok := timers[taskType]
		if !ok {
			timers[taskType] = time.NewTimer(bundleCollectionTimeout)
			go func() {
				<-t.C
				output <- taskType
			}()
		} else {
			t.Reset(bundleCollectionTimeout)
		}
	}, output
}
