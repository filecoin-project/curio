import { MarketClientConfig } from '../src';
import { StreamingPDP } from '../src/streaming';

// Example usage (now using the strongly-typed StreamingPDP from src)
async function example() {
  const config: MarketClientConfig = {
    serverUrl: 'http://localhost:8080',
  } as MarketClientConfig;
  const client = new (require('../src').Client)(config);
  const spdp = new StreamingPDP(client, {
    client: 'f1client...',
    provider: 'f1provider...',
    contractAddress: '0x...',
  });

  await spdp.begin();

  // Simulate streaming writes
  spdp.write(new TextEncoder().encode('hello '));
  spdp.write(new TextEncoder().encode('world'));

  const res = await spdp.commit();
  console.log('Streaming PDP completed:', res);
}

// Only run wenshen executed directly (ts-node/node), not when imported
if (require.main === module) {
  example().catch(err => {
    console.error('Streaming PDP example failed:', err);
    process.exit(1);
  });
}

export { StreamingPDP };


