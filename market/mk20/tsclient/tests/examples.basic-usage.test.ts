// Tests covering examples/basic-usage.ts

jest.mock('../generated', () => ({
  DefaultApi: jest.fn().mockImplementation(() => ({
    contractsGet: jest.fn().mockResolvedValue({ contracts: ['0xabc'] }),
    productsGet: jest.fn().mockResolvedValue({ ddo_v1: true, pdp_v1: true }),
    sourcesGet: jest.fn().mockResolvedValue({ http: true, aggregate: true }),
    statusIdGet: jest.fn().mockResolvedValue({ identifier: 'id', status: 'active' }),
    storePost: jest.fn().mockResolvedValue(200),
    uploadIdPut: jest.fn().mockResolvedValue(undefined),
    uploadsIdPost: jest.fn().mockResolvedValue(200),
    uploadsIdChunkNumPut: jest.fn().mockResolvedValue(200),
    uploadsFinalizeIdPost: jest.fn().mockResolvedValue(200),
  })),
}));

import { exampleUsage, uploadDataExample, pieceIdCalculationExample } from '../examples/basic-usage';
import { PieceCidUtils } from '../src';

describe('examples/basic-usage.ts', () => {
  beforeAll(() => {
    jest.spyOn(PieceCidUtils, 'computePieceCidV2').mockResolvedValue('btestcid');
  });

  afterAll(() => {
    jest.restoreAllMocks();
  });

  it('runs exampleUsage without errors', async () => {
    await expect(exampleUsage()).resolves.not.toThrow();
  });

  it('runs uploadDataExample without errors', async () => {
    await expect(uploadDataExample('deal-id', [1, 2, 3])).resolves.not.toThrow();
  });

  it('runs pieceIdCalculationExample and returns results', async () => {
    const res = await pieceIdCalculationExample();
    expect(res).toBeDefined();
    expect(res.dealId).toBe(200);
    expect(res.pieceCid).toBe('btestcid');
  });
});


