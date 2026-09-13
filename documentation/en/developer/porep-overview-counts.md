# PoRep Overview sector counts

The Overview and miner table share one `PorepPipelineSummary` response. This is
read-only telemetry, not a scheduling change or a native health check. Existing
`CountSDR` and the other legacy fields remain wire-compatible with their original
definitions. In particular, `CountSDR` means **all `after_sdr=false` sectors**,
not executing SDR tasks. Legacy stage counts overlap and can include failures;
the UI labels their limitation and never sums them into an "in flight" number.

## New `SectorCounts` fields

All counts have unit **one `(sp_id, sector_number)` pipeline row**, not task,
worker, piece or stage. Different miners can have the same sector number.

| Field | Meaning at `ObservedAt` |
| --- | --- |
| Total | All rows still present in `sectors_sdr_pipeline`. |
| Complete | `after_sdr && after_commit_msg_success && after_move_storage`. |
| Remaining | Total minus Complete; includes queues, failures and unknown states. |
| Failed | Remaining rows with `failed=true`, at any stage. |
| PostSDR | Non-failed Remaining with `after_sdr=true`; **not necessarily executing**. |
| SDRTotal | Every `after_sdr=false` row, including the exception categories below. |

The normal lifecycle in `tasks/seal/poller.go` starts Finalize after PoRep, and
MoveStorage after Finalize. Commit confirmation can precede either operation.
Such sectors remain in PostSDR until storage has moved. Complete is the pipeline
flag boundary, **not** proof of data availability, final market indexing, or
chain correctness. `PipelineGC.cleanupSealed` additionally requires sector/piece
metadata before removing the completed pipeline row. Complete rows can therefore
remain visible for a while. A completed row marked failed is not counted as
Remaining/Failed; the compatible legacy CountFailed still reports its flag.

### Disjoint SDR classification, in precedence order

Only `after_sdr=false` rows enter these categories:

1. **SDRFailed**: pipeline failed flag, even with a task or broken link.
2. **SDRUnknown**: contradictory later-stage flags (TreeC/TreeR, message, PoRep,
   Finalize or MoveStorage). TreeD alone can proceed independently and is allowed.
3. **SDRWaitingCreate**: NULL task link; no task yet. Not failure evidence.
4. **SDRMissingTask**: non-NULL link but task row absent.
5. **SDROtherTask**: linked name is not exactly `SDR`. This includes SDRKeyRegen,
   an erroneous type, and legitimate SupraSeal `Batch*` tasks. None is silently
   presented as a standard SDR execution. No batch-native phase is inferred.
6. **SDRWaitingTask**: unowned SDR task with cleared attempt metadata. This
   includes normal retry waits; it is not a claim about terminal failure.
7. **SDRUnknown**: missing owner/provenance, owner heartbeat older than two minutes
   or in the future, missing/untrusted ownership time, or ownership time in the
   future. The two-minute *display* threshold matches Overview's machine status;
   it does not alter Harmony's lease/cleanup thresholds.
8. **SDRPreparing**: claimed with no token/start, or prepared with a nonempty
   token but no start. Both require a fresh owner and trusted ownership time.
9. **SDRRunning**: nonempty current `attempt_id`, source `do_entry`, and
   `work_start <= attempt_started_at <= ObservedAt`, with the owner checks above.
10. **SDRUnknown**: everything else, including legacy/backfilled/partial metadata.

Ownership alone, task posted time, historical runs, or a preparation marker never
prove Do entry. The start record is the same current-attempt boundary used by
Cluster Tasks' Took and new History records. The existing ownership/acquisition
triggers clear attempt data on reassignment and same-owner recovery. They are
not changed here. A long-running SDR with a fresh owner remains counted; age
alone is not a timeout. A current DB Do-entry record does **not** establish that
native computation is progressing, or that a lost/old process has stopped.

The API and frontend tests assert:

```
SDRTotal = SDRRunning + SDRPreparing + SDRWaitingTask + SDRWaitingCreate
         + SDRMissingTask + SDROtherTask + SDRFailed + SDRUnknown
Total = Complete + Remaining
Remaining = SDRTotal + PostSDR + Failed - SDRFailed
```

Failed and complete sectors are excluded from the new normal execution and
post-SDR counters. Exceptions stay visible and are never silently discarded.

## Query and refresh contract

One SQL statement groups all pipeline rows, joining only primary keys
`harmony_task.id` and `harmony_machines.id`. It returns one small row per miner;
it does not fetch task histories, proof bytes, all sectors, or page results.
`statement_timestamp()` is shared by all returned miners. The pre-existing
single ChainHead call is retained only for the compatible seed-stage counts.
The SQL context is bounded to 15 seconds; the browser aborts its HTTP request at
20 seconds. There are no task-by-task polls.

The miner component owns the summary poll (five seconds after settlement,
bounded error backoff), and publishes its exact snapshot/status to Overview.
The former second Overview summary call is removed. Existing page/Cluster Tasks
limits, pending preview, coalescing and filters are not aggregate inputs.
The existing PoRep page poller provides cancellation and generation fencing;
late responses from hidden/disconnected views cannot replace the snapshot.

Loading, refreshing, unavailable, stale and paused states are explicit. On query
failure, valid previous values remain **labelled previous**, never replaced with
zero or advanced by a local clock. New fields missing from an older backend,
invalid counters, duplicate miners, or mixed timestamps are errors. An explicit
successful empty array is zero; NULL/partial responses are unavailable. No
fallback to legacy SDR or stage sums fabricates the new meanings.

## Local validation

Go package: `web/api/webrpcporep`; normal tags `cgo,fvm,nosupraseal`.
SQL tag set: `cgo,fvm,nosupraseal,integration`.

- `TestPoRepSummaryFailureIsNotZero`: production entry fails on ChainHead error.
- `TestPoRepSummaryAdditiveWireContract`: old fields retain their wire values.
- `TestPoRepSummarySQLClassification`: actual handler/query/mapper, state
  transitions and classification priorities, using real ownership/acquisition
  triggers and focused current schema migrations.
- `TestPoRepSummarySQLLargeSnapshot`: 32,108 queued rows plus six other rows,
  two miners, bounded payload, actual EXPLAIN ANALYZE and query-error propagation.
- `web/test/porep-summary.test.mjs`: reconciliation and missing-field semantics.
- `web/test/porep-page-poller.test.mjs`: reused transport cancellation/lifecycle.
- `web/test/porep-summary-browser.mjs`: real Chromium/Lit with intercepted offline
  RPC fixtures; baseline/current screenshots, single shared request, stale/error,
  missing fields, lower running count, hidden/late response and empty snapshot.

SQL fixture guards are reused from the PoRep page tests: explicit
`CURIO_POREP_PAGE_ITEST=1` plus dedicated `CURIO_POREP_PAGE_ITEST_HOST`, `_PORT`,
`_DATABASE`, `_USER`, optional `_PASSWORD`. HOST must be a literal loopback IP;
load balancing is off; both handles must report the same owned `itest_*` schema.
Only that namespace is cleaned. Clear inherited database/integration settings
before supplying these variables. Never target production.

```
go test -count=1 -timeout=3m -tags=cgo,fvm,nosupraseal,integration \
  -run '^TestPoRepSummarySQL' -v ./web/api/webrpcporep
node --test web/test/porep-summary.test.mjs web/test/porep-page-poller.test.mjs
```

The SQL fixture executes relevant real DDL (PKs, FK, column types, telemetry and
generation triggers); it is not a full startup/migration-runner or native task
test. No sealing, chain mutation, scheduler, worker, GC or configuration changes
are made. PostgreSQL execution, Yugabyte execution, offline browser evidence and
production observations must be reported separately in the review archive.

After separate deployment approval, an operator can compare Overview and the
miner table on one refresh, verify queued/preparing/Do-entry transitions and
temporarily disconnect the browser to inspect stale labels. No operational SQL
or service changes are required by this source handoff. Native health must still
be checked independently; this dashboard does not establish it.
