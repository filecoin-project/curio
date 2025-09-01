import { Client, MarketClientConfig, Deal, DataSource, Products, DDOV1 } from '../src';

// Example configuration
const config: MarketClientConfig = {
  basePath: 'http://localhost:8080/market/mk20',
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
      identifier: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16], // Example identifier
      client: 'f1abcdefghijklmnopqrstuvwxyz123456789',
      data: {
        piece_cid: 'bafybeihq6mbsd757cdm4sn6z5r7w6tdvkrb3q9iu3pjr7q3ip24c65qh2i',
        format: {
          raw: {}
        },
        source_httpput: {
          raw_size: 1024 * 1024 // 1MB
        }
      } as DataSource,
      products: {
        ddo_v1: {
          duration: 518400, // Minimum duration in epochs
          provider: { address: 'f1abcdefghijklmnopqrstuvwxyz123456789' },
          contractAddress: '0x1234567890123456789012345678901234567890',
          contractVerifyMethod: 'verifyDeal',
          contractVerifyMethodParams: [],
          pieceManager: { address: 'f1abcdefghijklmnopqrstuvwxyz123456789' },
          notificationAddress: 'f1abcdefghijklmnopqrstuvwxyz123456789',
          notificationPayload: []
        } as DDOV1
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

export { exampleUsage, uploadDataExample };
