# Current scheme of `pdpv0`

The following summarizes the flow of end-to-end lifecycle of a piece in the system:

```
STEP 1: UPLOAD PIECES (Off-chain)
├─ Client: POST /pdp/piece, `parked_pieces` lookup, returns uploadUUID or pieceCid
├─ Client: PUT /pdp/piece/upload/{uuid} + <piece data> (in case piece to be provided)
├─ Curio: Store piece → `parked_pieces` (long_term set to TRUE)
├─ Curio: Store ref → `parked_piece_refs` (data_url=custore://)
└─ Curio: Create → `pdp_piecerefs` (links service + piece_cid + piece_ref)
    ✓ Pieces now stored in Curio

STEP 2: ADD PIECES TO DATA SET (Triggers on-chain transaction)
├─ Client: POST /pdp/data-sets/{id}/pieces + {pieces: [...]}
├─ Curio: VALIDATES pieces exist in pdp_piecerefs ← CRITICAL CHECK!
│   └─ See handlers_add.go:97-127 - queries pdp_piecerefs
├─ Curio: Prepare ETH transaction (addPieces contract call)
├─ Curio: Send transaction to blockchain
├─ Curio: Insert tracking records:
│   ├─ message_waits_eth (tx_hash, status="pending")
│   └─ pdp_data_set_piece_adds (pending piece additions)
└─ Client: Receives 201 Created + transaction hash

STEP 3: TRANSACTION CONFIRMED (On-chain)
├─ MessageWatcher: Monitors blockchain for tx confirmation
├─ MessageWatcher: Updates message_waits_eth (tx_success=TRUE)
└─ Database Triggers: Update pdp_data_set_piece_adds (add_message_ok=TRUE)

STEP 4: WATCHERS FINALIZE (After confirmation)
├─ proofset_addroot_watch.go: processPendingDataSetPieceAdds()
├─ Extract piece_ids from tx receipt logs (PiecesAdded event)
├─ Update pdp_data_sets (set init_ready=TRUE)
└─ INSERT into pdp_data_set_pieces (final piece records with on-chain IDs)
    ✓ Pieces now linked to data set on-chain!

STEP 5: PROVING BECOMES AVAILABLE
├─ dataset_watch.go: Monitors for proving periods
├─ task_init_pp.go: Initializes first proving period
├─ task_prove.go: Generates proofs when challenged
└─ Uses pieces from pdp_data_set_pieces
```

---



1a. REGISTER UPLOAD SESSION
    Client → POST /pdp/piece {pieceCid, notify}
    ↓
    Curio: Check if piece already exists (parked_pieces lookup)
    ↓
    ┌─── IF PIECE EXISTS ────────────────────────┐
    │ • Create new parked_piece_refs entry       │
    │ • INSERT pdp_piece_uploads (with piece_ref)│
    │ • Generate uploadUUID                      │
    │ → Return: 200 OK + {pieceCid}             │
    │   (Client can skip upload!)                │
    └────────────────────────────────────────────┘
    ┌─── IF PIECE NEW ───────────────────────────┐
    │ • INSERT pdp_piece_uploads (piece_ref=NULL)│
    │ • Generate uploadUUID                      │
    │ → Return: 201 Created                     │
    │   Header: Location: /pdp/piece/upload/{uploadUUID}
    │   (Client MUST upload piece data)          │
    └────────────────────────────────────────────┘

1b. UPLOAD PIECE DATA (only if got 201 Created)
    Client → PUT /pdp/piece/upload/{uploadUUID} + <piece bytes>
    ↓
    Curio: Lookup upload session by UUID
    ├─ SELECT FROM pdp_piece_uploads WHERE id = {uploadUUID}
    ├─ Validate: piece_ref IS NULL (not already uploaded)
    ├─ Validate: check_hash, check_size match expectations
    ↓
    Curio: Stream piece data
    ├─ Calculate CommP (on-the-fly hashing)
    ├─ Store in StashStore → custore:// URL
    ├─ Verify hash matches pieceCid
    ↓
    Curio: Finalize storage
    ├─ INSERT parked_pieces (piece_cid, sizes, long_term=TRUE)
    ├─ INSERT parked_piece_refs (piece_id, data_url=custore://)
    └─ UPDATE pdp_piece_uploads SET piece_ref = {ref_id}
    → Return: 204 No Content

1c. MAKE PIECE AVAILABLE FOR PDP (background task)
    Task: pdp/notify_task.go monitors pdp_piece_uploads
    ↓
    When piece_ref is set (upload complete):
    ├─ INSERT pdp_piecerefs (service, piece_cid, piece_ref)
    ├─ Call notify_url webhook (if provided)
    └─ Piece now available for adding to datasets!
    
    ✓ Pieces now stored in Curio and ready for use

## Why the Two-Step Upload Process?

### The Upload Session Pattern (POST → PUT)

The upload flow uses a **session-based pattern** with a UUID to separate **registration** from **data transfer**:

```
POST /pdp/piece          →  Creates upload session
  ↓
Returns uploadUUID       →  Unique session identifier
  ↓
PUT /piece/upload/{UUID} →  Uploads data to that session
```

### Why Use a UUID?

**1. Separation of Concerns**
```
POST step:   "I WANT to upload piece X with this CommP"
             → Validates CID, creates session, checks for duplicates
             
PUT step:    "Here IS the actual piece data"
             → Validates data matches session expectations
```

**2. Security & Validation**
- POST verifies authentication ONCE and stores it with the UUID
- PUT can validate the upload belongs to the right service
- Prevents race conditions between metadata and data

**3. Deduplication**
```
Client: POST /pdp/piece {pieceCid: "baga..."}

IF piece already exists:
  → Server: 200 OK + {pieceCid: "baga..."}
  → Client: Skip PUT! Piece already stored, save bandwidth
  
IF piece is new:
  → Server: 201 Created + Location: /piece/upload/{uuid}
  → Client: Must PUT actual bytes to that UUID
```

**4. Resumability (Future)**
- UUID can track partial uploads
- Client can retry PUT without re-POSTing
- Server knows which upload session failed

### Detailed Interplay

**Step 1: POST /pdp/piece** ([handlers_upload.go:38](https://github.com/filecoin-project/curio/blob/pdpv0/pdp/handlers_upload.go#L38))

```http
POST /pdp/piece
Authorization: Bearer <jwt-token>
Content-Type: application/json

{
  "pieceCid": "baga6ea4seaqao7s73y24kcutaosvacpdjgfe5pw76ooefnyqw4ynr3d2y",
  "notify": "https://client.example.com/webhook"
}
```

**Server Actions:**
1. Parse pieceCidV2 → extract CommP + size
2. Generate uploadUUID = `550e8400-e29b-41d4-a716-446655440000`
3. Check if piece exists in `parked_pieces`
4. Create session in `pdp_piece_uploads`:
   ```sql
   INSERT INTO pdp_piece_uploads (
     id,              -- uploadUUID
     service,         -- "myservice" (from JWT)
     piece_cid,       -- "baga..." (v1 format)
     notify_url,      -- "https://client.example.com/webhook"
     piece_ref,       -- NULL (no piece data yet)
     check_hash,      -- Expected CommP hash
     check_size       -- Expected piece size
   ) VALUES (...)
   ```

**Server Response (piece is new):**
```http
HTTP/1.1 201 Created
Location: /pdp/piece/upload/550e8400-e29b-41d4-a716-446655440000

(no body)
```

**Client next action:** Extract UUID from Location header and upload data

---

**Step 2: PUT /pdp/piece/upload/{uuid}** ([handlers_upload.go:163](https://github.com/filecoin-project/curio/blob/pdpv0/pdp/handlers_upload.go#L163))

```http
PUT /pdp/piece/upload/550e8400-e29b-41d4-a716-446655440000
Content-Type: application/octet-stream
Content-Length: 268435456

<binary piece data - 256 MiB>
```

**Server Actions:**
1. Lookup session:
   ```sql
   SELECT piece_cid, notify_url, piece_ref, check_size 
   FROM pdp_piece_uploads 
   WHERE id = '550e8400-e29b-41d4-a716-446655440000'
   ```

2. Validate session state:
   - If `piece_ref IS NOT NULL` → 409 Conflict (already uploaded)
   - If not found → 404 Not Found

3. Stream data + calculate CommP simultaneously:
   ```go
   cp := &commp.Calc{}
   io.Copy(io.MultiWriter(cp, storageFile), request.Body)
   digest, paddedSize, err := cp.Digest()
   ```

4. Validate computed CommP matches expected:
   ```go
   if !bytes.Equal(computedHash, expectedHash) {
     return 400 Bad Request  // Piece data doesn't match CID!
   }
   ```

5. Store and finalize:
   ```sql
   BEGIN TRANSACTION;
   
   -- Create piece entry
   INSERT INTO parked_pieces 
   VALUES (piece_cid, padded_size, raw_size, long_term=TRUE, complete=TRUE);
   
   -- Create reference with storage location
   INSERT INTO parked_piece_refs 
   VALUES (piece_id, data_url='custore://9a8b7c6d5e4f', long_term=TRUE);
   
   -- Link upload session to stored piece
   UPDATE pdp_piece_uploads 
   SET piece_ref = <new_ref_id> 
   WHERE id = '550e8400...';
   
   COMMIT;
   ```

**Server Response:**
```http
HTTP/1.1 204 No Content

(indicates successful upload)
```

---

### Database State Transitions

**After POST (piece is new):**
```
pdp_piece_uploads:
┌──────────────────────┬─────────┬───────────┬──────────┬────────────┐
│ id (UUID)            │ service │ piece_cid │ piece_ref│ check_size │
├──────────────────────┼─────────┼───────────┼──────────┼────────────┤
│ 550e8400-e29b-41d4...│ public  │ baga...   │ NULL     │ 268435456  │
└──────────────────────┴─────────┴───────────┴──────────┴────────────┘
                                                  ↑
                                                  NULL means: awaiting upload
```

**After PUT (upload complete):**
```
parked_pieces:
┌────┬───────────┬─────────────────┬──────────┬───────────┐
│ id │ piece_cid │ piece_padded_sz │ raw_size │ long_term │
├────┼───────────┼─────────────────┼──────────┼───────────┤
│ 99 │ baga...   │ 268435456       │ 256MB    │ TRUE      │
└────┴───────────┴─────────────────┴──────────┴───────────┘

parked_piece_refs:
┌────────┬──────────┬───────────────────────┐
│ ref_id │ piece_id │ data_url              │
├────────┼──────────┼───────────────────────┤
│ 456    │ 99       │ custore://9a8b7c6d... │
└────────┴──────────┴───────────────────────┘

pdp_piece_uploads:
┌──────────────────────┬─────────┬───────────┬──────────┐
│ id                   │ service │ piece_cid │ piece_ref│
├──────────────────────┼─────────┼───────────┼──────────┤
│ 550e8400-e29b-41d4...│ public  │ baga...   │ 456      │ ← LINKED!
└──────────────────────┴─────────┴───────────┴──────────┘
```

**After notify task runs:**
```
pdp_piecerefs:  ← NOW piece is available for datasets!
┌────┬─────────┬───────────┬───────────┐
│ id │ service │ piece_cid │ piece_ref │
├────┼─────────┼───────────┼───────────┤
│ 1  │ public  │ baga...   │ 456       │
└────┴─────────┴───────────┴───────────┘
```

### Why This Matters

**For Clients:**
- Can check if piece exists BEFORE uploading gigabytes
- Save bandwidth with deduplication
- Get immediate feedback if piece already stored

**For Curio:**
- Separate authentication/validation from data transfer
- Track upload progress via session UUIDs
- Prevent unauthorized data uploads
- Enable future features like resumable uploads

**Security:**
- UUID is unpredictable → prevents unauthorized uploads to random sessions
- Each session bound to specific service + pieceCid
- Cannot upload wrong data to a session (hash validation)

---

## Visual Flow: POST → PUT Interplay

```
CLIENT                           CURIO SERVER                      DATABASE
  │                                     │                              │
  │  1. POST /pdp/piece                 │                              │
  │     {pieceCid: "baga..."}           │                              │
  ├──────────────────────────────────>  │                              │
  │                                     │  Check if piece exists       │
  │                                     ├────────────────────────────> │
  │                                     │  SELECT FROM parked_pieces   │
  │                                     │  WHERE piece_cid = "baga..." │
  │                                     │ <───────────────────────────┤
  │                                     │  (Not Found)                 │
  │                                     │                              │
  │                                     │  Generate UUID:              │
  │                                     │  uuid = "550e8400-..."       │
  │                                     │                              │
  │                                     │  Create upload session       │
  │                                     ├────────────────────────────> │
  │                                     │  INSERT pdp_piece_uploads    │
  │                                     │    id = "550e8400-..."       │
  │                                     │    piece_ref = NULL          │
  │                                     │ <───────────────────────────┤
  │                                     │  (Session created)           │
  │                                     │                              │
  │  201 Created                        │                              │
  │  Location: /piece/upload/550e8400...│                              │
  │ <────────────────────────────────── │                              │
  │                                     │                              │
  │  Extract UUID from Location header  │                              │
  │  uuid = "550e8400-..."              │                              │
  │                                     │                              │
  │  2. PUT /piece/upload/550e8400-...  │                              │
  │     <binary piece data>             │                              │
  ├──────────────────────────────────>  │                              │
  │                                     │  Lookup upload session       │
  │                                     ├────────────────────────────> │
  │                                     │  SELECT FROM pdp_piece_uploads
  │                                     │  WHERE id = "550e8400-..."   │
  │                                     │ <───────────────────────────┤
  │                                     │  {piece_cid, size, hash...}  │
  │                                     │                              │
  │                                     │  Validate: piece_ref IS NULL │
  │                                     │  (not already uploaded)      │
  │                                     │                              │
  │                                     │  Stream data + calc CommP    │
  │    [Streaming piece bytes...]       │  commp.Calc{}               │
  ├────────────────────────────────────>│  hash = SHA256(data)        │
  │    [256 MiB transferring...]        │                              │
  ├────────────────────────────────────>│                              │
  │                                     │                              │
  │                                     │  Store in StashStore         │
  │                                     │  custore://9a8b7c6d5e4f      │
  │                                     │                              │
  │                                     │  Verify hash matches         │
  │                                     │  computed == expected ✓      │
  │                                     │                              │
  │                                     │  BEGIN TRANSACTION           │
  │                                     ├────────────────────────────> │
  │                                     │  INSERT parked_pieces        │
  │                                     │    piece_cid = "baga..."     │
  │                                     │    complete = TRUE           │
  │                                     │ <───────────────────────────┤
  │                                     │  piece_id = 99               │
  │                                     │                              │
  │                                     │  INSERT parked_piece_refs    │
  │                                     ├────────────────────────────> │
  │                                     │    piece_id = 99             │
  │                                     │    data_url = "custore://..." │
  │                                     │ <───────────────────────────┤
  │                                     │  ref_id = 456                │
  │                                     │                              │
  │                                     │  UPDATE pdp_piece_uploads    │
  │                                     ├────────────────────────────> │
  │                                     │  SET piece_ref = 456         │
  │                                     │  WHERE id = "550e8400..."    │
  │                                     │                              │
  │                                     │  COMMIT                      │
  │                                     │ <───────────────────────────┤
  │                                     │                              │
  │  204 No Content                     │                              │
  │ <────────────────────────────────── │                              │
  │                                     │                              │
  │  Upload complete! ✓                 │                              │
  │                                     │                              │
  │                           [Background: NotifyTask runs]            │
  │                                     │  Watch pdp_piece_uploads     │
  │                                     │  WHERE piece_ref IS NOT NULL │
  │                                     ├────────────────────────────> │
  │                                     │  INSERT pdp_piecerefs        │
  │                                     │  (Piece now available!)      │
  │                                     │                              │
  │  (If notify URL provided)           │  Call webhook                │
  │ <───────────────────────────────────┤  POST https://client.com/... │
  │  Notification: Piece ready          │                              │
```

### Key Takeaways

1. **POST is fast** - Just metadata, no data transfer
2. **UUID is the link** - Connects POST request to PUT upload
3. **PUT is heavyweight** - Actual data transfer + validation
4. **Validation happens twice**:
   - POST: Validates CID format, size limits
   - PUT: Validates actual data matches promised CommP
5. **Deduplication saves bandwidth** - 200 OK means skip PUT entirely