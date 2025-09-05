// Tests covering examples/product-types.ts

jest.mock('../generated', () => ({
  DefaultApi: jest.fn().mockImplementation(() => ({
    storePost: jest.fn().mockResolvedValue(200),
    contractsGet: jest.fn().mockResolvedValue({ contracts: ['0xabc'] }),
    productsGet: jest.fn().mockResolvedValue({ ddo_v1: true, pdp_v1: true }),
    sourcesGet: jest.fn().mockResolvedValue({ http: true, aggregate: true }),
  })),
}));

import { pdpv1ProductExample, ddov1ProductExample, retrievalV1ProductExample, convenienceWrapperExample } from '../examples/product-types';
import { PieceCidUtils } from '../src';

describe('examples/product-types.ts', () => {
  beforeAll(() => {
    jest.spyOn(PieceCidUtils, 'computePieceCidV2').mockResolvedValue('btestcid');
  });

  afterAll(() => {
    jest.restoreAllMocks();
  });

  it('pdpv1ProductExample returns a deal structure', async () => {
    const deal = await pdpv1ProductExample();
    expect(deal.products).toBeDefined();
    expect(deal.products?.pdpV1).toBeDefined();
  });

  it('ddov1ProductExample returns a deal structure', async () => {
    const deal = await ddov1ProductExample();
    expect(deal.products).toBeDefined();
    expect(deal.products?.ddoV1).toBeDefined();
  });

  it('retrievalV1ProductExample returns a config', async () => {
    const cfg = await retrievalV1ProductExample();
    expect(cfg.indexing).toBe(true);
  });

  it('convenienceWrapperExample runs without errors', async () => {
    await expect(convenienceWrapperExample()).resolves.not.toThrow();
  });
});


