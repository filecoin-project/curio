# SupraSeal CC quota allocation

Each SupraSeal batch may fill unused slots from enabled CC schedules. The
planner first floors each weighted share and caps it at the provider's
remaining `to_seal` quota. It redistributes the remainder one sector per
eligible provider in descending weight order, breaking weight ties by
ascending provider ID. Exhausted providers receive nothing. A batch cannot
use a partial plan if the combined quota is insufficient.

This intentionally replaces the uncapped last-provider remainder. For
weights `3, 2, 1` and ten requested sectors with sufficient quota, the new
counts are `6, 3, 1`, rather than `5, 3, 2`. Exact proportional shares remain
unchanged. The planner does not alter the supplied schedule or duration.

Schedule discovery precedes the existing ascending provider-lock acquisition.
A concurrent transaction can consume or disable a discovered quota. The
conditional quota debit therefore rechecks `enabled` and `to_seal >= count`
for the exact provider and requires exactly one affected row. A failed debit
returns an error from the existing task-creation transaction, rolling back
its sector-number, pipeline, and task writes. No new lock domain is added;
chain reads, provider-lock ordering, proof selection, and storage transfer
behavior are unchanged. Configuration changes are not a cluster-wide fair
scheduling guarantee.

Database-free tests execute the production planner and debit-result path,
including capped redistribution, zero/exhausted quota, stable rounding,
invalid input, overflow-safe share arithmetic, and error propagation. The
production SQL/call-site assertion is static evidence only.

## Opt-in SQL regressions

`TestCCQuotaSQLClaimsAndRollback` calls the production
`claimsFromCCScheduler`, using a metadata-only chain API double. It checks
quota-capped allocation and rollback of an earlier provider's debit, sector
number allocations, pipeline rows and the test task when a later provider
becomes disabled after discovery. That same-transaction invalidation is a
guard test, not a cross-connection race.

`TestCCQuotaSQLConditionalDebit` executes the production conditional SQL and
checks insufficient, disabled and missing providers without changing other
providers. `TestCCQuotaSQLConcurrentDebit` holds a successful quota debit and
requires linked PostgreSQL blocking before releasing it; the contender must
then reject the exhausted quota rather than commit a negative count. Every
participant is released/cancelled and joined before fixture cleanup. This
observer targets PostgreSQL Read Committed; Yugabyte skips that observer,
not an assertion of Yugabyte correctness.

Use `cgo,fvm,nosupraseal,integration` with explicit
`CURIO_CC_QUOTA_ITEST=1` and all dedicated `_HOST`, `_PORT`, `_DATABASE`,
`_USER` variables (optional `_PASSWORD`). The host must be a literal loopback
IP. Normal database settings are not target fallbacks; remove inherited
database variables before setting the dedicated target. Connection load
balancing is disabled. Each test owns a random `itest_` schema, uses bounded
contexts, and applies only the actual migration definitions needed for its
tables. It does not execute native sealing, payload reads or live chain APIs.

```sh
go test -tags=cgo,fvm,nosupraseal,integration -count=1 -timeout=3m \
  -run '^TestCCQuotaSQL' -v ./tasks/sealsupra
```

Local PostgreSQL 16.15 execution passed these groups, including linked
contention and the complete rollback assertions. Current Yugabyte execution
is not available evidence. Neither these SQL tests nor the native-free tests
prove GPU execution, chain acceptance or cluster-wide proportional fairness.
