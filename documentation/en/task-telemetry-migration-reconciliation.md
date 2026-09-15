# Task telemetry migration reconciliation

The pinned HarmonyQuery runner records the first eight filename characters in
`base.entry`. Both `20260909-task-attempt-start.sql` and
`20260909-task-ownership-age.sql` therefore share a key. Fresh startup can apply
both because its initial applied-key set is not refreshed inside the loop.
An installation that already recorded either one skips the other on restart.
Renaming an old file or editing/deleting historical ledger rows is not needed.

`20260910-task-telemetry-reconcile.sql` has a new, unique runner key. It adds any
missing columns and recreates the **unchanged** two trigger definitions. It does
not update tasks, rewrite the old ledger, or backfill a start timestamp. In-flight
owners, retries, attempt tokens, and valid timestamps remain intact. Missing
provenance remains NULL. Existing runtime token/CAS, ownership, and SDR pacing
contracts are unchanged.

This is a forward-only compatibility repair. Dropping these columns is not a
safe inverse: some installations already depended on them before reconciliation.
Rolling source back to a pre-reconciliation version keeps compatible columns;
it neither reverses an applied ledger receipt nor repairs a different old schema.
DDL takes relation locks, so rollout still requires a separately authorized,
bounded migration window. This change does not make simultaneous startup
migrations globally serialized or change HarmonyQuery's separate SQL/ledger
commits.

## Executable runner coverage

`harmony/harmonydb/task_telemetry_migration_integration_test.go` uses the actual
`harmonydb.NewFromConfig` startup and pinned HarmonyQuery runner. Historical
fixtures embed the real migration files with one or both telemetry files,
including all preceding migrations. They do not fabricate applied ledger rows
or execute a copied migration directly. Current startup uses the production
embed, and assertions require the new reconciliation receipt.

- `TestTaskTelemetryRunnerReconciliation`: fresh, ownership-only, attempt-only,
  and both-applied states, preserving all original task/ledger values and
  verifying repeated startup.
- `TestTaskTelemetryRunnerPartialFailureRestart`: a bounded, independently held
  PostgreSQL relation lock lets the first historical migration finish, then
  causes the second to fail. The holder releases before restart; the new startup
  reaches reconciliation despite the existing shared-date receipt.
- `TestTaskTelemetryMigrationDates` and
  `TestTaskTelemetryReconciliationKeepsTriggerSemantics`: database-free shape
  checks for new date uniqueness and unchanged trigger semantics.

SQL tests require build tag `integration` and `CURIO_TASK_MIGRATION_ITEST=1`.
Set dedicated `CURIO_TASK_MIGRATION_ITEST_HOST` (exactly `127.0.0.1`), `_PORT`,
`_DATABASE` (prefix `curio_test_`), `_USER`, and optional `_PASSWORD`. Clear all
normal DB variables and other opt-ins first. No connection defaults are used;
load balancing is disabled. Each case owns a random schema and drops only it.
The disposable target must have `0 < statement_timeout <= 10000ms` and
`0 < lock_timeout <= 2000ms`, including new startup sessions.

```sh
go test -c -tags=integration -o task-migrations.test ./harmony/harmonydb
# Only in a separately authorized, explicitly configured disposable session:
./task-migrations.test -test.v -test.count=1 -test.timeout=6m -test.run '^TestTaskTelemetry'
```

Record the real startup matrix and attempt/CAS execution separately for each
database engine and source revision. Static checks, unit/race results, and
compilation are not substitutes for database DDL/locking behavior. The
historical SQL/ledger separation remains a runner limitation outside this repair.

The synchronous pre-dispatch attempt preparation/liveness finding is **not**
fixed here. Passing reservation/token/Do-entry tests does not prove that a slow
preparation query cannot delay other scheduler work.
