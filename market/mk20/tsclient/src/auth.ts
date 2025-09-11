// Lazy import to avoid hard dependency during build environments without install
let nacl: any;
async function getNacl(): Promise<any> {
  if (!nacl) {
    // eslint-disable-next-line @typescript-eslint/no-var-requires
    nacl = require('tweetnacl');
  }
  return nacl;
}

let nobleSecp: any;
async function getSecp(): Promise<any> {
  if (!nobleSecp) {
    // eslint-disable-next-line @typescript-eslint/no-var-requires
    nobleSecp = require('@noble/secp256k1');
    // Provide sync HMAC-SHA256 for RFC6979 (required by noble's sign)
    try {
      if (!nobleSecp.etc.hmacSha256Sync) {
        // eslint-disable-next-line @typescript-eslint/no-var-requires
        const nodeCrypto = require('crypto');
        const concat = (...arrs: Uint8Array[]) => {
          const total = arrs.reduce((s, a) => s + a.length, 0);
          const out = new Uint8Array(total);
          let off = 0;
          for (const a of arrs) { out.set(a, off); off += a.length; }
          return out;
        };
        nobleSecp.etc.hmacSha256Sync = (key: Uint8Array, ...msgs: Uint8Array[]) => {
          const h = nodeCrypto.createHmac('sha256', Buffer.from(key));
          const all = concat(...msgs);
          h.update(Buffer.from(all));
          return new Uint8Array(h.digest());
        };
      }
    } catch (_) {
      // leave as-is; if not set, noble will throw, which surfaces clearly
    }
  }
  return nobleSecp;
}

/**
 * Utilities to construct Curio Market 2.0 Authorization headers.
 *
 * Header format:
 *   Authorization: "CurioAuth <keyType>:<pubKeyBase64>:<signatureBase64>"
 *
 * - For ed25519:
 *   - pubKeyBase64: base64 of 32-byte raw public key
 *   - signatureBase64: base64 of detached ed25519 signature over sha256(pubKey || RFC3339Hour)
 * - For secp256k1 / bls / delegated: not implemented here (Filecoin signature envelope required)
 */
export class AuthUtils {
  /** Signer interface for pluggable key types. */
  static readonly KEYTYPE_ED25519 = 'ed25519' as const;

  /**
   * Build Authorization header from a provided signer and key type.
   * Currently supports 'ed25519'.
   */
  static async buildAuthHeader(
    signer: AuthSigner,
    keyType: 'ed25519' | 'secp256k1',
    now?: Date,
  ): Promise<string> {
    switch (keyType) {
      case 'ed25519':
        return this.buildEd25519AuthHeader(await signer.getPublicKey(), await signer.sign.bind(signer), now);
      case 'secp256k1': {
        const addrBytes = await signer.getPublicKey();
        const ts = this.rfc3339TruncatedToHour(now);
        const msg = await this.sha256Concat(addrBytes, new TextEncoder().encode(ts));
        const sigEnvelope = await signer.sign(msg); // expected to be Filecoin signature envelope bytes
        const pubB64 = this.toBase64(addrBytes);
        const sigB64 = this.toBase64(sigEnvelope);
        return `CurioAuth secp256k1:${pubB64}:${sigB64}`;
      }
      default:
        throw new Error(`Unsupported key type: ${keyType}`);
    }
  }

  /**
   * Build Authorization header for ed25519 keys.
   * @param publicKeyRaw - 32-byte ed25519 public key (raw)
   * @param privateKeyOrSign - ed25519 private key bytes (64 secretKey or 32 seed),
   *                           OR a sign function (message)=>signature
   * @param now - Optional date used for timestamp; defaults to current time
   * @returns Authorization header value (without the "Authorization: " prefix)
   */
  static async buildEd25519AuthHeader(
    publicKeyRaw: Uint8Array,
    privateKeyOrSign: Uint8Array | ((message: Uint8Array) => Promise<Uint8Array> | Uint8Array),
    now?: Date,
  ): Promise<string> {
    if (publicKeyRaw.length !== 32) {
      throw new Error(`ed25519 publicKey must be 32 bytes, got ${publicKeyRaw.length}`);
    }

    const ts = this.rfc3339TruncatedToHour(now);
    const message = await this.sha256Concat(publicKeyRaw, new TextEncoder().encode(ts));
    let signature: Uint8Array;
    if (typeof privateKeyOrSign === 'function') {
      signature = await privateKeyOrSign(message);
    } else {
      const secretKey = this.ensureEd25519SecretKey(privateKeyOrSign);
      const n = await getNacl();
      signature = n.sign.detached(message, secretKey);
    }

    const pubB64 = this.toBase64(publicKeyRaw);
    const sigB64 = this.toBase64(signature);
    return `CurioAuth ed25519:${pubB64}:${sigB64}`;
  }

  /** Return headers object with Authorization set for ed25519. */
  static async makeAuthHeadersEd25519(
    publicKeyRaw: Uint8Array,
    privateKey: Uint8Array,
    now?: Date,
  ): Promise<Record<string, string>> {
    const value = await this.buildEd25519AuthHeader(publicKeyRaw, privateKey, now);
    return { Authorization: value };
  }

  /** Convert a 32-byte seed or 64-byte secretKey into a 64-byte secretKey. */
  private static ensureEd25519SecretKey(privateKey: Uint8Array): Uint8Array {
    if (privateKey.length === 64) {
      return privateKey;
    }
    if (privateKey.length === 32) {
      const n = require('tweetnacl');
      const kp = n.sign.keyPair.fromSeed(privateKey);
      return kp.secretKey;
    }
    throw new Error(`ed25519 private key must be 32-byte seed or 64-byte secretKey, got ${privateKey.length}`);
  }

  /** RFC3339 timestamp truncated to the hour, always UTC, e.g., 2025-07-15T17:00:00Z */
  static rfc3339TruncatedToHour(date?: Date): string {
    const d = date ? new Date(date) : new Date();
    const y = d.getUTCFullYear();
    const m = (d.getUTCMonth() + 1).toString().padStart(2, '0');
    const day = d.getUTCDate().toString().padStart(2, '0');
    const h = d.getUTCHours().toString().padStart(2, '0');
    return `${y}-${m}-${day}T${h}:00:00Z`;
  }

  /** Compute sha256 over concatenation of two byte arrays. */
  private static async sha256Concat(a: Uint8Array, b: Uint8Array): Promise<Uint8Array> {
    const combined = new Uint8Array(a.length + b.length);
    combined.set(a, 0);
    combined.set(b, a.length);
    // Prefer WebCrypto when available
    if (typeof globalThis !== 'undefined' && (globalThis as any).crypto?.subtle) {
      const hashBuf = await (globalThis as any).crypto.subtle.digest('SHA-256', combined);
      return new Uint8Array(hashBuf);
    }
    // Fallback to Node crypto
    try {
      const nodeCrypto = await import('crypto');
      const hasher = nodeCrypto.createHash('sha256');
      hasher.update(Buffer.from(combined));
      return new Uint8Array(hasher.digest());
    } catch {
      throw new Error('No available crypto implementation to compute SHA-256 digest');
    }
  }

  /** Base64 encode Uint8Array across environments. */
  private static toBase64(bytes: Uint8Array): string {
    if (typeof Buffer !== 'undefined') {
      // Node
      return Buffer.from(bytes).toString('base64');
    }
    // Browser
    let binary = '';
    for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
    return btoa(binary);
  }

  /** Compute BLAKE2b-256 digest (32 bytes). */
  static async blake2b256(data: Uint8Array): Promise<Uint8Array> {
    try {
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      const nodeCrypto = require('crypto');
      try {
        const h = nodeCrypto.createHash('blake2b512', { outputLength: 32 });
        h.update(Buffer.from(data));
        return new Uint8Array(h.digest());
      } catch (_) {
        // fall back to blakejs
      }
    } catch (_) {}
    try {
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      const blake = require('blakejs');
      const out = blake.blake2b(data, undefined, 32);
      return new Uint8Array(out);
    } catch (_) {
      throw new Error('No available BLAKE2b-256 implementation');
    }
  }
}

export default AuthUtils;

// Configuration interface for authentication
export interface AuthConfig {
  serverUrl: string;
  clientAddr: string;
  recordKeeper: string;
  contractAddress: string;
  keyType: 'ed25519' | 'secp256k1';
  publicKeyB64?: string;
  privateKeyB64?: string;
  secpPrivateKeyHex?: string;
  secpPrivateKeyB64?: string;
}

/** Generic signer interface */
export interface AuthSigner {
  getPublicKey(): Promise<Uint8Array> | Uint8Array;
  sign(message: Uint8Array): Promise<Uint8Array> | Uint8Array;
}

/** Ed25519 signer that takes public and private key material at construction */
export class Ed25519KeypairSigner implements AuthSigner {
  private readonly publicKeyRaw: Uint8Array;
  private readonly secretKey: Uint8Array;

  constructor(publicKeyRaw: Uint8Array, privateKey: Uint8Array) {
    if (publicKeyRaw.length !== 32) {
      throw new Error(`ed25519 publicKey must be 32 bytes, got ${publicKeyRaw.length}`);
    }
    this.publicKeyRaw = publicKeyRaw;
    this.secretKey = AuthUtils['ensureEd25519SecretKey'](privateKey);
  }

  getPublicKey(): Uint8Array {
    return this.publicKeyRaw;
  }

  async sign(message: Uint8Array): Promise<Uint8Array> {
    const n = await getNacl();
    return n.sign.detached(message, this.secretKey);
  }
}

/** Secp256k1 signer using a Filecoin address and secp256k1 private key. */
export class Secp256k1AddressSigner implements AuthSigner {
  private readonly addressBytes: Uint8Array;
  private readonly privateKey: Uint8Array;

  /**
   * @param addressString - Filecoin address string (f1/t1)
   * @param privateKey - 32-byte secp256k1 private key (Uint8Array)
   */
  constructor(addressString: string, privateKey: Uint8Array) {
    this.addressBytes = Secp256k1AddressSigner.addressBytesFromString(addressString);
    if (privateKey.length !== 32) {
      throw new Error(`secp256k1 private key must be 32 bytes, got ${privateKey.length}`);
    }
    this.privateKey = privateKey;
  }

  getPublicKey(): Uint8Array {
    // For secp256k1 CurioAuth, the "public key" field is the Filecoin address bytes
    return this.addressBytes;
  }

  /**
   * Produce Filecoin signature envelope bytes: [SigType=0x01] || [65-byte secp256k1 signature (R||S||V)]
   */
  async sign(message: Uint8Array): Promise<Uint8Array> {
    const secp = await getSecp();
    const digest = await AuthUtils.blake2b256(message);
    const sigObj = secp.sign(digest, this.privateKey); // returns Signature with recovery
    const sig = typeof sigObj.toCompactRawBytes === 'function' ? sigObj.toCompactRawBytes() : sigObj.toBytes();
    const recid = sigObj.recovery ?? 0;
    if (!(sig instanceof Uint8Array) || sig.length !== 64) throw new Error('unexpected secp256k1 signature size');
    const data = new Uint8Array(1 + 65);
    data[0] = 0x01; // fcrypto.SigTypeSecp256k1
    data.set(sig, 1);
    data[1 + 64] = recid & 0xff;
    return data;
  }

  /** Parse Filecoin f1/t1 address string to address bytes: [protocol (1)] || payload (20). */
  static addressBytesFromString(address: string): Uint8Array {
    if (!address || address.length < 3) throw new Error('invalid address');
    const net = address[0];
    if (net !== 'f' && net !== 't') throw new Error('invalid network prefix');
    const protoCh = address[1];
    if (protoCh !== '1') throw new Error('unsupported protocol: only secp256k1 (1) supported');
    const b32 = address.slice(2).toLowerCase();
    const decoded = Secp256k1AddressSigner.base32Decode(b32);
    if (decoded.length < 4 + 20) throw new Error('invalid address payload');
    const payload = decoded.slice(0, decoded.length - 4); // drop checksum (last 4 bytes)
    if (payload.length !== 20) throw new Error('invalid secp256k1 payload length');
    const out = new Uint8Array(1 + payload.length);
    out[0] = 0x01; // protocol 1
    out.set(payload, 1);
    return out;
  }

  /** Base32 decode with alphabet 'abcdefghijklmnopqrstuvwxyz234567'. */
  private static base32Decode(s: string): Uint8Array {
    const alphabet = 'abcdefghijklmnopqrstuvwxyz234567';
    const map: Record<string, number> = {};
    for (let i = 0; i < alphabet.length; i++) map[alphabet[i]] = i;
    let bits = 0;
    let value = 0;
    const out: number[] = [];
    for (let i = 0; i < s.length; i++) {
      const ch = s[i];
      if (ch === '=') break;
      const v = map[ch];
      if (v === undefined) throw new Error('invalid base32 character');
      value = (value << 5) | v;
      bits += 5;
      if (bits >= 8) {
        out.push((value >> (bits - 8)) & 0xff);
        bits -= 8;
        value &= (1 << bits) - 1;
      }
    }
    return new Uint8Array(out);
  }
}

// Utility functions for authentication and client management

/**
 * Build authentication header from configuration
 */
export async function buildAuthHeader(config: AuthConfig): Promise<string> {
  if (config.keyType === 'ed25519') {
    if (!config.publicKeyB64 || !config.privateKeyB64) {
      throw new Error('PDP_PUBLIC_KEY_B64 and PDP_PRIVATE_KEY_B64 must be set for ed25519');
    }
    const pub = Uint8Array.from(Buffer.from(config.publicKeyB64, 'base64'));
    const priv = Uint8Array.from(Buffer.from(config.privateKeyB64, 'base64'));
    const signer = new Ed25519KeypairSigner(pub, priv);
    return await AuthUtils.buildAuthHeader(signer, 'ed25519');
  } else if (config.keyType === 'secp256k1') {
    // Derive pubKeyBase64 from Filecoin address bytes
    const addrBytes = Secp256k1AddressSigner.addressBytesFromString(config.clientAddr);
    const pubB64 = Buffer.from(addrBytes).toString('base64');
    if (!pubB64) throw new Error('Unable to derive address bytes from PDP_CLIENT');

    // Load secp256k1 private key from env (HEX preferred, else B64)
    let priv: Uint8Array | undefined;
    if (config.secpPrivateKeyHex) {
      const clean = config.secpPrivateKeyHex.startsWith('0x') ? config.secpPrivateKeyHex.slice(2) : config.secpPrivateKeyHex;
      if (clean.length !== 64) throw new Error('PDP_SECP_PRIVATE_KEY_HEX must be 32-byte (64 hex chars)');
      const bytes = new Uint8Array(32);
      for (let i = 0; i < 32; i++) bytes[i] = parseInt(clean.substr(i * 2, 2), 16);
      priv = bytes;
    } else if (config.secpPrivateKeyB64) {
      const buf = Buffer.from(config.secpPrivateKeyB64, 'base64');
      if (buf.length !== 32) throw new Error('PDP_SECP_PRIVATE_KEY_B64 must decode to 32 bytes');
      priv = new Uint8Array(buf);
    }
    if (!priv) throw new Error('Set PDP_SECP_PRIVATE_KEY_HEX or PDP_SECP_PRIVATE_KEY_B64 for secp256k1 signing');

    // Use Secp256k1AddressSigner (address bytes derived from PDP_CLIENT)
    const signer = new Secp256k1AddressSigner(config.clientAddr, priv);
    return await AuthUtils.buildAuthHeader(signer, 'secp256k1');
  } else {
    throw new Error(`Unsupported PDP_KEY_TYPE: ${config.keyType}`);
  }
}

/**
 * Create authenticated client from configuration and auth header
 */
export function createClient(config: AuthConfig, authHeader: string): any {
  const clientConfig = {
    serverUrl: config.serverUrl,
    headers: { Authorization: authHeader },
  };
  // Use the same pattern as the original unpkg-end-to-end.ts file
  return new (require('./client').MarketClient)(clientConfig);
}

/**
 * Sanitize auth header for logging (removes sensitive signature data)
 */
export function sanitizeAuthHeader(authHeader: string): string {
  return authHeader.replace(/:[A-Za-z0-9+/=]{16,}:/, (m) => `:${m.slice(1, 9)}...:`);
}

/**
 * Run preflight connectivity checks
 */
export async function runPreflightChecks(config: AuthConfig, authHeader: string): Promise<void> {
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
}


