# PDP Service API Documentation

## Base URL

All endpoints are rooted at `/pdp`.

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
- **Description:** Initiate the process of uploading a piece. If the piece already exists on the server, the server will respond accordingly.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **Request Body:**

```json
{
  "check": {
    "name": "<hash-function-name>",
    "hash": "<hex-encoded-hash>",
    "size": <size-in-bytes>
  },
  "notify": "<optional-notification-URL>"
}
```

- **Fields:**
    - `check`: An object containing the hash details of the piece.
        - `name`: The name of the hash function used:
            - `"sha2-256"` for SHA-256 of the raw piece data.
            - `"sha2-256-trunc254-padded"` for the CommP (Piece Commitment).
        - `hash`: The hex-encoded hash value (multihash payload, not the full multihash)
        - `size`: The size of the piece in bytes (unpadded size).
    - `notify`: *(Optional)* A URL to be notified when the piece has been processed successfully.

#### Responses

1. **Piece Already Exists**

    - **Status Code:** `200 OK`
    - **Response Body:**

      ```json
      {
        "pieceCID": "<piece-CID>"
      }
      ```

2. **Piece Does Not Exist (Upload Required)**

    - **Status Code:** `201 Created`
    - **Headers:**
        - `Location`: The URL where the piece data can be uploaded via `PUT`.

#### Errors

- `400 Bad Request`: Invalid request body or piece size exceeds the maximum allowed size.
- `401 Unauthorized`: Missing or invalid JWT token.

---

#### 2.2. Upload Piece Data

- **Endpoint:** `PUT /pdp/piece/upload/{uploadUUID}`
- **Description:** Upload the actual bytes of the piece to the server using the provided `uploadUUID`.
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

### 3. Notifications

When you initiate an upload with the `notify` field specified, the PDP Service will send a notification to the provided URL once the piece has been successfully processed and stored.

#### 3.1. Notification Request

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
    - `check_hash_codec`: The hash function used (e.g., `"sha2-256"` or `"sha2-256-trunc254-padded"`).
    - `check_hash`: The byte array of the original hash provided in the upload initiation.

#### 3.2. Expected Response from Your Server

- **Status Code:** `200 OK` to acknowledge receipt.
- **Response Body:** (Optional) Can be empty or contain a message.

#### 3.3. Notes

- The PDP Service may retry the notification if it fails.
- Ensure that your server is accessible from the PDP Service and can handle incoming POST requests.
- The notification does not include the piece data; it confirms that the piece has been successfully stored.

---

### 4. Create a Data Set

- **Endpoint:** `POST /pdp/data-sets`
- **Description:** Create a new data set.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **Request Body:**

```json
{
  "recordKeeper": "<Ethereum-address-of-record-keeper>"
}
```

- **Fields:**
    - `recordKeeper`: The Ethereum address of the record keeper.

#### Response

- **Status Code:** `201 Created`
- **Headers:**
    - `Location`: The URL to check the status of the data set creation.

#### Errors

- `400 Bad Request`: Missing or invalid `recordKeeper` address.
- `401 Unauthorized`: Missing or invalid JWT token.
- `500 Internal Server Error`: Failed to process the request.

---

### 5. Check Data Set Creation Status

- **Endpoint:** `GET /pdp/data-sets/created/{txHash}`
- **Description:** Retrieve the status of a data set creation.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `txHash`: The transaction hash returned when creating the data set.

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
  "dataSetId": <data-set-id-or-null>
}
```

- **Fields:**
    - `createMessageHash`: The transaction hash used to create the data set.
    - `dataSetCreated`: Whether the data set has been created (`true` or `false`).
    - `service`: The service name.
    - `txStatus`: The transaction status (`"pending"`, `"confirmed"`, etc.).
    - `ok`: `true` if the transaction was successful, `false` if it failed, or `null` if pending.
    - `dataSetId`: The ID of the created data set, if available.

#### Errors

- `400 Bad Request`: Missing or invalid `txHash`.
- `401 Unauthorized`: Missing or invalid JWT token, or service label mismatch.
- `404 Not Found`: Data set creation not found for the given `txHash`.

---

### 6. Get Data Set Details

- **Endpoint:** `GET /pdp/data-sets/{dataSetId}`
- **Description:** Retrieve the details of a data set, including its pieces.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "id": <dataSetId>,
  "pieces": [
    {
      "pieceId": <pieceId>,
      "pieceCid": "<pieceCID>",
      "subPieceCid": "<subpieceCID>",
      "subPieceOffset": <subpieceOffset>
    },
    // ...
  ]
}
```

- **Fields:**
    - `id`: The ID of the data set.
    - `pieces`: An array of piece entries.
        - `pieceId`: The ID of the piece.
        - `pieceCid`: The CID of the piece.
        - `subPieceCid`: The CID of the subPiece.
        - `subPieceOffset`: The offset of the subPiece.

#### Errors

- `400 Bad Request`: Missing or invalid `dataSetId`.
- `401 Unauthorized`: Missing or invalid JWT token, or data set does not belong to the service.
- `404 Not Found`: Data set not found.

---

### 7. Delete a Data Set *(To be implemented)*

- **Endpoint:** `DELETE /pdp/data-sets/{dataSetId}`
- **Description:** Remove the specified data set entirely.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.

#### Response

- **Status Code:** `204 No Content`

#### Errors

- `400 Bad Request`: Invalid request.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Data set not found.

---

### 8. Add Pieces to a Data Set

- **Endpoint:** `POST /pdp/data-sets/{dataSetId}/pieces`
- **Description:** Add pieces to a data set.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.
- **Request Body:** An object containing an array of piece entries.

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
        },
        // ...
      ]
    },
    // ...
  ],
  "extraData": "<optional-hex-data>"
}
```

- **Fields:**
    - `pieces`: An array of piece entries.
        - Each piece entry contains:
            - `pieceCid`: The piece CID.
            - `subPieces`: An array of subPiece entries.
                - Each subPiece entry contains:
                    - `subPieceCid`: The CID of the subPiece.
    - `extraData`: (Optional) Additional hex-encoded data for the transaction.

#### Constraints and Requirements

- **SubPieces Ordering:** The `subPieces` must be provided in order **from largest to smallest size**. This ensures that no padding is required between subPieces during the computation of the piece CID.
- **SubPieces Ownership:** All subPieces must belong to the service making the request, and they must be previously uploaded and stored on the PDP service.
- **SubPiece Sizes:**
    - Each subPiece size must be at least 128 bytes.
- **SubPiece Alignment:** The subPieces are concatenated without padding. Proper ordering ensures that the concatenated data aligns correctly for piece computation.
- **Piece CID Verification:**
    - The provider computes the piece CID from the provided subPieces and verifies that it matches the `pieceCid` specified in the request.
    - If the computed piece CID does not match, the request is rejected.

#### Response

- **Status Code:** `201 Created`

#### Errors

- `400 Bad Request`: Invalid request body, missing fields, validation errors, or subPieces not ordered correctly.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Data set not found or subPieces not found.
- `500 Internal Server Error`: Failed to process the request.

---

### 9. Get Piece Details *(To be implemented)*

- **Endpoint:** `GET /pdp/data-sets/{dataSetId}/pieces/{pieceId}`
- **Description:** Retrieve the details of a piece in a data set.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.
    - `pieceId`: The ID of the piece.

#### Response

- **Status Code:** `200 OK`
- **Response Body:** Piece details (to be defined).

#### Errors

- `400 Bad Request`: Invalid request.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Data set or piece not found.

---

### 10. Delete a Piece from a Data Set *(To be implemented)*

- **Endpoint:** `DELETE /pdp/data-sets/{dataSetId}/pieces/{pieceId}`
- **Description:** Remove a piece from a data set.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `dataSetId`: The ID of the data set.
    - `pieceId`: The ID of the piece.

#### Response

- **Status Code:** `204 No Content`

#### Errors

- `400 Bad Request`: Invalid request.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Data set or piece not found.

---

## Authentication

All endpoints require authentication using a JWT (JSON Web Token). The token should be provided in the `Authorization` header as follows:

```
Authorization: Bearer <JWT-token>
```

### Token Contents

The JWT token should be signed using ECDSA with the `ES256` algorithm and contain the following claim:

- `service_name`: The name of the service (string). This acts as the service identifier.

### Token Verification Process

The server verifies the JWT token as follows:

1. **Extracting the Token:**
    - The server reads the `Authorization` header and extracts the token following the `Bearer` prefix.

2. **Parsing and Validating the Token:**
    - The token is parsed and validated using the `ES256` signing method.
    - The `service_name` claim is extracted from the token.

3. **Retrieving the Public Key:**
    - The server retrieves the public key associated with the `service_name` from its database or configuration.
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
    - **Message:** `failed to retrieve public key for service_name`

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
    - **Important:** SubPieces must be ordered from **largest to smallest size**.
    - This ordering ensures that no padding is required between the subPieces, aligning them correctly for piece computation.

3. **Piece Sizes and Alignment:**
    - Each subPiece corresponds to a data segment with a size that is a power of two (e.g., 128 bytes, 256 bytes, 512 bytes).
    - The concatenation of the subPieces must not require padding to align to the sector size used in the computation.

4. **Computing the Piece CID:**
    - The server uses the `GenerateUnsealedCID` function to compute the piece CID from the subPieces.
    - This function emulates the computation performed in the Filecoin proofs implementation.
    - The process involves stacking the subPieces and combining them using a Merkle tree hash function.

5. **Validation of Computed Piece CID:**
    - The computed piece CID is compared with the `pieceCid` provided in the request.
    - If the computed piece CID does not match the provided `pieceCid`, the request is rejected with an error.

### Constraints and Requirements

- **SubPieces Ownership:** All subPiece CIDs must belong to the requesting service.
- **SubPieces Existence:** All subPiece CIDs must be valid and previously stored on the server.
- **Ordering of SubPieces:** Must be ordered from largest to smallest. The sizes must be decreasing or equal; no subPiece can be larger than the preceding one.
- **SubPiece Sizes:** Each subPiece size must be a power of two and at least 128 bytes.
- **Total Size Limit:** The total size of the concatenated subPieces must not exceed the maximum allowed sector size.

### Error Responses

- **Invalid SubPiece Order:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `SubPieces must be in descending order of size`

- **SubPiece Not Found or Unauthorized:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `subPiece CID <CID> not found or does not belong to service`

- **Piece CID Mismatch:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `provided PieceCID does not match generated PieceCID`

### Piece Computation Function

```go
func GenerateUnsealedCID(proofType abi.RegisteredSealProof, pieceInfos []abi.PieceInfo) (cid.Cid, error)
```

Where:

- `pieceInfos` is a list of pieces (subPieces) with their sizes and CIDs.
- The function builds a CommP tree from the subPieces, combining them correctly according to their sizes and alignment.

---

## Data Models

### PieceHash

Represents hash information about a piece.

```json
{
  "name": "<hash-function-name>",
  "hash": "<hex-encoded-hash>",
  "size": <size-in-bytes>
}
```

- **Fields:**
    - `name`: Name of the hash function used (e.g., `"sha2-256"`, `"sha2-256-trunc254-padded"`).
    - `hash`: Hex-encoded hash value.
    - `size`: Size of the piece in bytes.

### PieceEntry

Represents a piece entry in a data set.

```json
{
  "pieceId": <pieceID>,
  "pieceCid": "<pieceCID>",
  "subPieceCid": "<subPieceCID>",
  "subPieceOffset": <subPieceOffset>
}
```

- **Fields:**
    - `pieceId`: The ID of the piece.
    - `pieceCid`: The CID of the piece.
    - `subPieceCid`: The CID of the subPiece.
    - `subPieceOffset`: The offset of the subPiece.

---

## Common Errors

- **400 Bad Request:** The request is invalid or missing required parameters.
- **401 Unauthorized:** Missing or invalid JWT token.
- **404 Not Found:** The requested resource was not found.
- **409 Conflict:** The request could not be completed due to a conflict with the current state of the resource.
- **413 Payload Too Large:** The uploaded data exceeds the maximum allowed size.
- **500 Internal Server Error:** An unexpected error occurred on the server.

Error responses typically include an error message in the response body.

---

## Example Usage

### Uploading a Piece

1. **Initiate Upload:**

   **Request:**

   ```http
   POST /pdp/piece HTTP/1.1
   Host: example.com
   Authorization: Bearer <JWT-token>
   Content-Type: application/json

   {
     "check": {
       "name": "sha2-256",
       "hash": "<hex-encoded-sha256-hash>",
       "size": 12345
     },
     "notify": "https://example.com/notify"
   }
   ```

   **Possible Responses:**

    - **Piece Already Exists:**

      ```http
      HTTP/1.1 200 OK
      Content-Type: application/json
 
      {
        "pieceCID": "<piece-CID>"
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
     "check_hash_codec": "sha2-256",
     "check_hash": "<b64-byte-array-of-hash>"
   }
   ```

   **Your Response:**

   ```http
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
    },
    // ... Additional pieces if needed
  ]
}
```

**Response:**

```http
HTTP/1.1 201 Created
```
