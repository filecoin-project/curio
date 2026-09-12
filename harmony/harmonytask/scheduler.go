package harmonytask

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/samber/lo"

	"github.com/filecoin-project/curio/harmony/resources"
)

// schedulerEvent is the message type flowing through the scheduler channel.
// Every scheduling decision is triggered by an event. The Source field
// determines how the scheduler updates its internal state before attempting
// work. Events are lightweight value types — heavy data (DB results,
// pre-computed CanAccept results) is attached only to DB poll events.
type schedulerEvent struct {
	TaskID
	TaskType   string
	Source     schedulerSource
	PeerID     int64 // set for peer-originated events; used for resource-reservation tie-breaking
	Retries    int
	PostedTime time.Time // for new-task events; FIFO within a task type
	UpdateTime time.Time // for retries; zero means time.Now() at handle time

	Success bool // for schedulerSourceTaskCompleted: true = done, false = cancelled/failed

	// DBTasks is populated only for schedulerSourceDBPoll events. It contains
	// the successfully queried snapshots, keyed by task type. Omitted types
	// preserve their previous state; a present empty slice clears that type.
	DBTasks map[string][]task
}

// schedulerSource identifies the origin of a scheduler event. Each source
// triggers different state-management logic in the scheduler event loop:
//   - Added/PeerNewTask: add to available tasks, trigger scheduling
//   - TaskStarted/PeerStarted: remove from available tasks
//   - TaskCompleted: trigger scheduling (freed resources may allow new work)
//   - DBPoll: wholesale replacement of the available task map
type schedulerSource byte

const (
	schedulerSourceAdded         schedulerSource = iota // this node added a task via AddTaskByName
	schedulerSourcePeerNewTask                          // a peer notified us about a new task
	schedulerSourcePeerStarted                          // a peer claimed and started a task
	schedulerSourceTaskCompleted                        // a local task goroutine finished (success or failure)
	schedulerSourceTaskStarted                          // a local task goroutine began execution
	schedulerSourceDBPoll                               // background poller delivered fresh DB state
	schedulerSourceInitialPoll
	schedulerSourceStartTimeSensitive
)

func taskFromSchedulerEvent(event schedulerEvent) task {
	pt := event.PostedTime
	if pt.IsZero() {
		pt = time.Now().UTC()
	}
	ut := event.UpdateTime
	if ut.IsZero() {
		ut = time.Now()
	}
	return task{ID: event.TaskID, UpdateTime: ut, PostedTime: pt, Retries: event.Retries, retryClockUnknown: event.Retries > 0 && event.UpdateTime.IsZero()}
}

// chokePoint caps the number of task IDs held in memory per task type.
// Beyond this limit, the scheduler enters "choked" mode: it works with
// whatever IDs it has and relies on the next DB poll to fetch more.
// This prevents unbounded memory growth when thousands of tasks queue up
// (e.g., during bulk sector onboarding).
const chokePoint = 1000

// taskSchedule tracks the available (unowned) tasks of a single type
// within the scheduler's in-memory state. Accessed only from the scheduler
// goroutine, so no locking is needed.
type taskSchedule struct {
	hasID  map[TaskID]task
	choked bool // true for when this list is uncomfortably long for RAM.
}

// startScheduler launches three long-running goroutines that form the heart
// of the event-driven scheduling system:
//
//  1. **Cleanup goroutine**: periodically removes stale machine entries from
//     the DB (dead nodes whose heartbeats have expired).
//
//  2. **Background DB poller**: queries unowned tasks and node flags at the
//     configured poll interval. This runs off the scheduler thread to avoid
//     blocking the event loop with DB round-trips. It also pre-computes
//     CanAccept() for each task type so the scheduler can skip that
//     potentially expensive call. Results are sent as a single
//     schedulerSourceDBPoll event.
//
//  3. **Scheduler event loop**: the single-threaded core that reads events
//     from schedulerChannel, maintains the in-memory available-tasks map,
//     and decides when to attempt work via pollerTryAllWork.
func (e *TaskEngine) startScheduler() {
	// Goroutine 1: periodic dead-machine cleanup
	go func() {
		for {
			select {
			case <-e.cfg.ctx.Done():
				log.Infof("scheduler stopped")
				return
			case <-time.After(cleanupFrequency):
				resources.CleanupMachines(e.cfg.ctx, e.cfg.db)
			}
		}
	}()
	// Goroutine 2: background DB poller.
	// Runs CanAccept off the scheduler thread so the event loop stays fast.
	// Checks node flags (cordon/restart) and delivers a complete task snapshot.
	go func() {
		timer := time.NewTimer(0) // fire immediately for initial poll
		defer timer.Stop()
		for {
			select {
			case <-e.cfg.ctx.Done():
				return
			case <-timer.C:
				dbTasks := e.pollAllTaskTypes()

				if schedulable, err := e.checkNodeFlags(); err != nil {
					log.Errorw("failed to check node flags", "error", err)
				} else {
					wasCordoned := e.atomics.yieldBackground.Swap(!schedulable)
					if !schedulable && !wasCordoned {
						e.cancelPendingAdmissions()
					}
				}

				// Pre-compute CanAccept for all task types and write results
				// into each handler's acceptcache. This avoids calling
				// CanAccept on the scheduler event loop. The cache's internal
				// mutex protects against concurrent reads from the scheduler.
				if dbTasks != nil && !e.atomics.yieldBackground.Load() {
					for _, h := range e.handlers {
						tasks := dbTasks[h.Name]
						if len(tasks) == 0 {
							continue
						}

						sort.Slice(tasks, func(i, j int) bool {
							return taskLessByPostedTime(tasks[i], tasks[j])
						})
						dbTasks[h.Name] = tasks
						ids := make([]TaskID, len(tasks))
						for i, t := range tasks {
							ids[i] = t.ID
						}
						accepted, err := h.CanAccept(ids, e)
						if err != nil {
							log.Errorw("CanAccept pre-check failed", "taskType", h.Name, "error", err)
							continue
						}
						if len(accepted) > 0 {
							h.accept.Add(toInt64s(accepted))
						}
					}
				}

				e.schedulerChannel <- schedulerEvent{
					Source:  schedulerSourceDBPoll,
					DBTasks: dbTasks,
				}

				// Self-heal the poll rate: if we degraded to pollFrequently
				// because peers were unreachable at startup, restore to
				// pollRarely once peering is healthy again.
				if e.atomics.pollDuration.Load().(time.Duration) == pollFrequently && e.peering.HasPeers() {
					log.Infow("peering restored, switching to rare polling")
					e.atomics.pollDuration.Store(pollRarely)
				}

				timer.Reset(e.atomics.pollDuration.Load().(time.Duration))
			}
		}
	}()
	go e.runScheduler()
}

// runScheduler is the single owner of candidate and admission maps.
func (e *TaskEngine) runScheduler() {
	bundleCollector, bundleSleep := bundler(e.cfg.ctx)

	// availableTasks is the scheduler's authoritative view of unowned work.
	// Populated by DB polls and incrementally updated by events.
	// Only accessed from this goroutine — no locks needed.
	availableTasks := map[string]*taskSchedule{}
	for _, h := range e.handlers {
		availableTasks[h.Name] = &taskSchedule{hasID: make(map[TaskID]task)}
	}
	ee := eventEmitter{schedulerChannel: e.schedulerChannel, availableTasks: availableTasks, ctx: e.cfg.ctx}
	tryStartNow := func(taskName string) {
		if err := e.tryStartTask(taskName, taskSourceLocal{availableTasks}, ee); err != nil {
			log.Errorw("failed to try start task", "taskType", taskName, "error", err)
		}
	}
	ts := taskSourceLocal{availableTasks}
	defer e.cancelPendingAdmissions()
	retryTimer := time.NewTimer(time.Hour)
	defer retryTimer.Stop()
	var retryArmed time.Time

	for {
		e.drainRecovery(ee)
		// A stream of other events must not discard a deadline that became
		// due between select iterations.
		if !retryArmed.IsZero() && !time.Now().Before(retryArmed) {
			if err := e.pollerTryAllWork(ts, ee); err != nil {
				log.Errorw("failed retry waterfall", "error", err)
			}
		}
		retryArmed = time.Time{}
		if !retryTimer.Stop() {
			select {
			case <-retryTimer.C:
			default:
			}
		}
		var retryWake <-chan time.Time
		if next := nextRetryDeadline(availableTasks, e.taskMap, time.Now()); !next.IsZero() {
			retryArmed = next
			retryTimer.Reset(max(0, time.Until(next)))
			retryWake = retryTimer.C
		}

		select {
		case <-e.cfg.ctx.Done():
			log.Infof("scheduler stopped")
			return

		case <-e.admissionWake:
			for _, h := range e.handlers {
				h.drainAdmissions()
			}
			if err := e.pollerTryAllWork(ts, ee); err != nil {
				log.Errorw("admission wake failed", "error", err)
			}
		case event := <-e.schedulerChannel:
			switch event.Source {

			case schedulerSourceDBPoll:
				// Replace only the successfully queried task-type snapshots.
				// This garbage-collects stale entries (tasks claimed/deleted by
				// others). Always re-enter the waterfall afterward: RetryWait
				// may have elapsed for existing IDs, CanAccept cache was
				// refreshed, and IAmBored must run when capacity remains with
				// no claimable work (not only when new IDs appear).
				applyDBTaskSnapshot(availableTasks, event.DBTasks)

				if err := e.pollerTryAllWork(ts, ee); err != nil {
					log.Errorw("failed tryAllWork", "error", err)
				}

			case schedulerSourceAdded:
				// Local task addition: insert into available set, broadcast to peers.
				// TimeSensitive tasks (e.g., WindowPost) skip bundling for
				// immediate scheduling.
				if _, ok := availableTasks[event.TaskType]; ok {
					rememberTask(availableTasks[event.TaskType], taskFromSchedulerEvent(event))
					if h := e.taskMap[event.TaskType]; h != nil && h.TimeSensitive {
						if err := e.tryStartTask(event.TaskType, ts, ee); err != nil {
							log.Errorw("failed tryAllWork", "error", err)
						}
					} else {
						bundleCollector(event.TaskType)
					}
				}
				pt := event.PostedTime
				if pt.IsZero() {
					pt = time.Now().UTC()
				}
				e.peering.TellNewTask(event.TaskType, event.TaskID, event.Retries, pt, event.UpdateTime)
			case schedulerSourcePeerNewTask:
				// A peer added a task. Insert into our available set (if we
				// handle this type) and schedule. Respects chokePoint to
				// bound memory.
				t, ok := availableTasks[event.TaskType]
				if !ok {
					continue
				}
				if len(t.hasID) >= chokePoint {
					t.choked = true
					continue
				}
				rememberTask(t, taskFromSchedulerEvent(event))
				if h := e.taskMap[event.TaskType]; h != nil && h.TimeSensitive {
					if err := e.tryStartTask(event.TaskType, ts, ee); err != nil {
						log.Errorw("failed tryAllWork", "error", err)
					}
				} else {
					bundleCollector(event.TaskType)
				}

			case schedulerSourceTaskStarted:
				// Peer notify (+ idempotent local delete; NoteClaimed usually
				// already cleared the ID on the claim path).
				if avail := availableTasks[event.TaskType]; avail != nil {
					delete(avail.hasID, event.TaskID)
				}
				e.peering.TellOthers(messageTypeStarted, event.TaskType, event.TaskID, task{Retries: event.Retries, UpdateTime: event.UpdateTime})

			case schedulerSourcePeerStarted:
				// A peer started a task. Remove from our available set so we
				// don't try to claim it.
				avail, ok := availableTasks[event.TaskType]
				if !ok {
					continue
				}
				forgetPeerStarted(avail, event)
			case schedulerSourceTaskCompleted:
				// A local task finished (success or failure). Freed resources
				// may allow previously blocked work to start.
				if err := e.pollerTryAllWork(ts, ee); err != nil {
					log.Errorw("failed tryAllWork", "error", err)
				}
			case schedulerSourceStartTimeSensitive:
				h := e.taskMap[event.TaskType]
				if h == nil {
					continue
				}
				plan := e.computePreemptionPlan(h.Cost)
				if plan == nil {
					log.Debugw("preemption plan no longer viable", "task", event.TaskType, "taskID", event.TaskID)
					continue
				}
				e.executePreemption(plan)
				tasks := taskSourceLocal{availableTasks}.GetTasks(event.TaskType)
				if len(tasks) > 0 {
					h.considerWork(workSourcePreempt, tasks, ee)
					e.atomics.Count_TimeSensitivePreempt.Add(1)
				}
			case schedulerSourceInitialPoll:
				if err := e.pollerTryAllWork(ts, ee); err != nil {
					log.Errorw("failed tryAllWork", "error", err)
				}
			default:
				log.Errorw("unknown scheduler source", "source", event.Source)
			}
		case taskName := <-bundleSleep:
			tryStartNow(taskName)
		case <-retryWake:
			retryArmed = time.Time{}
			if err := e.pollerTryAllWork(ts, ee); err != nil {
				log.Errorw("failed retry waterfall", "error", err)
			}
		case <-time.After(idleTryInterval):
			// Quiet-period tick: IAmBored only. Does not claim known work
			// or query the DB — discovery/claims stay on events + the rare
			// background poller. Per-task passcall.Every throttles work.
			e.invokeIAmBored()
		}
	}
}

// taskSource abstracts where the scheduler gets its list of available tasks.
// The local implementation reads from the in-memory map; a DB-backed
// implementation could be used for fallback scenarios.
type taskSource interface {
	GetTasks(taskName string) []task
}

func (e *TaskEngine) tryStartTask(taskName string, taskSource taskSource, eventEmitter eventEmitter) error {
	h := e.taskMap[taskName]
	if h != nil && h.TimeSensitive {
		// When the machine is already full, free room by preempting cheaper
		// non-time-sensitive work. When there is capacity, fall through to the
		// waterfall below so the task starts immediately (oldestFirstSeq orders
		// time-sensitive types first); otherwise a lone time-sensitive task on
		// an idle node would wait for the next fallback poll before starting.
		cap, capErr := h.AssertMachineHasCapacity()
		if cap == 0 || capErr != nil {
			if tasks := taskSource.GetTasks(taskName); len(tasks) > 0 {
				for _, t := range tasks {
					go e.preemptForTimeSensitive(h, t.ID)
				}
			}
			return nil
		}
	}
	err := e.pollerTryAllWork(taskSource, eventEmitter)
	if err != nil {
		log.Errorw("failed to try waterfall", "error", err)
		return err
	}

	return nil
}

// taskSourceLocal serves tasks from the scheduler's in-memory available-tasks
// map. GetTasks returns tasks sorted by posted time (FIFO within the type).
type taskSourceLocal struct {
	availableTasks map[string]*taskSchedule
}

func (t taskSourceLocal) GetTasks(taskName string) []task {
	taskObject := t.availableTasks[taskName]
	tasks := make([]task, 0, len(taskObject.hasID))
	for _, tk := range taskObject.hasID {
		tasks = append(tasks, tk)
	}
	if len(tasks) == 0 {
		return tasks
	}
	sort.Slice(tasks, func(i, j int) bool {
		return taskLessByPostedTime(tasks[i], tasks[j])
	})
	return tasks
}

// eventEmitter is the feedback channel from task goroutines back to the
// scheduler. Because task goroutines run concurrently with the scheduler,
// they cannot modify the scheduler's in-memory state directly. Instead,
// they emit events that the scheduler processes on its own thread.
//
// Emits are called from other threads, so they must not mutate availableTasks
// directly — only the scheduler goroutine may. NoteClaimed is the exception:
// it runs on the scheduler thread from considerWork before task goroutines
// start, so clearing the local available set is race-free. TaskStarted events
// still notify peers (and idempotently re-delete).
type eventEmitter struct {
	schedulerChannel chan schedulerEvent
	availableTasks   map[string]*taskSchedule // nil outside the scheduler loop
	ctx              context.Context
}

// NoteClaimed removes claimed IDs from the in-memory available set immediately
// so a subsequent waterfall in the same process cannot re-claim them.
func (ee eventEmitter) NoteClaimed(taskName string, ids []TaskID) {
	if ee.availableTasks == nil || len(ids) == 0 {
		return
	}
	avail := ee.availableTasks[taskName]
	if avail == nil {
		return
	}
	for _, id := range ids {
		delete(avail.hasID, id)
	}
}

func (ee eventEmitter) EmitTaskStarted(taskName string, taskID TaskID, state ...task) {
	event := schedulerEvent{
		TaskID:   taskID,
		TaskType: taskName,
		Source:   schedulerSourceTaskStarted,
	}
	if len(state) > 0 {
		event.Retries = state[0].Retries
		event.UpdateTime = state[0].UpdateTime
	}
	ee.emit(event)
}

func (ee eventEmitter) EmitTaskNew(taskName string, task task) {
	ee.emit(schedulerEvent{
		TaskID:     task.ID,
		TaskType:   taskName,
		Source:     schedulerSourceAdded,
		Retries:    task.Retries,
		PostedTime: task.PostedTime,
		UpdateTime: task.UpdateTime,
	})
}
func (ee eventEmitter) EmitTaskCompleted(taskName string, success bool) {
	ee.emit(schedulerEvent{
		TaskType: taskName,
		Source:   schedulerSourceTaskCompleted,
		Success:  success,
	})
}

func (ee eventEmitter) emit(event schedulerEvent) {
	var done <-chan struct{}
	if ee.ctx != nil {
		done = ee.ctx.Done()
	}
	select {
	case ee.schedulerChannel <- event:
	case <-done:
	}
}

// applyDBTaskSnapshot runs on the scheduler goroutine. Missing keys preserve
// previous state; present empty keys clear a successfully queried task type.
func applyDBTaskSnapshot(available map[string]*taskSchedule, snapshot map[string][]task) {
	for taskName, tasks := range snapshot {
		if available[taskName] == nil {
			continue
		}
		previous := available[taskName]
		available[taskName] = &taskSchedule{
			hasID:  lo.Associate(tasks, func(t task) (TaskID, task) { return t.ID, t }),
			choked: len(tasks) >= chokePoint,
		}
		for _, t := range tasks {
			if old, ok := previous.hasID[t.ID]; ok {
				rememberTask(available[taskName], old)
			}
		}
	}
}

type polledTask struct {
	ID         TaskID    `db:"id"`
	Name       string    `db:"name"`
	UpdateTime time.Time `db:"update_time"`
	PostedTime time.Time `db:"posted_time"`
	Retries    int       `db:"retries"`
}

// pollAllTaskTypes enumerates unowned tasks on the background poller and then
// performs the optional task-specific bulk checks.
//
// A common query error preserves every type. A task-specific error omits only
// that type, so successful snapshots still reach the scheduler.
// Backing-work eligibility is checked before the per-type snapshot bound.
func (e *TaskEngine) pollAllTaskTypes() map[string][]task {
	return e.pollAllTaskTypesWithQuery(func(names []string) ([]polledTask, error) {
		var rows []polledTask
		err := e.cfg.db.Select(context.Background(), &rows,
			`SELECT id, name, update_time, posted_time, retries FROM harmony_task WHERE owner_id IS NULL AND name = ANY($1)`, names)
		return rows, err
	})
}

func (e *TaskEngine) pollAllTaskTypesWithQuery(query func([]string) ([]polledTask, error)) map[string][]task {
	names := make([]string, len(e.handlers))
	for i, h := range e.handlers {
		names[i] = h.Name
	}
	rows, err := query(names)
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
		result[r.Name] = append(result[r.Name], task{
			ID:         r.ID,
			UpdateTime: r.UpdateTime,
			PostedTime: r.PostedTime,
			Retries:    r.Retries,
		})
	}
	for _, h := range e.handlers {
		selected, err := filterPolledTasks(e.cfg.ctx, h, result[h.Name])
		if err != nil {
			log.Errorw("failed to filter task candidates", "name", h.Name, "error", err)
			delete(result, h.Name)
			continue
		}
		result[h.Name] = selected
	}
	return result
}

// bundleCollectionTimeout is the quiet period the bundler waits before firing.
// When a burst of events arrives (e.g., 50 tasks added in rapid succession),
// the bundler resets this timer on each event. Once 10ms passes with no new
// events for a task type, it fires a single scheduling attempt that considers
// all accumulated tasks at once. This dramatically reduces redundant
// CanAccept + DB claim round-trips during batch operations.
const bundleCollectionTimeout = time.Millisecond * 10

// bundler creates a coalescing timer system for non-TimeSensitive events.
// The returned bundler func is called from the scheduler thread to register
// an event; the returned channel fires when the quiet period expires.
//
// Thread safety: the bundler func is called from the scheduler goroutine
// (single-threaded), but the AfterFunc callbacks access the timers map
// concurrently, hence the mutex.
//
// The whole check-and-(reset|create) is done under the lock, closing the
// window where a callback could delete a timer between the map read and the
// Reset (which, with a manual <-t.C goroutine, would lose the wake). Using
// time.AfterFunc means a Reset on an already-fired timer simply re-runs the
// callback, and the callback only deletes its own entry, so the worst case is
// a harmless duplicate wake (pollerTryAllWork is idempotent) — never a lost one.
func bundler(contexts ...context.Context) (bundler func(string), bundleSleep <-chan string) {
	var done <-chan struct{}
	if len(contexts) > 0 {
		done = contexts[0].Done()
	}
	timers := make(map[string]*time.Timer)
	timerMx := sync.Mutex{}
	output := make(chan string)
	return func(taskType string) {
		timerMx.Lock()
		defer timerMx.Unlock()
		if t, ok := timers[taskType]; ok {
			t.Reset(bundleCollectionTimeout)
			return
		}
		var t *time.Timer
		t = time.AfterFunc(bundleCollectionTimeout, func() {
			timerMx.Lock()
			if timers[taskType] == t {
				delete(timers, taskType)
			}
			timerMx.Unlock()
			select {
			case output <- taskType:
			case <-done:
			}
		})
		timers[taskType] = t
	}, output
}

// taskLessByPostedTime defines FIFO order for the same task type: older posted_time first; unknown
// posted_time (zero) is treated as newest so DB-backed ordering wins once polled.
func taskLessByPostedTime(a, b task) bool {
	aUnk := a.PostedTime.IsZero()
	bUnk := b.PostedTime.IsZero()
	switch {
	case aUnk && bUnk:
		return a.ID < b.ID
	case aUnk:
		return false
	case bUnk:
		return true
	case a.PostedTime.Equal(b.PostedTime):
		return a.ID < b.ID
	default:
		return a.PostedTime.Before(b.PostedTime)
	}
}

func rememberTask(s *taskSchedule, t task) {
	if old, ok := s.hasID[t.ID]; ok {
		if old.Retries > t.Retries {
			return
		}
		if old.Retries == t.Retries {
			if !old.retryClockUnknown && t.retryClockUnknown {
				return
			}
			if old.retryClockUnknown == t.retryClockUnknown && !old.UpdateTime.Before(t.UpdateTime) {
				return
			}
		}
	}
	s.hasID[t.ID] = t
}

func forgetPeerStarted(s *taskSchedule, event schedulerEvent) {
	if old, ok := s.hasID[event.TaskID]; ok {
		if old.Retries > event.Retries || old.Retries == event.Retries && old.UpdateTime.After(event.UpdateTime) {
			return
		}
	}
	delete(s.hasID, event.TaskID)
}

func retryDeadline(t task, wait func(int) time.Duration) time.Time {
	if t.Retries == 0 || wait == nil {
		return time.Time{}
	}
	return t.UpdateTime.Add(wait(t.Retries))
}

func retryReady(t task, wait func(int) time.Duration, now time.Time) bool {
	return !now.Before(retryDeadline(t, wait))
}

func nextRetryDeadline(available map[string]*taskSchedule, handlers map[string]*taskTypeHandler, now time.Time) time.Time {
	var next time.Time
	for name, s := range available {
		h := handlers[name]
		if h == nil {
			continue
		}
		for _, t := range s.hasID {
			d := retryDeadline(t, h.RetryWait)
			if d.After(now) && (next.IsZero() || d.Before(next)) {
				next = d
			}
		}
	}
	return next
}
