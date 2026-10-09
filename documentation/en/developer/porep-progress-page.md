# Bounded PoRep progress view

The pipeline list uses `CurioWeb.PipelinePorepPage` over cancellable HTTP JSON-RPC.
The legacy `PipelinePorepSectors` method and sector detail API are unchanged.

## Read contract

The request takes `Offset` (nonnegative) and `HidePendingSDR`. Page size is fixed
at 100. A single SQL statement returns total/matching counts, pipeline-wide
waiting-for-PreCommit/Commit counts and the requested page. Failed, owned SDR
and post-SDR rows sort before unclaimed unfinished SDR rows, then by provider
and sector. The optional hide filter preserves those same categories. Previous,
next and first-page controls expose the remaining matching rows; hiding never
deletes data. Changing filter returns to offset zero. Counts and the applied
filter remain attached to the last successful snapshot during refresh/failure.

Only page rows get stage-task joins. Proof bytes, task-history polling and miner
bitfield/chain queries are not part of this endpoint. Counts and priority sorting
still inspect the pipeline: bounded rows/wire/DOM does **not** imply constant
database work. Deep offsets also have a cost. No index or migration is added.
Pages are independently refreshed snapshots, not an immutable export; changing
pipeline/ownership can move rows between requests. Use First page to revisit
new progress. This UI does not promise snapshot-consistent traversal of all pages.

Task labels use the page's stage markers and live task membership: done, owned,
queued, or not queued. Owned is not proof of `Do`/native execution. Not queued
does not by itself prove a task failed; retained task IDs can outlive execution.
Task links still open history/actions. Per-task automatic polling and inline
restart buttons are absent from the list; the existing task-detail restart path
is unchanged. The sector detail renderer still uses the existing task component.

Chain presence/active/seed readiness are explicitly unknown on this progress
view, never false just because enrichment was omitted. Open sector Details for
chain-enriched information. A chain outage must not prevent DB progress rows
from appearing here.

## Refresh lifecycle

The first pending/failed request is not an empty result. Only a successful
snapshot can show zero. Errors retain the last data with a stale warning and
last-success/server timestamps. A successful empty page beyond the current end
is not a zero-total pipeline; First page remains accessible.

The server query has a 45-second context budget; HTTP transport has a 60-second
deadline. A slow success inside the budget is allowed. Refresh settles for five
seconds after success. Errors back off to 10, 20, then 30 seconds. Pause,
document hiding and component disconnection abort the current HTTP request and
clear timers. Reconnection resumes once. Filter/page changes cancel and
invalidate older responses. The next request waits for the previous transport
to settle; there is no timeout-only promise race that leaves WS work running.
Database cancellation still depends on the driver/server honoring context.

## Evidence boundary

Go loader/RPC tests and SQL-shape assertions do not execute SQL. The offline
Chromium fixture runs actual component/Lit and HTTP cancellation behavior using
synthetic response pages. Its 31,477-row/44-owner planner is a test substitute,
not verification of PostgreSQL/Yugabyte ordering or query plans. The baseline
measurement executes real render functions and per-task connected/poll callbacks
but does not mount a million-cell DOM; it suppresses rendering only for the
isolated task-poll fanout measurement. The new view renders real DOM pages.

Run node unit tests under `web/test`; the optional browser fixture is
`web/test/porep-page-browser.mjs`. Supply an existing `PLAYWRIGHT_MODULE`,
`CHROMIUM_EXECUTABLE`, `LIT3_FIXTURE` and `LIT2_FIXTURE`. All routes are locally
fulfilled/blocked. `POREP_BASELINE` selects preserved source through `git show`;
`POREP_ASSERT_FIXED=1` checks the new loading assertion against that old source.
`POREP_NEGATIVE=loading` mutates only the browser-served module to verify that
the loading regression fails; no tracked source is edited.

Actual SQL plans/row effects, production browser timings and chain/native SDR
throughput remain separate operator validation. Apply this UI/backend change
only to the WebRPC/GUI-serving role after review; it is not a reason to restart
SDR workers or change pacing/configuration.
