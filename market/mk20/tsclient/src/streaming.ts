import { MarketClient as Client } from './client';
import type { Mk20Deal as Deal, Mk20Products as Products, Mk20PDPV1 as PDPV1, Mk20RetrievalV1 as RetrievalV1, Mk20DataSource, Mk20PieceDataFormat } from '../generated';
import { ulid } from 'ulid';
import { createPieceCIDStream, type PieceCID } from './piece';

// Removed old custom implementation - now using piece.ts streaming functions

/**
 * StreamingPDP provides a streaming workflow to create a deal without a data section,
 * push data via chunked upload, compute the piece CID while streaming, and finalize.
 */
export class StreamingPDP {
  private client: Client;
  private id: string;
  private identifierBytes: number[];
  private totalSize = 0;
  private deal: Deal | undefined;
  private clientAddr: string;
  private providerAddr: string;
  private contractAddress: string;
  private chunkSize: number;
  private buffer: number[] = [];
  private nextChunkNum = 0;
  private uploadedBytes = 0;
  private totalChunks = 0;
  private pieceCIDStream: { stream: TransformStream<Uint8Array, Uint8Array>; getPieceCID: () => PieceCID | null };
  private writer: WritableStreamDefaultWriter<Uint8Array>;

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
    
    // Initialize streaming piece CID computation
    this.pieceCIDStream = createPieceCIDStream();
    this.writer = this.pieceCIDStream.stream.writable.getWriter();
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
  async write(chunk: Uint8Array | Buffer): Promise<void> {
    const u8 = chunk instanceof Uint8Array ? chunk : new Uint8Array(chunk);
    this.totalSize += u8.length;
    
    // Write to streaming piece CID computation
    await this.writer.write(u8);

    let idx = 0;
    if (this.buffer.length > 0) {
      const needed = this.chunkSize - this.buffer.length;
      const take = Math.min(needed, u8.length);
      for (let i = 0; i < take; i++) this.buffer.push(u8[idx + i]);
      idx += take;
      if (this.buffer.length === this.chunkSize) {
        const toSend = this.buffer.slice(0, this.chunkSize);
        await this.uploadChunkNow(toSend);
        this.buffer = [];
      }
    }

    while (u8.length - idx >= this.chunkSize) {
      const sub = u8.subarray(idx, idx + this.chunkSize);
      const toSend = Array.from(sub);
      await this.uploadChunkNow(toSend);
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

    // Close the writer and get the piece CID
    await this.writer.close();
    const pieceCID = this.pieceCIDStream.getPieceCID();
    if (!pieceCID) {
      throw new Error('Failed to compute piece CID from stream');
    }
    const pieceCid = pieceCID.toString();

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
   * Clean up resources. Call this when done with the StreamingPDP instance.
   */
  async cleanup(): Promise<void> {
    try {
      await this.writer.close();
    } catch (error) {
      // Writer might already be closed
    }
  }

}


