# Optional per-instance SDR start pacing

Optional start pacing spreads SDR task entry on one process without changing
ordinary resource checks, task concurrency limits, or database ownership.

## Contract

`Subsystems.SealSDRMinStartInterval` retains its duration key and zero default.
Zero preserves the existing unpaced batch path, even when jitter is enabled.
Negative intervals reject SDR task construction instead of becoming unlimited.
`Subsystems.SealSDRStartJitter` retains its boolean key and false default.
These fields are read when tasks are constructed; changing them requires restart.

The paced unit is an **SDR task's entry into `Do` on one Curio process**, after
ordinary scheduler eligibility, resource capacity, authoritative SQL ownership,
and storage claim. It is not the start of the native SDR computation, which
occurs later after task-local preparation. It is not a global rate limiter,
sector quota, sealing concurrency limit, or substitute for database ownership.
UnsealSDR and SupraSeal batch tasks are not changed. CPU accounting, FFI backend,
existing minimum-queue and maximum-task settings remain independent.

With a positive interval, at most one provisional start is reserved per instance.
Reservation happens outside speculative/cached `CanAccept`; candidate checks do
not consume the interval. Claim loss/error, storage failure, or cancellation
before `Do` releases only that reservation's token. At `Do` entry the token is
committed and the minimum interval begins. Task errors, panic, or retries after
that entry do not refund it. Stale or repeated cancellation cannot clear another
attempt's reservation. No lock is held for SQL, storage calls, task execution,
logging, or waiting; no scheduler sleep is added.

## Phase and lifecycle

When jitter is enabled, a first start or a start after more than twice the
interval of inactivity waits for the next stable phase in that interval. Phase
is the existing SHA-256 construction, now keyed by `CURIO_NODE_NAME`, a separator,
and the instance's advertised listen identity. Both inputs are required
and distinguish instances sharing the same host or node name. Keep it stable
across restarts. Changing this identity changes phase; hashes can still collide.
Different phases do not guarantee collision-free starts across the cluster.

Wall time selects the phase once. The resulting wait is latched and measured
with process-local monotonic elapsed time. Repeated polls cannot keep moving
the deadline; wall-clock jumps cannot extend that latched wait or bypass the
minimum interval. Phase waiting holds neither task ownership nor storage.
After phase expiry, insufficient resources or lost claims may delay actual
start past the nominal phase; there is no catch-up burst.

| Lifecycle | Behavior |
| --- | --- |
| First start, jitter off | Immediately eligible after ordinary prerequisites. |
| First start, jitter on | Wait at most one interval for the stable phase. |
| Continuous work / retry | Minimum interval measured from the previous `Do` entry. |
| Long idle | Re-latch one phase wait; do not accumulate missed slots. |
| Claim or storage failure | Cancel provisional reservation; no interval charge. |
| Cancellation before `Do` | No interval charge; existing task retry semantics remain. |
| Failure after `Do` starts | Keep interval charge, including pre-native-computation failures. |
| Recovery after restart | Apply the same pacing hook. Existing recovery code disowns work it cannot currently accept, making it discoverable by normal scheduling; no separate bypass or sleep is added. |
| Config change/restart | Construct a fresh pacer with the new interval and stable identity. In-memory previous-start history is not persisted. |
| Graceful shutdown | Already dispatched task contexts retain the existing shutdown contract. No new starts are scheduled once the engine stops scheduling. |

Restart resets the minimum-interval history. Jitter re-phases the first start but
does not promise minimum spacing across two process lifetimes. Fleet-wide or
durable pacing would require a different policy and is outside this topic.

## Operational diagnostics

The `cu/seal` logger emits these structured INFO events:

- `SDR start pacing configured`: once on successful task configuration, including
  disabled pacing. Fields include `pacing_enabled`, `min_start_interval`, the
  configured `start_jitter` flag, `jitter_offset`, and `blocked_log_interval`.
  A true jitter flag with a zero interval still means pacing is disabled.
- `SDR start delayed`: the first refused reservation and at most once per minute
  thereafter per pacer, shared across tasks and reasons. `reason` distinguishes
  `min start interval`, `start jitter phase`, and `reservation pending`.
  `remaining` is derived from monotonic elapsed time; `next_start_at` is its
  wall-clock estimate at `observed_at`. For a pending reservation, claim/storage
  preparation has no known completion deadline: both values are `unknown` and
  `next_start_known` is false. `reservation_token` identifies that provisional
  reservation, not a committed start. Suppressed reason changes do not reset
  the one-minute log budget.
- `SDR Do entry committed`: once for a successful start-token commit at the
  existing scheduler entry hook, with `task`, `reservation_token`, `observed_at`,
  and the next minimum-interval wait. No such event is emitted for `CanAccept`,
  a provisional reservation, a stale token, or a cancelled entry. Later task
  failure does not refund the interval or erase this event. This is not evidence
  that native SDR computation or sealing has completed.

Snapshots are copied under the pacer mutex; formatting and every logger call
occur after unlocking. The separate diagnostic rate limiter also releases its
mutex before logging. Reading/formatting a snapshot never latches a phase,
reserves a token, or consumes an interval. It uses no new scheduler timer,
background goroutine, persistence, or configuration setting.

Rate limiting uses process-local elapsed time, not wall time. Wall-clock changes
may change the displayed next-start estimate but cannot change admission. The
estimate is not a promised dispatch time: resources, claims, later idle-phase
selection, and scheduling can delay entry. A snapshot can become stale before
the logger returns, and concurrent log lines need not arrive in event order;
use `observed_at` and process-local tokens for interpretation. Tokens and log
throttling reset on process restart. Diagnostics do not provide cluster-wide
ordering or a distributed rate guarantee. No node name or listen address is
included in the new events.

## Evidence and limits

Database-free tests execute the production pacer and scheduler reservation/run
helpers with injected elapsed/wall clocks, cancellation, and a simultaneous
reservation barrier. They include 43m45s and 25m20s as regression inputs, not
defaults or recommendations. Tests guard against recursive logging mutexes
and speculative `CanAccept` consumption: admission uses one locked state
transition with diagnostic emission after unlocking.

Diagnostic regressions additionally compare every admission-state field before
and after repeated snapshots/formatting/logging, force concurrent refused starts,
re-enter diagnostics from a log sink, and hold a sink at a barrier while verifying
reservation cancellation and committed-interval enforcement can proceed. They
check all three blocked reasons, monotonic log throttling across wall jumps,
configuration fields, and committed-only entry events. These are executable
database-free tests, not production-log or database validation.

The actual scheduler admission body is also exercised with injected ownership
outcomes and storage-claim failures: lost claims, claim errors, cancellation,
storage errors, failed ownership release, and restart recovery all cancel the
reservation without starting work. The production wrapper supplies its existing
SQL methods; call-site assertions additionally protect reserve/claim/storage/Do
ordering. These unit tests do not execute PostgreSQL/Yugabyte or prove distributed
claim behavior. Real scheduler/database integration and role-specific native
builds remain separate validation gates.
