// Set before running:
// PDP_URL=https://andyserver.thepianoexpress.com
// PDP_CLIENT=t1k7ctd3hvmwwjdpb2ipd3kr7n4vk3xzfvzbbdrai         // client wallet
// PDP_PROVIDER=t1k7ctd3hvmwwjdpb2ipd3kr7n4vk3xzfvzbbdrai     // provider wallet
// PDP_TOKEN=your-token-here
// PDP_CONTRACT=0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6
import { Client, MarketClientConfig, PieceCidUtils } from '../src';

async function downloadFromUnpkg(url: string): Promise<Uint8Array> {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Failed to download ${url}: ${res.status} ${res.statusText}`);
  const buf = await res.arrayBuffer();
  return new Uint8Array(buf);
}

async function sleep(ms: number) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function run() {
  const config: MarketClientConfig = {
    serverUrl: process.env.PDP_URL || 'http://localhost:8080',
    headers: process.env.PDP_TOKEN ? { Authorization: `Bearer ${process.env.PDP_TOKEN}` } : undefined,
  } as MarketClientConfig;
  const client = new (require('../src').Client)(config);

  const clientAddr = process.env.PDP_CLIENT || 'f1client...';
  const providerAddr = process.env.PDP_PROVIDER || 'f1provider...';
  const contractAddress = process.env.PDP_CONTRACT || '0x0000000000000000000000000000000000000000';

  const targetUrl = 'https://unpkg.com/react@18/umd/react.production.min.js';
  console.log(`⬇️  Downloading: ${targetUrl}`);
  const bytes = await downloadFromUnpkg(targetUrl);
  console.log(`   Downloaded ${bytes.length} bytes`);

  // Compute piece CID locally for retrieval
  const blob = new Blob([bytes], { type: 'application/octet-stream' });
  const pieceCid = await PieceCidUtils.computePieceCidV2([blob]);
  console.log(`🔗 Computed piece CID: ${pieceCid}`);

  console.log('📨 Submitting PDPv1 deal and uploading via helper');
  const res = await client.submitPDPv1DealWithUpload({
    blobs: [blob],
    client: clientAddr,
    provider: providerAddr,
    contractAddress,
  });
  const uploadId = res.uploadId;

  console.log('⏳ Polling deal status until complete/failed');
  for (let i = 0; i < 120; i++) { // up to ~10 minutes with 5s interval
    const status = await client.getStatus(uploadId);
    const pdp = status.pdpV1;
    const st = pdp?.status;
    console.log(`   status: ${st}${pdp?.errorMsg ? `, error: ${pdp.errorMsg}` : ''}`);
    if (st === 'complete' || st === 'failed') break;
    await sleep(5000);
  }

  console.log('📦 Retrieving piece via market server');
  try {
    const base = config.serverUrl.replace(/\/$/, '');
    const url = `${base}/piece/${pieceCid}`;
    const r = await fetch(url);
    console.log(`   retrieval HTTP ${r.status}`);
    if (r.ok) {
      const retrieved = new Uint8Array(await r.arrayBuffer());
      console.log(`   retrieved ${retrieved.length} bytes`);
    }
  } catch (e) {
    console.warn('   Retrieval attempt failed:', (e as Error).message);
  }

  console.log('🗑️  Requesting deletion (set delete flags via update)');
  await client.updateDeal(uploadId, {
    client: clientAddr,
    products: { pdpV1: { deletePiece: true, deleteDataSet: true } },
  } as any);

  console.log('⏳ Polling deal status post-deletion');
  for (let i = 0; i < 24; i++) { // up to ~2 minutes
    const status = await client.getStatus(uploadId);
    const pdp = status.pdpV1;
    const st = pdp?.status;
    console.log(`   status: ${st}${pdp?.errorMsg ? `, error: ${pdp.errorMsg}` : ''}`);
    if (st === 'complete' || st === 'failed') break;
    await sleep(5000);
  }

  console.log('✅ Example finished');
}

if (require.main === module) {
  run().catch(err => {
    console.error('Example failed:', err);
    process.exit(1);
  });
}

export {};
