# Indexing Offset Readiness: Operator SQL Verification

This follow-up adds executable SQL assertions to the existing NULL-offset
readiness fix. PostgreSQL and YugabyteDB execution remains **NOT RUN** until
an operator runs these tests in a separately authorized disposable database.
No production target, API, task engine, payload processing, or recovery is used.

## Statements and Coverage

`tasks/indexing/task_indexing_offset_integration_test.go` executes the exact
`indexingMK12AssignSQL` and `indexingMK20AssignSQL` constants consumed by
`IndexingTask.schedule`. Candidate selection and conditional assignment are
one atomic CTE-plus-UPDATE, not copied queries or a second eligibility model.
The fixture supplies only task-row creation and commit/rollback plumbing via
the repository-pinned pgx driver. It does not execute Harmony's full task adder,
retry runner, or scheduler loop. Existing database-free tests verify that the
production scheduler uses these constants.

Each of these tests has `MK12` and `MK20` subtests:

- `TestIndexingOffsetSQLReadiness`: an older otherwise-ready NULL offset,
  later zero and positive offsets, and older assigned/indexed/completed/
  unsealed/missing-readiness-time exclusions. The zero-offset row requests
  metadata-only processing; the positive row requests physical indexing.
  Snapshots verify that only the selected task-ID field changes, including
  preservation of another eligible provider/deal and, for MK20, another
  aggregate index with the same deal ID. The other market table is untouched.
- `TestIndexingOffsetSQLAllNullThenReady`: no assignment or retained new task
  for an all-NULL set. A fixture-owned producer transition supplies offset zero
  to the later row; a new scheduling statement can then assign it while the
  older NULL row remains untouched.
- `TestIndexingOffsetSQLAssignmentRollback`: rolling back a successful SQL
  assignment also rolls back its newly created task; a later commit succeeds,
  and subsequent scheduling cannot overwrite that existing assignment.

The conditional UPDATE's prerequisites execute in every operation. There is
no application callback between its CTE and UPDATE in which to inject a stale
candidate. These tests do not invent such an interleaving or claim independent
cross-connection proof of the outer recheck. Its presence remains separately
protected by the existing SQL-shape regression. Concurrent-writer/isolation
behavior is not established by these single-connection row-effect tests.

Normally completed fixtures have both `complete=true` and `indexed=true`.
The existing scheduler excludes them via `indexed`; neither statement adds a
new `complete` predicate. This work does not invent a contract for contradictory
historical stage flags or clear existing task IDs. Metadata-only assignment is
tested, but actual metadata insertion/completion in `Do` is outside this fixture.

## Schema and Safety Boundary

The focused projection follows `20240731-market-migration.sql` (MK12) and
`20250505-market-mk20.sql` (MK20), checked against subsequent migrations. It
retains the unique non-NULL MK12 UUID, MK20 `(id, aggr_index)` primary key,
provider types, nullable BIGINT offsets/task IDs, TIMESTAMPTZ readiness time,
and relevant stage/indexing flags. Neither task-ID column has a foreign key
to `harmony_task`. The minimal task table retains its SERIAL identity and
VARCHAR(16) name from `20230719-harmony.sql`; unused payload and engine fields
are not a substitute for full task-engine integration. No historical migration
replay or production schema change is performed.

The file requires build tags `integration && !skiff` **and** explicit
`CURIO_INDEXING_OFFSET_ITEST=1` before opening a connection. Supply all of:

- `CURIO_INDEXING_OFFSET_ITEST_HOST`: literal loopback IP, e.g. `127.0.0.1`
  or `::1`; not a hostname, host list, remote address, or implicit default.
- `CURIO_INDEXING_OFFSET_ITEST_PORT`: explicit integer in 1–65535.
- `CURIO_INDEXING_OFFSET_ITEST_DATABASE`: dedicated disposable database.
- `CURIO_INDEXING_OFFSET_ITEST_USER`: dedicated user allowed to create an
  isolated schema in that database.
- Optional `CURIO_INDEXING_OFFSET_ITEST_PASSWORD`, supplied only through the
  operator's existing secret environment, never a URI, command argument or log.

Start with normal Curio/HarmonyDB/libpq connection variables and other
integration opt-ins removed, then supply only this dedicated target. The
fixture overwrites target/runtime parameters, removes fallback hosts, and
explicitly disables load balancing. Each subtest creates a random owned
schema; cleanup drops only that schema. Connection timeout is five seconds,
statement timeout five seconds, lock timeout two seconds, subtest context
30 seconds, and cleanup/close contexts five seconds each. Cleanup follows
transaction rollback; no participant goroutines or server processes are started.
If the test process is forcibly killed, normal deferred cleanup is not assured;
do not broaden cleanup to other namespaces.

Logs record server version, default and transaction-reported isolation. Driver
default isolation is requested. For YugabyteDB 2025.2.2.2-b11, the operator must
record effective RC and wait-queue settings from the disposable server's own
configuration independently of `SHOW transaction_isolation`; otherwise effective
isolation stays **UNVERIFIED**. No remote flag query is part of this test.

## Compile and Operator Execution

Use the existing pinned Go/FFI toolchain and local OpenCL build environment.
No server installation/startup or connection command is needed for compilation:

```sh
go list -tags=cgo,fvm,nosupraseal,integration \
  -f '{{.TestGoFiles}}' ./tasks/indexing
go test -c -tags=cgo,fvm,nosupraseal,integration \
  -o /tmp/curio-indexing-offset-sql.test ./tasks/indexing
```

Verify that `task_indexing_offset_integration_test.go` is selected. Normal
`cgo,fvm,nosupraseal` tests exclude that file. Compiling it is not executing it;
running without opt-in skips before any connection.

Only the separately authorized operator, after supplying the dedicated target,
runs the following once per disposable PostgreSQL/Yugabyte instance:

```sh
CURIO_INDEXING_OFFSET_ITEST=1 /tmp/curio-indexing-offset-sql.test \
  -test.v -test.count=1 -test.timeout=5m \
  -test.run='^TestIndexingOffsetSQL(Readiness|AllNullThenReady|AssignmentRollback)$'
```

Expect all six market subtests to pass. Record the exact tested commit, version,
isolation evidence and results separately for each database. A compile, skip,
or static SQL assertion is not a database PASS.

## Operator-Only Negative Control

After the baseline SQL run passes, use a separate disposable source worktree
at the same commit. Do not edit the review branch or any deployed checkout.

1. In `task_indexing.go`, temporarily remove **only** the candidate clause
   `AND sector_offset IS NOT NULL` inside `indexingMK12AssignSQL`. Retain the
   outer `AND p.sector_offset IS NOT NULL` and all other SQL unchanged.
2. Recompile the integration binary from this mutated worktree, then execute
   only `^TestIndexingOffsetSQLReadiness$/^MK12$` with the same explicit opt-in,
   dedicated disposable target, count=1 and five-minute test timeout.
3. Require an executable assertion failure: expected one affected row, actual
   zero. The older NULL row wins the candidate LIMIT but is rejected by the
   outer guard, so the later ready zero-offset row is not assigned. A compile,
   setup, connection or timeout failure is **not** this negative-control result.
4. Restore that one mutation, rebuild, and rerun the same SQL subtest to PASS.
5. Repeat steps 1–4 for `indexingMK20AssignSQL` and the `MK20` subtest only.

Do not commit/push either mutation or disable the safety guard. This negative
control has been prepared, **not executed** here. It is different evidence
from the earlier database-free SQL-shape mutation.

The previously reported ownerless Indexing canary task was resolved by
uncordoning workers; it did not reproduce this NULL-offset bug. Existing
assigned tasks, exhausted retries, post-GC offset discovery, stored schedules,
and completion recovery remain outside this fix and this verification task.
