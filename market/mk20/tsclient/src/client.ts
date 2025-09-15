import { DefaultApi, ConfigurationParameters, Mk20Deal, Mk20DealProductStatusResponse, Mk20SupportedContracts, Mk20SupportedProducts, Mk20SupportedDataSources, Mk20Products, Mk20PDPV1, Mk20RetrievalV1, Mk20DDOV1, Mk20DataSource, Mk20DealState } from '../generated';
import { monotonicFactory } from 'ulid';
import { Configuration } from '../generated/runtime';
import { Mk20StartUpload } from '../generated/models/Mk20StartUpload';
import { StreamingPDP } from './streaming';
import { calculate as calculatePieceCID } from './piece';

const ulid = monotonicFactory(() => Math.random());
export interface MarketClientConfig extends Omit<ConfigurationParameters, 'basePath'> {
  serverUrl: string; // e.g. http://localhost:8080
}

/**
 * Utility class for computing Filecoin piece CID v2 from blobs
 * Uses the better implementation from piece.ts
 */
export class PieceCidUtils {
  /**
   * Compute piece CID v2 from an array of blobs
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

      // Use the better piece.ts implementation
      const pieceCID = calculatePieceCID(concatenatedData);
      return pieceCID.toString();
    } catch (error) {
      throw new Error(`Failed to compute piece CID v2: ${error}`);
    }
  }
}

export class MarketClient {
  private api: DefaultApi;
  private config: MarketClientConfig;
  
  /**
   * Try to extract a human-friendly error string from an HTTP Response.
   */
  private async formatHttpError(prefix: string, resp: Response): Promise<string> {
    const status = resp.status;
    const statusText = resp.statusText || '';
    const h = resp.headers;
    const reasonHeader = h.get('Reason') || h.get('reason') || h.get('X-Reason') || h.get('x-reason') || h.get('X-Error') || h.get('x-error') || '';
    let body = '';
    try {
      // clone() to avoid consuming the body in case other handlers need it
      body = await resp.clone().text();
    } catch {}
    const details = [reasonHeader?.trim(), body?.trim()].filter(Boolean).join(' | ');
    const statusPart = statusText ? `${status} ${statusText}` : String(status);
    return `${prefix} (HTTP ${statusPart})${details ? `: ${details}` : ''}`;
  }

  /**
   * Create a MarketClient instance.
   * @param config - Configuration object
   * @param config.serverUrl - Base server URL, e.g. http://localhost:8080
   * @param config.headers - Optional default headers to send with every request
   * @param config.fetchApi - Optional custom fetch implementation
   */
  constructor(config: MarketClientConfig) {
    this.config = config;
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
   * Convert a ULID string (26-char Crockford base32) into an ASCII byte array
   */
  private ulidToBytes(ulidString: string): number[] {
    // ULID is 26 characters, convert to ASCII byte array
    const bytes: number[] = [];
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
      // Use Raw call so we can inspect/handle non-JSON responses gracefully
      const apiResp = await this.api.storePostRaw({ body: deal });
      const ct = apiResp.raw.headers.get('content-type') || '';
      if (ct.toLowerCase().includes('application/json') || ct.toLowerCase().includes('+json')) {
        return await apiResp.value();
      }
      // Treat non-JSON 2xx as success; return HTTP status code
      return apiResp.raw.status;
    } catch (error: any) {
      // If this is a ResponseError, try to extract HTTP status and body text
      const resp = error?.response as Response | undefined;
      if (resp) {
        const msg = await this.formatHttpError('Failed to submit deal', resp);
        throw new Error(msg);
      }
      // Fallback
      throw new Error(`Failed to submit deal: ${error?.message || String(error)}`);
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
   * Start a PDPv1 deal in two steps and prepare for upload.
   * - Step 1: createDataSet
   * - Step 2: addPiece with data descriptor (pieceCid, raw format, HTTP PUT source)
   * Returns the upload identifier (ULID), computed pieceCid, total size, and the deal payload
   * that should be used at finalize time.
   */
  async startPDPv1DealForUpload(params: {
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

    await this.submitDeal(createDeal);
    await this.waitDealComplete(datasetId);

    var datasetIdNumber = 0; // TODO: get dataset id from response

    // Step 2: add piece with data under a new identifier (upload id)
    const uploadId = ulid();
    const addPieceDeal: Mk20Deal = {
      identifier: uploadId,
      client,
      data: {
        pieceCid: { "/": pieceCid } as object,
        format: { raw: {} },
        sourceHttpPut: {},
      } as Mk20DataSource,
      products: {
        pdpV1: {
          addPiece: true,
          dataSetId: datasetIdNumber,
          recordKeeper: recordKeeper,
          extraData: [],
          deleteDataSet: false,
          deletePiece: false,
        } as Mk20PDPV1,
        retrievalV1: {
          announcePayload: false, // not a CAR file.
          announcePiece: true,
          indexing: false, // not a CAR file.
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
        const chunkNum = String(totalChunks + 1);
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
      const prep = await this.startPDPv1DealForUpload(params);
      const ures = await this.uploadBlobs({ id: prep.id, blobs: params.blobs, deal: prep.deal });
      return {
        uuid: prep.id,
        totalSize: prep.totalSize,
        dealId: prep.dealId,
        uploadId: prep.id,
        pieceCid: prep.pieceCid ,
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
      const apiResp = await this.api.uploadsIdPostRaw({ id, data: startUpload });
      const ct = apiResp.raw.headers.get('content-type') || '';
      if (ct.toLowerCase().includes('application/json') || ct.toLowerCase().includes('+json')) {
        return await apiResp.value();
      }
      // Treat non-JSON 2xx as success; return HTTP status code
      return apiResp.raw.status;
    } catch (error: any) {
      const resp = error?.response as Response | undefined;
      if (resp) {
        const msg = await this.formatHttpError(`Failed to initialize chunked upload for deal ${id}`, resp);
        throw new Error(msg);
      }
      throw new Error(`Failed to initialize chunked upload for deal ${id}: ${error?.message || String(error)}`);
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
      const apiResp = await this.api.uploadsIdChunkNumPutRaw({ id, chunkNum, data }, {
        headers: {
          'Content-Type': 'application/octet-stream',
          'Authorization': this.config.headers?.Authorization || ''
        }
      });
      const ct = apiResp.raw.headers.get('content-type') || '';
      if (ct.toLowerCase().includes('application/json') || ct.toLowerCase().includes('+json')) {
        return await apiResp.value();
      }
      // Treat non-JSON 2xx as success; return HTTP status code
      return apiResp.raw.status;
    } catch (error: any) {
      const resp = error?.response as Response | undefined;
      if (resp) {
        const msg = await this.formatHttpError(`Failed to upload chunk ${chunkNum} for deal ${id}`, resp);
        throw new Error(msg);
      }
      throw new Error(`Failed to upload chunk ${chunkNum} for deal ${id}: ${error?.message || String(error)}`);
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
      const apiResp = await this.api.uploadsFinalizeIdPostRaw({ id, body: deal });
      const ct = apiResp.raw.headers.get('content-type') || '';
      if (ct.toLowerCase().includes('application/json') || ct.toLowerCase().includes('+json')) {
        return await apiResp.value();
      }
      // Treat non-JSON 2xx as success; return HTTP status code
      return apiResp.raw.status;
    } catch (error: any) {
      const resp = error?.response as Response | undefined;
      if (resp) {
        const msg = await this.formatHttpError(`Failed to finalize chunked upload for deal ${id}`, resp);
        throw new Error(msg);
      }
      throw new Error(`Failed to finalize chunked upload for deal ${id}: ${error?.message || String(error)}`);
    }
  }

  /**
   * Finalize a serial (single PUT) upload.
   * @param id - Deal identifier (ULID string)
   * @param deal - Optional deal payload to finalize with
   */
  async finalizeSerialUpload(id: string, deal?: Mk20Deal): Promise<number> {
    try {
      const apiResp = await this.api.uploadIdPostRaw({ id, body: deal });
      const ct = apiResp.raw.headers.get('content-type') || '';
      if (ct.toLowerCase().includes('application/json') || ct.toLowerCase().includes('+json')) {
        return await apiResp.value();
      }
      // Treat non-JSON 2xx as success; return HTTP status code
      return apiResp.raw.status;
    } catch (error: any) {
      const resp = error?.response as Response | undefined;
      if (resp) {
        const msg = await this.formatHttpError(`Failed to finalize serial upload for deal ${id}`, resp);
        throw new Error(msg);
      }
      throw new Error(`Failed to finalize serial upload for deal ${id}: ${error?.message || String(error)}`);
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
