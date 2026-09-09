# PreCommit SQL row-effect tests

The opt-in `integration` tests execute the same candidate, assignment, failed
detach, and message-CID SQL constants used by the PreCommit poller/task. They
complement the database-free `SubmitPrecommitTask.Do` tests, whose fake API and
sender inspect actual outgoing CBOR membership. They do not submit messages,
prove chain acceptance, or establish exactly-once send/persistence.

Use only an explicitly disposable local PostgreSQL or Yugabyte database. Remove
normal Curio/HarmonyDB/libpq connection settings and other integration opt-ins
before supplying all of these dedicated variables:

- `CURIO_PRECOMMIT_ITEST=1`
- `CURIO_PRECOMMIT_ITEST_HOST=127.0.0.1` (literal loopback only)
- `CURIO_PRECOMMIT_ITEST_PORT` (explicit nonzero port)
- `CURIO_PRECOMMIT_ITEST_DATABASE`
- `CURIO_PRECOMMIT_ITEST_USER`
- `CURIO_PRECOMMIT_ITEST_PASSWORD`, only if required, supplied privately

The connection disables load balancing and fallback targets. Each test creates
a random owned schema, uses a 30-second overall context, a 5-second statement
timeout and a 2-second lock timeout, and drops only that owned schema. The
projected schema preserves relevant column types, primary keys, initial-piece
foreign key/cascade, and the upstream removal of task foreign keys.

```sh
go test -tags=cgo,fvm,nosupraseal,integration ./tasks/seal \
  -run '^TestPrecommitSQL(CandidatesAndAssignment|DetachAndCIDMembership)$' \
  -count=1 -timeout=2m -v
```

`TestPrecommitSQLCandidatesAndAssignment` verifies failed exclusion before batch
numbering, stale-discovery revalidation, and provider/proof/sector/task scoping.
`TestPrecommitSQLDetachAndCIDMembership` verifies original failure evidence,
included-sector-only CID assignment, and transaction rollback. These are SQL
row-effect tests, not independent-handle contention tests. The stale discovery
fixture changes state between the actual distinct selection/update statements;
it does not invent an interleaving within one atomic statement.

Logs identify the database version and reported SQL isolation. Yugabyte effective
isolation must be independently recorded; `SHOW` alone is not proof of server
Read Committed or wait-queue configuration. Compilation and skipped opt-ins are
not database execution evidence.
