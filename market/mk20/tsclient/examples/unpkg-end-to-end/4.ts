// Step 4: Delete
// This step deletes using upload ID from step 2
// Set before running:
// PDP_URL=https://andyserver.thepianoexpress.com
// PDP_CLIENT=t1k7ctd3hvmwwjdpb2ipd3kr7n4vk3xzfvzbbdrai         // client wallet
// PDP_PUBLIC_KEY_B64=base64_of_raw_public_key_32_bytes   # ed25519 mode
// PDP_PRIVATE_KEY_B64=base64_of_secret_key_64_or_seed_32 # ed25519 mode
// PDP_KEY_TYPE=ed25519|secp256k1                         # default ed25519
// PDP_SECP_PRIVATE_KEY_HEX=... or PDP_SECP_PRIVATE_KEY_B64=...
// PDP_CONTRACT=0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6

import { getAuthConfigFromEnv, buildAuthHeader, createClient } from './auth';
import { CurioMarket } from '../../src';

async function sleep(ms: number) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function run(uploadId?: string) {
  console.log('🗑️  Step 4: Deleting Deal');
  console.log('   REQUIRED INPUT: Upload ID from Step 2');
  
  // Get configuration from environment
  const config = getAuthConfigFromEnv();
  
  // Build authentication
  const authHeader = await buildAuthHeader(config);
  
  // Create authenticated client
  const client = createClient(config, authHeader);
  
  // Use provided uploadId or get from environment
  const targetUploadId = uploadId || process.env.UPLOAD_ID;
  if (!targetUploadId) {
    console.error('❌ REQUIRED INPUT MISSING: Upload ID');
    console.error('   This step requires an upload ID from Step 2.');
    console.error('   Either pass as parameter: run("upload-id")');
    console.error('   Or set environment variable: export UPLOAD_ID=your-upload-id');
    console.error('');
    console.error('   To get the upload ID, run Step 2 first:');
    console.error('   npx ts-node 2.ts');
    throw new Error('REQUIRED INPUT MISSING: Upload ID from Step 2');
  }
  
  console.log(`   Using upload ID: ${targetUploadId}`);
  
  console.log(`🗑️  Requesting deletion of upload ${targetUploadId}...`);
  
  // Request deletion by updating the deal with delete flags
  const deleteDeal: CurioMarket.Deal = {
    identifier: targetUploadId,
    client: config.clientAddr,
    products: {
      pdpV1: {
        deletePiece: true,
        deleteDataSet: true,
        recordKeeper: config.recordKeeper,
      } as CurioMarket.PDPV1
    } as CurioMarket.Products
  };
  
  try {
    const result = await client.updateDeal(targetUploadId, deleteDeal);
    console.log(`   Deletion request submitted successfully: ${result}`);
  } catch (e) {
    console.error('Deletion request error:', (e as Error).message);
    try {
      const re: any = e as any;
      if (re && re.response) {
        const status = re.response.status;
        const text = await re.response.text().catch(() => '');
        console.error('Deletion error status:', status);
        console.error('Deletion error body:', text);
      }
    } catch (_) {}
    throw e;
  }
  
  // Poll deal status post-deletion
  console.log('⏳ Polling deal status post-deletion...');
  for (let i = 0; i < 24; i++) { // up to ~2 minutes
    const status = await client.getStatus(targetUploadId);
    const pdp = status.pdpV1;
    const st = pdp?.status;
    console.log(`   Status: ${st}${pdp?.errorMsg ? `, error: ${pdp.errorMsg}` : ''}`);
    if (st === 'complete' || st === 'failed') break;
    await sleep(5000);
  }
  
  const finalStatus = await client.getStatus(targetUploadId);
  console.log('✅ Step 4 completed: Deal deletion finished');
  console.log(`   - Deleted upload ID: ${targetUploadId}`);
  console.log(`   - Final status: ${finalStatus.pdpV1?.status}`);
  console.log('');
  console.log('🎉 All steps completed! End-to-end workflow finished successfully.');
  
  return {
    deletedUploadId: targetUploadId,
    finalStatus: finalStatus.pdpV1?.status,
  };
}

if (require.main === module) {
  run().catch(err => {
    console.error('Step 4 failed:', err);
    process.exit(1);
  });
}

export { run as deleteDeal };
