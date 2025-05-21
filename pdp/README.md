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

### 4. Create a Proof Set

- **Endpoint:** `POST /pdp/proof-sets`
- **Description:** Create a new proof set.
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
    - `Location`: The URL to check the status of the proof set creation.

#### Errors

- `400 Bad Request`: Missing or invalid `recordKeeper` address.
- `401 Unauthorized`: Missing or invalid JWT token.
- `500 Internal Server Error`: Failed to process the request.

---

### 5. Check Proof Set Creation Status

- **Endpoint:** `GET /pdp/proof-sets/created/{txHash}`
- **Description:** Retrieve the status of a proof set creation.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `txHash`: The transaction hash returned when creating the proof set.

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "createMessageHash": "<transaction-hash>",
  "proofsetCreated": <boolean>,
  "service": "<service-name>",
  "txStatus": "<transaction-status>",
  "ok": <null-or-boolean>,
  "proofSetId": <proof-set-id-or-null>
}
```

- **Fields:**
    - `createMessageHash`: The transaction hash used to create the proof set.
    - `proofsetCreated`: Whether the proof set has been created (`true` or `false`).
    - `service`: The service name.
    - `txStatus`: The transaction status (`"pending"`, `"confirmed"`, etc.).
    - `ok`: `true` if the transaction was successful, `false` if it failed, or `null` if pending.
    - `proofSetId`: The ID of the created proof set, if available.

#### Errors

- `400 Bad Request`: Missing or invalid `txHash`.
- `401 Unauthorized`: Missing or invalid JWT token, or service label mismatch.
- `404 Not Found`: Proof set creation not found for the given `txHash`.

---

### 6. Get Proof Set Details

- **Endpoint:** `GET /pdp/proof-sets/{proofSetID}`
- **Description:** Retrieve the details of a proof set, including its roots.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `proofSetID`: The ID of the proof set.

#### Response

- **Status Code:** `200 OK`
- **Response Body:**

```json
{
  "id": <proofSetID>,
  "roots": [
    {
      "rootId": <rootID>,
      "rootCid": "<rootCID>",
      "subrootCid": "<subrootCID>",
      "subrootOffset": <subrootOffset>
    },
    // ...
  ]
}
```

- **Fields:**
    - `id`: The ID of the proof set.
    - `roots`: An array of root entries.
        - `rootId`: The ID of the root.
        - `rootCid`: The CID of the root.
        - `subrootCid`: The CID of the subroot.
        - `subrootOffset`: The offset of the subroot.

#### Errors

- `400 Bad Request`: Missing or invalid `proofSetID`.
- `401 Unauthorized`: Missing or invalid JWT token, or proof set does not belong to the service.
- `404 Not Found`: Proof set not found.

---

### 7. Delete a Proof Set *(To be implemented)*

- **Endpoint:** `DELETE /pdp/proof-sets/{proofSetID}`
- **Description:** Remove the specified proof set entirely.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `proofSetID`: The ID of the proof set.

#### Response

- **Status Code:** `204 No Content`

#### Errors

- `400 Bad Request`: Invalid request.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Proof set not found.

---

### 8. Add Roots to a Proof Set

- **Endpoint:** `POST /pdp/proof-sets/{proofSetID}/roots`
- **Description:** Add roots to a proof set.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `proofSetID`: The ID of the proof set.
- **Request Body:** An array of root entries.

```json
[
  {
    "rootCid": "<rootCID>",
    "subroots": [
      {
        "subrootCid": "<subrootCID1>"
      },
      {
        "subrootCid": "<subrootCID2>"
      },
      // ...
    ]
  },
  // ...
]
```

- **Fields:**
    - Each root entry contains:
        - `rootCid`: The root CID.
        - `subroots`: An array of subroot entries.
            - Each subroot entry contains:
                - `subrootCid`: The CID of the subroot.

#### Constraints and Requirements

- **Subroots Ordering:** The `subroots` must be provided in order **from largest to smallest size**. This ensures that no padding is required between subroots during the computation of the root CID.
- **Subroots Ownership:** All subroots must belong to the service making the request, and they must be previously uploaded and stored on the PDP service.
- **Subroot Sizes:**
    - Each subroot size must be at least 128 bytes.
- **Subroot Alignment:** The subroots are concatenated without padding. Proper ordering ensures that the concatenated data aligns correctly for root computation.
- **Root CID Verification:**
    - The provider computes the root CID from the provided subroots and verifies that it matches the `rootCid` specified in the request.
    - If the computed root CID does not match, the request is rejected.

#### Response

- **Status Code:** `201 Created`

#### Errors

- `400 Bad Request`: Invalid request body, missing fields, validation errors, or subroots not ordered correctly.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Proof set not found or subroots not found.
- `500 Internal Server Error`: Failed to process the request.

---

### 9. Get Proof Set Root Details *(To be implemented)*

- **Endpoint:** `GET /pdp/proof-sets/{proofSetID}/roots/{rootID}`
- **Description:** Retrieve the details of a root in a proof set.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `proofSetID`: The ID of the proof set.
    - `rootID`: The ID of the root.

#### Response

- **Status Code:** `200 OK`
- **Response Body:** Root details (to be defined).

#### Errors

- `400 Bad Request`: Invalid request.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Proof set or root not found.

---

### 10. Delete a Root from a Proof Set *(To be implemented)*

- **Endpoint:** `DELETE /pdp/proof-sets/{proofSetID}/roots/{rootID}`
- **Description:** Remove a root from a proof set.
- **Authentication:** Requires a valid JWT token in the `Authorization` header.
- **URL Parameters:**
    - `proofSetID`: The ID of the proof set.
    - `rootID`: The ID of the root.

#### Response

- **Status Code:** `204 No Content`

#### Errors

- `400 Bad Request`: Invalid request.
- `401 Unauthorized`: Missing or invalid JWT token.
- `404 Not Found`: Proof set or root not found.

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

## Root CID Computation from Subroots

When adding roots to a proof set using the `POST /pdp/proof-sets/{proofSetID}/roots` endpoint, the server performs validation and computation of the root CID from the provided subroots.

### Root Computation Process

1. **Validating Subroots:**
    - Ensure that all subroots are owned by the requesting service.
    - The subroots must have been previously uploaded and stored on the server.

2. **Ordering Subroots:**
    - **Important:** Subroots must be ordered from **largest to smallest size**.
    - This ordering ensures that no padding is required between the subroots, aligning them correctly for root computation.

3. **Piece Sizes and Alignment:**
    - Each subroot corresponds to a piece with a size that is a power of two (e.g., 128 bytes, 256 bytes, 512 bytes).
    - The concatenation of the subroots must not require padding to align to the sector size used in the computation.

4. **Computing the Root CID:**
    - The server uses the `GenerateUnsealedCID` function to compute the root CID from the subroots.
    - This function emulates the computation performed in the Filecoin proofs implementation.
    - The process involves stacking the subroots and combining them using a Merkle tree hash function.

5. **Validation of Computed Root CID:**
    - The computed root CID is compared with the `rootCid` provided in the request.
    - If the computed root CID does not match the provided `rootCid`, the request is rejected with an error.

### Constraints and Requirements

- **Subroots Ownership:** All subroot CIDs must belong to the requesting service.
- **Subroots Existence:** All subroot CIDs must be valid and previously stored on the server.
- **Ordering of Subroots:** Must be ordered from largest to smallest. The sizes must be decreasing or equal; no subroot can be larger than the preceding one.
- **Subroot Sizes:** Each subroot size must be a power of two and at least 128 bytes.
- **Total Size Limit:** The total size of the concatenated subroots must not exceed the maximum allowed sector size.

### Error Responses

- **Invalid Subroot Order:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `Subroots must be in descending order of size`

- **Subroot Not Found or Unauthorized:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `subroot CID <CID> not found or does not belong to service`

- **Root CID Mismatch:**
    - **Status Code:** `400 Bad Request`
    - **Message:** `provided RootCID does not match generated RootCID`

### Root Computation Function

```go
func GenerateUnsealedCID(proofType abi.RegisteredSealProof, pieceInfos []abi.PieceInfo) (cid.Cid, error)
```

Where:

- `pieceInfos` is a list of pieces (subroots) with their sizes and CIDs.
- The function builds a CommP tree from the subroots, combining them correctly according to their sizes and alignment.

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

### RootEntry

Represents a root entry in a proof set.

```json
{
  "rootId": <rootID>,
  "rootCid": "<rootCID>",
  "subrootCid": "<subrootCID>",
  "subrootOffset": <subrootOffset>
}
```

- **Fields:**
    - `rootId`: The ID of the root.
    - `rootCid`: The CID of the root.
    - `subrootCid`: The CID of the subroot.
    - `subrootOffset`: The offset of the subroot.

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

### Creating a Proof Set

**Request:**

```http
POST /pdp/proof-sets HTTP/1.1
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
Location: /pdp/proof-sets/created/0xabc123...
```

### Adding Roots to a Proof Set

**Request:**

```http
POST /pdp/proof-sets/{proofSetID}/roots HTTP/1.1
Host: example.com
Authorization: Bearer <JWT-token>
Content-Type: application/json

[
  {
    "rootCid": "<rootCID>",
    "subroots": [
      { "subrootCid": "<subrootCID1>" },
      { "subrootCid": "<subrootCID2>" },
      { "subrootCid": "<subrootCID3>" }
    ]
  },
  // ... Additional roots if needed
]
```

**Response:**

```http
HTTP/1.1 201 Created
```
