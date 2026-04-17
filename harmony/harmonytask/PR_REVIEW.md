# PR: harmonytasknotify — Event-driven scheduler with peer-to-peer task notification

## Summary

Replaces the polling-only task scheduler with an event-driven scheduler that
uses peer-to-peer RPC to notify other nodes about new tasks, reservations,
and work starts. Removes the `Follows` mechanism in favor of explicit
`AddTaskByName` calls. Adds integration tests for the scheduler and peering.

### Key changes

- **New scheduler** (`scheduler.go`): single-threaded event loop reading from
  `schedulerChannel`. Events arrive from local `AddTaskByName`, peering
  messages, task completions, and retries. A `bundler` coalesces rapid-fire
  events before attempting work. DB polling remains as a fallback on a
  configurable interval.
- **Peering layer** (`peering.go`): on startup, each node connects to all
  known `harmony_machines` peers, performs an identity + capability handshake,
  then exchanges binary task messages (`new`, `reserve`, `started`).
- **PeerHTTP transport** (`lib/harmony_peer_http/`): HTTP POST based peer
  communication mounted at `/peer/v1`. Each message is a single POST with
  `X-Peer-ID` header.
- **`AddTaskByName`** moved from per-handler `AddTask` to a single method on
  `TaskEngine`. It inserts the DB row, emits a `schedulerSourceAdded` event,
  and the scheduler broadcasts `TellOthers`.
- **`Follows` removed**: all task-chaining now goes through `AddTaskByName`.
  The `Follows` field, `followWorkInDB` method, and `FOLLOW_FREQUENCY` const
  are gone. All `Follows: nil` annotations removed from task implementations.
- **`considerWork` now accepts `[]task` + `eventEmitter`** instead of
  `[]TaskID`. It emits `TaskStarted`, `TaskCompleted`, and retry events back
  to the scheduler.
- **`recordCompletion` uses `context.Background()`** so it always completes
  even during graceful shutdown.
- **CanAccept caching**: when the DB claim accepts fewer tasks than
  `CanAccept` approved, the remainder is cached for the next poll cycle.
- **`test_support.go` reduced**: only exports `PipeNetwork`/`PipeNode`
  (in-memory `PeerConnectorInterface`). All scheduler-internal test hooks
  removed.
- **Integration tests** (`itests/harmonytask/`): scheduler tests
  (routing, concurrency, retry, shared tasks, max concurrency) and peering
  end-to-end tests, all using real DB and full `TaskEngine` objects.
- **Self-peering fix**: `startPeering` skips connecting to self.

---

## Workflow trace

### 1. Local add → local execution → success  ✓

    AddTaskByName
      → DB INSERT (owner_id=NULL)
      → schedulerChannel ← schedulerSourceAdded
    Scheduler
      → add to availableTasks, bundleCollector(10ms), TellOthers(newTask)
    bundleSleep fires
      → waterfall → pollerTryAllWork → considerWork
      → SQL claim (SET owner_id, SKIP LOCKED) → goroutine
    Goroutine
      → EmitTaskStarted → scheduler removes from availableTasks, TellOthers(started)
      → Do() → success
      → recordCompletion: DELETE task, INSERT history
      → EmitTaskCompleted → scheduler waterfall (free capacity)

### 2. Local add → peer picks up  ✓

    Node A: AddTaskByName("X") → schedulerSourceAdded
    Scheduler A: add to local, TellOthers(newTask) → wire message to Node B
    Node B: handlePeerMessage → schedulerSourcePeerNewTask
    Scheduler B: add to availableTasks, bundleCollector
    bundleSleep → waterfall → considerWork → SQL claim
    Both race; SKIP LOCKED ensures single winner.

### 3. Task failure → retry  ✓

    Goroutine: Do() returns (false, err)
    Deferred:
      recordCompletion: UPDATE owner_id=NULL, retries++ (or DELETE if max)
      EmitTaskCompleted → scheduler waterfall
      EmitTaskNew (schedulerSourceAdded, Retries=old_count)
    Scheduler:
      add to availableTasks(UpdateTime=now, Retries=old)
      bundleCollector → 10ms → waterfall
      pollerTryAllWork filters by RetryWait(old_count) vs time.Since(now)
      → task not ready yet (only 10ms elapsed) → skipped
    Next DB poll:
      taskSourceDb.GetTasks reads fresh retries from DB
      If RetryWait elapsed → considerWork → claim → run

    Note: TellOthers broadcasts to peers with retries=0 (messageRenderTaskSend
    omits retries). Peers may attempt the task immediately. This is acceptable:
    the failure may be node-specific, and distributed retry is desirable.

### 4. Task failure → max failures → permanent drop  ✓

    recordCompletion: retries >= MaxFailures-1 → DELETE from harmony_task
    EmitTaskNew fires → schedulerSourceAdded → added to local availableTasks
    bundleSleep → waterfall → SQL claim → row doesn't exist → no work
    Stale local entry cleaned up on next DB poll (taskSourceDb replaces map).
    TellOthers to peers → same: they try, DB row gone, harmless.

### 5. DB poll fallback  ✓

    time.After(pollDuration) fires
    waterfall(taskSourceDb) → SQL SELECT unowned tasks → replaces local cache
    pollerTryAllWork → considerWork → claim → run
    This is the safety net: all tasks are eventually discovered even without
    peer notification.

### 6. Recover (startup resurrection)  ✓

    New() queries harmony_task WHERE owner_id=us
    considerWork(WorkSourceRecover, tasks) → skips SQL claim (already ours)
    Goroutines start, emit EmitTaskStarted
    Scheduler not started yet → events buffer in channel (pre-sized)
    startScheduler() → processes buffered events
    schedulerSourceTaskStarted: delete from availableTasks (no-op, wasn't added)
    TellOthers(started) → peers learn about it. Correct.

### 7. IAmBored → generate work  ⚠️

    pollerTryAllWork reaches IAmBored section (no existing work accepted)
    IAmBored callback calls AddTaskByName
    AddTaskByName does DB INSERT then: schedulerChannel <- event (BLOCKING)
    BUT: pollerTryAllWork runs on the scheduler goroutine
    The scheduler goroutine is the sole reader of schedulerChannel
    If IAmBored generates >100 tasks (channel capacity), the 101st
    AddTaskByName blocks forever → DEADLOCK.

    In practice IAmBored typically adds 1-2 tasks, but CC sector creation
    could theoretically add many. Worth protecting against.

### 8. Peer handshake (symmetric)  ✓

    Both sides of a connection call handlePeer simultaneously.
    Both send "i:<addr>", both read the other's identity.
    Both query DB for the other's task capabilities.
    Both add peer to p.peers and p.m.
    Both enter receive loop.

    If A and B connect to each other simultaneously, two connections form.
    TellOthers sends to both → receiver gets duplicate events.
    Duplicates are harmless: map overwrites, delete no-ops, extra DB attempts
    fail gracefully via SKIP LOCKED.

### 9. Bundler coalescing  ✓

    Rapid events: bundleCollector called N times for same taskType
    First call creates timer + goroutine. Subsequent calls Reset(10ms).
    After 10ms of quiet: timer fires, goroutine does delete(timers, key)
    then sends on output. Scheduler processes waterfall.
    Next burst: key absent → new timer + goroutine. Correct.

    Edge case: goroutine blocked on output send while new event arrives.
    delete() already ran, so new timer is created → two sends for same type.
    Scheduler runs two waterfalls. Extra work but harmless.

---

## Open issue

**IAmBored deadlock** (Flow 7): `AddTaskByName` does a blocking channel
send on the scheduler goroutine. If `IAmBored` adds enough tasks to fill
the 100-slot channel buffer, the scheduler deadlocks. Consider using a
non-blocking send with overflow to a local slice, or limiting IAmBored to
one AddTask call per invocation.
