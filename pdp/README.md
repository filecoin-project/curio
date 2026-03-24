# PDP Service API Documentation

## Base URL

All endpoints are rooted at `/pdp`.

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
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **Request Body:**

```json
{
  "pieceCid": "<CommP-v2-CID>",
  "notify": "<optional-notification-URL>"
}
```

- **Fields:**
    - `pieceCid`: The Piece CID in CommP **v2** format (CIDv1 with `fil-commitment-unsealed` codec and raw size encoded). This uniquely identifies the piece and encodes size information.
    - `notify`: *(Optional)* A URL to be notified when the piece has been processed successfully.

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

- `400 Bad Request`: Invalid pieceCid format or piece size exceeds the maximum allowed size.
- `401 Unauthorized`: Missing or invalid JWT token.

---

#### 2.2. Upload Piece Data (Classic)

- **Endpoint:** `PUT /pdp/piece/upload/{uploadUUID}`
- **Description:** Upload the actual bytes of the piece to the server using the provided `uploadUUID` from the `Location` header of `POST /pdp/piece`.
- **URL Parameters:**
    - `uploadUUID`: The UUID provided in the `Location` header from the previous `POST /pdp/piece` request.
- **Request Body:** The raw bytes of the piece data.
- **Headers:**
    - `Content-Length`: The size of the piece.
    - `Content-Type`: `application/octet-stream`.

#### Response

- **Status Code:** `204 No Content`

#### Errors

- `400 Bad Request`: Piece size does not match the expected size or computed hash does not match the expected hash.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: The provided `uploadUUID` is not found.
- `409 Conflict`: Data has already been uploaded for this `uploadUUID`.
- `413 Payload Too Large`: Piece data exceeds the maximum allowed size.

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
- **Description:** Create a new streaming upload session and get an upload URL.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **Request Body:** Empty.

#### Response

- **Status Code:** `201 Created`
- **Headers:**
    - `Location`: The URL for streaming data (e.g., `/pdp/piece/uploads/{uploadUUID}`).

#### Errors

- `401 Unauthorized`: Missing or invalid JWT token.

---

#### 3.2. Stream Piece Data

- **Endpoint:** `PUT /pdp/piece/uploads/{uploadUUID}`
- **Description:** Stream raw piece bytes to the server. The server computes the CommP hash incrementally.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `uploadUUID`: The UUID from the `Location` header of `POST /pdp/piece/uploads`.
- **Request Body:** Raw bytes of the piece data.

#### Response

- **Status Code:** `204 No Content`

#### Errors

- `400 Bad Request`: Invalid UUID.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Upload session not found.
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
    - `pieces`: An array of piece entries (same format as `POST /pdp/data-sets/{dataSetId}/pieces`).
    - `extraData`: *(Optional)* Hex-encoded additional data (max 8192 bytes decoded). If it contains `withIPFSIndexing` metadata, pieces will be marked for IPFS indexing.

#### Response

- **Status Code:** `201 Created`
- **Headers:**
    - `Location`: The URL to check the status of the data set creation (e.g., `/pdp/data-sets/created/{txHash}`).

#### Errors

- `400 Bad Request`: Invalid request body, validation errors, or size limit exceeded.
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
    - `extraData`: (Optional) Additional hex-encoded data for the transaction (max 8192 bytes decoded).

#### Constraints and Requirements

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

- `400 Bad Request`: Invalid request body, missing fields, validation errors, subPieces not ordered correctly, or raw size mismatch.
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
    - `pieces`: Array of pieces to pull.
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

- `400 Bad Request`: Validation error, missing parameters, invalid pieceCid format, or invalid `sourceUrl`.
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
    - The server uses the `GenerateUnsealedCID` function to compute the piece CID from the subPieces.
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
func GenerateUnsealedCID(proofType abi.RegisteredSealProof, pieceInfos []abi.PieceInfo) (cid.Cid, error)
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
