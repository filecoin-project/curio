// Step 3: Download Piece
// This step downloads the piece using piece CID from step 2
// Set before running:
// PDP_URL=https://andyserver.thepianoexpress.com
// PDP_CLIENT=t1k7ctd3hvmwwjdpb2ipd3kr7n4vk3xzfvzbbdrai         // client wallet
// PDP_PUBLIC_KEY_B64=base64_of_raw_public_key_32_bytes   # ed25519 mode
// PDP_PRIVATE_KEY_B64=base64_of_secret_key_64_or_seed_32 # ed25519 mode
// PDP_KEY_TYPE=ed25519|secp256k1                         # default ed25519
// PDP_SECP_PRIVATE_KEY_HEX=... or PDP_SECP_PRIVATE_KEY_B64=...
// PDP_CONTRACT=0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6

import { getAuthConfigFromEnv, buildAuthHeader, createClient } from './auth';

async function run(pieceCid?: string) {
  console.log('📦 Step 3: Downloading Piece');
  console.log('   REQUIRED INPUT: Piece CID from Step 2');
  
  // Get configuration from environment
  const config = getAuthConfigFromEnv();
  
  // Build authentication
  const authHeader = await buildAuthHeader(config);
  
  // Create authenticated client
  const client = createClient(config, authHeader);
  
  // Use provided pieceCid or get from environment
  const targetPieceCid = pieceCid || process.env.PIECE_CID;
  if (!targetPieceCid) {
    console.error('❌ REQUIRED INPUT MISSING: Piece CID');
    console.error('   This step requires a piece CID from Step 2.');
    console.error('   Either pass as parameter: run("your-piece-cid")');
    console.error('   Or set environment variable: export PIECE_CID=your-piece-cid');
    console.error('');
    console.error('   To get the piece CID, run Step 2 first:');
    console.error('   npx ts-node 2.ts');
    throw new Error('REQUIRED INPUT MISSING: Piece CID from Step 2');
  }
  
  console.log(`   Using piece CID: ${targetPieceCid}`);
  
  // Retrieve piece via market server
  console.log('📦 Retrieving piece via market server...');
  try {
    const base = config.serverUrl.replace(/\/$/, '');
    const url = `${base}/piece/${targetPieceCid}`;
    console.log(`   Retrieval URL: ${url}`);
    
    const r = await fetch(url);
    console.log(`   Retrieval HTTP status: ${r.status}`);
    
    if (r.ok) {
      const retrieved = new Uint8Array(await r.arrayBuffer());
      console.log(`   Retrieved ${retrieved.length} bytes`);
      console.log('✅ Content retrieval: SUCCESS');
      
      return {
        pieceCid: targetPieceCid,
        retrievedBytes: retrieved,
        success: true,
      };
    } else {
      const errorText = await r.text().catch(() => '');
      console.log(`   Retrieval failed with status ${r.status}: ${errorText}`);
      return {
        pieceCid: targetPieceCid,
        success: false,
        error: `HTTP ${r.status}: ${errorText}`,
      };
    }
  } catch (e) {
    console.warn('   Retrieval attempt failed:', (e as Error).message);
    return {
      pieceCid: targetPieceCid,
      success: false,
      error: (e as Error).message,
    };
  }
}

if (require.main === module) {
  run().catch(err => {
    console.error('Step 3 failed:', err);
    process.exit(1);
  });
}

export { run as downloadPiece };
