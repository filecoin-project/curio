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
production SQL/call-site assertion is static evidence only. Concurrent
provider debits and rollback row effects still require execution against an
explicitly authorized isolated database; unit and race tests are not that
evidence.
