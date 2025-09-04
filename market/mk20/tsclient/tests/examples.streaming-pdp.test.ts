// Tests covering examples/streaming-pdp.ts and StreamingPDP helper

jest.mock('../generated', () => ({
  DefaultApi: jest.fn().mockImplementation(() => ({
    storePost: jest.fn().mockResolvedValue(200),
    uploadsIdPost: jest.fn().mockResolvedValue(200),
    uploadsIdChunkNumPut: jest.fn().mockResolvedValue(200),
    uploadsFinalizeIdPost: jest.fn().mockResolvedValue(200),
  })),
}));

import { MarketClientConfig, Client, StreamingPDP, PieceCidUtils } from '../src';

describe('streaming-pdp example and helper', () => {
  beforeAll(() => {
    jest.spyOn(PieceCidUtils, 'computePieceCidV2').mockResolvedValue('btestcid');
  });

  afterAll(() => {
    jest.restoreAllMocks();
  });

  it('streams data via StreamingPDP and finalizes', async () => {
    const config: MarketClientConfig = { serverUrl: 'http://localhost:8080' } as MarketClientConfig;
    const client = new Client(config);
    const spdp = client.streamingPDP({
      client: 'f1client...',
      provider: 'f1provider...',
      contractAddress: '0x...',
    });

    await spdp.begin();
    spdp.write(new TextEncoder().encode('hello '));
    spdp.write(new TextEncoder().encode('world'));
    const res = await spdp.commit();

    expect(res.id).toBeDefined();
    expect(res.pieceCid).toMatch(/^b/);
    expect(res.totalSize).toBeGreaterThan(0);
  });
});


