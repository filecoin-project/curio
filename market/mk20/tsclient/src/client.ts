import { DefaultApi, ConfigurationParameters, Mk20Deal, Mk20DealProductStatusResponse, Mk20SupportedContracts, Mk20SupportedProducts, Mk20SupportedDataSources } from '../generated';
import { Configuration } from '../generated/runtime';

export interface MarketClientConfig extends ConfigurationParameters {
  basePath: string;
}

export class MarketClient {
  private api: DefaultApi;

  constructor(config: MarketClientConfig) {
    this.api = new DefaultApi(new Configuration(config));
  }

  /**
   * Get supported DDO contracts
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
  async submitDeal(deal: Mk20Deal): Promise<number> {
    try {
      const response = await this.api.storePost({ body: deal });
      return response;
    } catch (error) {
      throw new Error(`Failed to submit deal: ${error}`);
    }
  }

  /**
   * Upload deal data
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
  async initializeChunkedUpload(id: string, startUpload: any): Promise<number> {
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
  async finalizeChunkedUpload(id: string, deal?: any): Promise<number> {
    try {
      const result = await this.api.uploadsFinalizeIdPost({ id, body: deal });
      return result;
    } catch (error) {
      throw new Error(`Failed to finalize chunked upload for deal ${id}: ${error}`);
    }
  }

  /**
   * Get upload status for a deal
   * @param id - Deal identifier
   */
  async getUploadStatus(id: string): Promise<any> {
    try {
      return await this.api.uploadsIdGet({ id });
    } catch (error) {
      throw new Error(`Failed to get upload status for deal ${id}: ${error}`);
    }
  }
}
