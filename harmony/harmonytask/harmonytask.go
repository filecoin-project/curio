package harmonytask

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"sync"
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
// POLL_RARELY is the default DB poll interval. Under event-driven scheduling,
// DB polling is a fallback safety net — most work is discovered via peer
// notifications or local events. 30s keeps DB load minimal while ensuring
// no task is stranded indefinitely if a peer message is lost.
const POLL_RARELY = time.Second * 30

// POLL_FREQUENTLY is used when peer connections fail, reverting to
// polling-heavy mode until peering is re-established.
const POLL_FREQUENTLY = time.Second * 3

// CLEANUP_FREQUENCY controls how often dead worker cleanup runs. Each node
// independently checks for stale harmony_machines entries on this interval.
var CLEANUP_FREQUENCY = 5 * time.Minute

// ExitStatusRestartRequest is the exit code used when the DB restart_request
// flag triggers a graceful restart (e.g., for rolling upgrades).
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

	// IAmBored is called (when populated) when there's capacity but no work.
	// Tasks added will be proposed to CanAccept() on this machine.
	// CanAccept() can read taskEngine's WorkOrigin string to learn about a task.
	// Ex: make new CC sectors, clean-up, or retrying pipelines that failed in later states.
	// This is starved on busy machines, so use it to gather "above and beyond" work only.
	IAmBored func(AddTaskFunc) error

	// CanYield is true if the task should yield when the node is not schedulable.
	// This is implied for background tasks.
	CanYield bool

	// SchedOverrides is a map of task names which, when running while the node is not schedulable,
	// allow this task to continue being scheduled. This is useful in pipelines where a long-running
	// task would block a short-running task from being scheduled, blocking other related pipelines on
	// other machines.
	SchedulingOverrides map[string]bool

	// Should block shutdown until completion..
	TimeSensitive bool
}

// TaskInterface must be implemented in order to have a task used by harmonytask.
type TaskInterface interface {
	// Do the task assigned. Call stillOwned before making single-writer-only
	// changes to ensure the work has not been stolen.
	// This is the ONLY function that should attempt to do the work, and must
	// ONLY be called by harmonytask.
	// Indicate if the task no-longer needs scheduling with done=true including
	// cases where it's past the deadline.
	Do(ctx context.Context, taskID TaskID, stillOwned func() bool) (done bool, err error)

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

// TaskEngine is the central coordinator for distributed task scheduling.
// It owns the handler registry, the event-driven scheduler, and the peering
// layer. All scheduling decisions flow through the schedulerChannel as events.
//
// Thread safety: most fields are immutable after New(). The scheduler goroutine
// is the sole consumer of schedulerChannel and the sole writer of in-memory
// task state. Mutable atomics (pollDuration, yieldBackground) are safe for
// concurrent reads from the background poller and task goroutines.
type TaskEngine struct {
	ctx         context.Context
	handlers    []*taskTypeHandler
	db          *harmonydb.DB
	reg         *resources.Reg
	grace       context.CancelFunc
	taskMap     map[string]*taskTypeHandler
	ownerID     int
	hostAndPort string

	// peering manages connections to other nodes in the cluster for
	// real-time task event propagation (new tasks, reservations, starts).
	peering *peering

	// schedulerChannel is the single event bus feeding the scheduler goroutine.
	// All event sources (local adds, peer messages, task completions, DB polls)
	// write events here. The buffered channel (cap 100) absorbs bursts; the
	// scheduler drains it as fast as possible with no blocking I/O.
	schedulerChannel chan schedulerEvent

	// pollDuration stores the current DB poll interval as time.Duration.
	// Starts at POLL_RARELY; degrades to POLL_FREQUENTLY if peer connections fail.
	pollDuration atomic.Value

	// yieldBackground is set true when this node is cordoned (unschedulable).
	// Checked by pollerTryAllWork and task goroutines to skip new work or
	// yield in-progress background tasks.
	yieldBackground atomic.Bool

	lastCleanup atomic.Value

	// WorkOrigin is set to a WorkSource* constant before calling CanAccept(),
	// so task implementations can make decisions based on how work was discovered.
	WorkOrigin string

	// Preemption cost exchange: keyed by the specific TaskID being negotiated.
	// Set during preemptForTimeSensitive, read by peering handler.
	preemptCostMu      sync.Mutex
	preemptCostChs     map[TaskID]chan preemptCostResponse
	preemptCostPending map[TaskID][]preemptCostResponse // buffered until channel registered
}

type TaskID int

// New creates a TaskEngine that manages the given task implementations.
// The engine is task-agnostic: it handles scheduling, resource tracking,
// peering, and retries while delegating all domain logic to TaskInterface.
//
// Startup sequence:
//  1. Register this machine's resources in the DB.
//  2. Build handler registry and validate task names.
//  3. Start peering (connect to known cluster nodes for event propagation).
//  4. Resurrect any tasks this machine owned before a restart — these are
//     re-fed to considerWork so in-progress pipelines resume immediately
//     without waiting for a DB poll cycle.
//  5. Launch Adder goroutines for each task type (external event listeners).
//  6. Start the scheduler event loop and background poller.
func New(
	db *harmonydb.DB,
	impls []TaskInterface,
	hostnameAndPort string,
	peerConnector PeerConnectorInterface) (*TaskEngine, error) {

	reg, err := resources.Register(db, hostnameAndPort)
	if err != nil {
		return nil, fmt.Errorf("cannot get resources: %w", err)
	}
	return NewWithReg(db, impls, hostnameAndPort, peerConnector, reg)
}

// NewWithReg is like New but uses an existing *resources.Reg (from resources.Register or RegisterWithResources).
func NewWithReg(
	db *harmonydb.DB,
	impls []TaskInterface,
	hostnameAndPort string,
	peerConnector PeerConnectorInterface,
	reg *resources.Reg) (*TaskEngine, error) {

	if reg == nil {
		return nil, fmt.Errorf("harmonytask.NewWithReg: reg is nil")
	}
	ctx, grace := context.WithCancel(context.Background())
	e := &TaskEngine{
		ctx:              ctx,
		grace:            grace,
		db:               db,
		reg:              reg,
		ownerID:          reg.MachineID, // The current number representing "hostAndPort"
		taskMap:          make(map[string]*taskTypeHandler, len(impls)),
		hostAndPort:      hostnameAndPort,
		schedulerChannel: make(chan schedulerEvent, 100),
	}
	e.pollDuration.Store(POLL_RARELY)
	e.peering = startPeering(e, peerConnector)
	e.lastCleanup.Store(time.Now())

	for _, c := range impls {
		h := taskTypeHandler{
			TaskInterface:   c,
			TaskTypeDetails: c.TypeDetails(),
			TaskEngine:      e,
			storageFailures: make(map[TaskID]time.Time),
			runningTasks:    make(map[TaskID]*runningTaskInfo),
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

	// Resurrect tasks that were owned by this machine before a restart.
	// These tasks are already claimed in the DB (owner_id = us) so considerWork
	// skips the SQL claim step (WorkSourceRecover). This ensures pipelines
	// resume immediately rather than waiting for another node to notice and
	// re-assign them.
	{
		var taskRet []struct {
			ID         int
			Name       string
			UpdateTime time.Time
			Retries    int
		}

		err := db.Select(e.ctx, &taskRet, `SELECT id, name, update_time, retries from harmony_task WHERE owner_id=$1`, e.ownerID)
		if err != nil {
			return nil, err
		}

		// considerWork emits events (TaskStarted, etc.) before the scheduler
		// goroutine is running, so we must ensure the channel has enough
		// capacity to buffer all possible emit calls without blocking.
		emitTypes := reflect.TypeOf(eventEmitter{}).NumMethod()
		if len(taskRet)*emitTypes > cap(e.schedulerChannel) {
			e.schedulerChannel = make(chan schedulerEvent, len(taskRet)*3)
		}
		for _, w := range taskRet {
			h := e.taskMap[w.Name]
			if h == nil || !h.considerWork(WorkSourceRecover, []task{{ID: TaskID(w.ID), UpdateTime: w.UpdateTime, Retries: w.Retries}}, eventEmitter{e.schedulerChannel}) {
				// Task type no longer registered on this node (config change);
				// release the claim so another node can pick it up.
				_, err := db.Exec(e.ctx, `UPDATE harmony_task SET owner_id=NULL WHERE id=$1 AND owner_id=$2`, w.ID, e.ownerID)
				if err != nil {
					log.Errorw("Cannot remove self from owner field", "error", err)
					continue
				}
			}
		}
	}

	// Launch each task type's Adder goroutine. These are long-running listeners
	// (e.g., chain event watchers) that call AddTaskByName when external events
	// require new work to be scheduled.
	for _, h := range e.handlers {
		go func(name string) {
			h.Adder(func(extraInfo func(TaskID, *harmonydb.Tx) (bool, error)) {
				e.AddTaskByName(name, extraInfo)
			})
		}(h.Name)
	}
	e.startScheduler()
	e.schedulerChannel <- schedulerEvent{Source: schedulerSourceInitialPoll}
	go e.singletonRunNowPoller()

	return e, nil
}

// GracefullyTerminate hangs until all present tasks have completed.
// Call this to cleanly exit the process. As some processes are long-running,
// passing a deadline will ignore those still running (to be picked-up later).
func (e *TaskEngine) GracefullyTerminate() {

	// call the cancel func to avoid picking up any new tasks. Running tasks now inherit e.ctx.
	// Call shutdown to stop posting heartbeat to DB.
	e.grace()
	e.reg.Shutdown()

	// If there are any Post tasks then wait till Timeout and check again
	// When no Post tasks are active, break out of loop  and call the shutdown function
	for {
		var waited bool
		for _, h := range e.handlers {
			if h.TimeSensitive && h.Max.Active() > 0 {
				log.Infof("node shutdown deferred due to running %s task", h.Name)
				time.Sleep(time.Second * 3)
				waited = true
			}
		}
		if !waited {
			break
		}
	}
}

type task struct {
	ID                TaskID    `db:"id"`
	UpdateTime        time.Time `db:"update_time"`
	Retries           int       `db:"retries"`
	ReservedElsewhere bool
}

// pollerTryAllWork is the "waterfall" that attempts to claim and start tasks
// for every handler that has capacity. It is called on the scheduler thread
// whenever an event suggests new work may be available (DB poll, peer
// notification, task completion freeing resources, bundler timer expiry).
//
// The function iterates handlers in registration order, filtering tasks by
// retry-wait eligibility, then delegates to considerWork for CanAccept +
// SQL claim + goroutine launch. Resource metrics are updated on exit.
//
// When capacity remains after processing all known work, IAmBored callbacks
// are invoked in separate goroutines to avoid blocking the scheduler with
// DB writes from speculative work creation.
func (e *TaskEngine) pollerTryAllWork(taskSource taskSource, eventEmitter eventEmitter) error {
	schedulable := !e.yieldBackground.Load()
	if e.yieldBackground.Load() {
		return nil
	}

	defer func() {
		availableResources := e.ResourcesAvailable()
		totalResources := e.Resources()

		cpuUsage := 1 - float64(availableResources.Cpu)/float64(totalResources.Cpu)
		stats.Record(context.Background(), TaskMeasures.CpuUsage.M(cpuUsage*100))

		if totalResources.Gpu > 0 {
			gpuUsage := 1 - availableResources.Gpu/totalResources.Gpu
			stats.Record(context.Background(), TaskMeasures.GpuUsage.M(gpuUsage*100))
		}

		ramUsage := 1.0
		if totalResources.Ram > 0 {
			ramUsage = 1 - float64(availableResources.Ram)/float64(totalResources.Ram)
		}
		stats.Record(context.Background(), TaskMeasures.RamUsage.M(ramUsage*100))

	}()
	for _, v := range e.handlers {
		if !schedulable {
			for relatedTaskName := range v.SchedulingOverrides {
				if len(taskSource.GetTasks(relatedTaskName)) > 0 {
					goto doScheduling
				}
			}
			continue
		}
	doScheduling:
		if capacity, err := v.AssertMachineHasCapacity(); err != nil || capacity == 0 {
			if err != nil {
				log.Debugf("skipped scheduling %s type tasks due to %s", v.Name, err.Error())
			}
			continue
		}

		unownedTasks := lo.Filter(taskSource.GetTasks(v.Name), func(t task, _ int) bool {
			if v.RetryWait == nil || t.Retries == 0 {
				return true
			}
			if time.Since(t.UpdateTime) > v.RetryWait(t.Retries) {
				return true
			} else {
				log.Debugf("Task %d is not ready to retry yet, retries %d, wait: %s", t.ID, t.Retries, v.RetryWait(t.Retries))
				return false
			}
		})

		if len(unownedTasks) > 0 {
			if !v.considerWork(WorkSourcePoller, unownedTasks, eventEmitter) {
				log.Warn("Work not accepted for " + strconv.Itoa(len(unownedTasks)) + " " + v.Name + " task(s)")
			}
		}
	}

	if !schedulable {
		return nil
	}

	// if no work was accepted, are we bored? Then find work in priority order.
	// IAmBored: when all known work is exhausted and capacity remains, let
	// handlers generate speculative work (e.g., new CC sectors). Runs in a
	// goroutine to keep DB writes off the scheduler thread.
	for _, v := range e.handlers {
		v := v
		if v, err := v.AssertMachineHasCapacity(); err != nil || v == 0 {
			continue
		}
		if v.IAmBored != nil {
			go func() {
				err := v.IAmBored(func(extraInfo func(TaskID, *harmonydb.Tx) (shouldCommit bool, seriousError error)) {
					e.AddTaskByName(v.Name, extraInfo)
				})
				if err != nil {
					log.Error("IAmBored failed: ", err)
				}
			}()
		}
	}
	return nil
}

var rlog = logging.Logger("harmony-res")

// ResourcesAvailable determines what resources are still unassigned.
func (e *TaskEngine) ResourcesAvailable() resources.Resources {
	tmp := e.reg.Resources
	for _, t := range e.handlers {
		ct := t.Max.ActiveThis()
		tmp.Cpu -= ct * t.Cost.Cpu
		tmp.Gpu -= float64(ct) * t.Cost.Gpu
		ramUsed := uint64(ct) * t.Cost.Ram
		if ramUsed >= tmp.Ram {
			tmp.Ram = 0
		} else {
			tmp.Ram -= ramUsed
		}
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

// OwnerID returns the machine ID assigned to this TaskEngine.
func (e *TaskEngine) OwnerID() int { return e.ownerID }

// TestONLY_SetPollDuration overrides the DB polling interval (useful for tests).
func (e *TaskEngine) TestONLY_SetPollDuration(d time.Duration) { e.pollDuration.Store(d) }

// checkNodeFlags reads the cordon (unschedulable) and restart_request flags
// from the DB. This runs on the background poller goroutine — not the scheduler
// thread — to avoid adding DB latency to the event loop. The result is applied
// to yieldBackground atomically so the scheduler sees it on its next iteration.
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

const singletonRunNowPollInterval = 30 * time.Second

// singletonRunNowPoller is a single goroutine per node that polls the DB
// for singleton tasks with run_now_request=true. When found, it sets the
// corresponding atomic flag so that SingletonTaskAdder bypasses its interval
// on the next IAmBored cycle.
// This is one query per node per 30s regardless of the number of singleton tasks.
func (e *TaskEngine) singletonRunNowPoller() {
	timer := time.NewTimer(singletonRunNowPollInterval)
	defer timer.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-timer.C:
			timer.Reset(singletonRunNowPollInterval)
		}

		var requested []struct {
			TaskName string `db:"task_name"`
		}
		err := e.db.Select(e.ctx, &requested,
			`SELECT task_name FROM harmony_task_singletons WHERE run_now_request = TRUE`)
		if err != nil {
			log.Errorw("singletonRunNowPoller: failed to query", "error", err)
			continue
		}

		if len(requested) == 0 {
			continue
		}

		flags := singletonRunNowFlags()
		for _, r := range requested {
			if f, ok := flags[r.TaskName]; ok {
				f.Store(true)
				log.Infow("singleton run-now request detected", "task", r.TaskName)
			}
		}
	}
}

// AddTaskByName is the single entry point for creating new tasks in the system.
// It performs a transactional DB insert (owner_id=NULL), then emits a
// schedulerSourceAdded event so the scheduler immediately considers the new
// work without waiting for a DB poll cycle.
//
// The event emission is done in a goroutine to avoid blocking the caller
// (which may be an Adder listener or IAmBored callback) on channel backpressure.
// The scheduler will broadcast this task to peers via TellOthers(newTask).
//
// Duplicate detection relies on the caller's extra func: if the transaction
// violates a unique constraint, the task already exists and is silently skipped.
// Serialization errors are retried with exponential backoff.
func (e *TaskEngine) AddTaskByName(name string, extra func(TaskID, *harmonydb.Tx) (bool, error)) {
	var tID TaskID
	retryWait := time.Millisecond * 100
retryAddTask:
	_, err := e.db.BeginTransaction(e.ctx, func(tx *harmonydb.Tx) (bool, error) {
		// create taskID (from DB)
		err := tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time) 
          VALUES ($1, $2, CURRENT_TIMESTAMP) RETURNING id`, name, e.ownerID).Scan(&tID)
		if err != nil {
			return false, fmt.Errorf("could not insert into harmonyTask: %w", err)
		}
		return extra(tID, tx)
	})

	if err != nil {
		if harmonydb.IsErrUniqueContraint(err) {
			log.Debugf("addtask(%s) saw unique constraint, so it's added already.", name)
			return
		}
		if harmonydb.IsErrSerialization(err) {
			time.Sleep(retryWait)
			retryWait *= 2
			goto retryAddTask
		}
		log.Errorw("Could not add task. AddTasFunc failed", "error", err, "type", name)
		return
	}

	err = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(taskNameTag, name),
	}, TaskMeasures.AddedTasks.M(1))
	if err != nil {
		log.Errorw("Could not record added task", "error", err)
	}

	// Emit to the scheduler so it can attempt immediate local scheduling
	// and notify peers. The goroutine prevents blocking if the channel is full.
	if tID > 0 && nil != e.taskMap[name] {
		go func() {
			e.schedulerChannel <- schedulerEvent{
				TaskID:   tID,
				TaskType: name,
				Source:   schedulerSourceAdded,
			}
		}()
	}
}
