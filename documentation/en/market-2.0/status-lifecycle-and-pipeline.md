# Deal Lifecycle, Status, and Pipelines

This page defines the deal intake paths, lifecycle progression, status semantics, and pipeline behavior for Market 2.0.

## Deal Intake Paths

All intake paths use the same deal model and the same deal identifier (`id`, ULID). They differ only in when and how piece bytes are provided.

### 1. Provider-Pull Deal

Use this path when the storage provider should fetch data from client-supplied sources.

API sequence:

1. `POST /deal`
2. `GET /status/{id}` until terminal state

No upload endpoints are used.

### 2. Accept-Then-Upload Deal

Use this path when a client wants acceptance first, then uploads bytes.

API sequence:

1. `POST /deal`
2. Upload bytes using one mode:
3. Serial: `PUT /upload/{id}` then `POST /upload/{id}`
4. Chunked: `POST /uploads/{id}` then `PUT /uploads/{id}/{chunkNum}` (repeat) then `POST /uploads/finalize/{id}`
5. `GET /status/{id}` until terminal state

### 3. Upload-First-Then-Describe Deal

Use this path when bytes are uploaded first and full deal details are supplied at finalize.

API sequence:

1. `POST /deal`
2. `PUT /upload/{id}`
3. `POST /upload/{id}` with full deal payload
4. `GET /status/{id}` until terminal state

Current behavior: this path is supported in serial upload flow.

### 4. Control-Only Operation (No Piece Ingestion)

Use this path for PDP lifecycle operations that do not ingest piece data (for example dataset create, dataset delete, or piece-remove operations).

API sequence:

1. `POST /deal`
2. `GET /status/{id}` until terminal state

No upload endpoints are used.

## Deal Lifecycle

Once accepted, deals follow a common lifecycle model:

1. `accepted`: proposal admitted.
2. `uploading` (optional): waiting for or receiving client-uploaded bytes.
3. `processing`: product execution has started.
4. `sealing` (when applicable): sealing work in progress.
5. `indexing` (when applicable): indexing and retrieval preparation in progress.
6. Terminal outcome: `complete` or `failed`.

Not every deal uses every intermediate state. Upload-less operations typically skip `uploading`.

## Status Checks

Use `GET /status/{id}` for lifecycle tracking.

Status is returned per product present in the deal.

Status values:

1. `accepted`
2. `uploading`
3. `processing`
4. `sealing`
5. `indexing`
6. `complete` (terminal success)
7. `failed` (terminal failure)

Use upload endpoints for byte-transfer progress (`/upload*`, `/uploads*`). Use `/status/{id}` for lifecycle state.

## Pipelines

Pipelines are the asynchronous execution layer behind lifecycle transitions.

What pipelines do:

1. Run ingest work for pull and upload flows.
2. Run product-specific execution once data is ready.
3. Advance deals to terminal outcome.

When pipeline execution starts:

1. Pull-based deals: after acceptance.
2. Upload-based deals: after upload finalize.
3. Control-only operations: after acceptance.

Operational guidance:

1. If an upload-based deal appears stalled, verify finalize was called.
2. If status remains non-terminal unexpectedly, provider-side operational inspection is required.
