/*
Package harmonytask implements a pure (no task logic), distributed task
manager with event-driven scheduling and peer-to-peer coordination.

# Architecture Overview

The system is built around three cooperating layers:

 1. **Scheduler** (scheduler.go) — A single-threaded event loop that
    maintains an in-memory map of available tasks and decides when to
    attempt work. Events arrive from local task additions, peer
    notifications, task completions, and a background DB poller. The
    single-threaded design eliminates mutex contention on the hot path
    and guarantees that CanAccept() and resource accounting never race.

 2. **Peering** (peering.go) — On startup each node connects to every
    known peer (from harmony_machines) over HTTP. Peers exchange JSON
    messages for three verbs: newTask, reserve, and started. This lets
    nodes react to cluster-wide changes in milliseconds instead of
    waiting for the next DB poll, dramatically reducing task-start
    latency for latency-sensitive pipelines.

 3. **TaskEngine** (harmonytask.go) — The public API and glue. It owns
    the handler registry, the scheduler channel, and the peering
    instance. AddTaskByName writes to the DB, emits a scheduler event,
    and the scheduler broadcasts to peers — all without blocking the
    caller on network I/O.

# Key Design Decisions

**Event-driven over polling.** The previous design polled the DB every
few seconds for every task type, creating O(nodes × task_types) queries
per interval. The new design uses DB polling only as a fallback safety
net (POLL_RARELY = 30s). Normal operation is driven by events: local
adds, peer notifications, and task completions. This cuts steady-state
DB load by an order of magnitude.

**Offloading work from the scheduler thread.** The scheduler thread
must stay responsive to events. Heavy operations are pushed elsewhere:
  - DB queries run on a background poller goroutine.
  - CanAccept() is pre-computed by the background poller and written
    directly into each handler's cache (via SetAcceptCache with mutex).
  - Node flag checks (cordon/restart) run on the poller goroutine.
  - Task execution runs in per-task goroutines.
  - Network I/O (peer messages) is fire-and-forget from goroutines.

**Bundling rapid events.** When many tasks are added in quick
succession (e.g., a batch of sector jobs), the bundler coalesces them
into a single scheduling attempt after a 10ms quiet period. This
prevents the scheduler from doing redundant CanAccept + DB claim
round-trips for each individual task.

**Reservations for important tasks.** A node can "reserve" the next
task of a given type, setting aside resources so that when the current
task finishes, the reserved task can start immediately without competing
for capacity. This is critical for TimeSensitive pipelines (e.g.,
WindowPost) where latency between pipeline stages matters.

NOTE: The reservation protocol is not yet fully realized. The open
problem is cluster-wide over-reservation: if every node independently
reserves one task of the same type, the cluster holds back resources
on N nodes for work that only one node will actually run. A future
protocol extension needs to coordinate reservations across peers so
only the appropriate number of nodes reserve resources.

# Mental Model

	Things that block tasks:
	    - Task type not registered on any running node
	    - Max concurrency reached (per-type or shared limiters)
	    - Resource exhaustion (CPU, RAM, GPU, storage)
	    - CanAccept() refuses (task-specific logic, e.g., wrong miner)

	Ways tasks start (event sources):
	    - Peer notification: another node added a task (milliseconds)
	    - Local addition: this process called AddTaskByName (immediate)
	    - Task completion: freed resources trigger re-evaluation
	    - DB poll fallback: background poller finds unclaimed work (30s)

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
