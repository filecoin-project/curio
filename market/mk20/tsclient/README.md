# Curio TypeScript Market Client

This is a TypeScript API client for the Curio storage market API. It provides a strongly-typed interface for interacting with Curio storage providers.

## Installation

```bash
npm install @curio/market-client
```

## Building from Source

1. Install dependencies:
```bash
npm install
```

2. Generate the client from swagger files:
```bash
npm run generate
```

3. Compile TypeScript:
```bash
npm run compile
```

4. Or build everything at once:
```bash
npm run build
```

## Usage

```typescript
import { MarketClient, PieceCidUtils } from '@curio/market-client';

const client = new MarketClient({ serverUrl: 'http://localhost:8080' });

// Get supported contracts
const contracts = await client.getContracts();

// Get supported products
const products = await client.getProducts();

// Get supported data sources
const sources = await client.getSources();

// Get deal status
const status = await client.getStatus('deal-id-here');

// Submit a deal
const deal = {
  // ... deal configuration
};
const result = await client.submitDeal(deal);

// Upload data (single request - suitable for small deals)
await client.uploadData('deal-id', [1, 2, 3, 4]);

// Chunked upload (suitable for large deals)
await client.initializeChunkedUpload('deal-id', startUploadData);
await client.uploadChunk('deal-id', '0', chunkData);
await client.uploadChunk('deal-id', '1', chunkData);
await client.finalizeChunkedUpload('deal-id');

// Check upload status
const uploadStatus = await client.getUploadStatus('deal-id');

// Compute piece CID v2 from blobs
const blobs = [new Blob(['file content'])];
const pieceCid = await PieceCidUtils.computePieceCidV2(blobs);

// Convenience wrappers for common workflows (includes automatic chunked upload)
const result = await client.submitPDPv1DealWithUpload({
  blobs: [new Blob(['file content'])],
  client: 'f1client...',
  provider: 'f1provider...',
  contractAddress: '0x...'
});

// DDO deals with custom duration (includes automatic chunked upload)
const ddoResult = await client.submitDDOV1DealWithUpload({
  blobs: [new Blob(['file content'])],
  client: 'f1client...',
  provider: 'f1provider...',
  contractAddress: '0x...',
  // Optional lifespan (epochs); defaults to 518400 if omitted
  lifespan: 600000
});

// Results include upload statistics
console.log('Uploaded chunks:', result.uploadedChunks);
console.log('Uploaded bytes:', result.uploadedBytes);
```

## Streaming PDP (no upfront data section)

Create a deal without a `data` section, stream data using `uploadChunk`, compute the piece CID while streaming, then finalize with the computed `data`:

```typescript
import { Client, MarketClientConfig } from '@curio/market-client';

const config: MarketClientConfig = { serverUrl: 'http://localhost:8080' };
const client = new Client(config);

// Create the streaming helper (defaults to 1MB chunks)
const spdp = client.streamingPDP({
  client: 'f1client...',
  provider: 'f1provider...',
  contractAddress: '0x...',
  // chunkSize: 2 * 1024 * 1024, // optional
});

// Begin: submits deal without data and initializes chunked upload
await spdp.begin();

// Stream bytes (these are uploaded as chunks and hashed for CID)
spdp.write(new TextEncoder().encode('hello '));
spdp.write(new TextEncoder().encode('world'));

// Commit: flushes remaining chunk, computes piece CID, and finalizes with data
const { id, pieceCid, totalSize } = await spdp.commit();
console.log({ id, pieceCid, totalSize });
```

## Product Types

The client supports three main product types for different use cases:

### PDPv1 (Proof of Data Possession)
Used for creating datasets and proving data possession:
```typescript
products: {
  pdpV1: {
    createDataSet: true,        // Create new dataset
    addPiece: true,            // Add piece to dataset
    recordKeeper: 'provider-address',
    pieceIds: [123, 456, 789]  // Piece IDs for each individual blob
  },
  retrievalV1: {
    announcePayload: true,     // Announce to IPNI
    announcePiece: true,       // Announce piece info
    indexing: true             // Enable retrieval
  }
}
```

### DDOv1 (Direct Data Onboarding)
Used for direct data onboarding with contract verification:
```typescript
products: {
  ddoV1: {
    duration: 518400,          // Typically chosen per-deal (lifespan)
    provider: { address: 'provider-address' },
    contractAddress: '0x...',
    contractVerifyMethod: 'verifyDeal'
  },
  retrievalV1: {
    announcePayload: true,
    announcePiece: true,
    indexing: true
  }
}
```

### RetrievalV1
Configures retrieval behavior and indexing:
```typescript
retrievalV1: {
  announcePayload: true,       // Announce payload to IPNI
  announcePiece: true,         // Announce piece information
  indexing: true               // Index for CID-based retrieval
}
```

## API Endpoints

- `GET /contracts` - List supported DDO contracts
- `GET /products` - List supported products  
- `GET /sources` - List supported data sources
- `GET /status/{id}` - Get deal status
- `POST /store` - Submit a new deal
- `PUT /upload/{id}` - Upload deal data (single request)
- `POST /upload/{id}` - Initialize chunked upload
- `PUT /uploads/{id}/{chunkNum}` - Upload a chunk
- `POST /uploads/finalize/{id}` - Finalize chunked upload
- `GET /uploads/{id}` - Get upload status

## Automatic Chunked Upload

The convenience wrappers automatically handle chunked uploads after deal submission:

- **Automatic Processing**: After submitting a deal, all blobs are automatically uploaded in chunks
- **Configurable Chunk Size**: Uses 1MB chunks by default for optimal performance
- **Progress Tracking**: Provides detailed logging of upload progress
- **Complete Workflow**: Handles initialization, chunking, upload, and finalization
- **Upload Statistics**: Returns total chunks and bytes uploaded
- **Simple & Reliable**: Sequential uploads ensure data integrity and predictable behavior

```typescript
const result = await client.submitPDPv1DealWithUpload({
  blobs: [blob1, blob2, blob3],
  client: 'f1client...',
  provider: 'f1provider...',
  contractAddress: '0x...'
});

// The result includes upload statistics
console.log('Uploaded chunks:', result.uploadedChunks); // Total number of chunks
console.log('Uploaded bytes:', result.uploadedBytes);   // Total bytes uploaded
```

## Piece ID Calculation

The client automatically calculates unique piece IDs for each blob in a deal:

- **Individual Blob Piece IDs**: Each blob gets a unique piece ID based on its content hash and size
- **Deterministic**: The same blob content will always generate the same piece ID
- **Consistent**: Both PDPv1 and DDOv1 deals use the same piece ID calculation method
- **Returned**: Piece IDs are included in the deal creation and returned by convenience wrappers

```typescript
// Each blob gets its own piece ID
const result = await client.submitPDPv1DealWithUpload({
  blobs: [blob1, blob2, blob3],
  client: 'f1client...',
  provider: 'f1provider...',
  contractAddress: '0x...'
});

console.log('Piece IDs:', result.pieceIds); // [123, 456, 789]
console.log('Blob 1 → Piece ID:', result.pieceIds[0]); // 123
console.log('Blob 2 → Piece ID:', result.pieceIds[1]); // 456
console.log('Blob 3 → Piece ID:', result.pieceIds[2]); // 789
```

## Piece CID Computation

The client includes utilities for computing Filecoin piece CIDs using the [js-multiformats library](https://github.com/multiformats/js-multiformats):

### `PieceCidUtils.computePieceCidV2(blobs: Blob[])`
Computes a piece CID v2 from an array of blobs by:
1. Concatenating all blob data
2. Computing SHA2-256 hash
3. Creating a CID v1 with raw codec
4. Converting to piece CID v2 format

### `PieceCidUtils.pieceCidV2FromV1(cid: CID, payloadSize: number)`
Converts an existing CID v1 to piece CID v2 format, supporting:
- Filecoin unsealed commitments (SHA2-256)
- Filecoin sealed commitments (Poseidon)
- Raw data codecs

## Development

The client is generated from the OpenAPI/Swagger specification in `../http/swagger.json`. To regenerate after API changes:

```bash
npm run generate
npm run compile
```

## License

MIT
