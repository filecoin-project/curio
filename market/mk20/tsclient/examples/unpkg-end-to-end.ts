// Set before running:
// PDP_URL=https://andyserver.thepianoexpress.com
// PDP_CLIENT=t1k7ctd3hvmwwjdpb2ipd3kr7n4vk3xzfvzbbdrai         // client wallet
// PDP_PUBLIC_KEY_B64=base64_of_raw_public_key_32_bytes   # ed25519 mode
// PDP_PRIVATE_KEY_B64=base64_of_secret_key_64_or_seed_32 # ed25519 mode
// PDP_KEY_TYPE=ed25519|secp256k1                         # default ed25519
// PDP_SECP_PRIVATE_KEY_HEX=... or PDP_SECP_PRIVATE_KEY_B64=...
// PDP_CONTRACT=0x4A6867D8537f83c1cEae02dF9Df2E31a6c5A1bb6
import { Client, MarketClientConfig, PieceCidUtils, AuthUtils, Ed25519KeypairSigner, Secp256k1AddressSigner } from '../src';

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
  if (process.env.PDP_INSECURE_TLS === '1') {
    // Disable TLS verification (use only for debugging!)
    process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
    console.warn('WARNING: PDP_INSECURE_TLS=1 set. TLS verification disabled.');
  }
    
  const config: MarketClientConfig = {
    serverUrl: process.env.PDP_URL || 'http://localhost:8080',
  } as MarketClientConfig;
  const clientAddr = process.env.PDP_CLIENT || 'f1client...';
  const recordKeeper = process.env.PDP_RECORD_KEEPER || 't1000';
  const contractAddress = process.env.PDP_CONTRACT || '0x0000000000000000000000000000000000000000';

  // Build Authorization header
  const keyType = (process.env.PDP_KEY_TYPE || 'ed25519').toLowerCase();
  let authHeader: string;
  if (keyType === 'ed25519') {
    const pubB64 = process.env.PDP_PUBLIC_KEY_B64 || '';
    const privB64 = process.env.PDP_PRIVATE_KEY_B64 || '';
    if (!pubB64 || !privB64) throw new Error('PDP_PUBLIC_KEY_B64 and PDP_PRIVATE_KEY_B64 must be set for ed25519');
    const pub = Uint8Array.from(Buffer.from(pubB64, 'base64'));
    const priv = Uint8Array.from(Buffer.from(privB64, 'base64'));
    const signer = new Ed25519KeypairSigner(pub, priv);
    authHeader = await AuthUtils.buildAuthHeader(signer, 'ed25519');
  } else if (keyType === 'secp256k1') {
    // Derive pubKeyBase64 from Filecoin address bytes
    const addrStr = clientAddr;
    const { Secp256k1AddressSigner } = require('../src');
    const addrBytes = Secp256k1AddressSigner.addressBytesFromString(addrStr);
    const pubB64 = Buffer.from(addrBytes).toString('base64');
    if (!pubB64) throw new Error('Unable to derive address bytes from PDP_CLIENT');

    // Load secp256k1 private key from env (HEX preferred, else B64)
    const privHex = process.env.PDP_SECP_PRIVATE_KEY_HEX || '';
    const privB64 = process.env.PDP_SECP_PRIVATE_KEY_B64 || '';
    let priv: Uint8Array | undefined;
    if (privHex) {
      const clean = privHex.startsWith('0x') ? privHex.slice(2) : privHex;
      if (clean.length !== 64) throw new Error('PDP_SECP_PRIVATE_KEY_HEX must be 32-byte (64 hex chars)');
      const bytes = new Uint8Array(32);
      for (let i = 0; i < 32; i++) bytes[i] = parseInt(clean.substr(i * 2, 2), 16);
      priv = bytes;
    } else if (privB64) {
      const buf = Buffer.from(privB64, 'base64');
      if (buf.length !== 32) throw new Error('PDP_SECP_PRIVATE_KEY_B64 must decode to 32 bytes');
      priv = new Uint8Array(buf);
    }
    if (!priv) throw new Error('Set PDP_SECP_PRIVATE_KEY_HEX or PDP_SECP_PRIVATE_KEY_B64 for secp256k1 signing');

    // Use Secp256k1AddressSigner (address bytes derived from PDP_CLIENT)
    const signer = new Secp256k1AddressSigner(clientAddr, priv);
    authHeader = await AuthUtils.buildAuthHeader(signer, 'secp256k1');
  } else {
    throw new Error(`Unsupported PDP_KEY_TYPE: ${keyType}`);
  }

  const client = new (require('../src').Client)({
    ...config,
    headers: { Authorization: authHeader },
  } as MarketClientConfig);

  // Debug: show sanitized auth
  const sanitize = (h: string) => h.replace(/:[A-Za-z0-9+/=]{16,}:/, (m) => `:${m.slice(1, 9)}...:`);
  console.log('Auth header (sanitized):', sanitize(authHeader));
  console.log('Server URL:', config.serverUrl);

  // Debug: preflight connectivity
  try {
    const base = config.serverUrl.replace(/\/$/, '');
    const urls: Array<{ url: string; headers?: Record<string, string> }> = [
      { url: `${base}/health` },
      { url: `${base}/market/mk20/info/swagger.json` },
      { url: `${base}/market/mk20/products`, headers: { Authorization: authHeader } },
    ];
    for (const { url, headers } of urls) {
      try {
        const init: RequestInit = headers ? { headers } : {};
        const r = await fetch(url, init);
        console.log(`Preflight ${url}:`, r.status);
        if (!r.ok) {
          const text = await r.text().catch(() => '');
          console.log(`Preflight body (${url}):`, text);
        }
      } catch (e) {
        const err = e as any;
        console.error(`Preflight failed (${url}):`, err?.message || String(e), err?.cause?.code || '', err?.code || '');
      }
    }
  } catch (e) {
    console.error('Preflight orchestrator failed:', (e as Error).message);
  }

  const targetUrl = 'https://unpkg.com/react@18/umd/react.production.min.js';
  console.log(`⬇️  Downloading: ${targetUrl}`);
  const bytes = await downloadFromUnpkg(targetUrl);
  console.log(`   Downloaded ${bytes.length} bytes`);

  const products = await client.getProducts();
  console.log('Products:', products);

  // Compute piece CID locally for retrieval
  const blob = new Blob([Buffer.from(bytes)], { type: 'application/octet-stream' });
  const pieceCid = await PieceCidUtils.computePieceCidV2([blob]);
  console.log(`🔗 Computed piece CID: ${pieceCid}`);

  console.log('📨 Submitting PDPv1 deal and uploading via helper');
  let prep;
  try {
    prep = await client.startPDPv1DealForUpload({
      blobs: [blob],
      client: clientAddr,
      recordKeeper: recordKeeper,
      contractAddress,
    });
    await client.uploadBlobs({ id: prep.id, blobs: [blob], deal: prep.deal });
  } catch (e) {
    console.error('Submit error:', (e as Error).message);
    try {
      const re: any = e as any;
      if (re && re.response) {
        const status = re.response.status;
        const text = await re.response.text().catch(() => '');
        console.error('Submit error status:', status);
        console.error('Submit error body:', text);
      }
    } catch (_) {}

    throw e;
  }
  const uploadId = prep.id;

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
    identifier: uploadId,
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
