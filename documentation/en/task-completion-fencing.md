# Task completion and acquisition identity

Completion captures the owner, acquisition generation and attempt token before
dispatch. It locks and conditionally mutates only that same current task. Missing
or superseded acquisitions return a stale no-op, not an endless database retry.
Successful, retryable, terminal-failure and preempted outcomes all use the fence;
history/events share the transaction, and callbacks require a committed result.
Same-owner recovery is distinguished by the generation and token. No failure
limits, pacing, public configuration or native execution policy are changed.

Local resource release still belongs to the returning execution. A stale result
has a diagnostic log, not a normal success/failure history row. An uncertain
commit response cannot provide exactly-once callback delivery; this change does
not introduce a durable notification protocol.

This protects scheduler completion, not writes inside task Do methods. It does
not fence stage-result UPDATEs or establish native termination. Old workers
continue to use their old SQL; a migration alone cannot retrofit this behavior.
The correction must run on participating workers, not only the WebRPC process.

## Regression scope

With `cgo,fvm,nosupraseal,integration`, `TestCompletionSQL*` exercises actual
claim, preparation, completion, rollback and a PostgreSQL-linked row-lock wait.
The explicit `CURIO_TASK_ATTEMPT_ITEST` target and owned-schema safeguards apply.
The held SDR/TreeRC-body fixture also retains the historical
`CURIO_CLEANUP_BOUNDARY_ITEST=1` opt-in and dedicated disposable target checks;
its name does not authorize cleanup. Bodies substitute for native work.

Database-free identity and notification tests cover captured generations and
the absence of success callbacks or peer events for rejected results. Existing
retry/preemption tests also exercise current-attempt completion and refill.
No production, CUDA, real chain, native termination or safe removal is proved.
