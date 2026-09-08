# Finite MK20 Release Check (Operator Only)

This recipe is prepared and compiled, **NOT RUN**. Run it later only with
separate authorization against disposable PostgreSQL and, separately,
YugabyteDB **2025.2.2.2-b11**. No production target, shared operational schema,
payload, download, sealing, live API, or full-backlog drain is involved.
It is a small diagnostic sample, not a benchmark framework or cap-sizing rule.

## Target and Evidence Preconditions

- Use the exact follow-up commit and keep its source checkout available. The
  sampler extracts SQL literals from that checkout, not a second SQL copy.
- Use the existing explicit `CURIO_MK20_RELEASE_ITEST=1` guard plus all four
  dedicated target keys: `CURIO_MK20_RELEASE_ITEST_HOST`,
  `CURIO_MK20_RELEASE_ITEST_PORT`, `CURIO_MK20_RELEASE_ITEST_DATABASE`, and
  `CURIO_MK20_RELEASE_ITEST_USER`. Host must be a literal loopback IP, such as
  `127.0.0.1` or `::1`, not a DNS name; port must be explicit and valid.
  `CURIO_MK20_RELEASE_ITEST_PASSWORD` is optional and may be provided only through
  the operator's existing secret environment, never in a URI, command argument,
  log, or checked-in file. Do not use normal Curio/libpq defaults.
- Start from a sanitized shell with normal `CURIO_*`, `HARMONY*`, `PG*`,
  database URL variables, and unrelated integration opt-ins unset; then supply
  only the dedicated keys above. No credentials are printed by the recipe.
- The fixture creates a random `itest_...` namespace and two independent
  HarmonyDB pools, with load balancing explicitly disabled. Its cleanup drops
  only its own namespace. Never point it at an operational database. No server
  installation/startup instructions or production cleanup are provided here.
- Record server version, hardware/storage/topology, requested/default/reported
  transaction isolation, and the schema/index listing emitted by the sample.
  For Yugabyte, separately record the confirmed effective settings of
  `yb_enable_read_committed_isolation` and `enable_wait_queues` from the
  disposable server's operator-controlled configuration. Do not infer either
  flag or effective isolation from `SHOW transaction_isolation`. Unknown means
  **UNVERIFIED**. Preserve the default-isolation sample; the separate forced
  snapshot regression explicitly requests Repeatable Read.
- Have the disposable server's existing statement/error evidence available,
  scoped by the logged `mk20-release-itest-...` application name, backend/session,
  and `release_begin`/`release_end` timestamps. This is needed for retry counts;
  absent evidence makes that measurement **INCOMPLETE**, not zero retries.
  Do not enable broad logging or gather operational logs as part of this task.

## Exact Groups and Commands

These files have **no integration build tag**. On the validated macOS/OpenCL
toolchain the exact tags are `cgo,fvm,nosupraseal`; environment opt-in is what
permits a connection. Compilation must not be confused with execution:

```sh
env FFI_USE_OPENCL=1 LIBRARY_PATH=/opt/homebrew/lib \
  go test -c -tags=cgo,fvm,nosupraseal \
  -o /tmp/curio-release.test ./tasks/storage-market
env FFI_USE_OPENCL=1 LIBRARY_PATH=/opt/homebrew/lib \
  go test -c -tags=cgo,fvm,nosupraseal \
  -o /tmp/curio-release-core.test ./market/mk20release
```

With the dedicated target and opt-in supplied by the separately authorized
operator, run each group once (`-test.count=1`), not a repetition/soak loop:

```sh
/tmp/curio-release.test -test.v -test.count=1 -test.timeout=10m \
  -test.run='^TestMK20ReleaseDBAggregate(Capacity|InsertFailureRollsBack)$'

CURIO_MK20_RELEASE_COST_ITEST=1 /tmp/curio-release.test \
  -test.v -test.count=1 -test.timeout=10m \
  -test.run='^TestMK20ReleaseDBBoundedCostSample$'
```

The additional cost opt-in never substitutes for the standard release opt-in
and dedicated target checks. The aggregate group comprises `exact_fit`,
`insufficient_remaining_slots`, `larger_than_entire_cap`, and an insertion
failure after two real download/ref writes and one pipeline row. It invokes
`releaseMK20WaitingDeal` through the actual planner and insertion path. It
asserts zero partial state, preserved waiting/deal schedule, and successful
retry after removing only its own injected failure trigger.

The existing entry points remain available for correctness runs, separate
from the cost sample:

- `TestMK20ReleaseDBControlledPipelineInsertCallSite` (actual
  `insertDDODealInPipeline`, pressure, batching, cap, fixture-completion resume).
- `TestMK20ReleaseDBLastSlotIsGlobalAcrossProviders` and
  `TestMK20ReleaseDBSameWaitingDealReleasedOnce`.
- `TestMK20ReleaseDBGateMigrationIsIdempotent` and
  `TestMK20ReleaseDBStaleSnapshotRetriesCleanly` (supplementary probe).
- `TestMK20ReleaseDBCoreRetriesWithFreshAuthoritativeState` in `market/mk20release`
  (actual release core/adapter with controlled retry runner).

Use source inspection or `go list` before operator execution to confirm names
and selected files; do not infer SQL coverage from a package compiling.

## Fixed Sample and Stop Limits

The cost test runs two subcases only, aborting before the second if the first
fails:

| Per subcase | Fixed value |
| --- | --- |
| Waiting | 32,108 synthetic IDs with cloned valid offline deal JSON |
| Incomplete baseline | 2,216, then 4,096 |
| Completed population | 8,192 additional pipeline rows |
| Pressure context | `DoSnap=false`; empty SDR/task/open/parked tables |
| Release settings | Batch=4; MaxActive=baseline+5 (fixture values only) |
| Release sample | One pass, then two independent handles competing for one slot |
| Logical release calls | At most 6; at most 5 committed new single-piece rows |
| Read plans | 6 plain EXPLAIN + 6 EXPLAIN ANALYZE; no repetitions |
| Plan output | At most 64 KiB per plan |
| Measurement connection | statement timeout 10s, lock timeout 2s |
| Per subcase | 3-minute context after existing bounded fixture setup |
| Per pass/concurrent pair | 30-second context |
| Whole cost command | 10-minute Go test timeout, once per database |

Each of the six logical releases can make at most seven callback attempts in
the pinned HarmonyDB helper: at most 42 transaction attempts per subcase, 84
across the two subcases. This excludes finite fixture setup, twelve read-only
plan transactions per subcase, and verification reads. Errors stop the sample;
there is no added retry policy. The existing helper's serialization backoff can
sleep after cancellation (up to 18.4 seconds cumulatively across its seven
delays). A 30-second context budget is therefore not a hard retry wall-clock
guarantee. Record observed cancellation latency separately if a limit fires;
do not extend limits and rerun automatically.

The large fixture is inserted set-wise, not with 32,108 individual release
transactions. The baseline rows model row counts only; they are not a claim of
full production pipeline state. The focused schema retains the keys, types,
parked-piece active uniqueness and ref-to-piece cascading FK used by release,
but does not reproduce every production index/trigger or data distribution.
In particular ref-count maintenance triggers and unrelated indexes are not
installed by this projection. Treat measurements as that recorded projection's
cost, not production latency. Empty pressure-side tables and no task ownership
also underrepresent an active cluster; no fresh-pressure blind-spot fix is made.

## Plans, Phase Timing, and Retry Evidence

The sampler reads six actual SELECT literals from `mk20_release.go` and
`market/backpressure/backpressure.go`: global waiting EXISTS, first page
(LIMIT 64), keyset page (after synthetic ID 16,054, LIMIT 64), active count,
fresh MK20 pressure, and fresh SDR pressure. It emits their SQL/binds and plans.
The last two correspond to the SDR configuration's actual fresh-pressure path;
this finite recipe does not measure the Snap pressure branch.

Plain `EXPLAIN` reports estimates without executing the SELECT. `EXPLAIN
ANALYZE` **executes** it in a short READ ONLY transaction. PostgreSQL plans use
`ANALYZE, BUFFERS, FORMAT JSON`; Yugabyte uses `ANALYZE, DIST, FORMAT JSON`.
Unsupported syntax is a stopped/incomplete measurement, not a reason to claim
missing counters were zero. Read root Actual Rows and leaf rows/loops/rows
removed from the analyzed plan; retain storage read requests, rows scanned,
storage execution time and buffer counters where that engine exposes them.
Do not equate a COUNT result's one returned row with one scanned row or claim
O(64) database scanning because candidate output has LIMIT 64.

Record emitted client wall time for waiting, fresh pressure, active count,
candidate selection, each release, and the full pass. Fresh pressure's two SQL
phases also have separate analyzed plans. The pass uses `runMK20ReleasePass`
with real query/pressure/release adapters and only timing, error-stop and wake
observers; it is not a second release algorithm. The existing correctness test
separately covers the public `insertDDODealInPipeline` entry point. Both final
independent calls use production `releaseMK20WaitingDeal`. All insertion writes
belong to the disposable fixture; no EXPLAIN ANALYZE of production writes is used.

For each successful complete sample, use the owned-session statement evidence
between the release timestamps to count *executions* of the gate UPDATE,
including failed executions, exactly once (not both statement-start and duration
records). There are six initial release calls; extra gate executions identify
client transaction retries if all six calls reached their gate and no
connection/cancellation error occurred. Correlate each extra transaction with
recognized SQLSTATE 40001 evidence and its session/transaction outcome. Keep
server-internal YSQL statement retries separate if reported; they are not
HarmonyDB callback counts. Do not count generic errors, elapsed time, or a
simultaneous goroutine start as retry/contention evidence. If the logs cannot
distinguish attempts or six initial calls, report retry counts **NOT MEASURED**
and the recipe's evidence incomplete; do not subtract blindly or infer zero.

After the pass expect baseline+4 incomplete and 32,104 waiting; after the pair
expect baseline+5 and 32,103, with one `Released` and one `AtCapacity`. No further
release pass, completion UPDATE, or full backlog drain is run by this sample.
Record PostgreSQL and Yugabyte results separately, including missing counters,
retries, timeouts and errors. Compilation/database-free assertions alone leave
both engines **NOT RUN**. Review measurements before proposing any index,
schema, quota value, or performance claim.
