# PDP Service API Documentation

## Base URL

All endpoints are rooted at `/pdp`.

---

## Upload authentication gating

Piece upload endpoints support optional abuse protection controlled by a single node config flag:

| Config key | Location | Default |
|------------|----------|---------|
| `PDPUploadRequireAuth` | `Subsystems.PDPUploadRequireAuth` in Curio config | `false` |

When **`false`** (default), upload behavior matches the pre-gating API: anonymous callers are accepted, `dataSetId` and `X-PDP-Upload-Auth` are optional (validated when present, never required).

When **`true`**, three layers are enforced together on the piece-upload surface:

| Layer | Where enforced | What it proves |
|-------|----------------|----------------|
| **A — Service auth** | `POST /pdp/piece`, `POST /pdp/piece/uploads` | Caller holds a registered PDP service JWT (not the anonymous `"public"` label). |
| **B — Data set ownership** | Same init endpoints | Declared `dataSetId` exists locally, belongs to the caller's JWT `service_label`, and is not terminated. |
| **C — Wallet possession** | `PUT /pdp/piece/upload/{uuid}`, `PUT /pdp/piece/uploads/{uuid}` | Caller controls the data set's on-chain payer (or an authorized session key). Verified **before the request body is read**. |

Layers A and B run at upload-session init (UUID mint). Layer C runs on the bulk PUT only. Init and PUT are not linked by a server-side `data_set_id` on the upload row; the PUT carries the signed `dataSetId` in `X-PDP-Upload-Auth`.

### Service authentication (all `/pdp/*` routes)

Curio uses **HybridAuth**:

- **`Authorization` header absent** — caller is treated as service label `"public"` (legacy anonymous access).
- **`Authorization: Bearer <JWT>` present** — JWT is verified against `pdp_services.pubkey`; `service_name` claim becomes the service label.
- **Header present but JWT invalid** — `401 Unauthorized` (even when `PDPUploadRequireAuth=false`).

Layer A when gating is on rejects service label `"public"` with:

```text
401 Unauthorized: anonymous piece uploads are disabled; provide a registered service JWT
```

### Data set ownership (Layer B)

Init request bodies may include:

```json
{ "dataSetId": 12345 }
```

(`POST /pdp/piece` includes this field alongside `pieceCid`; `POST /pdp/piece/uploads` accepts a JSON body with only `dataSetId`, or an empty body.)

Validation uses the same local DB check as `POST /pdp/data-sets/{id}/pieces`:

- Data set row exists in `pdp_data_sets` with `service` matching the caller's JWT service label.
- `unrecoverable_proving_failure_epoch IS NULL`
- `terminated_at_epoch IS NULL`

| Condition | HTTP status |
|-----------|-------------|
| Missing `dataSetId` when gating on | `400 Bad Request` — `dataSetId is required` |
| Unknown or wrong-service data set | `404 Not Found` — `Data set not found` |
| Terminated / unrecoverable failure | `409 Conflict` — `data set has been terminated due to unrecoverable proving failure` |
| Repeated bad data set claims | IP offense throttle (`OffenseBadDataSetAdd`) may apply |

When gating is **off**, omitting `dataSetId` is allowed. If `dataSetId` **is** sent, it is validated the same way (early feedback for new clients).

**Important:** `dataSetId` is the **on-chain PDPVerifier data set id**. Upload gating cannot protect greenfield flows where bytes are uploaded **before** a data set exists on chain (e.g. upload → `create-and-add`). Those flows require gating off, or uploading only after the data set is created and the id is known.

### Wallet possession (Layer C)

**Header:** `X-PDP-Upload-Auth`

**Value format (dot-separated, no spaces):**

```text
{dataSetId}.{nonce}.{expiry}.{signature}
```

| Field | Type | Notes |
|-------|------|-------|
| `dataSetId` | decimal uint64 | On-chain data set id (must exist for payer lookup). |
| `nonce` | decimal uint256 | Client-chosen replay distinguisher; not server-tracked. |
| `expiry` | decimal uint64 | Unix seconds; must be ≥ server time. |
| `signature` | 65-byte ECDSA hex | `0x`-prefixed; EIP-712 over `PieceUploadAuth`. |

**EIP-712 domain** (read from FWSS `eip712Domain()` at runtime):

```text
name:              "FilecoinWarmStorageService"
version:           "1"
chainId:           <Filecoin chain id>
verifyingContract: <FWSS proxy address>
```

**Primary type:**

```solidity
PieceUploadAuth(uint256 dataSetId, uint256 nonce, uint256 expiry)
```

Full type graph includes `MetadataEntry`, `CreateDataSet`, `Cid`, `PieceMetadata`, `AddPieces`, `SchedulePieceRemovals`, `TerminateService`, `Permit` (matches `@filoz/synapse-core` `EIP712Types`).

**Verification (before body read):**

1. Parse header; reject expired `expiry`.
2. Recover signer via EIP-712 digest.
3. Resolve payer: `pdp_data_sets.payer_address` if cached, else FWSS `GetDataSetPayer` eth_call (cached on success).
4. Accept if `signer == payer`.
5. Else treat signer as session key: `SessionKeyRegistry.authorizationExpiry(payer, signer, AddPiecesPermission)` must return a future timestamp. Result cached ~30s per `(payer, signer)`.

`AddPiecesPermission` type hash: `0x954bdc254591a7eab1b73f03842464d9283a08352772737094d710a4428fd183`.

When gating is **off**, the header is **ignored** (not parsed).

| Condition | HTTP status |
|-----------|-------------|
| Gating on, header missing/malformed/expired | `401 Unauthorized` |
| Signer neither payer nor authorized session key | `401 Unauthorized` |
| Unknown data set / no payer | `401 Unauthorized` |

### Endpoint auth matrix (piece upload only)

| Endpoint | Gating off | Gating on |
|----------|------------|-----------|
| `POST /pdp/piece` | JWT optional → `"public"` OK; `dataSetId` optional | JWT required; `dataSetId` required + ownership check |
| `PUT /pdp/piece/upload/{uuid}` | No JWT; no wallet header | `X-PDP-Upload-Auth` required |
| `POST /pdp/piece/uploads` | JWT optional → `"public"` OK; body/`dataSetId` optional | JWT required; `dataSetId` required + ownership check |
| `PUT /pdp/piece/uploads/{uuid}` | JWT optional → `"public"` OK; no wallet header | JWT required; `X-PDP-Upload-Auth` required |
| `POST /pdp/piece/uploads/{uuid}` (finalize) | Unchanged | Unchanged (no new fields) |

Classic `PUT /pdp/piece/upload/{uuid}` never inspects `Authorization` (only Layer C when gating on). Streaming `PUT` still calls HybridAuth for service-label matching on the upload session row.

### Payer address caching

Migration `20260701-pdp-v0-upload-auth.sql` adds:

- `pdp_data_sets.payer_address` — populated when the data set is confirmed on chain (from create `extraData` captured in `pdp_data_set_creates`) or lazily via `GetDataSetPayer`.

### Client integration status

| Client | Gating off | Gating on |
|--------|------------|-----------|
| **Legacy / no JWT, no headers** | Works (anonymous `"public"` uploads). | **Blocked** (Layer A). |
| **`pdptool`** (`--data-set-id`, `--upload-auth-key`, `--jwt-token` / `--service-name`) | Works. | Supported when all credentials provided. |
| **`@filoz/synapse-sdk` `StorageContext.store()`** | Works (no upload auth when `_dataSetId` unset). | Sends `dataSetId` + signed header **only when `_dataSetId` is already set** (existing data set). Does **not** send PDP service JWT. Greenfield upload→create flows remain incompatible with gating on until JWT + pre-create data set id strategy is added. |
| **`@filoz/synapse-core` `uploadPiece` / `uploadPieceStreaming`** | Works. | Callers must pass `dataSetId`, `uploadAuthHeader`, and arrange JWT at the HTTP layer themselves. |

Rollout: deploy Curio + clients first with flag **off**; enable `PDPUploadRequireAuth` only when callers can supply JWT, on-chain `dataSetId`, and wallet proof on every bulk PUT.

---

## Endpoints Summary

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/pdp/ping` | Health check with JWT validation |
| POST | `/pdp/piece` | Initiate a piece upload (by pieceCid V2) |
| PUT | `/pdp/piece/upload/{uploadUUID}` | Upload piece data (classic) |
| GET | `/pdp/piece/` | Find a piece by pieceCid query param |
| GET | `/pdp/piece/{pieceCid}/status` | Get piece indexing/IPNI status |
| POST | `/pdp/piece/uploads` | Create a streaming upload session |
| PUT | `/pdp/piece/uploads/{uploadUUID}` | Stream piece data chunks |
| POST | `/pdp/piece/uploads/{uploadUUID}` | Finalize a streaming upload |
| POST | `/pdp/data-sets` | Create a new data set |
| POST | `/pdp/data-sets/create-and-add` | Create a data set and add pieces atomically |
| GET | `/pdp/data-sets/created/{txHash}` | Check data set creation status |
| GET | `/pdp/data-sets/{dataSetId}` | Get data set details |
| DELETE | `/pdp/data-sets/{dataSetId}` | Delete a data set *(not yet implemented)* |
| POST | `/pdp/data-sets/{dataSetId}/pieces` | Add pieces to a data set |
| GET | `/pdp/data-sets/{dataSetId}/pieces/added/{txHash}` | Get piece addition status |
| GET | `/pdp/data-sets/{dataSetId}/pieces/{pieceId}` | Get piece details |
| DELETE | `/pdp/data-sets/{dataSetId}/pieces/{pieceId}` | Schedule piece deletion |
| POST | `/pdp/piece/pull` | Pull pieces from other SPs |

---

## Endpoints

### 1. Ping

- **Endpoint:** `GET /pdp/ping`
- **Description:** A simple endpoint to verify that the service is reachable and the JWT token is valid.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.

#### Response

- **Status Code:** `200 OK`

---

### 2. Upload a Piece

#### 2.1. Initiate Upload

- **Endpoint:** `POST /pdp/piece`
- **Description:** Initiate the process of uploading a piece using its Piece CID (CommP v2 format). If the piece already exists on the server, the server responds with `200 OK` and the piece CID.
- **Authentication:** HybridAuth — see [Upload authentication gating](#upload-authentication-gating). JWT is optional when gating is off (`"public"` when omitted); required when gating is on.
- **Request Body:**

```json
{
  "pieceCid": "<CommP-v2-CID>",
  "notify": "<optional-notification-URL>",
  "dataSetId": 12345
}
```

- **Fields:**
    - `pieceCid` *(required)*: CommP **v2** Piece CID.
    - `notify` *(optional)*: Callback URL when the piece is processed.
    - `dataSetId` *(optional)*: On-chain data set id. Required when `PDPUploadRequireAuth=true`; validated when present regardless of flag.

#### Responses

1. **Piece Already Exists**

    - **Status Code:** `200 OK`
    - **Response Body:**

      ```json
      {
        "pieceCid": "<piece-CID-v2>"
      }
      ```

2. **Piece Does Not Exist (Upload Required)**

    - **Status Code:** `201 Created`
    - **Headers:**
        - `Location`: The URL where the piece data can be uploaded via `PUT` (e.g., `/pdp/piece/upload/{uploadUUID}`).

#### Errors

- `400 Bad Request`: Invalid `pieceCid`, missing `dataSetId` (gating on), or piece size over limit.
- `401 Unauthorized`: Invalid JWT when `Authorization` is sent; anonymous caller when gating on.
- `404 Not Found`: `dataSetId` not found for this service (when validated).
- `409 Conflict`: Data set terminated (when validated).

---

#### 2.2. Upload Piece Data (Classic)

- **Endpoint:** `PUT /pdp/piece/upload/{uploadUUID}`
- **Description:** Upload the actual bytes of the piece using the `uploadUUID` from the `Location` header of `POST /pdp/piece`.
- **Authentication:** No `Authorization` header is checked. When `PDPUploadRequireAuth=true`, **`X-PDP-Upload-Auth` is required** (verified before the body is read). See [Upload authentication gating](#upload-authentication-gating).
- **URL Parameters:**
    - `uploadUUID`: UUID from the init `Location` header.
- **Request Body:** Raw piece bytes.
- **Headers:**
    - `Content-Type`: `application/octet-stream`
    - `Content-Length`: Piece size (recommended)
    - `X-PDP-Upload-Auth`: Wallet proof when gating is on.

#### Response

- **Status Code:** `204 No Content`

#### Errors

- `400 Bad Request`: Size/hash/CID mismatch.
- `401 Unauthorized`: Missing or invalid `X-PDP-Upload-Auth` when gating is on.
- `404 Not Found`: Unknown `uploadUUID`.
- `409 Conflict`: Bytes already uploaded for this UUID.
- `413 Payload Too Large`: Over size limit.

---

#### 2.3. Find a Piece

- **Endpoint:** `GET /pdp/piece/`
- **Description:** Look up whether a piece exists on the server by its pieceCid.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **Query Parameters:**
    - `pieceCid`: The Piece CID (must be v2 format) to look up.

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "pieceCid": "<piece-CID-v2>"
}
```

#### Errors

- `400 Bad Request`: Invalid or missing `pieceCid` query parameter.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Piece not found.

---

#### 2.4. Get Piece Status

- **Endpoint:** `GET /pdp/piece/{pieceCid}/status`
- **Description:** Retrieve the indexing and IPNI advertisement status for a piece.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `pieceCid`: The Piece CID (v1 or v2) to check.

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "pieceCid": "<piece-CID-v2>",
  "status": "<status>",
  "indexed": <boolean>,
  "advertised": <boolean>,
  "retrieved": <boolean>,
  "retrievedAt": "<RFC3339-timestamp-or-omitted>"
}
```

- **Fields:**
    - `pieceCid`: The Piece CID in v2 format.
    - `status`: Overall status string. One of:
        - `"pending"` – Not yet indexed.
        - `"indexing"` – CAR indexing task is in progress.
        - `"creating_ad"` – IPNI advertisement is being created.
        - `"announced"` – Advertisement published to IPNI network.
        - `"retrieved"` – Piece has been retrieved by a client.
    - `indexed`: Whether the piece has been indexed and is ready for IPNI.
    - `advertised`: Whether an IPNI advertisement has been published.
    - `retrieved`: Whether the piece has been retrieved by a client.
    - `retrievedAt`: Timestamp of last retrieval (omitted if never retrieved).

#### Errors

- `400 Bad Request`: Invalid or missing `pieceCid`.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Piece not found or does not belong to this service.

---

### 3. Streaming Upload

The streaming upload API provides a way to upload large pieces in a streaming fashion without needing to know the pieceCid upfront. The workflow is:

1. Create a streaming upload session → get `uploadUUID`.
2. Stream the data via `PUT`.
3. Finalize the upload with the pieceCid to link and validate.

> **Note:** Each streaming upload chunk is limited to **1 GiB** (unpadded). The server computes the CommP on-the-fly.

#### 3.1. Create Streaming Upload Session

- **Endpoint:** `POST /pdp/piece/uploads`
- **Description:** Create a streaming upload session and receive an upload URL.
- **Authentication:** HybridAuth on init — see [Upload authentication gating](#upload-authentication-gating).
- **Request Body:** Empty, or JSON with optional/required `dataSetId`:

```json
{ "dataSetId": 12345 }
```

#### Response

- **Status Code:** `201 Created`
- **Headers:**
    - `Location`: The URL for streaming data (e.g., `/pdp/piece/uploads/{uploadUUID}`).

#### Errors

- `400 Bad Request`: Invalid JSON body, or missing `dataSetId` when gating is on.
- `401 Unauthorized`: Invalid JWT when `Authorization` is sent; anonymous caller when gating is on.
- `404 Not Found` / `409 Conflict`: Data set ownership failures (same as classic init).

---

#### 3.2. Stream Piece Data

- **Endpoint:** `PUT /pdp/piece/uploads/{uploadUUID}`
- **Description:** Stream raw piece bytes; server computes CommP incrementally.
- **Authentication:** HybridAuth (service label must match session row). When `PDPUploadRequireAuth=true`, **`X-PDP-Upload-Auth` is also required** before the body is read.
- **URL Parameters:**
    - `uploadUUID`: UUID from init `Location` header.
- **Request Body:** Raw bytes (chunked upload supported; 1 GiB unpadded max per session).
- **Headers:**
    - `Content-Type`: `application/octet-stream`
    - `Authorization`: Optional when gating off; required when gating on.
    - `X-PDP-Upload-Auth`: Required when gating is on.

#### Response

- **Status Code:** `204 No Content`

#### Errors

- `400 Bad Request`: Invalid UUID.
- `401 Unauthorized`: JWT / service mismatch, or missing/invalid `X-PDP-Upload-Auth` when gating is on.
- `404 Not Found`: Upload session not found for this service label.
- `413 Payload Too Large`: Data exceeds size limit.

---

#### 3.3. Finalize Streaming Upload

- **Endpoint:** `POST /pdp/piece/uploads/{uploadUUID}`
- **Description:** Finalize a streaming upload by providing the expected pieceCid. The server validates that the uploaded data matches the given pieceCid, then records the piece for use in data sets.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `uploadUUID`: The UUID from the upload session.
- **Request Body:**

```json
{
  "pieceCid": "<CommP-v2-CID>",
  "notify": "<optional-notification-URL>"
}
```

#### Response

- **Status Code:** `200 OK`

#### Errors

- `400 Bad Request`: Invalid pieceCid, size mismatch, or CID does not match the uploaded data.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Upload session not found.

---

### 4. Notifications

When you initiate an upload with the `notify` field specified, the PDP Service will send a notification to the provided URL once the piece has been successfully processed and stored.

#### 4.1. Notification Request

- **Method:** `POST`
- **URL:** The `notify` URL provided during the upload initiation (`POST /pdp/piece`).
- **Headers:**
    - `Content-Type`: `application/json`
- **Request Body:**

```json
{
  "id": "<upload-ID>",
  "service": "<service-name>",
  "pieceCID": "<piece-CID or null>",
  "notify_url": "<original-notify-URL>",
  "check_hash_codec": "<hash-function-name>",
  "check_hash": "<byte-array-of-hash>"
}
```

- **Fields:**
    - `id`: The upload ID.
    - `service`: The service name.
    - `pieceCID`: The Piece CID of the stored piece (may be `null` if not applicable).
    - `notify_url`: The original notification URL provided.
    - `check_hash_codec`: The hash function used (e.g., `"sha2-256-trunc254-padded"`).
    - `check_hash`: The byte array of the original hash provided in the upload initiation.

#### 4.2. Expected Response from Your Server

- **Status Code:** `200 OK` to acknowledge receipt.
- **Response Body:** (Optional) Can be empty or contain a message.

#### 4.3. Notes

- The PDP Service may retry the notification if it fails.
- Ensure that your server is accessible from the PDP Service and can handle incoming POST requests.
- The notification does not include the piece data; it confirms that the piece has been successfully stored.

---

### 5. Create a Data Set

- **Endpoint:** `POST /pdp/data-sets`
- **Description:** Create a new data set. This submits an on-chain transaction via the PDPVerifier smart contract.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **Request Body:**

```json
{
  "recordKeeper": "<Ethereum-address-of-record-keeper>",
  "extraData": "<optional-hex-encoded-extra-data>"
}
```

- **Fields:**
    - `recordKeeper`: The Ethereum address of the record keeper (required).
    - `extraData`: *(Optional)* Hex-encoded additional data to pass to the contract (max 4096 bytes decoded). Can include IPFS indexing metadata.

#### Response

- **Status Code:** `201 Created`
- **Headers:**
    - `Location`: The URL to check the status of the data set creation (e.g., `/pdp/data-sets/created/{txHash}`).

#### Errors

- `400 Bad Request`: Missing or invalid `recordKeeper` address, or `extraData` exceeds size limit.
- `401 Unauthorized`: Missing or invalid JWT token.
- `403 Forbidden`: `recordKeeper` address not allowed for this service.
- `500 Internal Server Error`: Failed to process the request.

---

### 6. Create a Data Set and Add Pieces (Atomic)

- **Endpoint:** `POST /pdp/data-sets/create-and-add`
- **Description:** Create a new data set and add pieces to it in a single on-chain transaction. This is the preferred approach when you already have the pieces stored.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **Request Body:**

```json
{
  "recordKeeper": "<Ethereum-address-of-record-keeper>",
  "pieces": [
    {
      "pieceCid": "<pieceCID>",
      "subPieces": [
        { "subPieceCid": "<subpieceCID1>" },
        { "subPieceCid": "<subpieceCID2>" }
      ]
    }
  ],
  "extraData": "<optional-hex-encoded-extra-data>"
}
```

- **Fields:**
    - `recordKeeper`: The Ethereum address of the record keeper (required).
    - `pieces`: An array of piece entries (same format as `POST /pdp/data-sets/{dataSetId}/pieces`). At most 40 pieces per call (larger batches would exceed on-chain event-size limits and are rejected with `400 Bad Request`).
    - `extraData`: *(Optional)* Hex-encoded additional data. If it contains `withIPFSIndexing` metadata, pieces will be marked for IPFS indexing.

#### Response

- **Status Code:** `201 Created`
- **Headers:**
    - `Location`: The URL to check the status of the data set creation (e.g., `/pdp/data-sets/created/{txHash}`).

#### Errors

- `400 Bad Request`: Invalid request body, validation errors, or more than 40 pieces in the batch.
- `401 Unauthorized`: Missing or invalid JWT token.
- `403 Forbidden`: `recordKeeper` address not allowed for this service.
- `500 Internal Server Error`: Failed to process the request.

---

### 7. Check Data Set Creation Status

- **Endpoint:** `GET /pdp/data-sets/created/{txHash}`
- **Description:** Retrieve the status of a data set creation (or create-and-add) transaction.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `txHash`: The transaction hash returned in the `Location` header when creating the data set.

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "createMessageHash": "<transaction-hash>",
  "dataSetCreated": <boolean>,
  "service": "<service-name>",
  "txStatus": "<transaction-status>",
  "ok": <null-or-boolean>,
  "dataSetId": <data-set-id-or-omitted>
}
```

- **Fields:**
    - `createMessageHash`: The transaction hash used to create the data set.
    - `dataSetCreated`: Whether the data set has been created (`true` or `false`).
    - `service`: The service name.
    - `txStatus`: The transaction status (`"pending"`, `"confirmed"`, etc.).
    - `ok`: `true` if the transaction was successful, `false` if it failed, or `null` if pending.
    - `dataSetId`: The ID of the created data set (only present when `dataSetCreated` is `true`).

#### Errors

- `400 Bad Request`: Missing or invalid `txHash`.
- `401 Unauthorized`: Missing or invalid JWT token, or service label mismatch.
- `404 Not Found`: Data set creation not found for the given `txHash`.

---

### 8. Get Data Set Details

- **Endpoint:** `GET /pdp/data-sets/{dataSetId}`
- **Description:** Retrieve the details of a data set, including its pieces and the next challenge epoch.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "id": <dataSetId>,
  "nextChallengeEpoch": <epoch-number>,
  "pieces": [
    {
      "pieceId": <pieceId>,
      "pieceCid": "<pieceCID-v2>",
      "subPieceCid": "<subpieceCID-v2>",
      "subPieceOffset": <subpieceOffset>
    }
  ]
}
```

- **Fields:**
    - `id`: The ID of the data set.
    - `nextChallengeEpoch`: The next epoch at which a proof of possession challenge must be answered. `0` means the data set has not yet been initialized on-chain.
    - `pieces`: An array of piece entries (excludes removed pieces by default).
        - `pieceId`: The ID of the piece.
        - `pieceCid`: The CID of the aggregate piece (v2 format).
        - `subPieceCid`: The CID of the sub-piece (v2 format).
        - `subPieceOffset`: The byte offset of the sub-piece within the aggregate piece.

#### Errors

- `400 Bad Request`: Missing or invalid `dataSetId`.
- `401 Unauthorized`: Missing or invalid JWT token, or data set does not belong to the service.
- `404 Not Found`: Data set not found.

---

### 9. Delete a Data Set *(Not yet implemented)*

- **Endpoint:** `DELETE /pdp/data-sets/{dataSetId}`
- **Description:** Remove the specified data set entirely.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.

#### Response

- **Status Code:** `501 Not Implemented`

---

### 10. Add Pieces to a Data Set

- **Endpoint:** `POST /pdp/data-sets/{dataSetId}/pieces`
- **Description:** Add pieces to a data set by submitting an on-chain transaction.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.
- **Request Body:**

```json
{
  "pieces": [
    {
      "pieceCid": "<pieceCID>",
      "subPieces": [
        {
          "subPieceCid": "<subpieceCID1>"
        },
        {
          "subPieceCid": "<subpieceCID2>"
        }
      ]
    }
  ],
  "extraData": "<optional-hex-data>"
}
```

- **Fields:**
    - `pieces`: An array of piece entries.
        - Each piece entry contains:
            - `pieceCid`: The aggregate piece CID (must be v2 format).
            - `subPieces`: An array of subPiece entries.
                - Each subPiece entry contains:
                    - `subPieceCid`: The CID of the subPiece (v1 or v2). Must be previously uploaded.
    - `extraData`: (Optional) Additional hex-encoded data for the transaction.

#### Constraints and Requirements

- **Batch Size:** At most 40 pieces may be added per call (larger batches would exceed on-chain event-size limits and are rejected with `400 Bad Request`).
- **SubPieces Ordering:** The `subPieces` must be provided in order **from largest to smallest size** (by padded size). This ensures correct Merkle tree computation.
- **SubPieces Ownership:** All subPieces must belong to the service making the request and have been previously uploaded.
- **SubPiece Sizes:** Each subPiece size must be at least 128 bytes.
- **Piece CID Verification:** The server computes the piece CID from the provided subPieces using `GenerateUnsealedCID` and verifies it matches the provided `pieceCid`.
- **Data Set Status:** The data set must not have an unrecoverable proving failure.

#### Response

- **Status Code:** `201 Created`
- **Headers:**
    - `Location`: URL to poll for addition status (e.g., `/pdp/data-sets/{dataSetId}/pieces/added/{txHash}`).

#### Errors

- `400 Bad Request`: Invalid request body, missing fields, validation errors, more than 40 pieces in the batch, subPieces not ordered correctly, or raw size mismatch.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Data set not found or subPieces not found.
- `409 Conflict`: Data set has been terminated due to unrecoverable proving failure.
- `500 Internal Server Error`: Failed to process the request.

---

### 11. Get Piece Addition Status

- **Endpoint:** `GET /pdp/data-sets/{dataSetId}/pieces/added/{txHash}`
- **Description:** Retrieve the status of a piece addition transaction.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.
    - `txHash`: The transaction hash returned in the `Location` header from `POST /pdp/data-sets/{dataSetId}/pieces`.

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "txHash": "<transaction-hash>",
  "txStatus": "<transaction-status>",
  "dataSetId": <dataSetId>,
  "pieceCount": <number-of-unique-pieces>,
  "addMessageOk": <null-or-boolean>,
  "piecesAdded": <boolean>,
  "confirmedPieceIds": [<pieceId>, ...]
}
```

- **Fields:**
    - `txHash`: The transaction hash.
    - `txStatus`: The transaction status (`"pending"`, `"confirmed"`, etc.).
    - `dataSetId`: The ID of the data set.
    - `pieceCount`: Number of unique pieces in this transaction.
    - `addMessageOk`: `true` if on-chain transaction succeeded, `false` if failed, `null` if pending.
    - `piecesAdded`: Whether the pieces have been fully processed and recorded.
    - `confirmedPieceIds`: Array of assigned piece IDs (only present when confirmed and successful).

#### Errors

- `400 Bad Request`: Invalid parameters.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Data set not found, or piece addition not found for given transaction.

---

### 12. Get Piece Details

- **Endpoint:** `GET /pdp/data-sets/{dataSetId}/pieces/{pieceId}`
- **Description:** Retrieve the details of a specific piece in a data set, including its sub-pieces.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.
    - `pieceId`: The ID of the piece.

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "pieceId": <pieceId>,
  "pieceCid": "<pieceCID>",
  "subPieces": [
    {
      "subPieceCid": "<subPieceCID>",
      "subPieceOffset": <offset>
    }
  ]
}
```

- **Fields:**
    - `pieceId`: The ID of the piece.
    - `pieceCid`: The CID of the aggregate piece.
    - `subPieces`: Ordered list of sub-pieces (by offset).
        - `subPieceCid`: The CID of the sub-piece.
        - `subPieceOffset`: The byte offset of the sub-piece.

#### Errors

- `400 Bad Request`: Invalid request.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Data set or piece not found.

---

### 13. Delete a Piece from a Data Set

- **Endpoint:** `DELETE /pdp/data-sets/{dataSetId}/pieces/{pieceId}`
- **Description:** Schedule a piece for deletion from a data set by submitting an on-chain `schedulePieceDeletions` transaction to the PDPVerifier contract. Deletion is asynchronous.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.
    - `pieceId`: The ID of the piece.
- **Request Body:** *(Optional)*

```json
{
  "extraData": "<optional-hex-encoded-extra-data>"
}
```

- **Fields:**
    - `extraData`: *(Optional)* Hex-encoded additional data for the contract call (max 256 bytes decoded).

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "txHash": "<transaction-hash>"
}
```

- **Fields:**
    - `txHash`: The hash of the on-chain `schedulePieceDeletions` transaction.

#### Errors

- `400 Bad Request`: Invalid request or `extraData` exceeds size limit.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Data set or piece not found.
- `500 Internal Server Error`: Failed to send on-chain transaction.

---

### 14. Pull Piece from Another SP

- **Endpoint:** `POST /pdp/piece/pull`
- **Description:** Request that the PDP service pull a piece from another storage provider or a remote URL. If pieces are successfully downloaded, they can later be added to a dataset via the contract. This request is idempotent when calling with the same `extraData`, `dataSetId`, and `recordKeeper`.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **Request Body:**

```json
{
  "extraData": "<hex-encoded-string-for-auth>",
  "dataSetId": 123,
  "recordKeeper": "<Ethereum-address-of-record-keeper>",
  "pieces": [
    {
      "pieceCid": "<CommP-v2-CID>",
      "sourceUrl": "https://example.com/piece/bafy..."
    }
  ]
}
```

- **Fields:**
    - `extraData`: *(Required)* Hex-encoded bytes that will be validated against the PDPVerifier contract via `eth_call`. Used for authorization and idempotency.
    - `dataSetId`: *(Optional)* The target dataset ID. If omitted or `0`, validation simulates creating a new dataset.
    - `recordKeeper`: *(Required if dataSetId is 0 or omitted)* The contract address that will receive callbacks.
    - `pieces`: Array of pieces to pull. At most 40 entries, since the pull is validated as an `addPieces` batch (larger batches would exceed on-chain event-size limits and are rejected with `400 Bad Request`).
        - `pieceCid`: The piece CID in CommP v2 format.
        - `sourceUrl`: HTTPS URL ending in `/piece/{pieceCid}` on a public host. Localhost and private IPs are blocked for security.

#### Response

Returns JSON with an overall status and per-piece status:

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "status": "inProgress",
  "pieces": [
    {
      "pieceCid": "<piece-CID-v2>",
      "status": "complete"
    },
    {
      "pieceCid": "<piece-CID-v2>",
      "status": "inProgress"
    }
  ]
}
```

- **Status Values:**
    - `"pending"`: Piece is queued but download hasn't started.
    - `"inProgress"`: Download task is actively running.
    - `"retrying"`: Download task is running after one or more failures.
    - `"complete"`: Piece successfully downloaded and verified.
    - `"failed"`: Piece permanently failed after exhausting retries.

#### Errors

- `400 Bad Request`: Validation error, missing parameters, more than 40 pieces in the batch, invalid pieceCid format, or invalid `sourceUrl`.
- `401 Unauthorized`: Missing or invalid JWT token.
- `403 Forbidden`: `recordKeeper` is not allowed.
- `500 Internal Server Error`: Failed to query or store pull task.

---

## Authentication

All endpoints require authentication using a JWT (JSON Web Token). The token should be provided in the `Authorization` header as follows:

```
Authorization: Bearer <JWT-token>
```

### Token Contents

The JWT token should be signed using **ECDSA** (`ES256`) or **EdDSA** (`Ed25519`) and contain the following claim:

- `service_name`: The name of the service (string). This acts as the service identifier.

### Token Verification Process

The server verifies the JWT token as follows:

1. **Extracting the Token:**
    - The server reads the `Authorization` header and extracts the token following the `Bearer` prefix.

2. **Parsing and Validating the Token:**
    - The token is parsed and validated using `ES256` (ECDSA) or `Ed25519` (EdDSA) signing methods.
    - The `service_name` claim is extracted from the token.

3. **Retrieving the Public Key:**
    - The server queries the `pdp_services` table for the public key associated with the `service_name`.
    - **Note:** The PDP Service should provide the PDP provider (SP) with the `service_name` they should use and the corresponding public key.

4. **Verifying the Signature:**
    - The public key is used to verify the token's signature.
    - If verification succeeds and the token is valid (not expired), the request is authorized.

### Error Responses

- **Missing Authorization Header:**
    - **Status Code:** `401 Unauthorized`
    - **Message:** `missing Authorization header`

- **Invalid Token Format:**
    - **Status Code:** `401 Unauthorized`
    - **Message:** `invalid Authorization header format` or `empty token`

- **Invalid Signing Method:**
    - **Status Code:** `401 Unauthorized`
    - **Message:** `unexpected signing method`

- **Missing or Invalid Claims:**
    - **Status Code:** `401 Unauthorized`
    - **Message:** `invalid token claims` or `missing service_name claim`

- **Public Key Retrieval Failure:**
    - **Status Code:** `401 Unauthorized`
    - **Message:** `failed to retrieve public key for service_id <name>`

- **Signature Verification Failure:**
    - **Status Code:** `401 Unauthorized`
    - **Message:** `invalid token`

---

## Piece CID Computation from SubPieces

When adding pieces to a data set using the `POST /pdp/data-sets/{dataSetId}/pieces` endpoint, the server performs validation and computation of the piece CID from the provided subPieces.

### Piece Computation Process

1. **Validating SubPieces:**
    - Ensure that all subPieces are owned by the requesting service.
    - The subPieces must have been previously uploaded and stored on the server.

2. **Ordering SubPieces:**
    - **Important:** SubPieces must be ordered from **largest to smallest padded size**.
    - This ordering ensures that no padding is required between the subPieces, aligning them correctly for piece computation.

3. **Piece Sizes and Alignment:**
    - Each subPiece corresponds to a data segment with a padded size that is a power of two (e.g., 128 bytes, 256 bytes, 512 bytes).
    - The concatenation of the subPieces must not require padding to align correctly.

4. **Computing the Piece CID:**
    - The server uses the `https://github.com/filecoin-project/go-commp-utils/v2 PieceAggregateCommp`  function to compute the piece CID from the subPieces.
    - This function emulates the computation performed in the Filecoin proofs implementation.
    - The process involves stacking the subPieces and combining them using a Merkle tree hash function.

5. **Validation of Computed Piece CID:**
    - The computed piece CID is compared with the `pieceCid` provided in the request.
    - The raw size encoded in the provided pieceCid (v2) must match the sum of all subPiece raw sizes.
    - If there is any mismatch, the request is rejected.

### Constraints and Requirements

- **SubPieces Ownership:** All subPiece CIDs must belong to the requesting service.
- **SubPieces Existence:** All subPiece CIDs must be valid and previously stored on the server.
- **Ordering of SubPieces:** Must be ordered from largest to smallest (descending padded size). No subPiece can be larger than the preceding one.
- **SubPiece Sizes:** Each subPiece padded size must be a power of two and at least 128 bytes.
- **Raw Size Consistency:** Sum of subPiece raw sizes must equal the raw size encoded in the aggregate `pieceCid` (v2).

### Error Responses

- **Invalid SubPiece Order:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `subPieces must be in descending order of size, piece <N> <CID> is larger than prev subPiece <CID>`

- **SubPiece Not Found or Unauthorized:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `subPiece CID <CID> not found or does not belong to service <name>`

- **Piece CID Mismatch:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `provided PieceCid does not match generated PieceCid: <provided> != <generated>`

- **Raw Size Mismatch:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `raw size mismatch: expected <N>, got <M>`

### Piece Computation Function

```go
func PieceAggregateCommp(proofType abi.RegisteredSealProof, pieceInfos []abi.PieceInfo) (cid.Cid, error)
```

Where:

- `pieceInfos` is a list of pieces (subPieces) with their padded sizes and CIDs.
- The function builds a CommP tree from the subPieces, combining them correctly according to their sizes and alignment.

---

## Data Models

### PieceCid V2 Format

The PDP API uses CommP **v2** CIDs in external-facing fields. A CommP v2 CID encodes both the commitment hash and the raw (unpadded) piece size:

- **Codec:** `fil-commitment-unsealed` (0xf101)
- **Multihash:** `sha2-256-trunc254-padded` with the raw size appended
- **Version:** CIDv1

This allows the recipient to know both the content hash and the raw size from the CID alone.

### PieceEntry

Represents a piece entry in a data set.

```json
{
  "pieceId": <pieceID>,
  "pieceCid": "<pieceCID-v2>",
  "subPieceCid": "<subPieceCID-v2>",
  "subPieceOffset": <subPieceOffset>
}
```

- **Fields:**
    - `pieceId`: The ID of the piece.
    - `pieceCid`: The CID of the aggregate piece (v2 format).
    - `subPieceCid`: The CID of the subPiece (v2 format).
    - `subPieceOffset`: The byte offset of the subPiece within the aggregate piece.

---

## Common Errors

- **400 Bad Request:** The request is invalid or missing required parameters.
- **401 Unauthorized:** Missing or invalid JWT token.
- **403 Forbidden:** The operation is not permitted (e.g., recordKeeper not whitelisted for public service).
- **404 Not Found:** The requested resource was not found.
- **409 Conflict:** The request could not be completed due to a conflict with the current state of the resource.
- **413 Payload Too Large:** The uploaded data exceeds the maximum allowed size.
- **500 Internal Server Error:** An unexpected error occurred on the server.
- **501 Not Implemented:** The endpoint exists in the routing but is not yet implemented.

Error responses typically include an error message in the response body.

---

## Example Usage

### Uploading a Piece (Classic)

1. **Initiate Upload:**

   **Request:**

   ```http
   POST /pdp/piece HTTP/1.1
   Host: example.com
   Authorization: Bearer <JWT-token>
   Content-Type: application/json

   {
     "pieceCid": "<CommP-v2-CID>",
     "notify": "https://example.com/notify"
   }
   ```

   **Possible Responses:**

    - **Piece Already Exists:**

      ```http
      HTTP/1.1 200 OK
      Content-Type: application/json
 
      {
        "pieceCid": "<piece-CID-v2>"
      }
      ```

    - **Upload Required:**

      ```http
      HTTP/1.1 201 Created
      Location: /pdp/piece/upload/{uploadUUID}
      ```

2. **Upload Piece Data:**

   **Request:**

   ```http
   PUT /pdp/piece/upload/{uploadUUID} HTTP/1.1
   Host: example.com
   Content-Type: application/octet-stream
   Content-Length: 12345

   <binary piece data>
   ```

   **Response:**

   ```http
   HTTP/1.1 204 No Content
   ```

3. **Receive Notification (if `notify` was provided):**

   **Server's Notification Request:**

   ```http
   POST /notify HTTP/1.1
   Host: example.com
   Content-Type: application/json

   {
     "id": "<upload-ID>",
     "service": "<service-name>",
     "pieceCID": "<piece-CID>",
     "notify_url": "https://example.com/notify",
     "check_hash_codec": "sha2-256-trunc254-padded",
     "check_hash": "<b64-byte-array-of-hash>"
   }
   ```

   **Your Response:**

   ```http
   HTTP/1.1 200 OK
   ```

### Uploading a Piece (Streaming)

1. **Create Upload Session:**

   ```http
   POST /pdp/piece/uploads HTTP/1.1
   Host: example.com
   Authorization: Bearer <JWT-token>

   HTTP/1.1 201 Created
   Location: /pdp/piece/uploads/{uploadUUID}
   ```

2. **Stream Data:**

   ```http
   PUT /pdp/piece/uploads/{uploadUUID} HTTP/1.1
   Host: example.com
   Authorization: Bearer <JWT-token>
   Content-Type: application/octet-stream

   <binary piece data>

   HTTP/1.1 204 No Content
   ```

3. **Finalize Upload:**

   ```http
   POST /pdp/piece/uploads/{uploadUUID} HTTP/1.1
   Host: example.com
   Authorization: Bearer <JWT-token>
   Content-Type: application/json

   {
     "pieceCid": "<CommP-v2-CID>"
   }

   HTTP/1.1 200 OK
   ```

### Creating a Data Set

**Request:**

```http
POST /pdp/data-sets HTTP/1.1
Host: example.com
Authorization: Bearer <JWT-token>
Content-Type: application/json

{
  "recordKeeper": "0x1234567890abcdef..."
}
```

**Response:**

```http
HTTP/1.1 201 Created
Location: /pdp/data-sets/created/0xabc123...
```

### Creating a Data Set and Adding Pieces (Atomic)

**Request:**

```http
POST /pdp/data-sets/create-and-add HTTP/1.1
Host: example.com
Authorization: Bearer <JWT-token>
Content-Type: application/json

{
  "recordKeeper": "0x1234567890abcdef...",
  "pieces": [
    {
      "pieceCid": "<pieceCID>",
      "subPieces": [
        { "subPieceCid": "<subPieceCID1>" },
        { "subPieceCid": "<subPieceCID2>" }
      ]
    }
  ]
}
```

**Response:**

```http
HTTP/1.1 201 Created
Location: /pdp/data-sets/created/0xabc123...
```

### Adding Pieces to a Data Set

**Request:**

```http
POST /pdp/data-sets/{dataSetId}/pieces HTTP/1.1
Host: example.com
Authorization: Bearer <JWT-token>
Content-Type: application/json

{
  "pieces": [
    {
      "pieceCid": "<pieceCID>",
      "subPieces": [
        { "subPieceCid": "<subPieceCID1>" },
        { "subPieceCid": "<subPieceCID2>" },
        { "subPieceCid": "<subPieceCID3>" }
      ]
    }
  ]
}
```

**Response:**

```http
HTTP/1.1 201 Created
Location: /pdp/data-sets/{dataSetId}/pieces/added/0xabc123...
```

### Deleting a Piece

**Request:**

```http
DELETE /pdp/data-sets/{dataSetId}/pieces/{pieceId} HTTP/1.1
Host: example.com
Authorization: Bearer <JWT-token>
Content-Type: application/json

{}
```

**Response:**

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "txHash": "0xabc123..."
}
```
