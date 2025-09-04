import { Client, MarketClientConfig } from '../src';

const config: MarketClientConfig = {
  basePath: 'http://localhost:8080/market/mk20',
  headers: { 'Authorization': 'Bearer your-token-here' }
};

const client = new Client(config);

// Simple PDPv1 workflow with blob array
async function pdpv1CompleteWorkflowExample() {
  try {
    console.log('🚀 Starting simple PDPv1 workflow...\n');

    // Create mock blobs (in real usage, these would be actual files)
    const mockBlobs = [
      new Blob(['file1 content'], { type: 'text/plain' }),
      new Blob(['file2 content'], { type: 'text/plain' }),
      new Blob(['file3 content'], { type: 'text/plain' })
    ];

    // Submit deal and initialize upload using simplified wrapper
    const result = await client.submitPDPv1DealWithUpload({
      blobs: mockBlobs,
      client: 'f1client123456789abcdefghijklmnopqrstuvwxyz',
      provider: 'f1provider123456789abcdefghijklmnopqrstuvwxyz',
      contractAddress: '0x1234567890123456789012345678901234567890'
    });

    console.log('✅ Deal and upload initialized successfully!');
    console.log('📋 Results:', {
      uuid: result.uuid,
      totalSize: result.totalSize,
      dealId: result.dealId,
      pieceCid: result.pieceCid,
      pieceIds: result.pieceIds
    });

    // Upload data in chunks using the actual blobs
    console.log('\n📤 Starting data upload...');
    const chunkSize = 1024 * 1024; // 1MB chunks
    let totalChunks = 0;
    let uploadedBytes = 0;

    for (const [fileIndex, blob] of mockBlobs.entries()) {
      const fileSize = blob.size;
      const fileChunks = Math.ceil(fileSize / chunkSize);
      
      console.log(`Uploading file ${fileIndex + 1}/${mockBlobs.length} (${fileSize} bytes, ${fileChunks} chunks)...`);
      
      for (let i = 0; i < fileSize; i += chunkSize) {
        const chunk = blob.slice(i, i + chunkSize);
        const chunkNum = totalChunks.toString();
        
        // Convert blob chunk to array of numbers for upload
        const chunkArray = new Uint8Array(await chunk.arrayBuffer());
        const chunkNumbers = Array.from(chunkArray);
        
        console.log(`  Uploading chunk ${chunkNum + 1} (${chunkNumbers.length} bytes)...`);
        await client.uploadChunk(result.uploadId, chunkNum, chunkNumbers);
        
        totalChunks++;
        uploadedBytes += chunkNumbers.length;
      }
    }

    // Finalize upload
    console.log('\n🔒 Finalizing upload...');
    const finalizeResult = await client.finalizeChunkedUpload(result.uploadId);
    console.log(`✅ Upload finalized: ${finalizeResult}`);

    // Check status
    const uploadStatus = await client.getUploadStatus(result.uploadId);
    const dealStatus = await client.getStatus(result.uploadId);
    
    console.log('📈 Upload Status:', uploadStatus);
    console.log('📈 Deal Status:', dealStatus);
    console.log('\n🎉 Workflow completed successfully!');
    
    return result;

  } catch (error) {
    console.error('❌ Workflow failed:', error);
    throw error;
  }
}

export { pdpv1CompleteWorkflowExample };
