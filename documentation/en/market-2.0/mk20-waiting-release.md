# Bounded MK20 Waiting Release

Curio releases accepted DDO deals from `market_mk20_pipeline_waiting` into
`market_mk20_pipeline` in bounded polling passes. This mechanism limits new
pipeline participation; it is not a general sealing-work scheduler and it
does not repair earlier incident state.

## Configuration

The release policy is controlled by two dynamic settings:

- `Ingest.MK20PipelineInsertBatch`: maximum number of deals that may be
  released successfully in one pass when the value is positive.
- `Ingest.MK20PipelineInsertMaxActive`: maximum number of incomplete rows in
  the global MK20 pipeline when the value is positive.

Both settings default to `0`.

`MK20PipelineInsertBatch = 0` means that there is no operator-configured
successful-deal limit. It does **not** mean that one pass processes the whole
waiting backlog. Every pass inspects at most 64 candidates, including when
the configured batch value is zero.

`MK20PipelineInsertMaxActive = 0` disables the active-row cap. The waiting
release path still participates in the database singleton gate so that its
transactions remain serialized with other participating release instances.

A missing dynamic value is treated as its default value of zero. A negative
value is invalid: Curio reports the configuration error and releases no new
waiting deals. The two values are read once at the start of a pass. A dynamic
change applies to the next pass; a transaction already in progress may finish
using the earlier snapshot.

Production values for these settings have not yet been selected or validated.
Operators should not treat a value used in a test fixture as a recommended
production setting.

## Active-Row Accounting

The active count is defined exactly as:

```sql
SELECT COUNT(*)
FROM market_mk20_pipeline
WHERE complete = FALSE;
```

This is a global row quota across all providers, not a per-provider quota and
not a count of distinct deal IDs. An aggregate deal can consume more than one
slot because every generated subpiece pipeline row is counted.

The following incomplete rows consume slots:

- rows that have not yet been assigned to a sector;
- rows with no task or no task owner;
- rows whose processing or sealing work is in progress;
- rows already assigned to sectors;
- rows associated with a failed sector, while `complete` remains false.

Rows that exist only in the waiting table do not consume slots. A row returns
its slot only when the existing downstream path marks it `complete = TRUE`,
normally after storage movement and indexing finish. Lowering the configured
cap below the current active count stops new releases; it does not delete or
rewrite existing work to force the count down.

The cap describes incomplete MK20 pipeline participation. It does not measure
all sector attempts, orphan files, capacity-commitment work, or actual disk
usage.

## Polling and Progress

The existing pipeline-insert loop continues to run every five seconds. Each
pass has a 30-second context budget and inspects at most 64 candidates using
`ORDER BY id`, a keyset cursor, and `LIMIT`. The cursor is only a progress
hint; it is neither ownership nor quota state.

When either release setting is positive, a pass first uses a bounded
`EXISTS` query to determine whether the global waiting table contains any
row. An empty table returns before the fresh pressure and active-count
queries. This observation is only a cheap early exit: a row inserted after it
may wait for the next five-second pass, while every discovered candidate is
still revalidated after the transactional gate write. The existence check is
global rather than cursor-relative, so a cursor beyond the last ID still
wraps and examines waiting rows from the beginning. With both settings at
zero, bounded candidate selection remains the emptiness check, avoiding an
extra query on every nonempty default-mode pass.

The cursor advances past malformed, temporarily deferred, failed, and
over-capacity candidates and wraps after reaching the end of the waiting
table. This best-effort traversal lets later valid deals be considered across
passes even when an earlier row cannot be released, but it is not a
starvation-free scheduling guarantee. A deal that does not fit the currently
available capacity remains waiting and can be reconsidered after slots are
returned.

When `MK20PipelineInsertBatch` is positive, a pass also stops after that many
successful deal releases. A pass stops early when the global active cap has no
remaining slot. Context cancellation and database errors fail closed and do
not add an unbounded retry loop. Database transaction retry/backoff may delay
return after cancellation, so that behavior must be included in runtime
validation.

Committed releases from earlier in a pass remain committed if a later
candidate fails or times out. Curio wakes the existing deal poller only when
at least one release transaction actually committed, and it preserves that
wake even when a later candidate fails.

When either setting is positive and the waiting-table probe finds work, Curio
performs fresh MK20 and sector pressure checks before selecting candidates.
Pressure, or an error while checking it, stops only new waiting releases for
that pass. Existing pipeline work keeps progressing and the next nonempty
pass checks again. When both settings are zero, this additional fresh pressure
check is disabled; the existing cached API pressure behavior and its cache
lifetime are unchanged.

## Transaction and Concurrency Model

An additive database migration creates a waiting-release gate table and its
singleton row. Each deal release transaction performs a real update of that
row, creating a shared database write-conflict point across Curio processes.
The updated token is not an active counter or a persistent reservation.

After updating the gate row, the same transaction authoritatively:

1. rechecks that the candidate is still waiting;
2. rejects a waiting/pipeline inconsistency for the same deal;
3. counts global incomplete rows when the active cap is enabled;
4. loads the deal and plans the exact pipeline row cost;
5. verifies available capacity without splitting aggregate deals;
6. inserts all planned download, reference, and pipeline rows;
7. verifies the inserted row count and the post-insert active count;
8. deletes exactly one waiting row; and
9. commits before reporting the deal as released.

The HTTP/offline single-piece paths cost one pipeline row. An aggregate costs
the number of subpiece pipeline rows produced by the existing insertion path.
Malformed or zero-row plans fail. An aggregate larger than the configured
maximum remains atomic and waiting; Curio does not release a partial
aggregate.

If any validation, insertion, postcondition, or waiting-row deletion fails,
the transaction rolls back all of that deal's changes, including download and
piece-reference rows. A missing gate row, or a gate update affecting anything
other than exactly one row, also fails closed. There is no fallback to the old
unbounded path.

The database helper may rerun the transaction callback after a serialization
conflict. Release results and per-attempt state are reinitialized on every
callback execution, and the callback contains only retry-safe database work;
it performs no HTTP, Lotus, download, or filesystem side effect.

The positive active cap is guaranteed only among participating release
instances that use the same positive cap and begin from a count at or below
that cap. Every participating MARKET instance must use the same release
settings. A configuration transition may briefly put in-flight passes on
different snapshots, so settings should be changed with that scope in mind.

The following are outside the guarantee:

- upload paths or direct database insertion paths that bypass this waiting
  release gate;
- an older Curio binary that does not participate in the gate;
- concurrently participating instances with different caps, or one instance
  using `MK20PipelineInsertMaxActive = 0` while another uses a positive cap;
- duplicate or orphan sector attempts that already exist outside the waiting
  release transaction;
- configuration changes spanning transactions that captured different
  dynamic snapshots.

## Deal Semantics Preserved

This release control does not change DDO scheduling data. An omitted
`StartEpoch` keeps its existing meaning, and an explicitly supplied
`StartEpoch`, `direct_start_epoch`, or `direct_end_epoch` is preserved.
Requested DDO `Duration` is also unchanged, including a requested duration of
5,256,000 epochs.

`StartEpoch` is a deal-start deadline, not a throughput throttle. Bounded
waiting release controls admission to the downstream pipeline; it does not
replace normal storage ingestion, sealing, MoveStorage, or indexing behavior.

## Migration, Rollback, and Incident Scope

The migration only creates and initializes the singleton release gate. It does
not modify existing deals, pipeline rows, task state, sector state, or incident
artifacts, and it does not recover earlier failed or orphaned attempts.

Rolling back to a binary that uses the previous release path removes the
protection even if the gate table remains present. Mixed old/new release
binaries are therefore outside the concurrency guarantee.

## Required YugabyteDB Validation

Unit and model tests are not a substitute for executing the real locking path
on the production YugabyteDB version. Before treating a particular build as
production ready, run that exact commit's opt-in integration tests against an
isolated disposable YugabyteDB instance and record the server version and
effective isolation level. Results from an earlier reference implementation
do not validate a later port. At minimum, validate:

- two independent database handles competing for the final active slot, with
  exactly one successful release;
- two transactions processing the same waiting ID, with one insertion and a
  safe skip/retry for the other;
- a stale transaction snapshot followed by the expected serialization retry;
- transaction callback re-execution without leaked result or counter state;
- rollback of pipeline, download/reference, and waiting-table changes after a
  partial insertion failure;
- fail-closed behavior when the singleton gate row is missing;
- one global cap shared by deals belonging to different providers; and
- release resuming after normal completion returns capacity.

These checks must use separate database connections and the real database
conflict behavior, not a process-local mutex. Until that YugabyteDB run is
completed successfully, production concurrency readiness remains unverified.

The opt-in tests record `SELECT version()`, the session default isolation,
and the value reported by `SHOW transaction_isolation` inside a HarmonyDB
transaction. The production-core retry test also records that it requested
`REPEATABLE READ` and the value SQL reports for that stale-snapshot
transaction.
These are requested/reported isolation values only. `SHOW
transaction_isolation` does not establish whether YugabyteDB's read-committed
isolation flag is enabled or which effective isolation implementation handled
the transaction, so the tests report effective YugabyteDB isolation as
`UNVERIFIED` unless that server configuration is independently confirmed in
the disposable test environment. A later YugabyteDB run should record the
server version and independently confirmed state of
`yb_enable_read_committed_isolation`; if that evidence is unavailable, its
effective isolation result must remain `UNVERIFIED` rather than being inferred
from `SHOW transaction_isolation`.

The focused fixture requires an explicit opt-in and an explicitly configured
loopback test target before it opens any connection. It reads only the
dedicated `CURIO_MK20_RELEASE_ITEST_HOST`,
`CURIO_MK20_RELEASE_ITEST_PORT`,
`CURIO_MK20_RELEASE_ITEST_DATABASE`, and
`CURIO_MK20_RELEASE_ITEST_USER` target variables (plus the optional
`CURIO_MK20_RELEASE_ITEST_PASSWORD`); normal Curio database variables and
connection defaults are not used. The host must be a literal loopback IP, and
execution also requires `CURIO_MK20_RELEASE_ITEST=1`. It applies the actual
`harmony/harmonydb/sql/20260906-mk20-release-gate.sql` migration. It verifies
singleton initialization, idempotent reapplication without resetting the
live token, and the singleton constraint. The call-site test executes
`CurioStorageDealMarket.insertDDODealInPipeline` with real fresh pressure SQL,
positive batch and active limits, cursor wrap, and slot return after a
fixture-owned completion. The retry test runs the production release core and
HarmonyDB transaction adapter, forces a real serialization failure after a
provisional release, and verifies that the retried callback uses fresh
capacity or waiting state without leaking the rolled-back result.
