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

	Success bool // for schedulerSourceTaskCompleted: true = done, false = cancelled/failed

	// DBTasks is populated only for schedulerSourceDBPoll events. It contains
	// the complete snapshot of unowned tasks from the DB, keyed by task type.
	// This replaces the scheduler's in-memory available task map, ensuring
	// stale entries (claimed by others, deleted) are garbage-collected.
	DBTasks map[string][]task
}

// schedulerSource identifies the origin of a scheduler event. Each source
// triggers different state-management logic in the scheduler event loop:
//   - Added/PeerNewTask: add to available tasks, trigger scheduling
//   - TaskStarted/PeerStarted: remove from available tasks, update reservations
//   - TaskCompleted: trigger scheduling (freed resources may allow new work)
//   - DBPoll: wholesale replacement of the available task map
//   - PeerReserved: mark task as reserved by another node (resource hold)
type schedulerSource byte

const (
	schedulerSourceTaskStarted
	schedulerSourceAdded         schedulerSource = iota // this node added a task via AddTaskByName
	schedulerSourcePeerNewTask                          // a peer notified us about a new task
	schedulerSourcePeerStarted                          // a peer claimed and started a task
	schedulerSourceTaskCompleted                        // a local task goroutine finished (success or failure)
	schedulerSourceTaskStarted                          // a local task goroutine began execution
	schedulerSourceDBPoll                               // background poller delivered fresh DB state
	schedulerSourceInitialPoll
	schedulerSourceStartTimeSensitive
)

// chokePoint caps the number of task IDs held in memory per task type.
// Beyond this limit, the scheduler enters "choked" mode: it works with
// whatever IDs it has and relies on the next DB poll to fetch more.
// This prevents unbounded memory growth when thousands of tasks queue up
// (e.g., during bulk sector onboarding).
const chokePoint = 1000

// taskSchedule tracks the available (unowned) tasks of a single type
// within the scheduler's in-memory state. It supports reservations:
// a node can earmark the next task it intends to run, holding resources
// aside so the task can start immediately when capacity frees up.
// NOTE: cluster-wide reservation coordination is incomplete — see doc.go.
type taskSchedule struct {
	hasID  map[TaskID]task
	choked bool // FUTURE PR: we choked the adding of TaskIDs for mem savings.
	// In this state, we try what we have, and go to DB if we need more.
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
//     and decides when to attempt work via pollerTryAllWork. By design,
//     this goroutine performs no DB reads, no network waits, and no blocking
//     I/O — only channel reads, map updates, and function calls to
//     considerWork (which does a single fast SQL UPDATE for claiming).
//
// The event loop handles each source differently:
//   - DBPoll: replaces the entire available-tasks map with fresh DB state,
//     preserves valid reservations, and triggers scheduling if new tasks
//     appeared.
//   - Added: inserts the task into the local map, broadcasts to peers.
//     TimeSensitive tasks trigger immediate scheduling; others are bundled.
//   - PeerNewTask: same as Added but from a remote node (no broadcast).
//   - TaskStarted: removes the task from available, advances resource
//     reservations, broadcasts "started" to peers.
//   - PeerStarted: same as TaskStarted but from a remote node.
//   - TaskCompleted: triggers scheduling since freed resources may allow
//     new work to start.
//   - PeerReserved: a peer is holding resources for this task. If they
//     have a higher machine ID (deterministic tie-break), we yield and
//     reserve a different task. (Protocol is incomplete — see doc.go.)
//
// The bundler coalesces rapid-fire non-TimeSensitive events (e.g., batch
// Adder calls producing many tasks) into a single scheduling attempt after
// a 10ms quiet period, reducing redundant CanAccept + DB claim cycles.
func (e *TaskEngine) startScheduler() {
	// Goroutine 1: periodic dead-machine cleanup
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
	// Goroutine 2: background DB poller.
	// Runs CanAccept off the scheduler thread so the event loop stays fast.
	// Checks node flags (cordon/restart) and delivers a complete task snapshot.
	go func() {
		timer := time.NewTimer(0) // fire immediately for initial poll
		defer timer.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-timer.C:
				dbTasks := e.pollAllTaskTypes()

				// Node flags are checked here (poller thread) rather than on
				// the scheduler thread to keep DB latency off the hot path.
				if schedulable, err := e.checkNodeFlags(); err != nil {
					log.Errorw("failed to check node flags", "error", err)
				} else {
					e.yieldBackground.Store(!schedulable)
				}

				// Pre-compute CanAccept for all task types and write results
				// directly into each handler's cache. This avoids calling
				// CanAccept (which may check disk paths, SP config, etc.)
				// on the scheduler event loop. The handler's acceptMu
				// protects against concurrent reads from the scheduler.
				if dbTasks != nil && !e.yieldBackground.Load() {
					for _, h := range e.handlers {
						tasks := dbTasks[h.Name]
						if len(tasks) == 0 {
							continue
						}
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
							h.SetAcceptCache(accepted)
						}
					}
				}

				e.schedulerChannel <- schedulerEvent{
					Source:  schedulerSourceDBPoll,
					DBTasks: dbTasks,
				}

				// Self-heal the poll rate: if we degraded to POLL_FREQUENTLY
				// because peers were unreachable at startup, restore to
				// POLL_RARELY once peering is healthy again.
				if e.pollDuration.Load().(time.Duration) == POLL_FREQUENTLY && e.peering.HasPeers() {
					log.Infow("peering restored, switching to rare polling")
					e.pollDuration.Store(POLL_RARELY)
				}

				timer.Reset(e.pollDuration.Load().(time.Duration))
			}
		}
	}()
	// Goroutine 3: the scheduler event loop (single-threaded, no blocking I/O).
	go func() {
		bundleCollector, bundleSleep := bundler()

		// availableTasks is the scheduler's authoritative view of unowned work.
		// Populated by DB polls and incrementally updated by events.
		// Only accessed from this goroutine — no locks needed.
		availableTasks := map[string]*taskSchedule{} // TaskType -> TaskID -> bool FUTURE PR: mem savings.
		for _, h := range e.handlers {
			availableTasks[h.Name] = &taskSchedule{hasID: make(map[TaskID]task)}
		}
		tryStartNow := func(taskName string) {
			if err := e.tryStartTask(taskName, taskSourceLocal{availableTasks, e.peering}, eventEmitter{e.schedulerChannel}); err != nil {
				log.Errorw("failed to try start task", "taskType", taskName, "error", err)
			}
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
					// Replace the entire available-tasks map with the DB snapshot.
					// This garbage-collects stale entries (tasks claimed/deleted by
					// others) while preserving reservations that are still valid.
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
						// If our previously reserved task was claimed by someone else
						// (no longer in DB results), pick a new reservation.
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
							log.Errorw("failed tryAllWork", "error", err)
						}
					}

				case schedulerSourceAdded:
					// Local task addition: insert into available set, broadcast to peers.
					// TimeSensitive tasks (e.g., WindowPost) skip bundling for
					// immediate scheduling.
					if _, ok := availableTasks[event.TaskType]; ok {
						pt := event.PostedTime
						if pt.IsZero() {
							pt = time.Now().UTC()
						}
						availableTasks[event.TaskType].hasID[event.TaskID] = task{ID: event.TaskID, UpdateTime: time.Now(), PostedTime: pt, Retries: event.Retries}
						if h := e.taskMap[event.TaskType]; h != nil && h.TimeSensitive {
							if _, err := e.tryStartTask(event.TaskType, ts, ee); err != nil {
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
					e.peering.TellNewTask(event.TaskType, event.TaskID, event.Retries, pt)
				case schedulerSourcePeerNewTask:
					// A peer added a task. Insert into our available set (if we
					// handle this type) and schedule. Respects chokePoint to
					// bound memory.
					t, ok := availableTasks[event.TaskType]
					if !ok {
						continue
					}
					if len(t.hasID) > chokePoint {
						t.choked = true
						continue
					}
					pt := event.PostedTime
					if pt.IsZero() {
						pt = time.Now().UTC()
					}
					t.hasID[event.TaskID] = task{ID: event.TaskID, UpdateTime: time.Now(), PostedTime: pt, Retries: event.Retries}
					if h := e.taskMap[event.TaskType]; h != nil && h.TimeSensitive {
						if _, err := e.tryStartTask(event.TaskType, ts, ee); err != nil {
							log.Errorw("failed tryAllWork", "error", err)
						}
					} else {
						bundleCollector(event.TaskType)
					}

				case schedulerSourceTaskStarted:
					// A local goroutine claimed and started a task. Remove from
					// available set and advance the resource reservation for
					// TimeSensitive types to keep the next task ready to go.
					avail := availableTasks[event.TaskType]
					delete(avail.hasID, event.TaskID)
					e.peering.TellOthers(messageTypeStarted, event.TaskType, event.TaskID)

				case schedulerSourcePeerStarted:
					// A peer started a task. Remove from our available set so we
					// don't try to claim it. If it was our reserved task, pick
					// a new one to hold resources for.
					avail, ok := availableTasks[event.TaskType]
					if !ok {
						continue
					}
					delete(avail.hasID, event.TaskID)
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
					tasks := taskSourceLocal{availableTasks, e.peering}.GetTasks(event.TaskType)
					if len(tasks) > 0 {
						h.considerWork(WorkSourcePreempt, tasks, eventEmitter{e.schedulerChannel})
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
			case <-time.After(e.pollDuration.Load().(time.Duration)): // fast life & early-gather at Go_1.26
				if err := e.pollerTryAllWork(ts, ee); err != nil {
					log.Errorw("failed tryAllWork", "error", err)
					continue
				}
			}
			// FUTURE: RetryWait could start timers.
		}
	}()
} // FUTURE Move all harmony_task writers to taskEngine.AddTask() to transmit over the RPC.

// taskSource abstracts where the scheduler gets its list of available tasks.
// The local implementation reads from the in-memory map; a DB-backed
// implementation could be used for fallback scenarios.
type taskSource interface {
	GetTasks(taskName string) []task
}

func (e *TaskEngine) tryStartTask(taskName string, taskSource taskSource, eventEmitter eventEmitter) error {

	h := e.taskMap[taskName]
	if h != nil && h.TimeSensitive {
		if _, capErr := h.AssertMachineHasCapacity(); capErr != nil {
			if tasks := taskSource.GetTasks(taskName); len(tasks) > 0 {
				for _, t := range tasks {
					go e.preemptForTimeSensitive(h, t.ID)
				}
			}
		}
	} else {
		err := e.pollerTryAllWork(taskSource, eventEmitter)
		if err != nil {
			log.Errorw("failed to try waterfall", "error", err)
			return err
		}
	}

	return nil
}

// taskSourceLocal serves tasks from the scheduler's in-memory available-tasks
// map. GetTasks returns the reserved task first (the one this node is holding
// resources for), then all remaining tasks. This ordering ensures the
// resource-reserved task gets first shot at CanAccept and claiming.
type taskSourceLocal struct {
	availableTasks map[string]*taskSchedule
	peering        *peering
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

// Emits are called from other threads, so we cannot change t.availableTasks.
// This pattern keeps the scheduler single-threaded (no locks on
// availableTasks) while allowing concurrent task execution to communicate
// state changes: a task starting (remove from available), completing
// (re-evaluate capacity), or failing with retry (re-add to available).
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
func (ee eventEmitter) EmitTaskCompleted(taskName string, success bool) {
	ee.schedulerChannel <- schedulerEvent{
		TaskType: taskName,
		Source:   schedulerSourceTaskCompleted,
		Success:  success,
	}
}

// expects single-threaded caller of

func (t taskSourceDb) GetTasks(taskName string) []task {
	tasks := []task{}
	err := t.db.Select(context.Background(), &tasks, `SELECT id, update_time, posted_time, retries FROM harmony_task WHERE name = $1 AND owner_id IS NULL ORDER BY posted_time ASC, id ASC LIMIT $2`, taskName, chokePoint+1)
	newHas := lo.Associate(tasks, func(t task) (TaskID, task) {
		return t.ID, t
	})
	t.availableTasks[taskName] = &taskSchedule{
		hasID:  newHas,
		choked: len(tasks) > chokePoint,
	}

	return tasks
}

// pollAllTaskTypes queries the DB for all unowned tasks across all registered
// task types in a single round-trip. This is the only DB read in the
// scheduling hot path, and it runs on the background poller goroutine — never
// on the scheduler thread.
//
// Returns nil on error so the scheduler preserves its existing in-memory state
// and reservations rather than replacing them with an empty/partial snapshot.
// Tasks beyond chokePoint per type are dropped to bound memory.
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
// (single-threaded), but the timer goroutines access the timers map
// concurrently, hence the mutex.
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
