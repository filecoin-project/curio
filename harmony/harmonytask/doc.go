/*
Package harmonytask implements a pure (no task logic), distributed task
manager. This clean interface lets task implementers avoid scheduling and
cluster coordination; they only implement work units.

Tasks are small pieces of work split out by hardware limits, parallelism,
reliability, or other reasons.

The hot path is event-driven scheduling with peer-to-peer coordination: nodes
tell each other about new work, reservations, and starts over HTTP so the
cluster reacts in milliseconds without waiting for a database round-trip for
every change. The database still drives authoritative claims (UPDATE ...
SKIP LOCKED) and holds the queue. A background DB poller remains as a safety
net and for periodic housekeeping (e.g. POLL_RARELY), and the poller
goroutine also runs heavier queries such as precomputing CanAccept caches and
node cordon/restart flags, so not all work is “push-driven.”

The task system tries to run any work the node can do up to resource limits.
As queues build, it can reserve resources for soft-claimed (reserved) tasks
so higher-priority work is not starved and clusters can respect run order.
Ordering priority may starve lower priorities, so within a class prefer the
oldest tasks first.

	ex: prio: prio.P0 | prio.LessThan('x', 'y', 'z') | prio.PipelineOrder('a1', 'b2', 'c3')

The scheduler is single-threaded on the decision path, so CanAccept() and
resource accounting do not need mutexes between it and tryWork.

# Architecture Overview

The system is built around three cooperating layers:

 1. **Scheduler** (scheduler.go) — A single-threaded event loop that
    maintains an in-memory map of available tasks and decides when to
    attempt work. Events arrive from local task additions, peer
    notifications, task completions, and a background DB poller. The
    single-threaded design avoids mutex contention on the hot path and
    keeps CanAccept() versus resource accounting consistent.

 2. **Peering** (peering.go) — On startup each node connects to every
    known peer (from harmony_machines) over HTTP. Peers exchange JSON
    messages for verbs such as newTask, reserve, and started. That
    replaces the old “poll the DB every few seconds per task type” steady
    state with push-style notifications, cutting average task-start
    latency for latency-sensitive pipelines.

 3. **TaskEngine** (harmonytask.go) — The public API and glue. It owns
    the handler registry, the scheduler channel, and the peering
    instance. AddTaskByName writes to the DB, emits a scheduler event,
    and the scheduler broadcasts to peers without blocking the caller on
    network I/O.

# Key Design Decisions

**Peers and events first; polling second.** Historically the stack polled the
DB often for every task type (O(nodes × task_types) queries per interval).
Normal operation is now driven by local adds, peer notifications, and task
completions. DB polling is a fallback (e.g. POLL_RARELY ≈ 30s) so nodes
eventually discover work if a notification is missed. Steady-state DB load
drops sharply; polling did not disappear entirely because the poller still
backs discovery and other periodic work.

**Offloading work from the scheduler thread.** The scheduler thread must stay
responsive. Heavy work runs elsewhere:
  - DB queries run on a background poller goroutine.
  - CanAccept() is pre-computed on the poller and cached on handlers (e.g.
    SetAcceptCache with mutex).
  - Node flag checks (cordon/restart) run on the poller goroutine.
  - Task execution runs in per-task goroutines.
  - Peer HTTP calls are fire-and-forget from goroutines.

**Bundling rapid events.** When many tasks land at once (e.g. a batch of
sector jobs), a bundler coalesces them into one scheduling attempt after a
short quiet period (~10ms), avoiding redundant CanAccept + claim cycles per
row.

**Reservations.** A node can reserve the next task of a type so that when
the current task finishes, the reserved one can start without competing for
capacity — important for time-sensitive pipelines (e.g. WindowPost).

NOTE: The reservation protocol is not fully realized cluster-wide. If every
node reserves one task of the same type independently, the cluster can hold
capacity on N nodes for work only one node will run. A future extension may
coordinate reservations across peers.

# Mental Model

Things that block tasks:
  - Task type not registered on any running node
  - Max concurrency reached (per-type or shared limiters)
  - Resource exhaustion (CPU, RAM, GPU, storage)
  - CanAccept() refuses (task-specific logic, e.g. wrong miner)

Ways tasks start (event sources):
  - Peer notification: another node added or announced work (fast path)
  - Local addition: this process called AddTaskByName (immediate)
  - Task completion: freed resources trigger re-evaluation
  - DB poll fallback: background poller finds unclaimed work (safety net)

Ways tasks get added:
  - Adder() goroutine: each task type's listener (chain events, etc.)
  - IAmBored: idle capacity triggers speculative work creation
  - External: any code path calling AddTaskByName

How duplicate tasks are avoided:
  - Unique constraints on extra-info tables (task-specific)
  - SKIP LOCKED in the claim query prevents double-claiming

# Database Tables

harmony_task: The queue of uncompleted work. Rows are INSERT'd by
AddTaskByName (owner_id=NULL) and claimed via UPDATE SET owner_id
with SKIP LOCKED. Completed tasks are DELETE'd; failed tasks have
owner_id reset to NULL with retries incremented.

harmony_task_history: Audit log of completed and permanently-failed
tasks. Grows continuously and needs periodic cleanup.

harmony_machines / harmony_machine_details: Node registry managed by
lib/harmony/resources. Used for peer discovery, resource tracking,
and the cordon/restart flag mechanism.

# Usage

 1. Implement TaskInterface for each task type.
 2. Pass all active implementations to New().
 3. The engine handles scheduling, peering, retries, and cleanup.
*/
package harmonytask
