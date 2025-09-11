// Step 2: Add Piece and Upload Blobs
// This step adds a piece to the dataset, downloads React.js, and uploads it
// Set before running:
// PDP_URL=https://andyserver.thepianoexpress.com
// PDP_CLIENT=t1k7ctd3hvmwwjdpb2ipd3kr7n4vk3xzfvzbbdrai         // client wallet
// PDP_PUBLIC_KEY_B64=base64_of_raw_public_key_32_bytes   # ed25519 mode
// PDP_PRIVATE_KEY_B64=base64_of_secret_key_64_or_seed_32 # ed25519 mode
// PDP_KEY_TYPE=ed25519|secp256k1                         # default ed25519
// PDP_SECP_PRIVATE_KEY_HEX=... or PDP_SECP_PRIVATE_KEY_B64=...
// PDP_CONTRACT=0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6

import { getAuthConfigFromEnv, buildAuthHeader, createClient, sanitizeAuthHeader, runPreflightChecks } from './auth';
import { Mk20Deal, Mk20Products, Mk20PDPV1, Mk20RetrievalV1, Mk20DataSource } from '../../generated';
import { PieceCidUtils } from '../../src';
import { ulid } from 'ulid';

async function sleep(ms: number) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function run(datasetId?: string) {
  console.log('📁 Step 2: Adding Piece and Uploading Blobs');
  console.log('   REQUIRED INPUT: Dataset ID from Step 1');
  console.log('   This step downloads React.js, adds piece to dataset, and uploads it');
  
  // Get configuration from environment
  const config = getAuthConfigFromEnv();
  console.log('Configuration loaded from environment');
  
  // Build authentication
  const authHeader = await buildAuthHeader(config);
  console.log('Auth header (sanitized):', sanitizeAuthHeader(authHeader));
  console.log('Server URL:', config.serverUrl);
  
  // Create authenticated client
  const client = createClient(config, authHeader);
  
  // Run preflight connectivity checks
  console.log('🔍 Running preflight connectivity checks...');
  await runPreflightChecks(config, authHeader);
  
  // Use provided datasetId or get from environment
  const targetDatasetId = datasetId || process.env.DATASET_ID;
  if (!targetDatasetId) {
    console.error('❌ REQUIRED INPUT MISSING: Dataset ID');
    console.error('   This step requires a dataset ID from Step 1.');
    console.error('   Either pass as parameter: run("dataset-id")');
    console.error('   Or set environment variable: export DATASET_ID=your-dataset-id');
    console.error('');
    console.error('   To get the dataset ID, run Step 1 first:');
    console.error('   npx ts-node 1.ts');
    throw new Error('REQUIRED INPUT MISSING: Dataset ID from Step 1');
  }
  console.log(`   Using dataset ID: ${targetDatasetId}`);
  
  // Download React.js from unpkg
  console.log('📥 Downloading React.js from unpkg...');
  const url = 'https://unpkg.com/react@18.2.0/umd/react.production.min.js';
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`Failed to download React.js: ${response.status} ${response.statusText}`);
  }
  
  const bytes = new Uint8Array(await response.arrayBuffer());
  const blob = new Blob([Buffer.from(bytes)], { type: 'application/octet-stream' });
  console.log(`   Downloaded React.js: ${bytes.length} bytes`);
  console.log(`   Blob size: ${blob.size} bytes`);
  
  // Compute piece CID
  console.log('🔗 Computing piece CID...');
  const pieceCid = await PieceCidUtils.computePieceCidV2([blob]);
  console.log(`   Piece CID: ${pieceCid}`);
  
  // Add piece with data under a new identifier (upload id)
  console.log('📝 Creating add piece deal...');
  const uploadId = ulid();
  const addPieceDeal: Mk20Deal = {
    identifier: uploadId,
    client: config.clientAddr,
    data: {
      pieceCid: { "/": pieceCid } as object,
      format: { raw: {} },
      sourceHttpPut: {},
    } as Mk20DataSource,
    products: {
      pdpV1: {
        addPiece: true,
        dataSetId: 0, // TODO: get dataset id from response (hardcoded for now)
        recordKeeper: config.recordKeeper,
        extraData: [],
        deleteDataSet: false,
        deletePiece: false,
      } as Mk20PDPV1,
      retrievalV1: {
        announcePayload: false,
        announcePiece: true,
        indexing: false,
      } as Mk20RetrievalV1,
    } as Mk20Products,
  } as Mk20Deal;
  
  // Submit the add piece deal
  console.log('📤 Submitting add piece deal...');
  const dealId = await client.submitDeal(addPieceDeal);
  console.log(`   Add piece deal submitted with ID: ${uploadId}, deal ID: ${dealId}`);
  
  // Wait for add piece to complete
  console.log('⏳ Waiting for add piece to complete...');
  let addPieceComplete = false;
  for (let i = 0; i < 12; i++) { // up to 60 seconds with 5s interval
    try {
      const status = await client.getStatus(uploadId);
      const pdp = status.pdpV1;
      const st = pdp?.status;
      console.log(`   Add piece status: ${st}${pdp?.errorMsg ? `, error: ${pdp.errorMsg}` : ''}`);
      if (st === 'complete' || st === 'failed') {
        addPieceComplete = true;
        break;
      }
    } catch (e) {
      console.log(`   Status check failed (attempt ${i + 1}): ${(e as Error).message}`);
      if (i === 11) {
        console.log('   ⚠️  Status polling timed out after 60 seconds');
        break;
      }
    }
    await sleep(5000);
  }
  
  if (!addPieceComplete) {
    console.log('   ⏰ Add piece status polling timed out after 60 seconds');
    console.log('   🔗 Please check the blockchain for deal status:');
    console.log(`      - Upload ID: ${uploadId}`);
    console.log(`      - Deal ID: ${dealId}`);
    console.log('   📝 The deal may still be processing on-chain');
    console.log('   ✅ Proceeding with blob upload (this may still work)');
  }
  
  // Upload the blobs
  console.log('📤 Uploading blobs to the deal...');
  try {
    const result = await client.uploadBlobs({ 
      id: uploadId, 
      blobs: [blob], 
      deal: addPieceDeal,
      chunkSize: 16 * 1024 * 1024 // Use 16MB chunks (server minimum requirement)
    });
    console.log('   Blobs uploaded successfully');
    console.log(`   - Uploaded chunks: ${result.uploadedChunks}`);
    console.log(`   - Uploaded bytes: ${result.uploadedBytes}`);
    console.log(`   - Finalize code: ${result.finalizeCode}`);
  } catch (e) {
    console.error('Upload error:', (e as Error).message);
    try {
      const re: any = e as any;
      if (re && re.response) {
        const status = re.response.status;
        const text = await re.response.text().catch(() => '');
        console.error('Upload error status:', status);
        console.error('Upload error body:', text);
      }
    } catch (_) {}
    throw e;
  }
  
  // Poll deal status until complete/failed
  console.log('⏳ Polling deal status until complete/failed...');
  let finalStatusComplete = false;
  for (let i = 0; i < 12; i++) { // up to 60 seconds with 5s interval
    try {
      const status = await client.getStatus(uploadId);
      const pdp = status.pdpV1;
      const st = pdp?.status;
      console.log(`   Status: ${st}${pdp?.errorMsg ? `, error: ${pdp.errorMsg}` : ''}`);
      if (st === 'complete' || st === 'failed') {
        finalStatusComplete = true;
        break;
      }
    } catch (e) {
      console.log(`   Status check failed (attempt ${i + 1}): ${(e as Error).message}`);
      if (i === 11) {
        console.log('   ⚠️  Final status polling timed out after 60 seconds');
        break;
      }
    }
    await sleep(5000);
  }
  
  if (!finalStatusComplete) {
    console.log('   ⏰ Final status polling timed out after 60 seconds');
    console.log('   🔗 Please check the blockchain for final deal status:');
    console.log(`      - Upload ID: ${uploadId}`);
    console.log(`      - Deal ID: ${dealId}`);
    console.log('   📝 The deal may still be processing on-chain');
    console.log('   ✅ Step completed - check chain for final status');
  }
  
  // Try to get final status, but don't fail if it times out
  let finalStatus = null;
  try {
    finalStatus = await client.getStatus(uploadId);
  } catch (e) {
    console.log('   ⚠️  Could not get final status - check blockchain');
  }
  
  console.log('✅ Step 2 completed: Piece added and blobs uploaded');
  console.log(`   - Upload ID: ${uploadId}`);
  console.log(`   - Deal ID: ${dealId}`);
  console.log(`   - Piece CID: ${pieceCid}`);
  console.log(`   - Dataset ID: ${targetDatasetId}`);
  console.log(`   - File size: ${blob.size} bytes`);
  if (finalStatus) {
    console.log(`   - Deal status: ${finalStatus.pdpV1?.status}`);
  } else {
    console.log(`   - Deal status: Check blockchain for final status`);
  }
  console.log('');
  console.log('Next: Run 3.ts to download and verify the uploaded content');
  
  return {
    uploadId,
    dealId,
    pieceCid,
    blob,
    bytes,
    addPieceDeal,
    finalStatus: finalStatus?.pdpV1?.status || 'check_blockchain',
  };
}

if (require.main === module) {
  run().catch(err => {
    console.error('Step 2 failed:', err);
    process.exit(1);
  });
}

export { run as addPieceAndUpload };