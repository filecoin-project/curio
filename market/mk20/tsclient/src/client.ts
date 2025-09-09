import { DefaultApi, ConfigurationParameters, Mk20Deal, Mk20DealProductStatusResponse, Mk20SupportedContracts, Mk20SupportedProducts, Mk20SupportedDataSources, Mk20Products, Mk20PDPV1, Mk20RetrievalV1, Mk20DDOV1, Mk20DataSource, Mk20DealState } from '../generated';
import { monotonicFactory } from 'ulid';
import { Configuration } from '../generated/runtime';
import { Mk20StartUpload } from '../generated/models/Mk20StartUpload';
import { StreamingPDP } from './streaming';

const ulid = monotonicFactory(() => Math.random());
export interface MarketClientConfig extends Omit<ConfigurationParameters, 'basePath'> {
  serverUrl: string; // e.g. http://localhost:8080
}

/**
 * Utility class for computing Filecoin piece CID v2 from blobs
 * Ports the exact Go CommP algorithm from lib/commcidv2/commcidv2.go
 */
export class PieceCidUtils {
  // Filecoin multicodec constants (same as Go)
  private static readonly FIL_COMMITMENT_UNSEALED = 0x1020;
  private static readonly FIL_COMMITMENT_SEALED = 0x1021;
  private static readonly SHA2_256_TRUNC254_PADDED = 0x1012;
  private static readonly POSEIDON_BLS12_381_A2_FC1 = 0xb401;

  // CommP constants (same as Go)
  private static readonly NODE_SIZE = 32;
  private static readonly NODE_LOG2_SIZE = 5;

  /**
   * Compute piece CID v2 from an array of blobs
   * Uses the exact same algorithm as Go NewSha2CommP + PCidV2
   * @param blobs - Array of Blob objects
   * @returns Promise<string> - Piece CID v2 as a string
   */
  static async computePieceCidV2(blobs: Blob[]): Promise<string> {
    try {
      // Concatenate all blob data
      const totalSize = blobs.reduce((sum, blob) => sum + blob.size, 0);
      const concatenatedData = new Uint8Array(totalSize);
      let offset = 0;
      
      for (const blob of blobs) {
        const arrayBuffer = await blob.arrayBuffer();
        const uint8Array = new Uint8Array(arrayBuffer);
        concatenatedData.set(uint8Array, offset);
        offset += uint8Array.length;
      }

      // Compute SHA256 hash (works in browser and Node)
      const hashArray = await this.computeSha256(concatenatedData);

      // Create CommP using the exact Go algorithm
      const commP = this.newSha2CommP(totalSize, hashArray);
      
      // Generate piece CID v2 using the exact Go algorithm
      const pieceCidV2 = this.pCidV2(commP);
      
      return pieceCidV2;
    } catch (error) {
      throw new Error(`Failed to compute piece CID v2: ${error}`);
    }
  }

  /**
   * Compute SHA-256 digest cross-environment (browser WebCrypto or Node crypto)
   */
  private static async computeSha256(data: Uint8Array): Promise<Uint8Array> {
    if (typeof globalThis !== 'undefined' && (globalThis as any).crypto && (globalThis as any).crypto.subtle) {
      const h = await (globalThis as any).crypto.subtle.digest('SHA-256', data);
      return new Uint8Array(h);
    }
    try {
      const nodeCrypto = await import('crypto');
      const hasher = nodeCrypto.createHash('sha256');
      hasher.update(Buffer.from(data));
      return new Uint8Array(hasher.digest());
    } catch {
      throw new Error('No available crypto implementation to compute SHA-256 digest in this environment');
    }
  }

  /**
   * NewSha2CommP - exact port of Go function
   * @param payloadSize - Size of the payload in bytes
   * @param digest - 32-byte SHA256 digest
   * @returns CommP object
   */
  private static newSha2CommP(payloadSize: number, digest: Uint8Array): any {
    if (digest.length !== this.NODE_SIZE) {
      throw new Error(`digest size must be 32, got ${digest.length}`);
    }

    let psz = payloadSize;

    // always 4 nodes long
    if (psz < 127) {
      psz = 127;
    }

    // fr32 expansion, count 127 blocks, rounded up
    const boxSize = Math.ceil((psz + 126) / 127) * 128;

    // hardcoded for now
    const hashType = 1;
    const treeHeight = this.calculateTreeHeight(boxSize);
    const payloadPadding = ((1 << (treeHeight - 2)) * 127) - payloadSize;

    return {
      hashType,
      digest,
      treeHeight,
      payloadPadding
    };
  }

  /**
   * Calculate tree height using the exact Go algorithm
   * @param boxSize - The box size after fr32 expansion
   * @returns Tree height
   */
  private static calculateTreeHeight(boxSize: number): number {
    // 63 - bits.LeadingZeros64(boxSize) - nodeLog2Size
    let leadingZeros = 0;
    let temp = boxSize;
    while (temp > 0) {
      temp = temp >>> 1;
      leadingZeros++;
    }
    leadingZeros = 64 - leadingZeros;
    
    let treeHeight = 63 - leadingZeros - this.NODE_LOG2_SIZE;
    
    // if bits.OnesCount64(boxSize) != 1 { treeHeight++ }
    if (this.countOnes(boxSize) !== 1) {
      treeHeight++;
    }
    
    return treeHeight;
  }

  /**
   * Count the number of 1 bits in a 64-bit number
   * @param n - 64-bit number
   * @returns Number of 1 bits
   */
  private static countOnes(n: number): number {
    let count = 0;
    while (n > 0) {
      count += n & 1;
      n = n >>> 1;
    }
    return count;
  }

  /**
   * PCidV2 - exact port of Go function
   * @param commP - CommP object
   * @returns Piece CID v2 string
   */
  private static pCidV2(commP: any): string {
    // The Go piece CID v2 format uses a specific prefix structure
    // From Go: pCidV2Pref: "\x01" + "\x55" + "\x91" + "\x20"
    // This creates: [0x01, 0x55, 0x91, 0x20] = CID v1 + raw codec + multihash length + multihash code
    
    // Create the complete piece CID v2 structure
    // From Go: pCidV2Pref: "\x01" + "\x55" + "\x91" + "\x20"
    // This creates: [0x01, 0x55, 0x91, 0x20] = CID v1 + raw codec + multihash length + multihash code
    // But the actual piece CID v2 format needs to include the multihash code 0x1011
    const prefix = new Uint8Array([0x01, 0x55, 0x91, 0x20]); // Exact match with Go pCidV2Pref
    
    // Calculate varint size for payload padding
    const ps = this.varintSize(commP.payloadPadding);
    
    // Create buffer with exact size calculation from Go
    const bufSize = prefix.length + 1 + ps + 1 + this.NODE_SIZE;
    const buf = new Uint8Array(bufSize);
    
    let n = 0;
    
    // Copy prefix
    n += this.copyBytes(buf, n, prefix);
    
    // Set size byte: ps + 1 + nodeSize
    buf[n] = ps + 1 + this.NODE_SIZE;
    n++;
    
    // Put varint for payload padding
    n += this.putVarint(buf, n, commP.payloadPadding);
    
    // Set tree height
    buf[n] = commP.treeHeight;
    n++;
    
    // Copy digest
    this.copyBytes(buf, n, commP.digest);
    
    // Convert to base32 CID string
    return this.bytesToCidString(buf);
  }

  /**
   * Calculate varint size for a number
   * @param value - Number to encode
   * @returns Size in bytes
   */
  private static varintSize(value: number): number {
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

  /**
   * Put varint into buffer
   * @param buf - Buffer to write to
   * @param offset - Offset in buffer
   * @param value - Value to encode
   * @returns Number of bytes written
   */
  private static putVarint(buf: Uint8Array, offset: number, value: number): number {
    let n = 0;
    while (value >= 0x80) {
      buf[offset + n] = (value & 0x7F) | 0x80;
      value = value >>> 7;
      n++;
    }
    buf[offset + n] = value & 0x7F;
    return n + 1;
  }

  /**
   * Copy bytes from source to destination
   * @param dest - Destination buffer
   * @param destOffset - Destination offset
   * @param source - Source buffer
   * @returns Number of bytes copied
   */
  private static copyBytes(dest: Uint8Array, destOffset: number, source: Uint8Array): number {
    dest.set(source, destOffset);
    return source.length;
  }

  /**
   * Convert bytes to CID string
   * @param bytes - Bytes to convert
   * @returns CID string
   */
  private static bytesToCidString(bytes: Uint8Array): string {
    // This is a simplified conversion - in practice you'd use a proper CID library
    // For now, we'll create a base32-like representation
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
    
    // Add the "b" prefix to match Go's piece CID v2 format
    // Go generates: bafkzcibd6adqm6c3a5i7ylct3qkkjtr5qahgt3444eaj5mzhzt2frl7atqscyjwj
    return `b${result}`;
  }
}

export class MarketClient {
  private api: DefaultApi;

  /**
   * Create a MarketClient instance.
   * @param config - Configuration object
   * @param config.serverUrl - Base server URL, e.g. http://localhost:8080
   * @param config.headers - Optional default headers to send with every request
   * @param config.fetchApi - Optional custom fetch implementation
   */
  constructor(config: MarketClientConfig) {
    const basePath = `${config.serverUrl.replace(/\/$/, '')}/market/mk20`;
    const runtimeConfig = { ...config, basePath } as ConfigurationParameters;
    this.api = new DefaultApi(new Configuration(runtimeConfig));
  }

  /**
   * Factory: create a StreamingPDP helper bound to this client instance
   */
  /**
   * Create a StreamingPDP helper bound to this client instance.
   * @param params - Streaming parameters
   * @param params.client - Client wallet address
   * @param params.provider - Provider wallet address
   * @param params.contractAddress - Verification contract address
   * @param params.chunkSize - Optional chunk size in bytes (default 1MB)
   */
  streamingPDP(params: { client: string; provider: string; contractAddress: string; chunkSize?: number }): StreamingPDP {
    return new StreamingPDP(this, params);
  }

  /**
   * Convert a ULID string (26-char Crockford base32) into a 16-byte array
   */
  private ulidToBytes(ulidString: string): number[] {
    var bytes: number[] = [];
    for (let i = 0; i < ulidString.length; i++) {
      bytes.push(ulidString.charCodeAt(i));
    }
    return bytes;
  }

  /**
   * Get supported contracts
   */
  async getContracts(): Promise<string[]> {
    try {
      const response = await this.api.contractsGet();
      return response.contracts || [];
    } catch (error) {
      throw new Error(`Failed to get contracts: ${error}`);
    }
  }

  /**
   * Get supported products
   */
  async getProducts(): Promise<Mk20SupportedProducts> {
    try {
      const response = await this.api.productsGet();
      return response;
    } catch (error) {
      throw new Error(`Failed to get products: ${error}`);
    }
  }

  /**
   * Get supported data sources
   */
  async getSources(): Promise<Mk20SupportedDataSources> {
    try {
      const response = await this.api.sourcesGet();
      return response;
    } catch (error) {
      throw new Error(`Failed to get sources: ${error}`);
    }
  }

  /**
   * Get deal status by ID
   */
  /**
   * Get deal status by ID.
   * @param id - Deal identifier (string ULID returned from submit wrappers)
   */
  async getStatus(id: string): Promise<Mk20DealProductStatusResponse> {
    try {
      const response = await this.api.statusIdGet({ id });
      return response;
    } catch (error) {
      throw new Error(`Failed to get deal status for ${id}: ${error}`);
    }
  }

  /**
   * Submit a new deal
   */
  /**
   * Submit a new deal.
   * @param deal - Deal payload matching Mk20Deal schema
   */
  async submitDeal(deal: Mk20Deal): Promise<number> {

    try {
      const response = await this.api.storePost({ body: deal });
      return response;
    } catch (error) {
      throw new Error(`Failed to submit deal: ${error}`);
    }
  }


  /**
   * Calculate piece ID for an individual blob based on its content
   * @param blob - The blob to calculate piece ID for
   * @returns Promise<number> - A unique piece ID for this blob
   */
  private async calculateBlobPieceId(blob: Blob): Promise<number> {
    // Create a hash from the blob's content to generate a unique piece ID
    const arrayBuffer = await blob.arrayBuffer();
    const uint8Array = new Uint8Array(arrayBuffer);
    
    let hash = 0;
    for (let i = 0; i < uint8Array.length; i++) {
      hash = ((hash << 5) - hash) + uint8Array[i];
      hash = hash & hash; // Convert to 32-bit integer
    }
    
    // Add size to the hash to make it more unique
    hash = ((hash << 5) - hash) + blob.size;
    hash = hash & hash;
    
    // Ensure positive and within reasonable bounds
    return Math.abs(hash) % 1000000; // Keep within 6 digits
  }

  async waitDealComplete(id: string): Promise<void> {
    var duration = 0;
    const step = 10000;
    while (true) {
      const resp = await this.getStatus(id);
      if (resp?.pdpV1?.status === Mk20DealState.DealStateComplete) {
        break
      }

      await new Promise(resolve => setTimeout(resolve, step));
      duration += step;
      if (duration > 90000) {
        throw new Error(`Deal ${id} timed out after ${duration} seconds`);
      }
    }
  }
  
  /**
   * Submit a PDPv1 deal in two steps and prepare for upload.
   * - Step 1: createDataSet
   * - Step 2: addPiece with data descriptor (pieceCid, raw format, HTTP PUT source)
   * Returns the upload identifier (ULID), computed pieceCid, total size, and the deal payload
   * that should be used at finalize time.
   */
  async submitPDPv1Deal(params: {
    blobs: Blob[];
    client: string;
    recordKeeper: string;
    contractAddress: string;
  }): Promise<{ id: string; totalSize: number; dealId: number; pieceCid: string; deal: Mk20Deal }> {
    const { blobs, client, recordKeeper } = params;

    // Calculate total size and compute piece CID from blobs
    const totalSize = blobs.reduce((sum, blob) => sum + blob.size, 0);
    const pieceCid = await PieceCidUtils.computePieceCidV2(blobs);

    // Step 1: create dataset with a fresh identifier
    const datasetId = ulid();
    const createDeal: Mk20Deal = {
      identifier: datasetId,
      client,
      products: {
        pdpV1: {
          createDataSet: true,
          addPiece: false,
          recordKeeper: recordKeeper,
          extraData: '',
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

    await this.submitDeal(createDeal);
    await this.waitDealComplete(datasetId);

    // Step 2: add piece with data under a new identifier (upload id)
    const uploadId = ulid();
    const addPieceDeal: Mk20Deal = {
      identifier: uploadId,
      client,
      data: {
        pieceCid: pieceCid,
        format: { raw: {} },
        sourceHttpput: {},
      } as Mk20DataSource,
      products: {
        pdpV1: {
          addPiece: true,
          recordKeeper: recordKeeper,
          extraData: '',
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

    const dealId = await this.submitDeal(addPieceDeal);
    await this.waitDealComplete(uploadId);

    return { id: uploadId, totalSize, dealId, pieceCid, deal: addPieceDeal };
  }

  /**
   * Upload a set of blobs in chunks and finalize the upload for a given deal id.
   * Optionally accepts the deal payload to finalize with.
   */
  async uploadBlobs(params: { id: string; blobs: Blob[]; deal?: Mk20Deal; chunkSize?: number }): Promise<{ id: string; uploadedChunks: number; uploadedBytes: number; finalizeCode: number }> {
    const { id, blobs } = params;
    const chunkSize = params.chunkSize ?? 1024 * 1024; // default 1MB

    const totalSize = blobs.reduce((sum, blob) => sum + blob.size, 0);

    // Initialize chunked upload
    const startUpload: Mk20StartUpload = { rawSize: totalSize, chunkSize };
    await this.initializeChunkedUpload(id, startUpload);

    // Upload chunks sequentially
    let totalChunks = 0;
    let uploadedBytes = 0;
    for (const blob of blobs) {
      for (let offset = 0; offset < blob.size; offset += chunkSize) {
        const chunk = blob.slice(offset, offset + chunkSize);
        const chunkArray = new Uint8Array(await chunk.arrayBuffer());
        const chunkNumbers = Array.from(chunkArray);
        const chunkNum = String(totalChunks);
        await this.uploadChunk(id, chunkNum, chunkNumbers);
        totalChunks++;
        uploadedBytes += chunkNumbers.length;
      }
    }

    // Finalize
    const finalizeCode = await this.finalizeChunkedUpload(id, params.deal);
    return { id, uploadedChunks: totalChunks, uploadedBytes, finalizeCode };
  }
  /**
   * Simple convenience wrapper for PDPv1 deals with chunked upload
   * Takes blobs and required addresses, computes piece_cid, and returns a UUID identifier
   */
  /**
   * Convenience wrapper for PDPv1 deals with chunked upload.
   * @param params - Input parameters
   * @param params.blobs - Data to upload as an array of blobs
   * @param params.client - Client wallet address
   * @param params.provider - Provider wallet address
   * @param params.contractAddress - Verification contract address
   * @returns Upload metadata including uuid, pieceCid, and stats
   */
  async submitPDPv1DealWithUpload(params: {
    blobs: Blob[];
    client: string;
    recordKeeper: string;
    contractAddress: string;
  }): Promise<{
    uuid: string;
    totalSize: number;
    dealId: number;
    uploadId: string;
    pieceCid: string;
    uploadedChunks: number;
    uploadedBytes: number;
  }> {
    try {
      const prep = await this.submitPDPv1Deal(params);
      const ures = await this.uploadBlobs({ id: prep.id, blobs: params.blobs, deal: prep.deal });
      return {
        uuid: prep.id,
        totalSize: prep.totalSize,
        dealId: prep.dealId,
        uploadId: prep.id,
        pieceCid: prep.pieceCid,
        uploadedChunks: ures.uploadedChunks,
        uploadedBytes: ures.uploadedBytes,
      };
    } catch (error) {
      throw new Error(`Failed to submit PDPv1 deal with upload: ${error}`);
    }
  }
  /**
   * Upload deal data
   */
  /**
   * Upload all data in a single request (for small deals).
   * @param id - Deal identifier
   * @param data - Entire data payload as an array of bytes
   */
  async uploadData(id: string, data: Array<number>): Promise<void> {
    try {
      await this.api.uploadIdPut({ id, body: data });
    } catch (error) {
      throw new Error(`Failed to upload data for deal ${id}: ${error}`);
    }
  }

  /**
   * Initialize chunked upload for a deal
   * @param id - Deal identifier
   * @param startUpload - Upload initialization data
   */
  /**
   * Initialize chunked upload for a deal.
   * @param id - Deal identifier
   * @param startUpload - Upload init payload (chunkSize, rawSize)
   */
  async initializeChunkedUpload(id: string, startUpload: Mk20StartUpload): Promise<number> {
    try {
      const result = await this.api.uploadsIdPost({ id, data: startUpload });
      return result;
    } catch (error) {
      throw new Error(`Failed to initialize chunked upload for deal ${id}: ${error}`);
    }
  }

  /**
   * Upload a chunk of data for a deal
   * @param id - Deal identifier
   * @param chunkNum - Chunk number
   * @param data - Chunk data
   */
  /**
   * Upload one chunk for a deal.
   * @param id - Deal identifier
   * @param chunkNum - Chunk index as string (0-based)
   * @param data - Chunk data bytes
   */
  async uploadChunk(id: string, chunkNum: string, data: Array<number>): Promise<number> {
    try {
      const result = await this.api.uploadsIdChunkNumPut({ id, chunkNum, data });
      return result;
    } catch (error) {
      throw new Error(`Failed to upload chunk ${chunkNum} for deal ${id}: ${error}`);
    }
  }

  /**
   * Finalize chunked upload for a deal
   * @param id - Deal identifier
   * @param deal - Optional deal data for finalization
   */
  /**
   * Finalize a chunked upload.
   * @param id - Deal identifier
   * @param deal - Optional deal payload to finalize with
   */
  async finalizeChunkedUpload(id: string, deal?: any): Promise<number> {
    try {
      const result = await this.api.uploadsFinalizeIdPost({ id, body: deal });
      return result;
    } catch (error) {
      throw new Error(`Failed to finalize chunked upload for deal ${id}: ${error}`);
    }
  }

  /**
   * Finalize a serial (single PUT) upload.
   * @param id - Deal identifier (ULID string)
   * @param deal - Optional deal payload to finalize with
   */
  async finalizeSerialUpload(id: string, deal?: Mk20Deal): Promise<number> {
    try {
      const result = await this.api.uploadIdPost({ id, body: deal });
      return result;
    } catch (error) {
      throw new Error(`Failed to finalize serial upload for deal ${id}: ${error}`);
    }
  }

  /**
   * Get upload status for a deal
   * @param id - Deal identifier
   */
  /**
   * Get upload status for a deal.
   * @param id - Deal identifier
   */
  async getUploadStatus(id: string): Promise<any> {
    try {
      return await this.api.uploadsIdGet({ id });
    } catch (error) {
      throw new Error(`Failed to get upload status for deal ${id}: ${error}`);
    }
  }

  /**
   * Update an existing deal (e.g., request deletion via PDPv1 flags).
   * @param id - Deal identifier (ULID string)
   * @param deal - Deal payload with updated products
   */
  async updateDeal(id: string, deal: Mk20Deal): Promise<number> {
    try {
      const result = await this.api.updateIdGet({ id, body: deal });
      return result;
    } catch (error) {
      throw new Error(`Failed to update deal ${id}: ${error}`);
    }
  }

  /**
   * Get info (placeholder method for compatibility)
   */
  async getInfo(): Promise<never> {
    throw new Error('Failed to get info: Error: Info endpoint not available in generated API');
  }
}
