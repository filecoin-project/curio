import assert from 'node:assert/strict';
import test from 'node:test';

import {
  CLUSTER_TASK_DEFAULTS,
  CLUSTER_TASK_ORDER_POLICY,
  PENDING_AGE_TOOLTIP,
  RUNNING_AGE_TOOLTIP,
  UNKNOWN_RUNNING_AGE_TOOLTIP,
  beginClusterTaskRefresh,
  buildClusterTaskLastSuccessPresentation,
  buildClusterTaskRequest,
  buildClusterTaskSections,
  buildClusterTaskTypeOptions,
  clusterTaskControlsMatchApplied,
  clusterTaskSectionEmptyMessage,
  clusterTaskSnapshotMismatchMessage,
  completeClusterTaskRefresh,
  createClusterTaskViewState,
  failClusterTaskRefresh,
  formatClusterTaskFreshness,
  formatClusterTaskSectionSummary,
  formatTaskAgeSeconds,
  normalizeClusterTaskResponse,
  parseBoundedInteger,
  shouldRunClusterTaskFreshness,
} from '../static/cluster-tasks-model.mjs';

test('controls use bounded defaults and the limited-RPC request shape', () => {
  assert.deepEqual(CLUSTER_TASK_DEFAULTS, {
    maxTasks: 500,
    maxPending: 500,
    includeBackground: false,
    taskName: '',
    coalesceEntries: true,
  });

  assert.deepEqual(buildClusterTaskRequest(CLUSTER_TASK_DEFAULTS), {
    MaxTasks: 500,
    MaxPending: 500,
    IncludeBackground: false,
    TaskName: null,
  });
  assert.equal(buildClusterTaskRequest({
    ...CLUSTER_TASK_DEFAULTS,
    taskName: 'SDR',
  }).TaskName, 'SDR');
  assert.deepEqual(buildClusterTaskRequest({
    maxTasks: 17,
    maxPending: 0,
    includeBackground: true,
    taskName: 'TreeRC',
  }), {
    MaxTasks: 17,
    MaxPending: 0,
    IncludeBackground: true,
    TaskName: 'TreeRC',
  });

  assert.equal(parseBoundedInteger('1', 1, 500), 1);
  assert.equal(parseBoundedInteger('500', 1, 500), 500);
  assert.equal(parseBoundedInteger('0', 0, 500), 0);
  assert.equal(parseBoundedInteger('', 1, 500), null);
  assert.equal(parseBoundedInteger('1.5', 1, 500), null);
  assert.equal(parseBoundedInteger('0', 1, 500), 1);
  assert.equal(parseBoundedInteger('501', 1, 500), 500);
  assert.equal(parseBoundedInteger('-1', 0, 30), 0);
});

test('task ages preserve second precision and distinguish unknown runtime', () => {
  assert.equal(formatTaskAgeSeconds(0), '0s');
  assert.equal(formatTaskAgeSeconds(42), '42s');
  assert.equal(formatTaskAgeSeconds(5 * 60 + 1), '5m1s');
  assert.equal(formatTaskAgeSeconds(18 * 60 + 39), '18m39s');
  assert.equal(formatTaskAgeSeconds(25 * 60 * 60 + 61), '25h1m1s');
  assert.equal(formatTaskAgeSeconds(null), 'unknown');
  assert.equal(formatTaskAgeSeconds(undefined), 'unknown');
  assert.equal(formatTaskAgeSeconds(-1), 'unknown');
  assert.match(UNKNOWN_RUNNING_AGE_TOOLTIP, /ownership|Ownership/);
});

test('freshness is relative to the last successful client update', () => {
  assert.equal(formatClusterTaskFreshness(10_000, 10_000), 'just now');
  assert.equal(formatClusterTaskFreshness(15_001, 10_000), '5s ago');
  assert.equal(formatClusterTaskFreshness(80_000, 10_000), '1m10s ago');
  assert.equal(formatClusterTaskFreshness(9_000, 10_000), 'just now');
  assert.equal(formatClusterTaskFreshness(Date.now(), null), '');
});

test('paused freshness uses an absolute date and time instead of frozen relative text', () => {
  const lastSuccessAt = Date.UTC(2026, 8, 5, 3, 4, 5);
  const absolute = new Date(lastSuccessAt).toLocaleString();

  assert.deepEqual(buildClusterTaskLastSuccessPresentation({
    paused: true,
    now: lastSuccessAt + 60_000,
    lastSuccessAt,
  }), {
    text: `Last successful update: ${absolute}`,
    title: absolute,
  });
  assert.doesNotMatch(buildClusterTaskLastSuccessPresentation({
    paused: true,
    now: lastSuccessAt + 60_000,
    lastSuccessAt,
  }).text, /ago/);
  assert.deepEqual(buildClusterTaskLastSuccessPresentation({
    paused: true,
    now: lastSuccessAt,
    lastSuccessAt: null,
  }), {
    text: 'No successful snapshot is available yet.',
    title: '',
  });
  assert.equal(buildClusterTaskLastSuccessPresentation({
    paused: false,
    now: lastSuccessAt + 5_000,
    lastSuccessAt,
  }).text, 'Last successful update: 5s ago');
});

test('freshness ticks only for a loaded, active, unpaused view', () => {
  const active = {
    isConnected: true,
    viewVisible: true,
    documentVisible: true,
    paused: false,
    hasSuccessfulLoad: true,
  };
  assert.equal(shouldRunClusterTaskFreshness(active), true);

  for (const field of ['isConnected', 'viewVisible', 'documentVisible', 'hasSuccessfulLoad']) {
    assert.equal(shouldRunClusterTaskFreshness({...active, [field]: false}), false, field);
  }
  assert.equal(shouldRunClusterTaskFreshness({...active, paused: true}), false, 'paused');
});

test('section summaries expose shown and total counts', () => {
  assert.equal(formatClusterTaskSectionSummary(12, 40_000, true), '12 of 40000');
  assert.equal(
      formatClusterTaskSectionSummary(12, 0, false),
      '12 shown; total unavailable',
  );
});

test('pending empty states distinguish capacity, filters, disabled previews, and unknown totals', () => {
  const runningAtLimit = Array.from({length: 500}, (_, index) => ({ID: index + 1}));
  const capacityResponse = normalizeClusterTaskResponse({
    Running: runningAtLimit,
    Pending: [],
    Applied: {MaxTasks: 500, MaxPending: 30},
    RunningTotal: 600,
    PendingTotal: 40_000,
    TotalsAvailable: true,
  });
  assert.equal(
      clusterTaskSectionEmptyMessage('pending', capacityResponse),
      'Pending preview is omitted because running tasks use the display limit.',
  );
  assert.equal(
      formatClusterTaskSectionSummary(
          capacityResponse.Pending.length,
          capacityResponse.PendingTotal,
          capacityResponse.TotalsAvailable,
      ),
      '0 of 40000',
  );

  assert.equal(clusterTaskSectionEmptyMessage('pending', {
    Pending: [],
    Applied: {MaxTasks: 500, MaxPending: 0},
    PendingTotal: 40_000,
    TotalsAvailable: true,
  }), 'Pending preview is disabled.');
  assert.equal(clusterTaskSectionEmptyMessage('pending', {
    Pending: [],
    Applied: {MaxTasks: 500, MaxPending: 30},
    PendingTotal: 0,
    TotalsAvailable: true,
  }), 'No pending tasks match the applied snapshot filters.');
  assert.equal(clusterTaskSectionEmptyMessage('pending', {
    Pending: [],
    Applied: {MaxTasks: 500, MaxPending: 30},
    TotalsAvailable: false,
  }), 'No pending rows displayed; total unavailable.');
});

test('snapshot mismatch compares every applied control and normalizes empty task names', () => {
  const selected = {
    maxTasks: 500,
    maxPending: 30,
    includeBackground: false,
    taskName: '',
  };
  const applied = {
    MaxTasks: 500,
    MaxPending: 30,
    IncludeBackground: false,
    TaskName: null,
  };
  assert.equal(clusterTaskControlsMatchApplied(selected, applied), true);

  for (const changed of [
    {MaxTasks: 499},
    {MaxPending: 29},
    {IncludeBackground: true},
    {TaskName: 'SDR'},
  ]) {
    assert.equal(
        clusterTaskControlsMatchApplied(selected, {...applied, ...changed}),
        false,
        JSON.stringify(changed),
    );
  }
});

test('retained snapshots identify filters that have not been applied', () => {
  const allTasksResponse = {
    Running: [{ID: 1}],
    Pending: [],
    Applied: {
      MaxTasks: 500,
      MaxPending: 30,
      IncludeBackground: false,
      TaskName: '',
    },
  };
  const selectedSDR = {
    maxTasks: 500,
    maxPending: 30,
    includeBackground: false,
    taskName: 'SDR',
  };

  let state = completeClusterTaskRefresh(
      createClusterTaskViewState(),
      allTasksResponse,
      1000,
  );
  state = beginClusterTaskRefresh(state);
  const refreshing = clusterTaskSnapshotMismatchMessage({
    controls: selectedSDR,
    response: state.response,
    refreshing: state.refreshing,
    failed: Boolean(state.error),
    paused: false,
  });
  assert.match(refreshing, /last snapshot for All tasks/);
  assert.match(refreshing, /selected controls have not been applied yet: SDR tasks/);

  state = failClusterTaskRefresh(state, new Error('offline'));
  const failed = clusterTaskSnapshotMismatchMessage({
    controls: selectedSDR,
    response: state.response,
    refreshing: state.refreshing,
    failed: Boolean(state.error),
    paused: false,
  });
  assert.match(failed, /last snapshot for All tasks/);
  assert.match(failed, /were not applied because the refresh failed: SDR tasks/);

  const paused = clusterTaskSnapshotMismatchMessage({
    controls: selectedSDR,
    response: state.response,
    refreshing: false,
    failed: false,
    paused: true,
  });
  assert.match(paused, /last snapshot for All tasks/);
  assert.match(paused, /have not been applied while updates are paused: SDR tasks/);

  state = completeClusterTaskRefresh(state, {
    ...allTasksResponse,
    Applied: {...allTasksResponse.Applied, TaskName: 'SDR'},
  }, 2000);
  assert.equal(clusterTaskSnapshotMismatchMessage({
    controls: selectedSDR,
    response: state.response,
    refreshing: false,
    failed: false,
    paused: false,
  }), '');
});

test('task-type options preserve an active filter while metadata changes', () => {
  assert.deepEqual(buildClusterTaskTypeOptions('SDR', []), [{Name: 'SDR'}]);
  assert.deepEqual(
      buildClusterTaskTypeOptions('SDR', [
        {Name: 'TreeD'},
        {Name: 'SDR'},
        {Name: 'TreeD'},
      ]).map((entry) => entry.Name),
      ['TreeD', 'SDR'],
  );
  assert.deepEqual(
      buildClusterTaskTypeOptions('', [{Name: 'TreeD'}]),
      [{Name: 'TreeD'}],
  );
});

test('limited response normalization keeps server metadata and safe arrays', () => {
  const normalized = normalizeClusterTaskResponse({
    Running: null,
    Pending: [{ID: 1}],
    Applied: {
      MaxTasks: 50,
      MaxPending: 10,
      IncludeBackground: true,
      TaskName: 'SDR',
    },
    RunningTotal: 7,
    PendingTotal: 100,
    TotalsAvailable: true,
    Partial: true,
    ObservedAt: '2026-09-05T12:00:00Z',
    TaskTypes: [{Name: 'SDR', Total: 107}, null, {Name: 12}],
    TaskTypesAvailable: true,
    Warnings: [{Code: 'partial', Message: 'Partial result'}, {Code: 'bad'}],
  });

  assert.deepEqual(normalized.Running, []);
  assert.deepEqual(normalized.Pending, [{ID: 1}]);
  assert.deepEqual(normalized.Applied, {
    MaxTasks: 50,
    MaxPending: 10,
    IncludeBackground: true,
    TaskName: 'SDR',
  });
  assert.equal(normalized.PendingTotal, 100);
  assert.equal(normalized.TotalsAvailable, true);
  assert.equal(normalized.Partial, true);
  assert.deepEqual(normalized.TaskTypes, [{Name: 'SDR', Total: 107}]);
  assert.deepEqual(normalized.Warnings, [{Code: 'partial', Message: 'Partial result'}]);
});

test('failed refresh preserves the last rows, ages, and last-success timestamp', () => {
  const firstResponse = {
    Running: [{ID: 1, State: 'running', AgeSeconds: 720}],
    Pending: [{ID: 2, State: 'pending', AgeSeconds: 240}],
  };

  let state = createClusterTaskViewState();
  state = beginClusterTaskRefresh(state);
  state = completeClusterTaskRefresh(state, firstResponse, 1234);
  const acceptedResponse = state.response;

  state = beginClusterTaskRefresh(state);
  state = failClusterTaskRefresh(state, new Error('database unavailable'));

  assert.equal(state.response, acceptedResponse);
  assert.equal(state.response.Running[0].AgeSeconds, 720);
  assert.equal(state.response.Pending[0].AgeSeconds, 240);
  assert.equal(state.lastSuccessAt, 1234);
  assert.equal(state.hasSuccessfulLoad, true);
  assert.equal(state.refreshing, false);
  assert.equal(state.stale, true);
  assert.equal(state.error, 'database unavailable');
});

test('first refresh failure remains a first-state error, not stale data', () => {
  let state = createClusterTaskViewState();
  state = beginClusterTaskRefresh(state);
  state = failClusterTaskRefresh(state, new Error('offline'));

  assert.equal(state.response, null);
  assert.equal(state.hasSuccessfulLoad, false);
  assert.equal(state.stale, false);
  assert.equal(state.error, 'offline');
});

test('sections are Running then Pending and coalesce only inside each section', () => {
  const response = {
    Running: [
      {ID: 1, State: 'running', SpID: '1000', Name: 'SDR', OwnerID: '7'},
      {ID: 2, State: 'running', SpID: '1000', Name: 'SDR', OwnerID: '7'},
    ],
    Pending: [
      {ID: 3, State: 'pending', SpID: '1000', Name: 'SDR', OwnerID: null},
      {ID: 4, State: 'pending', SpID: '1000', Name: 'SDR', OwnerID: null},
    ],
  };

  const expanded = buildClusterTaskSections(response, false);
  assert.deepEqual(expanded.map((section) => section.key), ['running', 'pending']);
  assert.deepEqual(expanded[0].groups.map((group) => group.map((entry) => entry.ID)), [[1], [2]]);
  assert.deepEqual(expanded[1].groups.map((group) => group.map((entry) => entry.ID)), [[3], [4]]);

  const coalesced = buildClusterTaskSections(response, true);
  assert.deepEqual(coalesced[0].groups.map((group) => group.map((entry) => entry.ID)), [[1, 2]]);
  assert.deepEqual(coalesced[1].groups.map((group) => group.map((entry) => entry.ID)), [[3, 4]]);
  assert.equal(coalesced[0].ageLabel, 'Took');
  assert.equal(coalesced[1].ageLabel, 'Waiting');
  assert.match(RUNNING_AGE_TOOLTIP, /Do attempt/);
  assert.match(PENDING_AGE_TOOLTIP, /posted/);
  assert.equal(
      CLUSTER_TASK_ORDER_POLICY,
      'Current ownership age first; task ID breaks ties',
  );
});

test('coalescing preserves the selected-record bound', () => {
  const selected = Array.from({length: 500}, (_, index) => ({
    ID: index + 1,
    State: 'running',
    SpID: '1000',
    Name: 'SDR',
    OwnerID: 7,
  }));

  const sections = buildClusterTaskSections({Running: selected, Pending: []}, true);
  assert.equal(sections[0].groups.length, 1);
  assert.equal(
      sections[0].groups.reduce((count, group) => count + group.length, 0),
      500,
  );
  assert.equal(sections[1].groups.length, 0);
});
