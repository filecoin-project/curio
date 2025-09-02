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

const client = new MarketClient({
  basePath: 'http://localhost:8080/market/mk20'
});

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

// Convert CID v1 to piece CID v2
const cidV1 = CID.create(1, 0x55, hash);
const pieceCidV2 = await PieceCidUtils.pieceCidV2FromV1(cidV1, dataSize);
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
