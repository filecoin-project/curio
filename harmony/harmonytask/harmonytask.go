package harmonytask

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

// Consts (except for unit test)
const constPollRarely = time.Second * 30
const constPollFrequently = time.Second * 3

var POLL_DURATION = time.Second * 3             // Poll for Work this frequently
var POLL_NEXT_DURATION = 100 * time.Millisecond // After scheduling a task, wait this long before scheduling another
var CLEANUP_FREQUENCY = 5 * time.Minute         // Check for dead workers this often * everyone
var FOLLOW_FREQUENCY = 1 * time.Minute          // Check for work to follow this often

var ExitStatusRestartRequest = 100

type TaskTypeDetails struct {
	// Max returns how many tasks this machine can run of this type.
	// Nil (default)/Zero or less means unrestricted.
	// Counters can either be independent when created with Max, or shared between tasks with SharedMax.Make()
	Max taskhelp.Limiter

	// Name is the task name to be added to the task list.
	Name string

	// Peak costs to Do() the task.
	Cost resources.Resources

	// Max Failure count before the job is dropped.
	// 0 = retry forever
	MaxFailures uint

	// RetryWait is the time to wait before retrying a failed task.
	// It is called with the number of retries so far.
	// If nil, it will retry immediately.
	RetryWait func(retries int) time.Duration

	// Follow another task's completion via this task's creation.
	// The function should populate extraInfo from data
	// available from the previous task's tables, using the given TaskID.
	// It should also return success if the trigger succeeded.
	// NOTE: if refatoring tasks, see if your task is
	// necessary. Ex: Is the sector state correct for your stage to run?
	Follows map[string]func(TaskID, AddTaskFunc) (bool, error)

	// IAmBored is called (when populated) when there's capacity but no work.
	// Tasks added will be proposed to CanAccept() on this machine.
	// CanAccept() can read taskEngine's WorkOrigin string to learn about a task.
	// Ex: make new CC sectors, clean-up, or retrying pipelines that failed in later states.
	IAmBored func(AddTaskFunc) error

	// CanYield is true if the task should yield when the node is not schedulable.
	// This is implied for background tasks.
	CanYield bool

	// SchedOverrides is a map of task names which, when running while the node is not schedulable,
	// allow this task to continue being scheduled. This is useful in pipelines where a long-running
	// task would block a short-running task from being scheduled, blocking other related pipelines on
	// other machines.
	SchedulingOverrides map[string]bool
}

// TaskInterface must be implemented in order to have a task used by harmonytask.
type TaskInterface interface {
	// Do the task assigned. Call stillOwned before making single-writer-only
	// changes to ensure the work has not been stolen.
	// This is the ONLY function that should attempt to do the work, and must
	// ONLY be called by harmonytask.
	// Indicate if the task no-longer needs scheduling with done=true including
	// cases where it's past the deadline.
	Do(taskID TaskID, stillOwned func() bool) (done bool, err error)

	// CanAccept should return if the task can run on this machine. It should
	// return null if the task type is not allowed on this machine.
	// It should select the task it most wants to accomplish.
	// It is also responsible for determining & reserving disk space (including scratch).
	CanAccept([]TaskID, *TaskEngine) ([]TaskID, error)

	// TypeDetails() returns static details about how this task behaves and
	// how this machine will run it. Read once at the beginning.
	TypeDetails() TaskTypeDetails

	// This listener will consume all external sources continuously for work.
	// Do() may also be called from a backlog of work. This must not
	// start doing the work (it still must be scheduled).
	// Note: Task de-duplication should happen in ExtraInfoFunc by
	//  returning false, typically by determining from the tx that the work
	//  exists already. The easy way is to have a unique joint index
	//  across all fields that will be common.
	// Adder should typically only add its own task type, but multiple
	//   is possible for when 1 trigger starts 2 things.
	// Usage Example:
	// func (b *BazType)Adder(addTask AddTaskFunc) {
	//	  for {
	//      bazMaker := <- bazChannel
	//	    addTask("baz", func(t harmonytask.TaskID, txn db.Transaction) (bool, error) {
	//	       _, err := txn.Exec(`INSERT INTO bazInfoTable (taskID, qix, mot)
	//			  VALUES ($1,$2,$3)`, id, bazMaker.qix, bazMaker.mot)
	//         if err != nil {
	//				scream(err)
	//	 		 	return false
	//		   }
	// 		   return true
	//		})
	//	  }
	// }
	Adder(AddTaskFunc)
}

// AddTaskFunc is responsible for adding a task's details "extra info" to the DB.
// It should return true if the task should be added, false if it was already there.
// This is typically accomplished with a "unique" index on your detals table that
// would cause the insert to fail.
// The error indicates that instead of a conflict (which we should ignore) that we
// actually have a serious problem that needs to be logged with context.
type AddTaskFunc func(extraInfo func(TaskID, *harmonydb.Tx) (shouldCommit bool, seriousError error))

type TaskEngine struct {
	// Static After New()
	ctx         context.Context
	handlers    []*taskTypeHandler
	db          *harmonydb.DB
	reg         *resources.Reg
	grace       context.CancelFunc
	taskMap     map[string]*taskTypeHandler
	ownerID     int
	follows     map[string][]followStruct
	hostAndPort string

	peering          *peering
	schedulerChannel chan schedulerEvent
	pollDuration     atomic.Value

	// runtime flags
	yieldBackground atomic.Bool

	// synchronous to the single-threaded poller
	lastFollowTime time.Time
	lastCleanup    atomic.Value
	WorkOrigin     string
}
type followStruct struct {
	f    func(TaskID, AddTaskFunc) (bool, error)
	h    *taskTypeHandler
	name string
}

type TaskID int

// New creates all the task definitions. Note that TaskEngine
// knows nothing about the tasks themselves and serves to be a
// generic container for common work
func New(
	db *harmonydb.DB,
	impls []TaskInterface,
	hostnameAndPort string,
	peerConnector PeerConnectorInterface) (*TaskEngine, error) {

	reg, err := resources.Register(db, hostnameAndPort)
	if err != nil {
		return nil, fmt.Errorf("cannot get resources: %w", err)
	}
	ctx, grace := context.WithCancel(context.Background())
	e := &TaskEngine{
		ctx:              ctx,
		grace:            grace,
		db:               db,
		reg:              reg,
		ownerID:          reg.MachineID, // The current number representing "hostAndPort"
		taskMap:          make(map[string]*taskTypeHandler, len(impls)),
		follows:          make(map[string][]followStruct),
		hostAndPort:      hostnameAndPort,
		schedulerChannel: make(chan schedulerEvent, 100),
	}
	e.pollDuration.Store(POLL_DURATION)
	e.peering = startPeering(e, peerConnector)
	e.lastCleanup.Store(time.Now())

	for _, c := range impls {
		h := taskTypeHandler{
			TaskInterface:   c,
			TaskTypeDetails: c.TypeDetails(),
			TaskEngine:      e,
			storageFailures: make(map[TaskID]time.Time),
		}
		if h.Max == nil {
			h.Max = taskhelp.Max(0)
		}
		h.Max = h.Max.Instance()

		if Registry[h.Name] == nil {
			return nil, fmt.Errorf("task %s not registered: var _ = harmonytask.Reg(t TaskInterface)", h.Name)
		}

		if len(h.Name) > 16 {
			return nil, fmt.Errorf("task name too long: %s, max 16 characters", h.Name)
		}

		e.handlers = append(e.handlers, &h)
		e.taskMap[h.Name] = &h
	}

	// resurrect old work
	{
		var taskRet []struct {
			ID   int
			Name string
		}

		err := db.Select(e.ctx, &taskRet, `SELECT id, name from harmony_task WHERE owner_id=$1`, e.ownerID)
		if err != nil {
			return nil, err
		}
		for _, w := range taskRet {
			// edge-case: if old assignments are not available tasks, unlock them.
			h := e.taskMap[w.Name]
			if h == nil || !h.considerWork(WorkSourceRecover, []TaskID{TaskID(w.ID)}) {
				_, err := db.Exec(e.ctx, `UPDATE harmony_task SET owner_id=NULL WHERE id=$1 AND owner_id=$2`, w.ID, e.ownerID)
				if err != nil {
					log.Errorw("Cannot remove self from owner field", "error", err)
					continue // not really fatal, but not great
				}
			}
		}
	}
	for _, h := range e.handlers {
		go h.Adder(h.AddTask)
	}
	go e.poller() // TODO replace with startScheduler()

	return e, nil
}

// GracefullyTerminate hangs until all present tasks have completed.
// Call this to cleanly exit the process. As some processes are long-running,
// passing a deadline will ignore those still running (to be picked-up later).
func (e *TaskEngine) GracefullyTerminate() {

	// call the cancel func to avoid picking up any new tasks. Running tasks have context.Background()
	// Call shutdown to stop posting heartbeat to DB.
	e.grace()
	e.reg.Shutdown()

	// If there are any Post tasks then wait till Timeout and check again
	// When no Post tasks are active, break out of loop  and call the shutdown function
	for {
		timeout := time.Millisecond
		for _, h := range e.handlers {
			if h.Name == "WinPost" && h.Max.Active() > 0 {
				timeout = time.Second
				log.Infof("node shutdown deferred for %f seconds", timeout.Seconds())
				continue
			}
			if h.Name == "WdPost" && h.Max.Active() > 0 {
				timeout = time.Second * 3
				log.Infof("node shutdown deferred for %f seconds due to running WdPost task", timeout.Seconds())
				continue
			}

			if h.Name == "WdPostSubmit" && h.Max.Active() > 0 {
				timeout = time.Second
				log.Infof("node shutdown deferred for %f seconds due to running WdPostSubmit task", timeout.Seconds())
				continue
			}

			if h.Name == "WdPostRecover" && h.Max.Active() > 0 {
				timeout = time.Second
				log.Infof("node shutdown deferred for %f seconds due to running WdPostRecover task", timeout.Seconds())
				continue
			}

			// Test tasks for itest
			if h.Name == "ThingOne" && h.Max.Active() > 0 {
				timeout = time.Second
				log.Infof("node shutdown deferred for %f seconds due to running itest task", timeout.Seconds())
				continue
			}
		}
		if timeout > time.Millisecond {
			time.Sleep(timeout)
			continue
		}
		break
	}
}

func (e *TaskEngine) poller() {
	nextWait := POLL_NEXT_DURATION
	timer := time.NewTimer(nextWait)
	defer timer.Stop()

	for {
		stats.Record(context.Background(), TaskMeasures.PollerIterations.M(1))

		select {
		case <-timer.C: // Find work periodically
			nextWait = POLL_DURATION
			timer.Reset(nextWait)
		case <-e.ctx.Done(): ///////////////////// Graceful exit
			return
		}

		// Check if the machine is schedulable
		schedulable, err := e.checkNodeFlags()
		if err != nil {
			log.Error("Unable to check schedulable status: ", err)
			continue
		}

		e.yieldBackground.Store(!schedulable)

		accepted := e.pollerTryAllWork(schedulable)
		if accepted {
			nextWait = POLL_NEXT_DURATION
			timer.Reset(nextWait)
		}

		if !schedulable {
			log.Debugf("Machine %s is not schedulable. Please check the cordon status.", e.hostAndPort)
			continue
		}

		if time.Since(e.lastFollowTime) > FOLLOW_FREQUENCY {
			e.followWorkInDB()
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

	}
}

// followWorkInDB implements "Follows"
func (e *TaskEngine) followWorkInDB() {
	// Step 1: What are we following?
	var lastFollowTime time.Time
	lastFollowTime, e.lastFollowTime = e.lastFollowTime, time.Now()

	for fromName, srcs := range e.follows {
		var cList []int // Which work is done (that we follow) since we last checked?
		err := e.db.Select(e.ctx, &cList, `SELECT h.task_id FROM harmony_task_history
   		WHERE h.work_end>$1 AND h.name=$2`, lastFollowTime.UTC(), fromName)
		if err != nil {
			log.Error("Could not query DB: ", err)
			return
		}
		for _, src := range srcs {
			for _, workAlreadyDone := range cList { // Were any tasks made to follow these tasks?
				var ct int
				err := e.db.QueryRow(e.ctx, `SELECT COUNT(*) FROM harmony_task
					WHERE name=$1 AND previous_task=$2`, src.h.Name, workAlreadyDone).Scan(&ct)
				if err != nil {
					log.Error("Could not query harmony_task: ", err)
					return // not recoverable here
				}
				if ct > 0 {
					continue
				}
				// we need to create this task
				b, err := src.h.Follows[fromName](TaskID(workAlreadyDone), src.h.AddTask)
				if err != nil {
					log.Errorw("Could not follow: ", "error", err)
					continue
				}
				if !b {
					// But someone may have beaten us to it.
					log.Debugf("Unable to add task %s following Task(%d, %s)", src.h.Name, workAlreadyDone, fromName)
				}
			}
		}
	}
}

// pollerTryAllWork starts the next 1 task
func (e *TaskEngine) pollerTryAllWork(schedulable bool) bool {
	if time.Since(e.lastCleanup.Load().(time.Time)) > CLEANUP_FREQUENCY {
		e.lastCleanup.Store(time.Now())
		resources.CleanupMachines(e.ctx, e.db)
	}
	for _, v := range e.handlers {
		if !schedulable {
			if v.SchedulingOverrides == nil {
				continue
			}

			// Override the schedulable flag if the task has any assigned overrides
			var foundOverride bool
			for relatedTaskName := range v.SchedulingOverrides {
				var assignedOverrideTasks []int
				err := e.db.Select(e.ctx, &assignedOverrideTasks, `SELECT id
					FROM harmony_task
					WHERE owner_id = $1 AND name=$2
					ORDER BY update_time LIMIT 1`, e.ownerID, relatedTaskName)
				if err != nil {
					log.Error("Unable to read assigned overrides ", err)
					break
				}
				if len(assignedOverrideTasks) > 0 {
					log.Infow("found override, scheduling despite schedulable=false flag", "ownerID", e.ownerID, "relatedTaskName", relatedTaskName, "assignedOverrideTasks", assignedOverrideTasks)
					foundOverride = true
					break
				}
			}
			if !foundOverride {
				continue
			}
		}

		if _, err := v.AssertMachineHasCapacity(); err != nil {
			log.Debugf("skipped scheduling %s type tasks on due to %s", v.Name, err.Error())
			continue
		}
		type task struct {
			ID         TaskID    `db:"id"`
			UpdateTime time.Time `db:"update_time"`
			Retries    int       `db:"retries"`
		}

		var allUnownedTasks []task
		err := e.db.Select(e.ctx, &allUnownedTasks, `SELECT id, update_time, retries
			FROM harmony_task
			WHERE owner_id IS NULL AND name=$1
			ORDER BY update_time`, v.Name)
		if err != nil {
			log.Error("Unable to read work ", err)
			continue
		}

		unownedTasks := lo.FlatMap(allUnownedTasks, func(t task, _ int) []TaskID {
			if v.RetryWait == nil || t.Retries == 0 {
				return []TaskID{t.ID}
			}
			if time.Since(t.UpdateTime) > v.RetryWait(t.Retries) {
				return []TaskID{t.ID}
			} else {
				log.Debugf("Task %d is not ready to retry yet, retries %d, wait: %s", t.ID, t.Retries, v.RetryWait(t.Retries))
				return nil
			}
		})

		if len(unownedTasks) > 0 {
			accepted := v.considerWork(WorkSourcePoller, unownedTasks)
			if accepted {
				return true // accept new work slowly and in priority order
			}
			log.Warn("Work not accepted for " + strconv.Itoa(len(unownedTasks)) + " " + v.Name + " task(s)")
		}
	}

	if !schedulable {
		return false
	}

	// if no work was accepted, are we bored? Then find work in priority order.
	for _, v := range e.handlers {
		v := v
		if _, err := v.AssertMachineHasCapacity(); err != nil {
			continue
		}
		if v.IAmBored != nil {
			var added []TaskID
			err := v.IAmBored(func(extraInfo func(TaskID, *harmonydb.Tx) (shouldCommit bool, seriousError error)) {
				v.AddTask(func(tID TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
					b, err := extraInfo(tID, tx)
					if err == nil && b {
						added = append(added, tID)
					}
					return b, err
				})
			})
			if err != nil {
				log.Error("IAmBored failed: ", err)
				continue
			}
		}
	}

	return false
}

var rlog = logging.Logger("harmony-res")

// ResourcesAvailable determines what resources are still unassigned.
func (e *TaskEngine) ResourcesAvailable() resources.Resources {
	tmp := e.reg.Resources
	for _, t := range e.handlers {
		ct := t.Max.ActiveThis()
		tmp.Cpu -= ct * t.Cost.Cpu
		tmp.Gpu -= float64(ct) * t.Cost.Gpu
		tmp.Ram -= uint64(ct) * t.Cost.Ram
		rlog.Debugw("Per task type", "Name", t.Name, "Count", ct, "CPU", ct*t.Cost.Cpu, "RAM", uint64(ct)*t.Cost.Ram, "GPU", float64(ct)*t.Cost.Gpu)
	}
	rlog.Debugw("Total", "CPU", tmp.Cpu, "RAM", tmp.Ram, "GPU", tmp.Gpu)
	return tmp
}

// Resources returns the resources available in the TaskEngine's registry.
func (e *TaskEngine) Resources() resources.Resources {
	return e.reg.Resources
}

func (e *TaskEngine) Host() string {
	return e.hostAndPort
}

func (e *TaskEngine) checkNodeFlags() (bool, error) {
	var unschedulable bool
	var restartRequest *time.Time
	err := e.db.QueryRow(e.ctx, `SELECT unschedulable, restart_request FROM harmony_machines WHERE host_and_port=$1`, e.hostAndPort).Scan(&unschedulable, &restartRequest)
	if err != nil {
		return false, err
	}

	if restartRequest != nil {
		e.restartIfNoTasksPending(*restartRequest)
	}

	return !unschedulable, nil
}

func (e *TaskEngine) restartIfNoTasksPending(pendingSince time.Time) {
	var tasksPending int
	err := e.db.QueryRow(e.ctx, `SELECT COUNT(*) FROM harmony_task WHERE owner_id=$1`, e.ownerID).Scan(&tasksPending)
	if err != nil {
		log.Error("Unable to check for tasks pending: ", err)
		return
	}
	if tasksPending == 0 {
		log.Infow("no tasks pending, restarting", "ownerID", e.ownerID, "pendingSince", pendingSince, "took", time.Since(pendingSince))

		// unset the flags first
		_, err = e.db.Exec(e.ctx, `UPDATE harmony_machines SET restart_request=NULL, unschedulable=FALSE WHERE host_and_port=$1`, e.hostAndPort)
		if err != nil {
			log.Error("Unable to unset restart request: ", err)
			return
		}

		// then exit
		os.Exit(ExitStatusRestartRequest)
	}
}

// About the Registry
// This registry exists for the benefit of "static methods" of TaskInterface extensions.
// For example, GetSPID(db, taskID) (int, err) is a static method that can be called
//
//	from any task that has a GetSPID method. This is useful for the web UI to
//	be able to indicate the SpID for a task.
//
// Reg is a task registry full of nil implementations.
// Even if NOT running, a nil task should be registered here.
var Registry = map[string]TaskInterface{}

func Reg(t TaskInterface) bool {
	name := t.TypeDetails().Name
	Registry[name] = t

	// reset metrics
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(taskNameTag, name),
	}, TaskMeasures.ActiveTasks.M(0))

	return true
}

func (e *TaskEngine) RunningCount(name string) int {
	return int(e.taskMap[name].Max.Active())
}
