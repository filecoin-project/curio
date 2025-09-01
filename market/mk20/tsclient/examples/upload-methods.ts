import { Client, MarketClientConfig, StartUpload } from '../src';

// Example configuration
const config: MarketClientConfig = {
  basePath: 'http://localhost:8080/market/mk20',
  headers: {
    'Authorization': 'Bearer your-token-here'
  }
};

// Create client instance
const client = new Client(config);

// Example 1: Single upload (suitable for small deals)
async function singleUploadExample(dealId: string, data: number[]) {
  try {
    console.log(`Uploading ${data.length} bytes for deal ${dealId}...`);
    await client.uploadData(dealId, data);
    console.log('Single upload completed successfully');
  } catch (error) {
    console.error('Single upload failed:', error);
  }
}

// Example 2: Chunked upload (suitable for large deals)
async function chunkedUploadExample(dealId: string, largeData: number[], chunkSize: number = 1024 * 1024) {
  try {
    console.log(`Starting chunked upload for deal ${dealId}...`);
    
    // Step 1: Initialize the upload
    const startUpload: StartUpload = {
      rawSize: largeData.length,
      chunkSize: chunkSize
    };
    
    const initResult = await client.initializeChunkedUpload(dealId, startUpload);
    console.log('Upload initialized with result:', initResult);
    
    // Step 2: Upload data in chunks
    const chunks: Array<{ chunkNum: string; result: number }> = [];
    for (let i = 0; i < largeData.length; i += chunkSize) {
      const chunk = largeData.slice(i, i + chunkSize);
      const chunkNum = Math.floor(i / chunkSize).toString();
      
      console.log(`Uploading chunk ${chunkNum} (${chunk.length} bytes)...`);
      const uploadResult = await client.uploadChunk(dealId, chunkNum, chunk);
      chunks.push({ chunkNum, result: uploadResult });
      
      // Optional: Check upload status periodically
      if (chunks.length % 10 === 0) {
        const status = await client.getUploadStatus(dealId);
        console.log(`Upload status after ${chunks.length} chunks:`, status);
      }
    }
    
    console.log(`All ${chunks.length} chunks uploaded successfully`);
    
    // Step 3: Finalize the upload
    console.log('Finalizing upload...');
    const finalizeResult = await client.finalizeChunkedUpload(dealId);
    console.log('Upload finalized with result:', finalizeResult);
    
    console.log('Chunked upload completed successfully');
    
  } catch (error) {
    console.error('Chunked upload failed:', error);
  }
}

// Example 3: Parallel chunk uploads for better performance
async function parallelChunkUploadExample(dealId: string, largeData: number[], chunkSize: number = 1024 * 1024) {
  try {
    console.log(`Starting parallel chunked upload for deal ${dealId}...`);
    
    // Step 1: Initialize the upload
    const startUpload: StartUpload = {
      rawSize: largeData.length,
      chunkSize: chunkSize
    };
    
    await client.initializeChunkedUpload(dealId, startUpload);
    console.log('Upload initialized');
    
    // Step 2: Prepare all chunks
    const chunks: Array<{ chunkNum: string; data: number[] }> = [];
    for (let i = 0; i < largeData.length; i += chunkSize) {
      const chunk = largeData.slice(i, i + chunkSize);
      const chunkNum = Math.floor(i / chunkSize).toString();
      chunks.push({ chunkNum, data: chunk });
    }
    
    console.log(`Uploading ${chunks.length} chunks in parallel...`);
    
    // Step 3: Upload chunks in parallel (with concurrency limit)
    const concurrencyLimit = 5; // Limit concurrent requests
    const results: Array<{ chunkNum: string; result: number }> = [];
    
    for (let i = 0; i < chunks.length; i += concurrencyLimit) {
      const batch = chunks.slice(i, i + concurrencyLimit);
      const batchPromises = batch.map(async ({ chunkNum, data }) => {
        const result = await client.uploadChunk(dealId, chunkNum, data);
        return { chunkNum, result };
      });
      
      const batchResults = await Promise.all(batchPromises);
      results.push(...batchResults);
      
      console.log(`Completed batch ${Math.floor(i / concurrencyLimit) + 1}/${Math.ceil(chunks.length / concurrencyLimit)}`);
    }
    
    console.log(`All ${results.length} chunks uploaded successfully`);
    
    // Step 4: Finalize the upload
    console.log('Finalizing upload...');
    const finalizeResult = await client.finalizeChunkedUpload(dealId);
    console.log('Upload finalized with result:', finalizeResult);
    
    console.log('Parallel chunked upload completed successfully');
    
  } catch (error) {
    console.error('Parallel chunked upload failed:', error);
  }
}

// Example 4: Monitor upload progress
async function monitoredUploadExample(dealId: string, data: number[], chunkSize: number = 1024 * 1024) {
  try {
    console.log(`Starting monitored upload for deal ${dealId}...`);
    
    // Initialize upload
    const startUpload: StartUpload = {
      rawSize: data.length,
      chunkSize: chunkSize
    };
    
    await client.initializeChunkedUpload(dealId, startUpload);
    
    // Upload with progress monitoring
    const totalChunks = Math.ceil(data.length / chunkSize);
    let completedChunks = 0;
    
    for (let i = 0; i < data.length; i += chunkSize) {
      const chunk = data.slice(i, i + chunkSize);
      const chunkNum = Math.floor(i / chunkSize).toString();
      
      await client.uploadChunk(dealId, chunkNum, chunk);
      completedChunks++;
      
      // Show progress
      const progress = ((completedChunks / totalChunks) * 100).toFixed(1);
      console.log(`Progress: ${progress}% (${completedChunks}/${totalChunks} chunks)`);
      
      // Check status every 10 chunks
      if (completedChunks % 10 === 0) {
        const status = await client.getUploadStatus(dealId);
        console.log('Current upload status:', status);
      }
    }
    
    // Finalize
    const finalizeResult = await client.finalizeChunkedUpload(dealId);
    console.log('Upload completed and finalized:', finalizeResult);
    
  } catch (error) {
    console.error('Monitored upload failed:', error);
  }
}

// Example 5: Error handling and retry logic
async function robustUploadExample(dealId: string, data: number[], chunkSize: number = 1024 * 1024, maxRetries: number = 3) {
  try {
    console.log(`Starting robust upload for deal ${dealId}...`);
    
    // Initialize upload
    const startUpload: StartUpload = {
      rawSize: data.length,
      chunkSize: chunkSize
    };
    
    await client.initializeChunkedUpload(dealId, startUpload);
    
    // Upload with retry logic
    const totalChunks = Math.ceil(data.length / chunkSize);
    let completedChunks = 0;
    
    for (let i = 0; i < data.length; i += chunkSize) {
      const chunk = data.slice(i, i + chunkSize);
      const chunkNum = Math.floor(i / chunkSize).toString();
      
      let retries = 0;
      let success = false;
      
      while (!success && retries < maxRetries) {
        try {
          await client.uploadChunk(dealId, chunkNum, chunk);
          success = true;
          completedChunks++;
          console.log(`Chunk ${chunkNum} uploaded successfully (${completedChunks}/${totalChunks})`);
        } catch (error) {
          retries++;
          console.warn(`Chunk ${chunkNum} upload failed (attempt ${retries}/${maxRetries}):`, error);
          
          if (retries >= maxRetries) {
            throw new Error(`Failed to upload chunk ${chunkNum} after ${maxRetries} attempts`);
          }
          
          // Wait before retry (exponential backoff)
          await new Promise(resolve => setTimeout(resolve, Math.pow(2, retries) * 1000));
        }
      }
    }
    
    // Finalize
    const finalizeResult = await client.finalizeChunkedUpload(dealId);
    console.log('Robust upload completed successfully:', finalizeResult);
    
  } catch (error) {
    console.error('Robust upload failed:', error);
    throw error;
  }
}

export { 
  singleUploadExample, 
  chunkedUploadExample, 
  parallelChunkUploadExample, 
  monitoredUploadExample, 
  robustUploadExample 
};
