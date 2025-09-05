import { Client, MarketClientConfig, Deal, Products, PDPV1, DDOV1, RetrievalV1 } from '../src';

const config: MarketClientConfig = {
  serverUrl: 'http://localhost:8080',
  headers: { 'Authorization': 'Bearer your-token-here' }
};

const client = new Client(config);

// Example 1: PDPv1 Product (Proof of Data Possession)
async function pdpv1ProductExample() {
  console.log('🔐 PDPv1 Product Example');
  console.log('Used for: Creating datasets, adding pieces, and proving data possession\n');

  const pdpv1Deal: Deal = {
    identifier: '01H0EXAMPLEULIDIDENTIFIER00000000',
    client: 'f1client123456789abcdefghijklmnopqrstuvwxyz',
    data: {
      pieceCid: 'bafybeihq6mbsd757cdm4sn6z5r7w6tdvkrb3q9iu3pjr7q3ip24c65qh2i',
      format: { raw: {} },
      sourceHttpput: { raw_size: 1024 * 1024 } as unknown as object
    } as any,
    products: {
      pdpV1: {
        createDataSet: true,        // Create a new dataset
        addPiece: true,            // Add piece to the dataset
        dataSetId: undefined,      // Not needed when creating dataset
        recordKeeper: 'f1provider123456789abcdefghijklmnopqrstuvwxyz',
        extraData: '',             // Additional data for verification
        pieceIds: [0],             // Initial piece ID
        deleteDataSet: false,      // Don't delete dataset
        deletePiece: false         // Don't delete piece
      } as PDPV1,
      retrievalV1: {
        announcePayload: true,     // Announce to IPNI
        announcePiece: true,       // Announce piece info
        indexing: true             // Enable CID-based retrieval
      } as RetrievalV1
    } as Products
  };

  console.log('PDPv1 Deal Structure:', JSON.stringify(pdpv1Deal.products, null, 2));
  return pdpv1Deal;
}

// Example 2: DDOv1 Product (Direct Data Onboarding)
async function ddov1ProductExample() {
  console.log('📥 DDOv1 Product Example');
  console.log('Used for: Direct data onboarding with contract verification\n');

  const ddov1Deal: Deal = {
    identifier: '01H0EXAMPLEULIDIDENTIFIER00000000',
    client: 'f1client123456789abcdefghijklmnopqrstuvwxyz',
    data: {
      pieceCid: 'bafybeihq6mbsd757cdm4sn6z5r7w6tdvkrb3q9iu3pjr7q3ip24c65qh2i',
      format: { raw: {} },
      sourceHttpput: { raw_size: 1024 * 1024 } as unknown as object
    } as any,
    products: {
      ddoV1: {
        duration: 518400,          // Typical lifespan value (epochs)
        provider: { address: 'f1provider123456789abcdefghijklmnopqrstuvwxyz' },
        contractAddress: '0x1234567890123456789012345678901234567890',
        contractVerifyMethod: 'verifyDeal',
        contractVerifyMethodParams: '',
        pieceManager: { address: 'f1provider123456789abcdefghijklmnopqrstuvwxyz' },
        notificationAddress: 'f1client123456789abcdefghijklmnopqrstuvwxyz',
        notificationPayload: ''
      } as DDOV1,
      retrievalV1: {
        announcePayload: true,     // Announce to IPNI
        announcePiece: true,       // Announce piece info
        indexing: true             // Enable CID-based retrieval
      } as RetrievalV1
    } as Products
  };

  console.log('DDOv1 Deal Structure:', JSON.stringify(ddov1Deal.products, null, 2));
  return ddov1Deal;
}

// Example 3: RetrievalV1 Product (Retrieval Configuration)
async function retrievalV1ProductExample() {
  console.log('🔍 RetrievalV1 Product Example');
  console.log('Used for: Configuring retrieval behavior and indexing\n');

  const retrievalConfig: RetrievalV1 = {
    announcePayload: true,         // Announce payload to IPNI
    announcePiece: true,           // Announce piece information to IPNI
    indexing: true                 // Index for CID-based retrieval
  };

  console.log('RetrievalV1 Configuration:', JSON.stringify(retrievalConfig, null, 2));
  return retrievalConfig;
}

// Example 4: Using the convenience wrappers
async function convenienceWrapperExample() {
  console.log('🚀 Convenience Wrapper Examples');
  console.log('Using the simplified methods for common workflows\n');

  // Create mock blobs
  const mockBlobs = [
    new Blob(['file1 content'], { type: 'text/plain' }),
    new Blob(['file2 content'], { type: 'text/plain' })
  ];

  try {
    // PDPv1 workflow
    console.log('\n📋 PDPv1 Workflow:');
    const pdpResult = await client.submitPDPv1DealWithUpload({
      blobs: mockBlobs,
      client: 'f1client123456789abcdefghijklmnopqrstuvwxyz',
      provider: 'f1provider123456789abcdefghijklmnopqrstuvwxyz',
      contractAddress: '0x1234567890123456789012345678901234567890'
    });

    console.log('PDPv1 Result:', {
      uuid: pdpResult.uuid,
      totalSize: pdpResult.totalSize,
      dealId: pdpResult.dealId,
      pieceCid: pdpResult.pieceCid,
      uploadedChunks: pdpResult.uploadedChunks,
      uploadedBytes: pdpResult.uploadedBytes
    });

    // Show blob to piece ID mapping
    console.log('📁 Blob to Piece ID Mapping:');
    mockBlobs.forEach((blob, index) => {
      console.log(`  Blob ${index + 1} (${blob.size} bytes)`);
    });

    // DDOv1 workflow
    console.log('\n📋 DDOv1 Workflow:');
    const ddoResult = await client.submitDDOV1DealWithUpload({
      blobs: mockBlobs,
      client: 'f1client123456789abcdefghijklmnopqrstuvwxyz',
      provider: 'f1provider123456789abcdefghijklmnopqrstuvwxyz',
      contractAddress: '0x1234567890123456789012345678901234567890',
      lifespan: 518400
    });

    console.log('DDOv1 Result:', {
      uuid: ddoResult.uuid,
      totalSize: ddoResult.totalSize,
      dealId: ddoResult.dealId,
      pieceCid: ddoResult.pieceCid,
      uploadedChunks: ddoResult.uploadedChunks,
      uploadedBytes: ddoResult.uploadedBytes
    });

    // Show blob to piece ID mapping
    console.log('📁 Blob to Piece ID Mapping:');
    mockBlobs.forEach((blob, index) => {
      console.log(`  Blob ${index + 1} (${blob.size} bytes)`);
    });

  } catch (error) {
    console.error('❌ Error in convenience wrapper example:', error);
  }
}

// Main function to run all examples
async function runAllProductExamples() {
  console.log('🎯 Market Client Product Types Examples\n');
  console.log('=====================================\n');

  try {
    // Show product structures
    await pdpv1ProductExample();
    console.log('\n' + '='.repeat(50) + '\n');
    
    await ddov1ProductExample();
    console.log('\n' + '='.repeat(50) + '\n');
    
    await retrievalV1ProductExample();
    console.log('\n' + '='.repeat(50) + '\n');
    
    // Show convenience wrappers
    await convenienceWrapperExample();
    
    console.log('\n✅ All product examples completed successfully!');
    
  } catch (error) {
    console.error('❌ Error running product examples:', error);
  }
}

export { 
  pdpv1ProductExample, 
  ddov1ProductExample, 
  retrievalV1ProductExample, 
  convenienceWrapperExample,
  runAllProductExamples 
};
