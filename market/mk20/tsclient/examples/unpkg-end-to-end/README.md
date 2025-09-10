# Unpkg End-to-End Example

This folder contains a step-by-step example of the complete PDPv1 workflow, broken down into individual steps that run in order and use output from previous steps.

## Prerequisites

Set the following environment variables before running any step:

```bash
# Required
export PDP_URL=https://your-server.com
export PDP_CLIENT=t1k7ctd3hvmwwjdpb2ipd3kr7n4vk3xzfvzbbdrai  # client wallet
export PDP_CONTRACT=0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6
export PDP_RECORD_KEEPER=t1000  # record keeper address (required for PDPv1)

# For ed25519 authentication (default)
export PDP_PUBLIC_KEY_B64=base64_of_raw_public_key_32_bytes
export PDP_PRIVATE_KEY_B64=base64_of_secret_key_64_or_seed_32
export PDP_KEY_TYPE=ed25519

# OR for secp256k1 authentication
export PDP_KEY_TYPE=secp256k1
export PDP_SECP_PRIVATE_KEY_HEX=your_32_byte_hex_key
# OR
export PDP_SECP_PRIVATE_KEY_B64=your_32_byte_base64_key

# Optional
export PDP_INSECURE_TLS=1  # Only for debugging - disables TLS verification
```

## Steps (Run in Order)

### 1. Create Dataset (`1.ts`)
Creates a PDPv1 dataset (first part of `startPDPv1DealForUpload`).

```bash
npx ts-node 1.ts
```

**What it does:**
- Creates a PDPv1 dataset with `createDataSet: true`
- Uses `submitDeal` API with dataset creation deal
- Waits for dataset creation to complete
- Returns dataset ID for use in step 2

**Output:** Dataset ID that should be passed to step 2

### 2. Add Piece (`2.ts`)
Adds a piece to the dataset (second part of `startPDPv1DealForUpload`).

```bash
# Set the dataset ID from step 1
export DATASET_ID=your_dataset_id_from_step_1
npx ts-node 2.ts
```

**What it does:**
- Downloads file from unpkg to compute piece CID
- Creates add piece deal with `addPiece: true` and `dataSetId`
- Uses `submitDeal` API with add piece deal
- Waits for add piece to complete
- Returns upload ID, deal ID, and piece CID

**Output:** Upload ID, deal ID, piece CID, and deal object for step 3

### 3. Upload Blobs (`3.ts`)
Uploads blobs using the deal from step 2.

```bash
# Use the upload ID and deal from step 2
export UPLOAD_ID=your_upload_id_from_step_2
npx ts-node 3.ts
```

**What it does:**
- Uses `uploadBlobs` API with the deal from step 2
- Uploads the file data to the deal
- Monitors upload progress until completion

**Output:** Upload completion status

### 4. Download Piece (`4.ts`)
Downloads the piece using piece CID from step 2.

```bash
# Use the piece CID from step 2
export PIECE_CID=your_piece_cid_from_step_2
npx ts-node 4.ts
```

**What it does:**
- Retrieves the uploaded piece via market server
- Uses the piece CID provided from step 2
- Verifies successful retrieval

**Output:** Retrieved content and success status

### 5. Delete (`5.ts`)
Deletes using upload ID from step 3.

```bash
# Use the upload ID from step 3
export UPLOAD_ID=your_upload_id_from_step_3
npx ts-node 5.ts
```

**What it does:**
- Updates the deal with `deletePiece: true` and `deleteDataSet: true`
- Uses `updateDeal` API to request deletion
- Monitors deletion progress
- Completes the end-to-end workflow

**Output:** Deletion confirmation and final status

## Running the Complete Workflow

You can run all steps in sequence, passing data between them:

```bash
# Step 1: Create dataset
DATASET_ID=$(npx ts-node 1.ts | grep "Dataset ID:" | cut -d' ' -f3)

# Step 2: Add piece
export DATASET_ID
UPLOAD_ID=$(npx ts-node 2.ts | grep "Upload ID:" | cut -d' ' -f3)
PIECE_CID=$(npx ts-node 2.ts | grep "Piece CID:" | cut -d' ' -f3)

# Step 3: Upload blobs
export UPLOAD_ID
npx ts-node 3.ts

# Step 4: Download piece
export PIECE_CID
npx ts-node 4.ts

# Step 5: Delete
export UPLOAD_ID
npx ts-node 5.ts
```

## Files

- `auth.ts` - Authentication helpers and configuration management
- `1.ts` - Create dataset step
- `2.ts` - Add piece step  
- `3.ts` - Upload blobs step
- `4.ts` - Download piece step
- `5.ts` - Delete step
- `README.md` - This documentation

## Notes

- **Each step builds on the previous**: Steps are designed to run in order and use output from previous steps
- **Environment variables**: Use `DATASET_ID`, `UPLOAD_ID`, and `PIECE_CID` environment variables to pass data between steps
- **Matches startPDPv1DealForUpload**: Steps 1-2 replicate the internal logic of `startPDPv1DealForUpload` function
- **Real workflow**: This demonstrates the actual API calls you'd make in a production environment
- **Error handling**: All steps include comprehensive error handling and status reporting
