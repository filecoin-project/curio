import { MarketClient, MarketClientConfig } from '../src/client';
import { mockResponse, mockError } from './setup';

// Mock the generated API
jest.mock('../generated', () => ({
  DefaultApi: jest.fn().mockImplementation(() => ({
    contractsGet: jest.fn(),
    productsGet: jest.fn(),
    sourcesGet: jest.fn(),
    statusIdGet: jest.fn(),
    storePost: jest.fn(),
    uploadIdPut: jest.fn(),
  })),
}));

describe('MarketClient', () => {
  let client: MarketClient;
  let mockApi: any;

  beforeEach(() => {
    const config: MarketClientConfig = {
      basePath: 'http://localhost:8080/market/mk20',
    } as MarketClientConfig;
    
    client = new MarketClient(config);
    
    // Get the mocked API instance and set up the mocks
    const { DefaultApi } = require('../generated');
    mockApi = new DefaultApi();
    
    // Set up the mock methods on the client's API instance
    (client as any).api = mockApi;
  });

  describe('getContracts', () => {
    it('should return contracts successfully', async () => {
      const mockContracts = ['0x123', '0x456'];
      mockApi.contractsGet.mockResolvedValue({
        contracts: mockContracts,
      });

      const result = await client.getContracts();
      expect(result).toEqual(mockContracts);
    });

    it('should handle errors gracefully', async () => {
      mockApi.contractsGet.mockRejectedValue(new Error('API Error'));

      await expect(client.getContracts()).rejects.toThrow('Failed to get contracts: Error: API Error');
    });
  });

  describe('getProducts', () => {
    it('should return products successfully', async () => {
      const mockProducts = { ddo_v1: true, pdp_v1: true };
      mockApi.productsGet.mockResolvedValue(mockProducts);

      const result = await client.getProducts();
      expect(result).toEqual(mockProducts);
    });
  });

  describe('getSources', () => {
    it('should return sources successfully', async () => {
      const mockSources = { http: true, aggregate: true };
      mockApi.sourcesGet.mockResolvedValue(mockSources);

      const result = await client.getSources();
      expect(result).toEqual(mockSources);
    });
  });

  describe('getStatus', () => {
    it('should return deal status successfully', async () => {
      const mockStatus = { identifier: 'test-id', status: 'active' };
      mockApi.statusIdGet.mockResolvedValue(mockStatus);

      const result = await client.getStatus('test-id');
      expect(result).toEqual(mockStatus);
    });

    it('should handle errors with deal ID context', async () => {
      mockApi.statusIdGet.mockRejectedValue(new Error('Not Found'));

      await expect(client.getStatus('test-id')).rejects.toThrow('Failed to get deal status for test-id: Error: Not Found');
    });
  });

  describe('submitDeal', () => {
    it('should submit deal successfully', async () => {
      const mockDeal = { identifier: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16] };
      const mockResult = 200; // DealCode.Ok
      mockApi.storePost.mockResolvedValue(mockResult);

      const result = await client.submitDeal(mockDeal);
      expect(result).toEqual(mockResult);
    });
  });

  describe('uploadData', () => {
    it('should upload data successfully', async () => {
      const testData = [1, 2, 3, 4, 5, 6, 7, 8];
      mockApi.uploadIdPut.mockResolvedValue(undefined);

      await expect(client.uploadData('test-id', testData)).resolves.not.toThrow();
    });

    it('should handle upload errors', async () => {
      const testData = [1, 2, 3, 4, 5, 6, 7, 8];
      mockApi.uploadIdPut.mockRejectedValue(new Error('Upload failed'));

      await expect(client.uploadData('test-id', testData)).rejects.toThrow('Failed to upload data for deal test-id: Error: Upload failed');
    });
  });

  describe('getInfo', () => {
    it('should handle info endpoint not available', async () => {
      await expect(client.getInfo()).rejects.toThrow('Failed to get info: Error: Info endpoint not available in generated API');
    });
  });
});
