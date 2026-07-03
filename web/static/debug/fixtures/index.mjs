/**
 * Mock RPC fixtures for UI development and visual tests.
 * Activate with ?mock=1 on any page.
 */

const fixtures = {
  Version: () => 'curio-mock-ui-dev',
  UIVariant: () => 'curio',
  AlertUnacknowledgedCount: () => 2,

  SyncerState: () => [
    { Address: 'wss://api.node.glif.io/rpc/v1', Reachable: true, SyncState: 'ok', Version: '1.28.0' },
    { Address: 'wss://calibration.node.glif.io/rpc/v1', Reachable: true, SyncState: 'ok', Version: '1.28.0' },
    { Address: 'wss://backup.example.com/rpc/v1', Reachable: false, SyncState: '', Version: '' },
  ],

  NetSummary: () => ({
    nodeCount: 2,
    nodes: [
      {
        node: 'curio-node-primary',
        epoch: 4123456,
        peerCount: 847,
        bandwidth: { rateIn: 12582912, rateOut: 8388608 },
        reachability: { status: 'public' },
      },
      {
        node: 'curio-node-secondary',
        epoch: 4123456,
        peerCount: 612,
        bandwidth: { rateIn: 5242880, rateOut: 3145728 },
        reachability: { status: 'private' },
      },
    ],
  }),

  ClusterMachines: () => [
    {
      ID: 'm-001',
      Name: 'worker-01',
      Address: '10.0.1.10',
      Cpu: 64,
      RamHumanized: '256 GiB',
      Gpu: '2x A6000',
      SinceContact: '2s ago',
      Version: 'abc1234 (main)',
      Uptime: '14d 6h',
      Unschedulable: false,
      Restarting: false,
      RunningTasks: 0,
      TasksSupported: ['sdr', 'tree_d', 'tree_r', 'tree_c'],
      LayersEnabled: ['seal', 'unseal'],
    },
    {
      ID: 'm-002',
      Name: 'worker-02',
      Address: '10.0.1.11',
      Cpu: 32,
      RamHumanized: '128 GiB',
      Gpu: '1x A6000',
      SinceContact: '5s ago',
      Version: 'abc1234 (main)',
      Uptime: '14d 6h',
      Unschedulable: true,
      Restarting: false,
      RunningTasks: 3,
      TasksSupported: ['sdr', 'tree_d'],
      LayersEnabled: ['seal'],
    },
    {
      ID: 'm-003',
      Name: 'worker-03',
      Address: '10.0.1.12',
      Cpu: 48,
      RamHumanized: '192 GiB',
      Gpu: '2x A6000',
      SinceContact: '1s ago',
      Version: 'def5678 (feature/ui)',
      Uptime: '3d 2h',
      Unschedulable: false,
      Restarting: true,
      RestartRequest: '12m ago',
      RunningTasks: 0,
      TasksSupported: ['sdr', 'tree_d', 'tree_r', 'tree_c', 'snap'],
      LayersEnabled: ['seal', 'unseal', 'snap'],
    },
  ],

  ClusterTaskHistory: () => [
    { Name: 'sdr', Machine: 'worker-01', Started: '2026-07-03T12:00:00Z', Finished: '2026-07-03T12:45:00Z', Result: 'ok' },
    { Name: 'tree_d', Machine: 'worker-02', Started: '2026-07-03T11:30:00Z', Finished: '2026-07-03T11:55:00Z', Result: 'ok' },
    { Name: 'commit_batch', Machine: 'worker-01', Started: '2026-07-03T11:00:00Z', Finished: '2026-07-03T11:05:00Z', Result: 'ok' },
    { Name: 'wdpost', Machine: 'worker-03', Started: '2026-07-03T10:45:00Z', Finished: '2026-07-03T10:50:00Z', Result: 'error' },
  ],

  HarmonyTaskStats: () => [
    { name: 'sdr', success: 142, failure: 2, total: 144 },
    { name: 'tree_d', success: 138, failure: 0, total: 138 },
    { name: 'tree_r', success: 135, failure: 1, total: 136 },
    { name: 'tree_c', success: 130, failure: 0, total: 130 },
    { name: 'commit_batch', success: 48, failure: 0, total: 48 },
    { name: 'wdpost', success: 287, failure: 5, total: 292 },
    { name: 'winning_post', success: 12, failure: 0, total: 12 },
  ],

  ClusterTaskSummary: () => [
    { Name: 'sdr', Running: 4, Queued: 12, Failed: 1 },
    { Name: 'tree_d', Running: 2, Queued: 8, Failed: 0 },
    { Name: 'tree_r', Running: 1, Queued: 5, Failed: 0 },
    { Name: 'commit_batch', Running: 0, Queued: 2, Failed: 0 },
    { Name: 'wdpost', Running: 1, Queued: 0, Failed: 1 },
  ],

  StorageUseStats: () => ({
    Total: 1024000000000000,
    Used: 768000000000000,
    Available: 256000000000000,
    PercentUsed: 75,
  }),

  StorageStoreTypeStats: () => [
    { Type: 'sealed', Used: 512000000000000, Count: 8420 },
    { Type: 'cache', Used: 128000000000000, Count: 8420 },
    { Type: 'unsealed', Used: 64000000000000, Count: 1200 },
    { Type: 'snap', Used: 64000000000000, Count: 450 },
  ],

  StorageGCStats: () => [
    { Address: 'f01234', MarkedForGC: 12 },
    { Address: 'f05678', MarkedForGC: 3 },
  ],

  ActorSummary: () => [
    {
      Address: 'f012345678901234567890123456789012345678',
      Balance: '125.432 FIL',
      Deadlines: Array.from({ length: 48 }, (_, i) => ({
        Current: i === 12,
        Proven: i < 12,
        PartFaulty: i === 8,
        Faulty: false,
        OpenAt: `epoch ${4123400 + i * 60}`,
        PartitionCount: i < 12 ? 4 : 0,
        PartitionsProven: i < 11,
        ElapsedMinutes: i === 12 ? 18 : 0,
        Count: { Total: 1200, Live: 1180, Active: 1150, Fault: i === 8 ? 2 : 0 },
      })),
    },
  ],

  WinStats: () => [
    { Epoch: 4123450, Miner: 'f01234', Sector: 8421, Timestamp: '2026-07-03T08:00:00Z' },
    { Epoch: 4123440, Miner: 'f01234', Sector: 8419, Timestamp: '2026-07-03T07:00:00Z' },
  ],

  PorepPipelineSummary: () => [
    { Stage: 'SDR', Count: 24, AvgDuration: '42m' },
    { Stage: 'TreeD', Count: 18, AvgDuration: '28m' },
    { Stage: 'TreeRC', Count: 12, AvgDuration: '35m' },
    { Stage: 'PreCommit', Count: 8, AvgDuration: '5m' },
    { Stage: 'WaitSeed', Count: 6, AvgDuration: '75m' },
    { Stage: 'Commit', Count: 4, AvgDuration: '12m' },
  ],

  AlertMuteList: () => [],
  AlertCategoriesList: () => ['chain', 'storage', 'sealing', 'market'],
  AlertHistoryListPaginated: () => ({
    Alerts: [
      {
        ID: 1,
        Name: 'ChainSyncLag',
        Message: 'Node curio-node-primary is 3 epochs behind chain head.',
        Category: 'chain',
        SentAt: '2026-07-03T14:00:00Z',
        Acknowledged: false,
        Sent: true,
      },
      {
        ID: 2,
        Name: 'StorageLowSpace',
        Message: 'Storage path /sealed is at 92% capacity.',
        Category: 'storage',
        SentAt: '2026-07-03T13:30:00Z',
        Acknowledged: false,
        Sent: true,
      },
    ],
    Total: 2,
    Page: 0,
    PageSize: 20,
  }),

  HarmonyTaskDetails: (params) => ({
    ID: params?.[0] ?? 1,
    Name: 'sdr',
    Status: 'running',
    Machine: 'worker-01',
    Started: '2026-07-03T14:00:00Z',
    Params: { sector: 8421, sp: 'f01234' },
  }),

  WalletNames: () => [
    { Address: 'f012345678901234567890123456789012345678', Name: 'Primary Miner' },
    { Address: 'f098765432109876543210987654321098765432', Name: 'Market Escrow' },
  ],

  WalletInfoShort: () => ({ Balance: '125.432 FIL', Nonce: 1042 }),
  PendingMessages: () => [],

  EpochPretty: (params) => {
    const epoch = params?.[0] ?? 4123456;
    return `epoch ${epoch} (~${new Date().toISOString().slice(0, 16)})`;
  },
};

export function getMockResult(method, params) {
  const handler = fixtures[method];
  if (typeof handler === 'function') {
    return handler(params);
  }
  console.warn(`[mock] No fixture for CurioWeb.${method}`, params);
  return { _mock: true, method, params, note: 'No fixture defined' };
}

export default fixtures;
