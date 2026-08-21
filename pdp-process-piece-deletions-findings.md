# Curio node-side changes for `processPieceDeletions`

Findings for [curio#1422](https://github.com/filecoin-project/curio/issues/1422), tracking
[FilOzone/pdp#297](https://github.com/FilOzone/pdp/pull/297) (branch `feat/resumable-piece-deletion`,
fixes [FilOzone/pdp#283](https://github.com/FilOzone/pdp/issues/283)).

Status: research + design notes. No code changed.

**Decisions taken** (owner input, incorporated below):

- No listener callback. Challenge-epoch zeroing is the settled anti-grinding mechanism.
- A `nextProvingPeriod` revert on a non-empty queue is a **recoverable signal**, not a deadlock —
  Curio recognises it and goes back to draining. Liveness holds because the queue drains incrementally
  across many messages (assuming one removal never consumes a whole block's gas).
- Curio gates draining on its existing **"challenge window is over"** record, the same one that keeps
  `nextProvingPeriod` from being called early. Recording prove-tx landings to drain *earlier* is
  possible but overkill for an infrequent operation.
- The drain is driven by a **tipset watcher**, not a periodic poll — waiting efficiently is not worth
  faulting for.
- The watcher's work queue is a **dedicated table seeded once by a SQL migration**, written thereafter
  by the delete-intake path. The seed picks up datasets already sitting on a non-empty queue at upgrade
  time, including any stuck by #283.
- Delete intake is **refused while a drain is outstanding**, so the queue cannot grow while it drains.
- Delete intake is also **refused before the dataset has an initialized proving schedule**, which is
  what keeps the drain gate satisfiable (§4.3).
- The enqueue ceiling **stays at 35**. Resumable draining removes the reason it was capped there, but
  raising it is deferred until someone actually wants more deletion throughput.
- All new removal behaviour sits behind a **PDPVerifier `VERSION()` check** (§4.8). Against the live
  contract (`3.4.0`) the drain task is a no-op that hands straight to `nextProvingPeriod`; only a
  version bump switches on real processing.
- `tasks/pdp` (the mk20 pipeline) is dead code and out of scope.

---

## 1. What the contract change does

### 1.1 New entrypoint

```solidity
function processPieceDeletions(uint256 setId, uint256 removalCount) external;
```

1. Requires `dataSetLive(setId)` and `storageProvider[setId] == msg.sender` (new `OnlyStorageProvider()`).
2. Requires `removalCount > 0` (`EmptyRemovalBatch()`) and `<= scheduledRemovals[setId].length`
   (`InvalidPieceDeletionBatch()`).
3. Takes the **suffix** of the queue — `removals[queueLength - removalCount ..]`, i.e. drains
   **LIFO from the tail**. Curio chooses *how many*, never *which*.
4. `removePieces()` decrements `dataSetLeafCount` and clears live piece data.
5. `_popProcessedRemovals()` pops those entries and clears only their legacy bitmap bits —
   partial-drain safe on both legacy-bitmap and compact `PieceV2` datasets.
6. **Sets `nextChallengeEpoch[setId] = NO_CHALLENGE_SCHEDULED` (zero).**
7. Emits `PiecesRemoved(setId, pieceIds)` chunked at 100 ids
   (`PIECE_ID_EVENT_BATCH_SIZE`, from [#298](https://github.com/FilOzone/pdp/issues/298) — an 8KiB
   event cap otherwise limits a batch to ~255).

**There is no listener callback**, by decision. The PR *body* still advertises
`piecesRemoved(uint256,uint256)`, and also claims `SimplePDPService` proof/deadline enforcement and
`PDPRecordKeeper.REMOVE_PROCESSED`. None of that is in the diff — read the diff, not the description.

### 1.2 `nextProvingPeriod` changes

```solidity
uint256 pendingDeletionCount = scheduledRemovals[setId].length;
require(pendingDeletionCount == 0, PendingPieceDeletions(pendingDeletionCount));
require(dataSetLeafCount[setId] > 0 || (dataSetLive(setId) && challengeRange[setId] > 0), NoPiecesToProve());
```

- The removal-draining block is **gone**. `nextProvingPeriod` no longer removes anything.
- It **reverts** while the queue is non-empty, and the revert carries the pending count — directly
  useful to Curio as a "drain this much more" hint.
- The second `require` above is **not landing**: the zero-leaf-rollover relaxation is being dropped
  upstream. `nextProvingPeriod` continues to require `dataSetLeafCount > 0`, as today. Curio's existing
  empty-dataset handling therefore stays semantically correct and needs no new behaviour — see §4.5 for
  the one thing that does change.

### 1.3 New errors

`OnlyStorageProvider()`, `InvalidPieceDeletionBatch()`, `PendingPieceDeletions(uint256 count)`,
`NoPiecesToProve()`, plus the pre-existing `EmptyRemovalBatch()`.

`NoPiecesToProve()` **re-encodes the string revert** matched at `tasks/pdpv0/error_detection.go:50`
(`provingRevertNoLeavesForProvingPeriod`). The *condition* is unchanged — still `dataSetLeafCount > 0`
— only its encoding, so Curio must match both forms while it spans two contract versions (§4.5, §4.8).
Confirm the custom error survives the removal of the zero-leaf relaxation (§6.4); if the `require`
reverts to the string form, no matcher work is needed at all.

---

## 2. Timing: when Curio drains

`processPieceDeletions` zeroes `nextChallengeEpoch`, and `provePossession` requires
`challengeEpoch != NO_CHALLENGE_SCHEDULED`. Draining therefore destroys the current period's
challenge: **drain before proving and the proof becomes impossible — a fault.**

The way to never be in that position is to drain only once the challenge window has closed. Curio
already has that predicate — it is what the nextPP watcher gates on today
(`tasks/pdpv0/task_next_pp.go:70`): `prove_at_epoch + challenge_window <= height`.

```
  nextPP(N) lands     prove_at_epoch(N)      prove_at_epoch + challenge_window
       |                     |                            |
       |-- challenge sampled |--- prove tx lands here ----|=== DRAIN ===|--> nextPP(N+1)
```

At that boundary the proof has either landed or been missed, so zeroing the challenge costs nothing.
Draining earlier (right after the prove tx confirms) would widen the window but requires persisting
prove-tx confirmations Curio does not track (§3.4) — not worth it for an infrequent operation.

**Overrun is safe.** If the queue does not fully drain before nextPP is attempted, nextPP reverts with
`PendingPieceDeletions(count)`; Curio drains more and retries, making progress every message. An
overrun costs a delayed rollover — and a fault if it stretches past the next deadline — but never a
stuck dataset. That is the improvement over the pre-#283 status quo, where an over-large queue made
`nextProvingPeriod` permanently un-executable.

**Benign race**: a prove tx sent near the end of the window may land after the drain zeroes the
challenge. It reverts with `"no challenge scheduled"`, which `IsSkipCurrentProvingPeriodError` already
classifies correctly, and that proof was past its FWSS deadline anyway. This stays benign only while
the drain never starts before the boundary.

---

## 3. Curio's current state

### 3.1 Scope: pdpv0 only

`pdpnode/tasks.go` registers two independent PDP pipelines: `tasks/pdpv0` (+ the `pdp/handlers.go`
HTTP API) and `tasks/pdp` (mk20 market path). **`tasks/pdp` is dead code and out of scope.**

pdpv0 shape relevant here:

| concern | location |
|---|---|
| dataset / piece tables | `pdp_data_sets`, `pdp_data_set_pieces` |
| delete intake | `pdp/handlers.go` `DeletePiece`, sends `schedulePieceDeletions` inline (reason `pdp-delete-piece`) |
| delete tracking | `pdp_data_set_pieces.rm_message_hash`, `.removed` |
| removal reconcile | `tasks/pdpv0/watch_proving_period.go` `processPendingPieceDeletes` |
| nextPP / initPP tasks | `task_next_pp.go` (`PDPv0_ProvPeriod`), `task_init_pp.go` (`PDPv0_InitPP`) |
| watcher phases | `tasks/pdpv0/watcher.go:22-38` |
| piece data GC | `task_piece_gc.go`, keyed on `removed = TRUE AND rm_message_hash IS NOT NULL` |
| closest template for the new task | `tasks/pdpv0/task_cleanup_pieces.go` |

### 3.2 The "deletion tracking code that currently lives in nextProvingPeriod"

Issue #1422 refers to `tasks/pdpv0/watch_proving_period.go`. Today, after a **confirmed nextPP
message**, `processPendingPieceDeletes` (line 279):

1. Selects `pdp_data_set_pieces` rows with `rm_message_hash IS NOT NULL AND removed = FALSE`.
2. Clears `rm_message_hash` if the `schedulePieceDeletions` tx failed.
3. Reads `getScheduledRemovals(setId)`; still queued → not yet processed → leave pending.
4. Otherwise `pieceLive(setId, pieceId)` decides: not live → `removed = TRUE`; still live → warn and
   clear stale tracking.

It is keyed on "nextPP confirmed" as the proxy for "removals applied". That premise is now false —
**`processPieceDeletions` confirmation is the trigger.** The logic itself transfers verbatim; only its
trigger and host watcher move.

### 3.3 A zeroed challenge epoch is now ambiguous — two call sites must discriminate

Today `getNextChallengeEpoch == 0` has one meaning: the dataset is empty and proving should stop.
`processPieceDeletions` introduces a second, entirely healthy meaning: deletions were processed and a
fresh challenge is due at the next rollover. **Leaf count is the discriminator** — `leafCount > 0` is
the drain case, `leafCount == 0` the genuinely empty one. Two places read the zero and must branch:

**`processEmptyProvingPeriods`** (`watch_proving_period.go:192`) treats the zero after a confirmed
nextPP as "went empty" and nulls `prove_at_epoch`, `challenge_request_msg_hash` and
`prev_challenge_request_epoch`. It already reads `GetDataSetLeafCount` (line 217) for `init_ready`;
promote that read above the reset and skip when non-zero.

**`ProveTask.Do`** (`task_prove.go:247-256`) reads the challenge epoch and, on zero, calls
`disableProving` — which nulls `prove_at_epoch` and sets `init_ready = FALSE`. Against a drained
dataset that would disable proving on a perfectly healthy dataset. It must branch:

- `leafCount > 0` — deletions were processed early, before this period's proof. There is nothing to
  prove this period, so **complete the task without a proof and leave the proving schedule intact**;
  the nextPP watcher then fires normally at `prove_at_epoch + challenge_window` and resamples a
  challenge. Note the prove watcher already cleared `challenge_request_msg_hash` when it claimed the
  task (line 141), and the nextPP watcher keys on `challenge_request_task_id IS NULL` plus the epoch
  predicate — so simply *not* calling `disableProving` is sufficient to hand back to nextPP.
- `leafCount == 0` — genuinely empty; `disableProving` exactly as today.

The §2 gate should make the early-drain case rare (drains run after the window closes), but it is
reachable by races and by out-of-band drains, and the cost of misclassifying it is a wrongly disabled
dataset. Worth factoring the discriminator into one shared helper rather than duplicating it.

### 3.4 Prove-tx landings are not recorded (and don't need to be)

`task_prove.go` sends with reason `pdp-prove` and does **not** persist the tx hash — no
`message_waits_eth` row, no column on `pdp_data_sets`. The prove watcher instead *clears*
`challenge_request_msg_hash` when it claims the task (line 141) as its de-dup mechanism.

Under the §2 design this does not matter — the gate is the window boundary, not proof confirmation.
Noted only because it forecloses the "drain as soon as the proof lands" optimisation without new
schema, and because the reorg checker flags the same gap (`task_reorg_check.go:231`).

### 3.5 Intake backpressure

`pdp/contract/ethcall.go:22` — `ConservativeEnqueuedRemovalsLimit = 35`, far under the on-chain
`MAX_ENQUEUED_REMOVALS = 2000`. Enforced at `pdp/handlers.go:1129` (HTTP 429) and
`market/mk20/mk20.go:762` (dead path).

That ceiling exists only because `nextProvingPeriod` had to drain the whole queue in one transaction.
Resumable draining removes the reason, but **the limit stays at 35** — raising it is deferred until
there is demand for more deletion throughput. Leaving it alone also keeps it out of the version gate
(§4.8): 35 is correct under both contract versions, whereas a raised value would be actively unsafe on
`3.4.0`, reproducing #283.

The only text that needs changing is the operator-facing message at both call sites — "retry after the
next proving period flushes the queue" no longer describes what happens.

**Steady state is therefore one drain message per period**, since 35 is below any plausible batch cap.
The resumable machinery is not there for steady state — it is there for the **backlog**. The limit was
historically far higher (#283 describes "500 per call, ~2k per proving period"), so migration-seeded
datasets carry deeper queues, including the #283 datasets stuck precisely because `nextProvingPeriod`
could not drain them in one transaction. In practice those run to at most a few hundred pieces —
fewer than 10 messages at a batch cap of 100.

**Timing budget**, which bounds the backlog case and would justify any future raise of the ceiling.
Deployed config is a 1-day proving period and a 30-minute challenge window — 2880 and 60 epochs at 30s.
The drain must finish *and* `nextProvingPeriod` must land before the next challenge window opens, so
the slack is `2880 - 60 = 2820 epochs ≈ 23.5 hours`.

Under 10 sequential messages (sequential because each re-reads the queue length), even at a pessimistic
5 minutes per round trip, is under an hour against 23.5 available. The margin is wide enough that it
does not need defending: at ~4 epochs per message the budget holds ~700 messages, so the deepest real
backlog still fits with a batch size of **3**. Per-removal gas can be off by more than an order of
magnitude without threatening a deadline.

**Why a depth check suffices as a rate limit.** The queue only grows during a period (the drain runs at
the window boundary, at period end) and is empty once `nextProvingPeriod` succeeds — so depth at the
boundary equals enqueues that period, and no counter state is needed. This holds only while nothing
enqueues between the drain and the rollover, which is what the intake gate (§4.3) guarantees.

Unrelated but adjacent: `MaxDeletePiecesBatchSize` (`pdp/handlers.go:64`) is *aliased* to this constant
though the two mean different things — ids per HTTP request versus outstanding queue depth. Harmless
while both are 35; worth splitting before either is ever changed independently.

---

## 4. Proposed implementation

Per the issue's steer: a **separate task**, not a repurposed nextPP.

### 4.1 New task `PDPv0_ProcDel`

New `tasks/pdpv0/task_process_deletions.go`, modelled on `task_cleanup_pieces.go`, which is already
the right shape: resumable, no local cursor, gas-estimate-driven batch halving.

Per invocation:

0. Check `PDPVerifier.VERSION()`. Below the threshold → drop the work-queue row and finish without
   sending; the old `nextProvingPeriod` still drains the queue itself (§4.8).
1. Read the dataset's queue length. If zero → drop the work-queue row and finish.
2. Confirm the drain gate (§2).
3. Pick `removalCount`; send `processPieceDeletions(setId, count)` with a new send reason
   `pdp-process-deletions`; insert `message_waits_eth`; record the tx hash on the work-queue row (§4.2).
4. On gas-estimate failure, halve `count` and retry — the loop at `task_cleanup_pieces.go:142-184`
   plus `isCleanupPiecesGasEstimateOutOfGas` is directly reusable. This is what the contract PR's
   follow-up note asks for: *"Curio must call `processPieceDeletions()` with a suitable removal count
   and retry with smaller counts when necessary."*
5. Repeat until the queue is empty.

**Batch sizing**: start at `min(queueLength, 100)` — one `PiecesRemoved` event chunk, well under the
block gas limit. With the ceiling held at 35 this is inert in steady state (one message drains
everything) and only engages on migration-seeded backlogs, which run under 10 messages (§3.5). The
listener's gas cost is invisible to PDPVerifier — the root cause of #283 — so the halving loop, not
the constant, is what guarantees progress. 100 is a starting point, not a load-bearing number.

**Scheduling and ordering**: a tipset watcher, not `IAmBored`. The drain gate and the nextPP watcher's
gate are the *same* predicate, so both fire on the same tipset. Order them explicitly:

- Add a `WatcherOrderProcessDeletions` phase to `watcher.go:22-38`, between `WatcherOrderCleanupPieces`
  and `WatcherOrderProving`, and register the drain watcher there.
- The watcher selects from the work queue (§4.2) joined against `pdp_data_sets` for the boundary
  predicate — pure SQL, no `eth_call` in the watcher itself.
- Have `NextProvingPeriodTask.Do` and `InitProvingPeriodTask.Do` **preflight** for an outstanding
  drain and, if there is one, kick the drain task and return rather than sending a doomed transaction.

A poll (`IAmBored`, as `CleanupPiecesTask` uses) was rejected: nothing waits on piece cleanup, but a
tipset spent on an undrained queue is a tipset `nextProvingPeriod` cannot roll over, and a poll
interval on the order of the challenge window risks turning a wait into a fault.

**Revert backstop** (preflight races the chain): classify `PendingPieceDeletions` in
`handleNextProvingPeriodSendError` (`task_next_pp.go:441`) as "drain and retry" — schedule the drain
and return the send error so Harmony retries. Do **not** route it into `disableProvingForEmptyDataset`
or any terminal path. The revert's `count` can seed the next batch size if decoded. `task_init_pp.go`
sends the same `nextProvingPeriod` calldata (line 185) and needs the same treatment.

### 4.2 Drain work queue: `pdpv0_deletion_drain`

The watcher needs to know which datasets have a non-empty on-chain queue, which is not DB state. A
dedicated work-queue table carries it, with two writers:

1. **Seeded once by the SQL migration** shipping this change:

   ```sql
   INSERT INTO pdpv0_deletion_drain (data_set)
   SELECT id FROM pdp_data_sets
   ON CONFLICT (data_set) DO NOTHING;
   ```

2. **Written by intake** thereafter — `pdp/handlers.go` `DeletePiece` inserts the row in the same DB
   transaction that records the `schedulePieceDeletions` send.

Rows are **candidates**, not confirmed work. The task's first act on a claimed row is
`getScheduledRemovals`; an empty queue means the row is dropped and nothing is sent. So the seed can
be indiscriminate, and the migration needs no chain access.

This follows `pdp_data_set_piece_scrub`
(`harmony/harmonydb/sql/20260804-pdpv0-repair-missing-pieces.sql`, for
[#1359](https://github.com/filecoin-project/curio/issues/1359)) — same seed-from-local-rows shape,
same `task_id` claim, same `failures` retry bound. Two differences:

- **No resume cursor.** The scrub table needs `next_piece_id`; here the contract drains from the tail
  and re-reading the queue length *is* the cursor.
- **Not one-shot.** The scrub table goes permanently idle and uses a `complete` flag. This queue has a
  permanent writer, so rows are **deleted** when the dataset's queue empties.

The in-flight drain tx is tracked here too (`msg_hash` alongside `task_id`) rather than on
`pdp_data_sets`. One in-flight drain per dataset is the correct constraint — drains must be sequential
because each re-reads the queue — and it gives the nextPP/initPP preflight a cheap DB check with no
`eth_call`.

Seeding by migration rather than sweeping at process start matters because:

- **Curio is a cluster.** "Task startup" is not a single event; every machine would sweep, or leader
  election would be needed to stop them. A migration runs once, cluster-wide, by construction.
- **No completion bookkeeping** — no "have we swept yet" flag, no persisted cursor, no resume path if
  the process dies mid-sweep. An empty table means done.
- **No boot-time RPC burst** — thousands of `getScheduledRemovals` calls spread across batched,
  claimed task runs.

This is not a chain enumeration: `processPieceDeletions` requires `storageProvider[setId] == msg.sender`,
so the seed domain is local `pdp_data_sets` rows. The seed is complete for the backlog by definition —
a queue exists only because Curio sent the `schedulePieceDeletions` that created it.

### 4.3 Intake gating

`pdp/handlers.go` `DeletePiece` currently refuses only on queue depth (§3.5). It gains two more
refusals, both in the same `429` shape with distinct messages.

**(a) Reject while a drain is outstanding** for that dataset — a `pdpv0_deletion_drain` row exists and
the dataset is past `prove_at_epoch + challenge_window`. Two things depend on it:

- **The rollover stops slipping.** Without it, intake arriving between the drain and the
  `nextProvingPeriod` send re-fills the queue, nextPP reverts again, and the dataset chases its own
  tail across the period.
- **The intake ceiling becomes a true per-period rate limit** rather than only a depth limit (§3.5).

**(b) Reject before the dataset has an initialized proving schedule** — i.e. `prove_at_epoch IS NULL`.
This is what keeps the drain gate satisfiable. The gate (§2) means "wait until the proving window
closes", which presupposes a window exists; with no proving schedule there is none, so a queue
accumulated in that state could never drain, while `nextProvingPeriod` and `initProvingPeriod` would
both revert with `PendingPieceDeletions` for as long as it stayed non-empty. Refusing at intake means
that state is never reachable.

Express the check as `prove_at_epoch IS NOT NULL` specifically — that is the exact precondition the
drain gate needs, and testing anything weaker (such as `init_ready`) re-opens the gap. The refusal is
temporary: `initProvingPeriod` is triggered by the add-piece path, so a client that gets this 429
succeeds shortly afterwards.

**Why (b) is sufficient**, given the other two paths that null `prove_at_epoch` —
`disableProvingForEmptyDataset` and `processEmptyProvingPeriods` — both fire only for datasets with
zero leaves, and a dataset with zero leaves has no pieces left to schedule for removal. Their queues
are also necessarily empty already, because `nextProvingPeriod` checks `PendingPieceDeletions` *before*
`NoPiecesToProve` (confirmed in the contract diff), so neither path can be reached while a queue
remains. The one remaining path — a never-initialized dataset whose pieces are scheduled for deletion
before proving starts — is exactly what (b) blocks.

**LIFO starvation is a residual property, not a bug to fix here.** The contract pops from the tail, so
new removals are processed before older ones and the oldest can be drained last. Curio cannot reorder;
the intake gate is the only lever. The gate plus a full drain every period bounds starvation to one
proving period. Stated explicitly so it is not later mistaken for a Curio scheduling defect.

### 4.4 Move the tracking out of `watch_proving_period.go`

Relocate `processPendingPieceDeletes`, `getScheduledRemovalSet`, `clearPendingPieceDelete` and
`markPendingPieceRemoved` into a new `watch_process_deletions.go`, retriggered on
`processPieceDeletions` confirmation rather than nextPP confirmation. The bodies barely change, and
the reconciliation gets *more* accurate now that a distinct on-chain call corresponds to the removal.

Apply the §3.3 leaf-count fix to `processEmptyProvingPeriods` at the same time.

### 4.5 Error detection — add the new custom errors

In `tasks/pdpv0/error_detection.go`:

- Add ABI lookups for `PendingPieceDeletions`, `InvalidPieceDeletionBatch`, `EmptyRemovalBatch`,
  `OnlyStorageProvider` and `NoPiecesToProve` in `init()`, following the existing
  `parsedPDPVerifier.Errors[...]` + panic-on-missing pattern.
- Add `IsPendingPieceDeletionsError` (→ drain and retry), plus predicates for the drain task's own
  reverts: `InvalidPieceDeletionBatch` / `EmptyRemovalBatch` mean Curio's queue-length view is stale
  (re-read and retry); `OnlyStorageProvider` is an operator-attention condition.
- **Extend, do not replace**, `IsNextProvingPeriodEmptyDatasetError` (line 217): it must match both the
  `provingRevertNoLeavesForProvingPeriod` string (line 50, emitted by `3.4.0`) and the new
  `NoPiecesToProve()` selector. One build spans both contract versions (§4.8), and the two encode the
  same condition — `dataSetLeafCount > 0` — so matching both is correct rather than merely tolerant.
  The string matcher can only be dropped once no deployment runs the old contract.

### 4.6 Reorg handling

Add `pdp-process-deletions` to `pdpv0SendReasons` (`task_reorg_check.go:54`) and a
`rollbackProcessDeletionsTx` case in `rollbackByReasonTx` (line 441). A reorged-out drain means the
pieces are live and re-queued on-chain, so local `removed = TRUE` must be reverted.

This is time-critical, not merely bookkeeping: `task_piece_gc.go:94` GCs on
`removed = TRUE AND rm_message_hash IS NOT NULL`, deleting the `pdp_data_set_pieces` row, its
`pdp_piecerefs` and the underlying `parked_piece_refs`. Once GC has run, a reorg cannot be repaired —
the same hazard `rollbackDeletePieceTx` already warns about (*"would unmark rows but pieceref data
already cleaned up — possible DATA LOSS"*).

### 4.7 Bindings and constants

- Regenerate `pdp/contract/PDPVerifier.abi` / `PDPVerifier.go` per `pdp/contract/README.md`
  (forge `make build` in FilOzone/pdp → `jq '.abi'` → `abigen`). The README's `--out` paths say
  `pdp_verifier.go`; the checked-in filename is `PDPVerifier.go` — follow the existing filenames.
- Consider asking upstream for a `getScheduledRemovalsLength(uint256)` view. Curio polls queue state
  per dataset and currently marshals the entire array just to read its length.
- Leave `ConservativeEnqueuedRemovalsLimit` at 35 (§3.5); only the two operator-facing messages need
  rewording.

### 4.8 Version gating and rollout

The upgrade is a UUPS proxy upgrade, so one Curio build must work against both contract versions. All
new removal behaviour therefore sits behind a `PDPVerifier.VERSION()` check — the getter already
exists in the ABI and binding. The live contract reports:

```solidity
string public constant VERSION = "3.4.0";
```

Against that version, `processPieceDeletions` does not exist and `nextProvingPeriod` still drains the
queue itself. So on the old version the drain task **succeeds immediately as a no-op** — it drops its
work-queue row without sending anything and lets `nextProvingPeriod` proceed exactly as today. Only a
version at or above the bump switches on real processing.

One earlier decision changes as a consequence: **error detection keeps both matchers** (§4.5). A
single build spans both versions, so the empty-dataset condition arrives as the `"can only start
proving once leaves are added"` string on `3.4.0` and as the `NoPiecesToProve()` selector afterwards.
Match both; they mean the same thing and neither can be dropped until every deployment is upgraded.
This reverses the earlier "switch to the new values outright" decision, which assumed a coupled
release.

Holding `ConservativeEnqueuedRemovalsLimit` at 35 (§3.5) keeps the intake ceiling *out* of the version
gate, which is a real simplification: a raised limit would have to read 35 on `3.4.0` and the higher
value on the new contract, since raising it under the old one reproduces #283.

Everything else is naturally version-safe: the intake rules (§4.3) are harmless on the old contract,
the nextPP/initPP preflight finds no drain rows to block on, and the removal reconciliation (§4.4) is
chain-authoritative — so it can stay wired to *both* the nextPP and drain confirmations and behave
correctly under either contract.

Implementation notes:

- `VERSION` is constant per implementation but the proxy is upgradeable in place, so the value must be
  re-read rather than cached for the process lifetime. Reading it once per drain-task invocation is
  cheap enough; a short TTL cache is the obvious refinement if the call volume matters.
- Compare with semver ordering against a threshold constant, not string equality — the deployment will
  keep moving past whatever version introduces this.
- Curio can now ship before or after the contract upgrade, and needs no coordinated release.

---

## 5. Still open upstream

- Nothing bounds `removalCount` on-chain; Curio's cap plus the halving loop is the only limit.
- The PR body's claims about `SimplePDPService` proof/deadline enforcement and
  `PDPRecordKeeper.REMOVE_PROCESSED` are not backed by the diff — confirm whether they land here or in
  a sibling PR.
- **Tail-order draining is fixed by the contract** (§4.3). Nothing in pdpv0 appears to depend on
  deletion ordering, but anything reporting deletion progress to a client deserves a second look.

## 6. Open questions

### 6.1 Confirm no legacy dataset is already un-drainable

Resolved for new state by the intake rule in §4.3(b): a dataset can no longer accumulate a removal
queue while `prove_at_epoch IS NULL`, so the drain gate is always eventually satisfiable.

What that rule cannot do is repair a dataset already in that state when the migration runs. The
reasoning says none should exist, because the *old* contract drained the queue inside
`nextProvingPeriod` whenever leaves were present — so the never-initialized case self-resolved at
initPP, and the #283 gas-stuck datasets kept a non-NULL `prove_at_epoch` throughout (they revert on
gas, never reaching a leaf-count or empty-dataset path). That argument is sound but intricate, and it
rests on the behaviour of a contract version that is being replaced.

Cheap insurance rather than more analysis: have the drain task **alert** when it finds a work-queue row
whose dataset has a non-empty on-chain queue and `prove_at_epoch IS NULL`, instead of silently skipping
it. If the reasoning holds the alert never fires; if it does fire, an operator sees a stuck dataset
rather than a silent one. Worth a query against production `pdp_data_sets` before the upgrade to
confirm the set is empty.

### 6.2 Failed (not reorged) drain transactions

A drain tx that lands with `tx_success = false` leaves `msg_hash` set on the work-queue row. Nothing
clears it, so the dataset stalls behind a dead transaction. Needs the same treatment
`processPendingPieceDeletes` gives failed `schedulePieceDeletions` sends: clear the hash and let the
watcher re-schedule. Mechanical, but easy to omit.

### 6.3 Draining a dataset that is no longer live

`processPieceDeletions` and `getScheduledRemovals` both `require(dataSetLive(setId))`. A dataset in
deletion/cleanup keeps its work-queue row, so the drain task's first `eth_call` reverts with
`DataSetNotLive` on every attempt until `failures` exhausts. Should be classified as "drop the row",
not as a failure — cheap to handle, worth deciding deliberately.

### 6.4 Remaining items to confirm

- **What version string introduces `processPieceDeletions`?** The gate threshold (§4.8) cannot be
  written until the bump from `3.4.0` is decided upstream.
- **Does `NoPiecesToProve()` survive** the removal of the zero-leaf relaxation, or does the `require`
  revert to the string form? Decides whether §4.5 needs a second matcher at all.

Both are upstream questions. Backlog depth and batch sizing are settled: real queues run to at most a
few hundred pieces, under 10 drain messages, comfortably inside the §3.5 budget.
