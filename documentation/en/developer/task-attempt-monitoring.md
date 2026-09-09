# Current-attempt task monitoring

Cluster Tasks separates owned work from a bounded Pending preview. Owned rows
display **Took**, measured from the current task's `Do` entry; Pending rows retain
posted-based **Waiting**. Ownership alone is not proof of execution: a prepared
or newly claimed task displays `awaiting-start` and a dash. Missing provenance or
a future timestamp displays unknown. Existing API ownership-age fields and old
History records are unchanged.

The runner creates a fresh task/owner/attempt identity after acceptance and
ownership acquisition, before storage acquisition. This includes same-owner
recovery. Immediately before `Do`, it captures the worker clock once and shares
that instant with the asynchronous live timestamp write and the new History
record. This measures the task body, not native FFI/kernel execution. The earlier
run-registry timestamp still governs preemption. Pre-entry failures do not claim
that a task body ran.

## Execution and database effects

Preparation and failure-release each have one five-second batch budget. A failed
preparation is **not dispatched**: the existing task/owner CAS releases ownership
for later scheduling. This is a real scheduler readiness change, including for
time-sensitive tasks, not a claim of zero execution-path overhead. The engine's
resource/acceptance/early-skip checks still run first; no storage is held while
preparing. A database outage may delay admission or leave ownership for recovery
if its bounded release also fails. It never fabricates a successful task.

The start writer requires the exact task, owner and attempt token, a NULL start,
and prepared provenance. A delayed prior attempt cannot replace a newer start.
It has a five-second context and is canceled and joined when `Do` returns or
panics. A panic inside the asynchronous writer is contained and logged separately
instead of escaping the task runner's panic boundary. Timestamp write failure
leaves live timing unconfirmed, but does not
change the task's execution result. Per accepted task, telemetry adds one
preparation UPDATE, one asynchronous conditional start UPDATE and one bounded
writer goroutine. A short task may finish before its timestamp persists; History
still uses its captured entry. There is no new scheduler, timer RPC or global lock.

The exact existing migrations `20260909-task-ownership-age.sql` and
`20260909-task-attempt-start.sql` are reused without renaming, backfill or a second
writer/trigger. The latter adds nullable fields and clears them on ownership
change/release. Reapplication preserves existing starts and one trigger. Upgrading
an already instrumented schema does not reinterpret timestamps. Downgrading to
older workers leaves the additive columns unused; there is no destructive SQL
rollback or historical rewrite. Mixed-version old workers do not report Do entry;
in particular an old same-owner recovery without an ownership write cannot be
recognized as a new attempt. Roll out compatible workers before relying on timing.

Worker/database clocks must be synchronized. Future starts are rejected, but a
past clock skew cannot be detected from these fields. A server snapshot anchors
monotonic local interpolation; pause, hidden/disconnected views or refresh failure
freeze that estimate. A new snapshot/attempt may reduce Took. Display ticks neither
reorder rows nor issue per-second RPCs. Both duration columns use text-start
alignment. Neutral generic defaults remain a 500-row maximum, 500 pending preview
limit and consecutive coalescing enabled; presentation preferences are not worker
scheduling priority.

## Verification

Database-free tests exercise the actual preparation/entry helpers, scheduler
prepare-failure boundary, writer cancellation/join, response conversion and UI
clock/polling logic. SQL tests use the production HarmonyDB adapter and snapshot
source, plus the actual migration files, under the `integration` build tag:

```sh
go test -tags=cgo,fvm,nosupraseal,integration -count=1 -timeout=2m \
  ./harmony/harmonytask ./web/api/webrpc \
  -run 'TestTaskAttemptSQL|TestClusterTaskSnapshotSQL'
```

They require `CURIO_TASK_ATTEMPT_ITEST=1` and dedicated suffixes `_HOST`, `_PORT`,
`_DATABASE`, `_USER`, optionally `_PASSWORD`. HOST must be a literal loopback IP;
ordinary Curio/libpq defaults are not accepted. Each test owns a random schema.
The SQL tests cover fresh/upgrade migration, stale identities, same-owner recovery,
reassignment, existing starts, FK ownership release, rollback, actual Do entry and
bounded snapshot rows including NULL provenance. Sequential independent-handle
stale-writer checks prove conditional row effects, not simultaneous lock contention.
The bounded query's counts can examine more rows than its returned-row limit.

Current disposable PostgreSQL execution and browser evidence belong to the review
validation report. Yugabyte execution is separate evidence; compilation or static
SQL checks are never equivalent to an executed database test.
