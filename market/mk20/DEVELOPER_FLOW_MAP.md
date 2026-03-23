# Market 2.0 Intake -> Pipeline-With-Data (Developer Flow Map)

## Scope
This map covers the runtime path from API intake to the point where a deal is in pipeline with data available.

Pipeline-with-data means:
1. DDO path: row in `market_mk20_pipeline` has `downloaded=TRUE` and `url='pieceref:<ref_id>'` (or is inserted directly in that state).
2. PDP AddPiece path: row in `pdp_pipeline` has `downloaded=TRUE` and `piece_ref IS NOT NULL` (or is inserted directly in that state).

## Code anchors
1. Intake/auth/routes: `market/mk20/http/http.go`
2. Deal intake + product branching: `market/mk20/mk20.go`
3. Core validation: `market/mk20/utils.go`
4. DDO contract verification: `market/mk20/ddo_v1.go`
5. Upload flows: `market/mk20/mk20_upload.go`
6. Pipeline insertion/background loops: `tasks/storage-market/mk20.go`
7. Chunk-finalize aggregation task: `tasks/piece/task_aggregate_chunks.go`
8. Offline/download SQL + downloaded markers: `harmony/harmonydb/sql/20250505-market-mk20.sql`, `harmony/harmonydb/sql/20260310-mk20-ddo-contracts.sql`

## Decision tree (actual control flow)

```text
POST /deal
 -> AuthMiddleware
 -> mk20deal handler
 -> ExecuteDeal(deal, auth)

ExecuteDeal
 -> deal.Validate()
    -> validateClient(auth)
    -> Products.Validate()
    -> Data.Validate() only if Data != nil
 -> if DDO present: processDDODeal
 -> else if PDP present: processPDPDeal
 -> else reject unsupported product
```

```text
processDDODeal
 -> sanitizeDDODeal (Data required, retrieval required, provider/allocation/size checks)
 -> VerifyMarketDeal (only if market_address != "")
    -> ddo_contracts.allowed must be TRUE
    -> CurioDealViewV1.version() == 1
    -> CurioDealViewV1.getDeal(marketDealID) matches local provider/client/piece/allocation/duration
    -> reject if market state = Finalized
 -> SaveToDB
 -> queue
    -> SourceHttpPut: insert market_mk20_upload_waiting
    -> all other data sources: insert market_mk20_pipeline_waiting
```

```text
processPDPDeal
 -> sanitizePDPDeal
    -> AddPiece requires RetrievalV1
    -> SourceOffline rejected
 -> SaveToDB
 -> if AddPiece:
    -> SourceHTTP or SourceAggregate: insertPDPPipeline (download + pdp rows)
    -> SourceHttpPut: insert market_mk20_upload_waiting
    -> Data=nil: insert market_mk20_upload_waiting
 -> if CreateDataSet: insert pdp_data_set_create
 -> if DeleteDataSet: insert pdp_data_set_delete
 -> if DeletePiece: insert pdp_piece_delete
```

## Product x data source x upload type (what is valid)

| Product operation | Data source | Upload type | Result |
|---|---|---|---|
| DDO | `http` | none | accepted, goes `market_mk20_pipeline_waiting` |
| DDO | `aggregate` | none | accepted, goes `market_mk20_pipeline_waiting` |
| DDO | `offline` | none | accepted, goes `market_mk20_pipeline_waiting` |
| DDO | `source_http_put` | serial/chunked APIs later | accepted, goes `market_mk20_upload_waiting` |
| DDO | `data=nil` | N/A | rejected by `sanitizeDDODeal` |
| PDP `AddPiece` | `http` | none | accepted, `insertPDPPipeline` |
| PDP `AddPiece` | `aggregate` | none | accepted, `insertPDPPipeline` |
| PDP `AddPiece` | `source_http_put` | serial/chunked APIs later | accepted, `market_mk20_upload_waiting` |
| PDP `AddPiece` | `data=nil` | serial/chunked APIs later | accepted, `market_mk20_upload_waiting` |
| PDP `AddPiece` | `offline` | N/A | rejected by `sanitizePDPDeal` |
| PDP control ops (`CreateDataSet/DeleteDataSet/DeletePiece`) | usually `data=nil` | none | accepted, writes control tables only |

## Upload type is operational, not a deal field
There is no `uploadType` field in `Deal`. Upload mode is chosen by endpoint sequence:
1. Chunked: `POST /uploads/{id}` -> `PUT /uploads/{id}/{chunk}` -> `POST /uploads/finalize/{id}`
2. Serial: `PUT /upload/{id}` -> `POST /upload/{id}`

## Upload flow details to pipeline-with-data

### Chunked upload path
1. `HandleUploadStart` creates `market_mk20_deal_chunk` rows and sets `market_mk20_upload_waiting.chunked=TRUE`.
2. `HandleUploadChunk` stores each chunk in parked piece refs and marks chunk row complete/ref_id.
3. `HandleUploadFinalize` only marks chunk rows `finalize=TRUE`, deletes `upload_waiting`, and optionally updates deal details.
4. Background `AggregateChunksTask` picks finalized chunk sets, reconstructs full piece, verifies against deal piece CID/size, then inserts:
   - `market_mk20_pipeline` (DDO) with `started=TRUE, downloaded=TRUE, after_commp=TRUE, url=pieceref:*`
   - `pdp_pipeline` (PDP AddPiece) with `downloaded=TRUE, piece_ref=* , after_commp=TRUE`
5. Chunk rows and temporary refs are removed.

Important: for deals accepted with `data=nil`, finalize must include deal body; otherwise finalize rejects (`cannot finalize deal with missing data source`).

### Serial upload path
1. `HandleSerialUpload` writes full piece (or reuses existing parked piece) and sets `market_mk20_upload_waiting` to `chunked=FALSE, ref_id=<pieceRef>`.
2. `HandleSerialUploadFinalize` validates uploaded piece against deal piece info, optionally updates deal details, then directly inserts:
   - `market_mk20_pipeline` (DDO) in downloaded state
   - `pdp_pipeline` (PDP AddPiece) in downloaded state
3. `market_mk20_upload_waiting` row is deleted.

## Non-upload shortcut path (piece already present)
`insertDealInPipelineForUpload` scans `market_mk20_upload_waiting` rows with `chunked IS NULL AND ref_id IS NULL`.
If deal has `Data` and matching piece already exists in `parked_pieces`, it inserts DDO/PDP pipeline rows immediately and deletes the waiting row.

## Pipeline-waiting -> data-ready path (no upload APIs)

### DDO from `market_mk20_pipeline_waiting`
1. `pipelineInsertLoop -> insertDDODealInPipeline -> insertPiecesInTransaction`.
2. Source behavior:
   - HTTP: create parked refs + `market_mk20_download_pipeline`, set pipeline `started=TRUE`.
   - Offline: create pipeline row with `offline=TRUE, started=FALSE`.
   - Aggregate: one pipeline row per subpiece; HTTP subpieces also get download refs.
3. `processMK20Deals` then progresses data readiness:
   - `downloadMk20Deal` calls SQL `mk20_ddo_mark_downloaded` to bind completed refs to `url=pieceref:*` and set `downloaded=TRUE`.
   - `findOfflineURLMk20Deal` calls `process_offline_download(...)` and piece-locator probing for offline rows.
4. At `downloaded=TRUE + url=pieceref:*`, DDO is in pipeline with data.

### PDP AddPiece from `insertPDPPipeline`
1. Rows are inserted into `pdp_pipeline` plus `market_mk20_download_pipeline`.
2. Background `markDownloaded()` calls SQL `mk20_pdp_mark_downloaded`, which sets `pdp_pipeline.downloaded=TRUE` and `piece_ref`.
3. At `downloaded=TRUE + piece_ref set`, PDP AddPiece is in pipeline with data.

## DDO contract-gated verification path
If `market_address` is provided:
1. `market_deal_id` is mandatory.
2. Contract must be in `ddo_contracts` with `allowed=TRUE`.
3. ABI call is read-only (`CurioDealViewV1`):
   - `version()` must be `1`
   - `getDeal(marketDealID)` must match local provider/client/piece/allocation/duration
   - `state != Finalized`
4. `DealNotFound` revert selector maps to market rejection.

## Upload housekeeping state machine
1. SQL triggers set `market_mk20_upload_waiting.ready_at` when serial upload is ready or when all chunks become complete.
2. `removeNotFinalizedUploads` cleans stale uploads (`ready_at` older than 60 minutes), resets waiting state, and removes temporary refs/chunks.
