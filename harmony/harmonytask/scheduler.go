package harmonytask

import (
	"context"
	"fmt"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/samber/lo"
	"go.opencensus.io/stats"
)

type schedulerEvent struct {
	TaskID
	TaskType string
	Source   schedulerSource
	PeerID   int64
	Retries  int
}

type schedulerSource byte

const (
	schedulerSourceAdded schedulerSource = iota
	schedulerSourcePeerNewTask
	schedulerSourcePeerStarted
	schedulerSourcePeerReserved // FUTURE PR: schedulerSourcePeerReserved
	schedulerSourceTaskCompleted
	schedulerSourceRetryTask
)

const chokePoint = 1000

type taskSchedule struct {
	hasID  map[TaskID]task
	choked bool // FUTURE PR: we choked the adding of TaskIDs for mem savings.
	// In this state, we try what we have, and go to DB if we need more.
	reservedTask TaskID // This will be ran when resources are available. 0 for none.
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

	go func() {
		bundleCollector, bundleSleep := bundler()
		availableTasks := map[string]*taskSchedule{} // TaskType -> TaskID -> bool FUTURE PR: mem savings.
		for _, h := range e.handlers {
			availableTasks[h.Name] = &taskSchedule{hasID: make(map[TaskID]task)}
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
						availableTasks[event.TaskType].hasID[event.TaskID] = task{ID: event.TaskID, UpdateTime: time.Now(), Retries: 0}
						bundleCollector(event.TaskType)
					}
					e.peering.TellOthers(messageTypeNewTask, event.TaskType, event.TaskID)
				case schedulerSourcePeerNewTask:
					t := availableTasks[event.TaskType]
					if len(t.hasID) > chokePoint {
						t.choked = true
						continue
					}
					availableTasks[event.TaskType].hasID[event.TaskID] = task{ID: event.TaskID, UpdateTime: time.Now(), Retries: event.Retries}
					bundleCollector(event.TaskType)
				case schedulerSourcePeerStarted:
					t := availableTasks[event.TaskType]
					delete(t.hasID, event.TaskID)
					if t.reservedTask == event.TaskID {
						t.reservedTask = 0
					}
				case schedulerSourceTaskCompleted:
					err := e.waterfall(taskSourceLocal{availableTasks, e.peering}, eventEmitter{e.schedulerChannel})
					if err != nil {
						log.Errorw("failed to full waterfall", "error", err)
						continue
					}
				case schedulerSourcePeerReserved:
					avail := availableTasks[event.TaskType]
					if event.PeerID > int64(e.ownerID) && avail.reservedTask == event.TaskID {
						avail.reservedTask = 0
						t := avail.hasID[event.TaskID]
						t.ReservedElsewhere = true
						avail.hasID[event.TaskID] = t
						if len(avail.hasID) > 0 {
							for _, t := range avail.hasID {
								if !t.ReservedElsewhere {
									avail.reservedTask = t.ID
									e.peering.TellOthers(messageTypeReserve, event.TaskType, t.ID)
									break
								}
							}
						}
					}
				default:
					log.Warnw("unknown scheduler source", "source", event.Source)
				}
			case taskName := <-bundleSleep:
				shouldReserve, err := e.tryStartTask(taskName, eventEmitter{e.schedulerChannel})
				if err != nil {
					log.Errorw("failed to try waterfall", "error", err)
					continue
				}
				if shouldReserve != 0 {
					availableTasks[taskName].reservedTask = shouldReserve
					e.peering.TellOthers(messageTypeReserve, taskName, shouldReserve)
				}
			case <-time.After(e.pollDuration.Load().(time.Duration)): // fast life & early-gather at Go_1.26
				err := e.waterfall(taskSourceDb{e.db, availableTasks, e.peering, taskSourceLocal{availableTasks, e.peering}}, eventEmitter{e.schedulerChannel})
				if err != nil {
					log.Errorw("failed to full waterfall", "error", err)
					continue
				}
			}
		}
	}()
} // TODO Move all harmony_task writers to taskEngine.AddTask()

type taskSource interface {
	GetTasks(taskName string) []task
	ReserveTask(taskName string, taskID TaskID)
}

func (e *TaskEngine) tryStartTask(taskName string) (TaskID, error) {
	// TODO can this just be consider work?
	return 0, nil
}

// Waterfall is the main function that will start tasks.
// It will start tasks from the taskSource and reserve tasks as we go.
// It must be called only by the scheduler.
func (e *TaskEngine) waterfall(taskSource taskSource, eventEmitter eventEmitter) error {

	// Check if the machine is schedulable
	schedulable, err := e.checkNodeFlags()
	if err != nil {
		return fmt.Errorf("Unable to check schedulable status: %w", err)

	}

	e.yieldBackground.Store(!schedulable)

	// TODO Implement this here
	/* THis should:
	4. bring decreasing resources down (respecting reservations).
	5. Rethink "retryWait" from poller-only to include schedule-able.
	6. double-check where we receive events that source =0 works as us.
	7. tryStartTask should be thin wrapper around considerWork, but that requires moving most of the pollerTryAllWork logic into considerWork, and maybe the rest can go into waterfall.

	*/
	e.pollerTryAllWork(schedulable, taskSource, eventEmitter)

	if !schedulable {
		log.Debugf("Machine %s is not schedulable. Please check the cordon status.", e.hostAndPort)
		return nil
	}

	// update resource usage
	availableResources := e.ResourcesAvailable()
	totalResources := e.Resources()

	cpuUsage := 1 - float64(availableResources.Cpu)/float64(totalResources.Cpu)
	stats.Record(context.Background(), TaskMeasures.CpuUsage.M(cpuUsage*100))

	if totalResources.Gpu > 0 {
		gpuUsage := 1 - availableResources.Gpu/totalResources.Gpu
		stats.Record(context.Background(), TaskMeasures.GpuUsage.M(gpuUsage*100))
	}

	ramUsage := 1 - float64(availableResources.Ram)/float64(totalResources.Ram)
	stats.Record(context.Background(), TaskMeasures.RamUsage.M(ramUsage*100))

	return nil
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
		Source:   0,
	}
}

func (ee eventEmitter) EmitTaskNew(taskName string, task task) {
	ee.schedulerChannel <- schedulerEvent{
		TaskID:   task.ID,
		TaskType: taskName,
		Source:   schedulerSourceRetryTask,
		Retries:  task.Retries,
	}
}

func (ee eventEmitter) EmitTaskCompleted(taskName string) {
	ee.schedulerChannel <- schedulerEvent{
		TaskType: taskName,
		Source:   schedulerSourceTaskCompleted,
	}
}

type taskSourceDb struct {
	db             *harmonydb.DB
	availableTasks map[string]*taskSchedule
	peering        *peering
	taskSourceLocal
}

func (t taskSourceDb) GetTasks(taskName string) []task {
	tasks := []task{}
	err := t.db.Select(context.Background(), &tasks, `SELECT id, update_time, retries FROM harmony_task WHERE task_type = $1 LIMIT $2`, taskName, chokePoint+1)
	if err != nil {
		log.Errorw("failed to get tasks from db", "error", err)
		return nil
	}
	previousReservedTask := t.availableTasks[taskName].reservedTask
	newHas := lo.Associate(tasks, func(t task) (TaskID, task) {
		return t.ID, t
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
