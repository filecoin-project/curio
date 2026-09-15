# Local PoRep backend unavailability and retry handoff

This change keeps a failed task available to other eligible workers. It does
not repair already exhausted tasks or establish that every incomplete sector
is recoverable. Existing proof verification, task limits, schedules and
pipeline completion rules are unchanged.

## Retry clock

Completion returns the committed database `update_time` and retry count. Local
and peer notifications use that same timestamp. The scheduler wakes at the
earliest future retry deadline, even without another peer or poll event.
Duplicate or older notifications cannot replace a newer known retry state.
Legacy messages without a retry timestamp start a conservative local wait;
the next authoritative database snapshot corrects that estimate. A legacy
receiver may still wait again: mixed versions do not provide equal deadlines.

The claim statement rechecks owner absence and retry count, plus the observed
timestamp and database deadline for positive retries. Retry zero has no backoff
and a first notification need not supply its database timestamp. Peer
notifications are advisory, not authority to
ignore backoff. No schema change or cross-process mutex is added. The migrated
`update_time` column is TIMESTAMPTZ: its instant crosses SQL, Go and peer messages
without a session-wall-time conversion. The historical migration's explicit UTC
conversion remains unchanged; this change does not reinterpret historical data.

Preemption is not a new sector failure. It preserves both the retry count and
the failure timestamp whose wait was already satisfied at claim, so immediate
notification is also eligible for an authoritative SQL reclaim. Participating
instances must use consistent RetryWait policies. Ordinary failures and typed
worker deferrals still record their own committed return timestamp.

## Narrow worker-local policy

Only the exact `No CUDA devices available` error returned from the standard
local C2 invocation is classified as backend unavailability. The RPC error's
message is inspected when present. Generic child failure, invalid proof, EOF,
vanilla-source transport failure, database error and cancellation are not this
classification. No CUDA version check is performed. CuZK admission bypasses
this local-backend policy; other valid backends are not proactively probed.

After this error, that PoRep task instance stops accepting work for two
minutes. Subsequent failed probes double the pause up to thirty minutes. Once
the pause expires, one ordinary task is allowed as a recovery probe. Concurrent
requests cannot reserve a second probe. A failed claim or preparation returns
only its reservation; cleanup from an older reservation cannot cancel a newer
one. Slow native/DB work and logging do not run under the gate mutex.

An older in-flight success cannot erase a newer failure. Successful C2/proof
verification in the current health generation reopens admission. A probe that
reaches C2 but fails for another reason retains the existing pause. Failures
before C2 still follow normal task error handling and release their reservation;
they do not establish backend health. Resume requires an ordinary scheduling
wake (normally the next DB poll), not a new background probing service.

In-memory health state is lost on process restart. Before the first error is
observed, up to the instance's configured concurrency may already be in flight.
Afterward, unsuccessful probes are bounded by the local backoff. Repeatedly
restarting a broken process defeats that pacing. Some native initialization
failures require an operator restart after repairing the environment; automatic
native reinitialization is not promised.

## Failure budget and evidence

A recognized worker error returns ownership without spending the sector's
ordinary failure budget, but only for the exact current owner and attempt
token. Its failed history and pipeline event remain. Retry state and timestamp
come from the committed return. A stale attempt cannot return a newer owner's
task. Ordinary sector failures still consume the existing ten-attempt budget;
terminal pipeline references are not automatically cleared or requeued.

This is deliberately different from treating a successful proof, preemption,
and backend failure as the same outcome. With all workers unavailable, tasks
remain pending and bounded probes continue; no progress guarantee is made.
Slots are released only after Do returns, never merely because cancellation was
requested. Other task types and the rest of the machine are not cordoned.

## Tests and limits

Database-free tests exercise production peer encoding/decoding, completion
notification timing, retry wake, error classification and reservation logic.
Current-schema opt-in `integration` fixtures use the complete embedded startup
migration runner and assert catalog types and task triggers before testing real
HarmonyDB claims, attempt tokens, history and independent handles. The separately
named historical migration test intentionally starts with the old schema.
Mixed UTC/Asia-Seoul session tests compare instants and actual claim row effects.
The fleet test substitutes
native/chain work with complete loopback HTTP input and finite timers: one
millisecond represents one second. DB time and the ten-millisecond message
bundler are not accelerated. This is a contention/flow characterization, not a
GPU throughput benchmark or evidence about any particular production outage.

Disposable PostgreSQL results, negative controls and exact candidate identities
are recorded in the external review archive. Native PoRep/CUDA, operating-fleet
behavior and Yugabyte execution require separate validation. No operating data,
server credentials or deployment commands are required by these tests.
