import { MarketClient as Client } from './client';
import type { Mk20Deal as Deal, Mk20Products as Products, Mk20PDPV1 as PDPV1, Mk20RetrievalV1 as RetrievalV1, Mk20DataSource, Mk20PieceDataFormat } from '../generated';
import { ulid } from 'ulid';

namespace StreamingCommP {
  const NODE_SIZE = 32;
  const NODE_LOG2_SIZE = 5;

  function calculateTreeHeight(boxSize: number): number {
    let leadingZeros = 0;
    let temp = boxSize;
    while (temp > 0) {
      temp = temp >>> 1;
      leadingZeros++;
    }
    leadingZeros = 64 - leadingZeros;
    let treeHeight = 63 - leadingZeros - NODE_LOG2_SIZE;
    if (countOnes(boxSize) !== 1) treeHeight++;
    return treeHeight;
  }

  function countOnes(n: number): number {
    let count = 0;
    while (n > 0) {
      count += n & 1;
      n = n >>> 1;
    }
    return count;
  }

  function varintSize(value: number): number {
    if (value < 0x80) return 1;
    if (value < 0x4000) return 2;
    if (value < 0x200000) return 3;
    if (value < 0x10000000) return 4;
    if (value < 0x800000000) return 5;
    if (value < 0x40000000000) return 6;
    if (value < 0x2000000000000) return 7;
    if (value < 0x100000000000000) return 8;
    return 9;
  }

  function putVarint(buf: Uint8Array, offset: number, value: number): number {
    let n = 0;
    while (value >= 0x80) {
      buf[offset + n] = (value & 0x7f) | 0x80;
      value = value >>> 7;
      n++;
    }
    buf[offset + n] = value & 0x7f;
    return n + 1;
  }

  function bytesToCidString(bytes: Uint8Array): string {
    const base32Chars = 'abcdefghijklmnopqrstuvwxyz234567';
    let result = '';
    let value = 0;
    let bits = 0;
    for (let i = 0; i < bytes.length; i++) {
      value = (value << 8) | bytes[i];
      bits += 8;
      while (bits >= 5) {
        result += base32Chars[(value >>> (bits - 5)) & 31];
        bits -= 5;
      }
    }
    if (bits > 0) {
      result += base32Chars[(value << (5 - bits)) & 31];
    }
    return `b${result}`;
  }

  export function pieceCidV2FromDigest(payloadSize: number, digest: Uint8Array): string {
    let psz = payloadSize;
    if (psz < 127) psz = 127;
    const boxSize = Math.ceil((psz + 126) / 127) * 128;
    const treeHeight = calculateTreeHeight(boxSize);
    const payloadPadding = ((1 << (treeHeight - 2)) * 127) - payloadSize;

    const prefix = new Uint8Array([0x01, 0x55, 0x91, 0x20]);
    const ps = varintSize(payloadPadding);
    const bufSize = prefix.length + 1 + ps + 1 + NODE_SIZE;
    const buf = new Uint8Array(bufSize);

    let n = 0;
    buf.set(prefix, n); n += prefix.length;
    buf[n] = ps + 1 + NODE_SIZE; n++;
    n += putVarint(buf, n, payloadPadding);
    buf[n] = treeHeight; n++;
    buf.set(digest, n);

    return bytesToCidString(buf);
  }
}

/**
 * StreamingPDP provides a streaming workflow to create a deal without a data section,
 * push data via chunked upload, compute the piece CID while streaming, and finalize.
 */
export class StreamingPDP {
  private client: Client;
  private id: string;
  private identifierBytes: number[];
  private totalSize = 0;
  private hashBuffers: Uint8Array[] = [];
  private deal: Deal | undefined;
  private clientAddr: string;
  private providerAddr: string;
  private contractAddress: string;
  private chunkSize: number;
  private buffer: number[] = [];
  private nextChunkNum = 0;
  private uploadedBytes = 0;
  private totalChunks = 0;

  /**
   * @param client - Market client instance
   * @param opts - Streaming options
   * @param opts.client - Client wallet address
   * @param opts.provider - Provider wallet address
   * @param opts.contractAddress - Verification contract address
   * @param opts.chunkSize - Optional chunk size in bytes (default 1MB)
   */
  constructor(client: Client, opts: { client: string; provider: string; contractAddress: string; chunkSize?: number }) {
    this.client = client;
    this.clientAddr = opts.client;
    this.providerAddr = opts.provider;
    this.contractAddress = opts.contractAddress;
    this.chunkSize = opts.chunkSize ?? 1024 * 1024;
    this.id = ulid();
    this.identifierBytes = Array.from(this.id).map(c => c.charCodeAt(0)).slice(0, 16);
    while (this.identifierBytes.length < 16) this.identifierBytes.push(0);
  }

  /**
   * Begin the streaming deal by submitting a deal without data and initializing upload.
   */
  async begin(): Promise<void> {
    var products: Products = {
      pdpV1: {
        createDataSet: true,
        recordKeeper: this.providerAddr,
        extraData: [],
        pieceIds: undefined,
        deleteDataSet: false,
        deletePiece: false,
      } as PDPV1,
      retrievalV1: {
        announcePayload: true,
        announcePiece: true,
        indexing: true,
      } as RetrievalV1,
    } as Products;

    const deal: Deal = {
      identifier: this.id,
      client: this.clientAddr,
      products,
    } as Deal;

    this.deal = deal;
    await this.client.submitDeal(deal);

    await this.client.waitDealComplete(this.id);

    var products: Products = {
      pdpV1: {
        addPiece: true,
        recordKeeper: this.providerAddr,
        extraData: [],
        pieceIds: undefined,
        deleteDataSet: false,
        deletePiece: false,
      } as PDPV1,
      retrievalV1: {
        announcePayload: true,
        announcePiece: true,
        indexing: true,
      } as RetrievalV1,
    } as Products;

    await this.client.submitDeal({
      identifier: this.id,
      client: this.clientAddr,
      products,
    } as Deal);


    await this.client.initializeChunkedUpload(this.id, { chunkSize: this.chunkSize });
  }

  /**
   * Write a chunk of data into the stream. This uploads full chunks immediately
   * and buffers any remainder until the next write or commit.
   * @param chunk - Data bytes to write
   */
  write(chunk: Uint8Array | Buffer): void {
    const u8 = chunk instanceof Uint8Array ? chunk : new Uint8Array(chunk);
    this.totalSize += u8.length;
    // Cross-env hashing fallback: store chunks for hashing at commit using WebCrypto or Node crypto
    this.hashBuffers.push(u8);

    let idx = 0;
    if (this.buffer.length > 0) {
      const needed = this.chunkSize - this.buffer.length;
      const take = Math.min(needed, u8.length);
      for (let i = 0; i < take; i++) this.buffer.push(u8[idx + i]);
      idx += take;
      if (this.buffer.length === this.chunkSize) {
        const toSend = this.buffer.slice(0, this.chunkSize);
        void this.uploadChunkNow(toSend);
        this.buffer = [];
      }
    }

    while (u8.length - idx >= this.chunkSize) {
      const sub = u8.subarray(idx, idx + this.chunkSize);
      const toSend = Array.from(sub);
      void this.uploadChunkNow(toSend);
      idx += this.chunkSize;
    }

    for (let i = idx; i < u8.length; i++) this.buffer.push(u8[i]);
  }

  /**
   * Finalize the streaming deal: flush remaining data, compute piece CID, and finalize.
   * @returns Object containing id (ULID), pieceCid, and totalSize
   */
  async commit(): Promise<{ id: string; pieceCid: string; totalSize: number }> {
    if (!this.deal) throw new Error('StreamingPDP not started. Call begin() first.');

    if (this.buffer.length > 0) {
      const toSend = this.buffer.slice();
      await this.uploadChunkNow(toSend);
      this.buffer = [];
    }

    const digest = await this.computeDigest();
    const pieceCid = StreamingCommP.pieceCidV2FromDigest(this.totalSize, digest);

    const dataSource: Mk20DataSource = {
      pieceCid: { "/": pieceCid } as { [key: string]: string; },
      format: { raw: {} } as Mk20PieceDataFormat,
      sourceHttpPut: { raw_size: this.totalSize } as unknown as object,
    };

    const finalizedDeal: Deal = {
      ...this.deal,
      data: dataSource,
    } as Deal;

    await this.client.finalizeChunkedUpload(this.id, finalizedDeal);

    return { id: this.id, pieceCid, totalSize: this.totalSize };
  }

  /**
   * Upload a single chunk immediately.
   * @param data - Chunk bytes
   */
  private async uploadChunkNow(data: number[]): Promise<void> {
    const chunkNum = String(this.nextChunkNum);
    await this.client.uploadChunk(this.id, chunkNum, data);
    this.nextChunkNum++;
    this.uploadedBytes += data.length;
    this.totalChunks++;
  }

  /**
   * Compute SHA-256 digest of all streamed bytes using WebCrypto in browsers
   * and Node crypto as a fallback in Node environments.
   */
  private async computeDigest(): Promise<Uint8Array> {
    const total = this.hashBuffers.reduce((n, b) => n + b.length, 0);
    const all = new Uint8Array(total);
    let offset = 0;
    for (const b of this.hashBuffers) {
      all.set(b, offset);
      offset += b.length;
    }

    if (typeof globalThis !== 'undefined' && (globalThis as any).crypto && (globalThis as any).crypto.subtle) {
      const h = await (globalThis as any).crypto.subtle.digest('SHA-256', all);
      return new Uint8Array(h);
    }

    try {
      const nodeCrypto = await import('crypto');
      const hasher = nodeCrypto.createHash('sha256');
      hasher.update(Buffer.from(all));
      return new Uint8Array(hasher.digest());
    } catch {
      // Last resort: simple JS fallback (not streaming) using built-in SubtleCrypto if available, else throw
      throw new Error('No available crypto implementation to compute SHA-256 digest in this environment');
    }
  }
}


