# Curio TypeScript Market Client

This is a TypeScript API client for the Curio storage market API. It provides a strongly-typed interface for interacting with Curio storage providers.

## Installation

```bash
npm install @curiostorage/market-client
```

## Prerequisites

**Authentication is required** for all API operations. You must configure authentication before using the client.

### Authentication Methods

The client supports two authentication methods:

1. **Ed25519** (default) - Uses Ed25519 key pairs
2. **Secp256k1** - Uses Secp256k1 key pairs (compatible with Ethereum wallets)

### Authentication Configuration

Authentication can be configured programmatically or via environment variables (used in examples):

**Programmatic Configuration:**
```typescript
const authConfig = {
  serverUrl: 'https://your-server.com',
  clientAddr: 'f1client...',
  recordKeeper: 't1000',  // Required for PDPv1 deals
  contractAddress: '0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6',
  keyType: 'ed25519' as 'ed25519' | 'secp256k1',
  publicKeyB64: 'your_base64_public_key',
  privateKeyB64: 'your_base64_private_key',
  // OR for secp256k1:
  // secpPrivateKeyHex: 'your_hex_private_key',
  // secpPrivateKeyB64: 'your_base64_private_key',
};
```

**Environment Variables (for examples):**
```bash
# Used in the example scripts
export PDP_URL=https://your-server.com
export PDP_CLIENT=f1client...
export PDP_RECORD_KEEPER=t1000
export PDP_CONTRACT=0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6
export PDP_KEY_TYPE=ed25519
export PDP_PUBLIC_KEY_B64=your_base64_public_key
export PDP_PRIVATE_KEY_B64=your_base64_private_key
```

**Running Example Scripts:**
```bash
# Example: Running step 1 with all environment variables inline
PDP_INSECURE_TLS=1 \
PDP_URL="https://your-server.com" \
PDP_CLIENT=f1client... \
PDP_KEY_TYPE=secp256k1 \
PDP_SECP_PRIVATE_KEY_B64="your_base64_private_key" \
PDP_CONTRACT="0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6" \
PDP_RECORD_KEEPER="0x158c8f05A616403589b99BE5d82d756860363A92" \
DATASET_ID="01K4TKYS9302Y42BRBT0V0S389" \
npx ts-node 1.ts
```

You'll need a wallet:
 lotus wallet new delegated

You can get your private key (for demo only) from:
   lotus wallet export <your-delegated-wallet-address> | xxd -r -p | jq -r ‘.PrivateKey’ | base64 -d | xxd -p -c 32

Be sure to setup Curio with:
- ui —> pdp —> ownerAddress —> (hex key)
- Your curio also needs storage attached:
  -- ./curio cli storage attach -snap -seal /home/ubuntu/curiofolder
  -- And a market enabled, such as taking the following with ./curio config set market.yaml
market.yaml:
[Batching]
  [Batching.Commit]
    Timeout = "0h0m5s"
  [Batching.PreCommit]
    Slack = "6h0m0s"
    Timeout = "0h0m5s"

[HTTP]
  DelegateTLS = false
  DomainName = "yourserver.yourdomain.com"
  Enable = true
  ListenAddress = "0.0.0.0:443"

[Ingest]
  MaxDealWaitTime = "0h0m30s"

[Market]
  [Market.StorageMarketConfig]
    [Market.StorageMarketConfig.MK12]
      ExpectedPoRepSealDuration = "0h1m0s"
      ExpectedSnapSealDuration = "0h1m0s"
      PublishMsgPeriod = "0h0m10s"

[Subsystems]
  EnableCommP = true
  EnableDealMarket = true
  EnablePDP = true
  EnableParkPiece = true


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
import { MarketClient, PieceCidUtils, AuthUtils } from '@curiostorage/market-client';

// Configure authentication programmatically
const authConfig = {
  serverUrl: 'https://your-server.com',
  clientAddr: 'f1client...',
  recordKeeper: 't1000',  // Required for PDPv1 deals
  contractAddress: '0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6',
  keyType: 'ed25519' as 'ed25519' | 'secp256k1',
  publicKeyB64: 'your_base64_public_key',
  privateKeyB64: 'your_base64_private_key',
};

// Build authentication header
const authHeader = await AuthUtils.buildAuthHeader(authConfig);

// Create authenticated client
const client = new MarketClient({ 
  serverUrl: authConfig.serverUrl,
  authHeader 
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
import { Client, MarketClientConfig, AuthUtils } from '@curiostorage/market-client';

// Configure authentication programmatically
const authConfig = {
  serverUrl: 'https://your-server.com',
  clientAddr: 'f1client...',
  recordKeeper: 't1000',  // Required for PDPv1 deals
  contractAddress: '0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6',
  keyType: 'ed25519' as 'ed25519' | 'secp256k1',
  publicKeyB64: 'your_base64_public_key',
  privateKeyB64: 'your_base64_private_key',
};

const authHeader = await AuthUtils.buildAuthHeader(authConfig);

const config: MarketClientConfig = { 
  serverUrl: authConfig.serverUrl,
  authHeader 
};
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
then without createDataSet & with:
    addPiece: true,            // Add piece to dataset

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

## Troubleshooting

### Common Authentication Issues

**Error: "REQUIRED ENVIRONMENT VARIABLE MISSING: PDP_RECORD_KEEPER"**
- This error only appears when running the example scripts
- The `PDP_RECORD_KEEPER` environment variable is required for the examples
- Set it with: `export PDP_RECORD_KEEPER=your-record-keeper-address`
- For programmatic usage, include `recordKeeper` in your auth config

**Error: "Authentication failed" or "Invalid signature"**
- Verify your private key is correctly formatted (base64 or hex)
- Ensure the key type matches your key format (`ed25519` or `secp256k1`)
- Check that the public key corresponds to the private key

**Error: "TLS verification failed"**
- For debugging only, you can disable TLS verification with `export PDP_INSECURE_TLS=1`
- **Warning**: Never use this in production

**Error: "Connection refused" or "Network error"**
- Verify the `PDP_URL` is correct and accessible
- Check that the server is running and accepting connections
- Ensure firewall settings allow the connection

### Key Generation

**Ed25519 Key Generation:**
```bash
# Generate Ed25519 key pair
node -e "
const crypto = require('crypto');
const keyPair = crypto.generateKeyPairSync('ed25519');
console.log('Public key (base64):', keyPair.publicKey.export({ type: 'spki', format: 'der' }).toString('base64'));
console.log('Private key (base64):', keyPair.privateKey.export({ type: 'pkcs8', format: 'der' }).toString('base64'));
"
```

**Secp256k1 Key Generation:**
```bash
# Generate Secp256k1 key pair
node -e "
const crypto = require('crypto');
const keyPair = crypto.generateKeyPairSync('ec', { namedCurve: 'secp256k1' });
console.log('Private key (hex):', keyPair.privateKey.export({ type: 'sec1', format: 'der' }).toString('hex'));
console.log('Private key (base64):', keyPair.privateKey.export({ type: 'sec1', format: 'der' }).toString('base64'));
"
```

### Environment Variable Validation

The example client validates your configuration:

```typescript
import { AuthUtils } from '@curiostorage/market-client';

const authConfig = {
  // ... your config
};

try {
  const authHeader = await AuthUtils.buildAuthHeader(authConfig);
  console.log('✅ Authentication configuration is valid');
} catch (error) {
  console.error('❌ Authentication configuration error:', error.message);
}
```

## Examples

See the `examples/unpkg-end-to-end/` directory for a complete step-by-step workflow that demonstrates:

- Authentication setup and configuration (using environment variables)
- Creating PDPv1 datasets
- Adding pieces to datasets
- Uploading data with chunked uploads
- Downloading pieces
- Deleting datasets and pieces

Each step is documented and can be run independently, making it easy to understand the complete workflow. The examples use environment variables for configuration, but you can adapt the code to use programmatic configuration instead.

**Quick Start with Examples:**
```bash
# Set your configuration
export PDP_URL="https://your-server.com"
export PDP_CLIENT="f1client..."
export PDP_RECORD_KEEPER="0x158c8f05A616403589b99BE5d82d756860363A92"
export PDP_CONTRACT="0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6"
export PDP_KEY_TYPE="secp256k1"
export PDP_SECP_PRIVATE_KEY_B64="your_base64_private_key"

# Run the complete workflow
cd examples/unpkg-end-to-end/
npx ts-node 1.ts  # Create dataset
npx ts-node 2.ts  # Add piece and upload
npx ts-node 3.ts  # Download piece
npx ts-node 4.ts  # Delete
```

## Development

The client is generated from the OpenAPI/Swagger specification in `../http/swagger.json`. To regenerate after API changes:

```bash
npm run generate
npm run compile
```

## License

MIT
