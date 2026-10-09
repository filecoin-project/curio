# Committed retry clocks

Retry handoff already exists in Harmony. This change carries the committed
`harmony_task.update_time` instant through local notifications, peer messages,
polling, and claim comparison instead of restarting the wait at each receiver.
The embedded migrations make this column TIMESTAMPTZ; it must not be converted
to a session-local timestamp before decoding. Transaction retries reset the
provisional result, and only the committed result is advertised.

The scheduler keeps newer retry snapshots over delayed notifications and arms
one wake for its earliest known future deadline. Unknown legacy positive-retry
messages retain a conservative receive-time clock until polling supplies an
authoritative snapshot. Retry zero has no backoff and does not require a
fabricated timestamp match. SQL rechecks owner absence, retry count, the
positive-retry instant, and the deadline before claiming.

Preemption preserves the already-satisfied retry clock and failure count. Its
conditional release checks the current owner and retry count, so another owner
can immediately reclaim the returned task. This standalone topic does not add
attempt tokens or acquisition generations; the separate admission topic adds
those fences. Ordinary task failure limits and storage policy are unchanged.

## Tests

`TestRetrySQL*` uses the real embedded HarmonyDB migration runner in a random
owned namespace. Build tags are `cgo,fvm,nosupraseal,integration`. Execution
requires `CURIO_TASK_ATTEMPT_ITEST=1` and explicit `_HOST`, `_PORT`, `_DATABASE`,
`_USER`, and optional `_PASSWORD` variables with that prefix. Hosts must be
literal loopback; load balancing and connection fallbacks are disabled. Do not
use ordinary Curio or libpq configuration. The dedicated test role must be
able to create/drop its owned schema and temporary roles for the session-zone
matrix. Every participant is bounded and the fixture cleans up only its own
namespace and roles.

The four writer/claimant zone pairs are UTC/UTC, Asia/Seoul/Asia/Seoul,
UTC/Asia/Seoul, and Asia/Seoul/UTC. Tests check stored instants, early/due claims,
stale snapshots, retry zero, and immediate independent-handle preemption reclaim.
PostgreSQL results are not evidence of Yugabyte effective isolation or native
PoRep throughput. Mixed-version peer behavior remains conservative, not a
guarantee that old writers implement the new authoritative claim contract.
