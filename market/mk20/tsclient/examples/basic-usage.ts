import { Client, MarketClientConfig, Deal, DataSource, Products, DDOV1, RetrievalV1 } from '../src';

// Example configuration
const config: MarketClientConfig = {
  serverUrl: 'http://localhost:8080',
  // Optional: Add custom headers
  headers: {
    'Authorization': 'Bearer your-token-here'
  }
};

// Create client instance
const client = new Client(config);

async function exampleUsage() {
  try {
    // Get supported contracts
    console.log('Getting supported contracts...');
    const contracts = await client.getContracts();
    console.log('Contracts:', contracts);

    // Get supported products
    console.log('\nGetting supported products...');
    const products = await client.getProducts();
    console.log('Products:', products);

    // Get supported data sources
    console.log('\nGetting supported data sources...');
    const sources = await client.getSources();
    console.log('Sources:', sources);

    // Example: Submit a deal
    console.log('\nSubmitting a deal...');
    const deal: Deal = {
      identifier: '01H0EXAMPLEULIDIDENTIFIER00000000', // Example ULID string
      client: 'f1abcdefghijklmnopqrstuvwxyz123456789',
      data: {
        pieceCid: 'bafybeihq6mbsd757cdm4sn6z5r7w6tdvkrb3q9iu3pjr7q3ip24c65qh2i',
        format: {
          raw: {}
        },
        sourceHttpput: {
          raw_size: 1024 * 1024 // 1MB
        } as unknown as object
      } as DataSource,
      products: {
        ddoV1: {
          duration: 518400, // Typical lifespan value (epochs)
          provider: { address: 'f1abcdefghijklmnopqrstuvwxyz123456789' },
          contractAddress: '0x1234567890123456789012345678901234567890',
          contractVerifyMethod: 'verifyDeal',
          contractVerifyMethodParams: '',
          pieceManager: { address: 'f1abcdefghijklmnopqrstuvwxyz123456789' },
          notificationAddress: 'f1abcdefghijklmnopqrstuvwxyz123456789',
          notificationPayload: ''
        } as DDOV1,
        retrievalV1: {
          announcePayload: true, // Announce payload to IPNI
          announcePiece: true, // Announce piece information to IPNI
          indexing: true // Index for CID-based retrieval
        } as RetrievalV1
      } as Products
    };

    const result = await client.submitDeal(deal);
    console.log('Deal submitted:', result);

    // Get deal status
    if (result && result === 200) { // DealCode.Ok
      console.log('\nGetting deal status...');
      const status = await client.getStatus('example-deal-id');
      console.log('Deal status:', status);
    }

  } catch (error) {
    console.error('Error:', error);
  }
}

// Example: Upload data for a deal
async function uploadDataExample(dealId: string, data: number[]) {
  try {
    console.log(`Uploading data for deal ${dealId}...`);
    await client.uploadData(dealId, data);
    console.log('Data uploaded successfully');
  } catch (error) {
    console.error('Upload failed:', error);
  }
}

// Example: Demonstrate piece ID calculation for individual blobs
async function pieceIdCalculationExample() {
  try {
    console.log('🔍 Piece ID Calculation Example');
    console.log('Calculating piece IDs for individual blobs...\n');

    // Create mock blobs with different content
    const mockBlobs = [
      new Blob(['file1 content'], { type: 'text/plain' }),
      new Blob(['file2 content'], { type: 'text/plain' }),
      new Blob(['file3 content'], { type: 'text/plain' })
    ];

    // Use the convenience wrapper to see piece IDs
    const result = await client.submitPDPv1DealWithUpload({
      blobs: mockBlobs,
      client: 'f1client123456789abcdefghijklmnopqrstuvwxyz',
      provider: 'f1provider123456789abcdefghijklmnopqrstuvwxyz',
      contractAddress: '0x1234567890123456789012345678901234567890'
    });

    console.log('📋 Deal and Upload Results:');
    console.log('UUID:', result.uuid);
    console.log('Total Size:', result.totalSize, 'bytes');
    console.log('Deal ID:', result.dealId);
    console.log('Piece CID:', result.pieceCid);
    console.log('Uploaded Chunks:', result.uploadedChunks);
    console.log('Uploaded Bytes:', result.uploadedBytes);

    return result;

  } catch (error) {
    console.error('❌ Piece ID calculation example failed:', error);
    throw error;
  }
}

export { exampleUsage, uploadDataExample, pieceIdCalculationExample };
