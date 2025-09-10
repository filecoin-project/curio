// Step 1: Create Dataset
// This step creates a PDPv1 dataset (first part of startPDPv1DealForUpload)
// Set before running:
// PDP_URL=https://andyserver.thepianoexpress.com
// PDP_CLIENT=t1k7ctd3hvmwwjdpb2ipd3kr7n4vk3xzfvzbbdrai         // client wallet
// PDP_PUBLIC_KEY_B64=base64_of_raw_public_key_32_bytes   # ed25519 mode
// PDP_PRIVATE_KEY_B64=base64_of_secret_key_64_or_seed_32 # ed25519 mode
// PDP_KEY_TYPE=ed25519|secp256k1                         # default ed25519
// PDP_SECP_PRIVATE_KEY_HEX=... or PDP_SECP_PRIVATE_KEY_B64=...
// PDP_CONTRACT=0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6

import { getAuthConfigFromEnv, buildAuthHeader, createClient, sanitizeAuthHeader, runPreflightChecks } from './auth';
import { Mk20Deal, Mk20Products, Mk20PDPV1, Mk20RetrievalV1 } from '../../generated';
import { ulid } from 'ulid';

async function sleep(ms: number) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function run() {
  console.log('🚀 Step 1: Creating PDPv1 Dataset');
  console.log('   This is the first step and requires no inputs.');
  
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
  
  // Create dataset with a fresh identifier (first part of startPDPv1DealForUpload)
  console.log('📝 Creating PDPv1 dataset...');
  const datasetId = ulid();
  const createDeal: Mk20Deal = {
    identifier: datasetId,
    client: config.clientAddr,
    products: {
      pdpV1: {
        createDataSet: true,
        addPiece: false,
        recordKeeper: config.recordKeeper,
        extraData: [],
        deleteDataSet: false,
        deletePiece: false,
      } as Mk20PDPV1,
      retrievalV1: {
        announcePayload: true,
        announcePiece: true,
        indexing: true,
      } as Mk20RetrievalV1,
    } as Mk20Products,
  } as Mk20Deal;
  
  // Submit the dataset creation deal
  console.log('📤 Submitting dataset creation deal...');
  await client.submitDeal(createDeal);
  console.log(`   Dataset creation deal submitted with ID: ${datasetId}`);
  
  // Wait for dataset creation to complete
  console.log('⏳ Waiting for dataset creation to complete...');
  for (let i = 0; i < 60; i++) { // up to ~5 minutes with 5s interval
    const status = await client.getStatus(datasetId);
    const pdp = status.pdpV1;
    const st = pdp?.status;
    console.log(`   Dataset status: ${st}${pdp?.errorMsg ? `, error: ${pdp.errorMsg}` : ''}`);
    if (st === 'complete' || st === 'failed') break;
    await sleep(5000);
  }
  
  console.log('✅ Step 1 completed: PDPv1 dataset created');
  console.log(`   - Dataset ID: ${datasetId}`);
  console.log(`   - Client: ${config.clientAddr}`);
  console.log(`   - Record Keeper: ${config.recordKeeper}`);
  console.log('');
  console.log('Next: Run 2.ts to add piece to the dataset');
  
  return {
    datasetId,
    config,
  };
}

if (require.main === module) {
  run().catch(err => {
    console.error('Step 1 failed:', err);
    process.exit(1);
  });
}

export { run as startDeal };
