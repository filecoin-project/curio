# Task-start admission and liveness

This is a scheduler change, not UI-only telemetry. The scheduler retains sole
ownership of candidate maps, cooldowns, acceptance decisions, NoteClaimed and
storage admission. Independently cancellable workers prepare current-attempt
metadata and return failed acquisitions. A held row in either phase no longer
holds the event loop. Claim/filter/storage I/O still has its previous synchronous
boundary; whole-pool exhaustion and whole-database outages are not covered by
this progress guarantee.

## Ownership and resources

Claimed work is pending until Do entry. Pending plus running uses the existing
shared Max/ActiveThis and CPU/RAM/GPU accounting, without a second debit at
dispatch. Each handler permits at most 100 outstanding admissions, including
quarantined results; other handlers have independent slots. A local ID cannot
be admitted again from cache, poll, peer, recovery or completion while pending.
Results live on their admission and signal a coalesced nonblocking wake. Only
the scheduler consumes ready results and claims storage. Preparation can finish
out of order within an unpaced batch; FIFO candidate/claim ordering is retained.

A forward owner_generation trigger advances the acquisition identity on owner
change. Recovery advances it explicitly even when the owner machine is unchanged.
The migration does not rewrite existing owner, token, start, retry or task state.
Preparation requires the claimed generation and an empty or identical token.
Preparation-failure cleanup matches generation and empty/identical token, covering
both failure before installation and commit followed by an unknown response.
Prepared storage-failure cleanup retains its exact owner/token/unstarted CAS.
Neither path can clear a newer attempt or an already recorded Do entry.

Startup places observed recovery rows on the real scheduler, not a constructor
waiting for an unstarted loop. A pending result is accepted, not a refusal.
Capacity/readiness refusal defers the observed recovery generation; subsequent
recovery still uses CAS. This relies on the existing single live engine per
registered machine identity. It does not make legacy completion persistence
safe for two live processes deliberately sharing that identity.

## Cancellation and completion

Pending preemption/cancellation returns only its local reservation, storage and
Max debit, once. A stale pending-only preemption cannot cancel a task which won
the Do-entry race. Entry serializes a short non-I/O reservation commit against
pending cancellation; implementations must keep logging/external I/O outside
that hook. After entry, failure does not refund pacing and running time-sensitive
or uninterruptible shutdown behavior is unchanged.

Cordon cancels outstanding pending admissions. Existing explicit scheduling
overrides (Finalize for related batch work) remain available on later passes;
normal late-ready work cannot use that exception. Engine shutdown cancels all
pending admissions, including overrides. Result delivery and local capacity
return never require a running event loop. Both preparation and cleanup have
five-second context budgets. An uncooperative driver can outlive its context,
but consumes one bounded admission slot, not an unbounded worker queue.

Cleanup failure quarantines that ID until process recovery. It is logged, not
reported as released, retried in a hot loop, or converted to a task failure.
Other known work is woken when local resources return; rediscovery of returned
IDs still uses existing polls/backoff. Post-Do completion persistence retains
its existing retry policy. Legacy diagnostic lookups also retain their existing
limits. No universal I/O-free scheduler, real-time deadline, chain/native SDR,
or cross-process fairness claim follows from this change.

## Evidence

TestAdmissionR3OtherTaskAndEventsProgress uses the production event loop and
handler. For preparation and cleanup separately, a barrier holds A while B
enters Do after a completion event and a time-sensitive peer event starts C.
Restoring synchronous preparation in a temporary copy makes both assertions
fail; restoring workers passes. Other admission tests cover shared pending Max,
cancellation/late success, bounded quarantine, partial failures, startup refill,
post-entry preemption and stopped-loop result delivery.

TestAdmissionSQLRowLockProgress acquires a real PostgreSQL row lock only AFTER
the production claim commits. It observes a linked lock wait, then independent
SQL and another scheduler Do before releasing the lock. TestAdmissionSQLAcquisitionFence
executes generation/token/owner/unstarted predicates, recovery, rollback and
cancellation through the real adapter. TaskTelemetryRunner tests use actual
HarmonyDB/HarmonyQuery startup and preserve existing ledger/in-flight metadata.
These tests do not establish Yugabyte concurrency behavior: new Yugabyte
execution and a supported Yugabyte wait observer remain separate validation.
