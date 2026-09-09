import {groupConsecutiveTasks} from './cluster-tasks-grouping.mjs';

export const CLUSTER_TASK_DEFAULTS = Object.freeze({
  maxTasks: 500,
  maxPending: 500,
  includeBackground: false,
  taskName: '',
  coalesceEntries: true,
});

export const CLUSTER_TASK_ORDER_POLICY =
  'Current ownership age first; task ID breaks ties';
export const RUNNING_AGE_TOOLTIP =
  'Took starts at entry into the current task Do attempt, matching new History records; not at claim or FFI entry.';
export const PENDING_AGE_TOOLTIP =
  'Waiting time starts when the task was posted.';
export const UNKNOWN_RUNNING_AGE_TOOLTIP =
  'Ownership age is unknown because the timestamp or its provenance is unavailable.';
export const INTERPOLATED_AGE_TOOLTIP =
  'Between successful refreshes, displayed time is estimated locally from the last task snapshot.';

export function parseBoundedInteger(value, min, max) {
  const text = String(value).trim();
  if (!/^-?\d+$/.test(text)) {
    return null;
  }

  const parsed = Number(text);
  if (!Number.isSafeInteger(parsed)) {
    return null;
  }
  return Math.min(max, Math.max(min, parsed));
}

export function buildClusterTaskRequest(controls) {
  return {
    MaxTasks: controls.maxTasks,
    MaxPending: controls.maxPending,
    IncludeBackground: controls.includeBackground,
    TaskName: controls.taskName || null,
  };
}

export function formatTaskAgeSeconds(value) {
  if (!Number.isFinite(value) || value < 0) {
    return 'unknown';
  }

  let seconds = Math.floor(value);
  const hours = Math.floor(seconds / 3600);
  seconds %= 3600;
  const minutes = Math.floor(seconds / 60);
  seconds %= 60;

  if (hours > 0) {
    return `${hours}h${minutes}m${seconds}s`;
  }
  if (minutes > 0) {
    return `${minutes}m${seconds}s`;
  }
  return `${seconds}s`;
}

export function createClusterTaskDisplayClock() {
  return {
    anchorMonotonic: null,
    elapsedSeconds: 0,
    interpolating: false,
  };
}

export function resetClusterTaskDisplayClock(monotonicNow) {
  if (!Number.isFinite(monotonicNow)) {
    return createClusterTaskDisplayClock();
  }
  return {
    anchorMonotonic: monotonicNow,
    elapsedSeconds: 0,
    interpolating: true,
  };
}

export function advanceClusterTaskDisplayClock(clock, monotonicNow) {
  if (
    !clock?.interpolating ||
    !Number.isFinite(clock.anchorMonotonic) ||
    !Number.isFinite(monotonicNow)
  ) {
    return clock;
  }

  const elapsedSeconds = Math.max(
      clock.elapsedSeconds,
      Math.floor(Math.max(0, monotonicNow - clock.anchorMonotonic) / 1000),
  );
  return elapsedSeconds === clock.elapsedSeconds
    ? clock
    : {...clock, elapsedSeconds};
}

export function freezeClusterTaskDisplayClock(clock) {
  if (!clock?.interpolating) {
    return clock;
  }
  return {...clock, interpolating: false};
}

export function interpolateClusterTaskAgeSeconds(ageSeconds, clock) {
  if (!Number.isFinite(ageSeconds) || ageSeconds < 0) {
    return null;
  }
  const elapsedSeconds = Number.isSafeInteger(clock?.elapsedSeconds) &&
    clock.elapsedSeconds >= 0
    ? clock.elapsedSeconds
    : 0;
  return Math.floor(ageSeconds) + elapsedSeconds;
}

export function formatClusterTaskFreshness(now, updatedAt) {
  if (!Number.isFinite(now) || !Number.isFinite(updatedAt)) {
    return '';
  }

  const elapsedSeconds = Math.max(0, Math.floor((now - updatedAt) / 1000));
  return elapsedSeconds === 0
    ? 'just now'
    : `${formatTaskAgeSeconds(elapsedSeconds)} ago`;
}

export function buildClusterTaskLastSuccessPresentation({
  paused,
  now,
  lastSuccessAt,
}) {
  if (!Number.isFinite(lastSuccessAt)) {
    return {
      text: paused ? 'No successful snapshot is available yet.' : '',
      title: '',
    };
  }

  const absolute = new Date(lastSuccessAt).toLocaleString();
  if (paused) {
    return {
      text: `Last successful update: ${absolute}`,
      title: absolute,
    };
  }

  const relative = formatClusterTaskFreshness(now, lastSuccessAt);
  return {
    text: relative ? `Last successful update: ${relative}` : '',
    title: absolute,
  };
}

export function formatClusterTaskSectionSummary(shown, total, totalsAvailable) {
  return totalsAvailable
    ? `${shown} of ${total}`
    : `${shown} shown; total unavailable`;
}

export function clusterTaskSectionEmptyMessage(sectionKey, value) {
  const response = normalizeClusterTaskResponse(value);
  const entries = sectionKey === 'pending' ? response.Pending : response.Running;
  if (sectionKey === 'pending' && response.Applied.MaxPending === 0) {
    return 'Pending preview is disabled.';
  }
  if (entries.length > 0) {
    return '';
  }
  if (!response.TotalsAvailable) {
    return `No ${sectionKey} rows displayed; total unavailable.`;
  }

  const total = sectionKey === 'pending'
    ? response.PendingTotal
    : response.RunningTotal;
  if (total === 0) {
    return `No ${sectionKey} tasks match the applied snapshot filters.`;
  }
  if (
    sectionKey === 'pending' &&
    response.Running.length >= response.Applied.MaxTasks
  ) {
    return 'Pending preview is omitted because running tasks use the display limit.';
  }
  return `No ${sectionKey} rows displayed for this snapshot.`;
}

function normalizeTaskName(value) {
  return typeof value === 'string' && value.length > 0 ? value : '';
}

function normalizeQueryControls(value) {
  const controls = value && typeof value === 'object' ? value : {};
  return {
    MaxTasks: controls.MaxTasks ?? controls.maxTasks,
    MaxPending: controls.MaxPending ?? controls.maxPending,
    IncludeBackground: Boolean(
        controls.IncludeBackground ?? controls.includeBackground,
    ),
    TaskName: normalizeTaskName(controls.TaskName ?? controls.taskName),
  };
}

export function clusterTaskControlsMatchApplied(controls, applied) {
  const selected = normalizeQueryControls(controls);
  const snapshot = normalizeQueryControls(applied);
  return selected.MaxTasks === snapshot.MaxTasks &&
    selected.MaxPending === snapshot.MaxPending &&
    selected.IncludeBackground === snapshot.IncludeBackground &&
    selected.TaskName === snapshot.TaskName;
}

export function formatClusterTaskControls(value) {
  const controls = normalizeQueryControls(value);
  const taskScope = controls.TaskName ? `${controls.TaskName} tasks` : 'All tasks';
  const background = controls.IncludeBackground
    ? 'background included'
    : 'background hidden';
  return `${taskScope}; ${background}; max ${controls.MaxTasks}; ` +
    `pending preview ${controls.MaxPending}`;
}

export function clusterTaskSnapshotMismatchMessage({
  controls,
  response,
  refreshing,
  failed,
  paused,
}) {
  if (!response?.Applied || clusterTaskControlsMatchApplied(controls, response.Applied)) {
    return '';
  }

  const snapshot = formatClusterTaskControls(response.Applied);
  const selected = formatClusterTaskControls(controls);
  let explanation = 'The selected controls have not been applied yet';
  if (paused) {
    explanation = 'The selected controls have not been applied while updates are paused';
  } else if (failed) {
    explanation = 'The selected controls were not applied because the refresh failed';
  } else if (refreshing) {
    explanation = 'The selected controls have not been applied yet';
  }

  return `Showing the last snapshot for ${snapshot}. ${explanation}: ${selected}.`;
}

export function shouldRunClusterTaskFreshness({
  isConnected,
  viewVisible,
  documentVisible,
  paused,
  hasSuccessfulLoad,
}) {
  return isConnected && viewVisible && documentVisible && !paused && hasSuccessfulLoad;
}

export function buildClusterTaskTypeOptions(selectedTaskName, taskTypes) {
  const available = Array.isArray(taskTypes) ? taskTypes : [];
  const seen = new Set();
  const options = [];

  if (selectedTaskName && !available.some((entry) => entry?.Name === selectedTaskName)) {
    options.push({Name: selectedTaskName});
    seen.add(selectedTaskName);
  }

  for (const taskType of available) {
    if (taskType && typeof taskType.Name === 'string' && !seen.has(taskType.Name)) {
      options.push(taskType);
      seen.add(taskType.Name);
    }
  }
  return options;
}

function nonNegativeInteger(value, fallback = 0) {
  return Number.isSafeInteger(value) && value >= 0 ? value : fallback;
}

export function normalizeClusterTaskResponse(value) {
  const response = value && typeof value === 'object' ? value : {};
  const applied = response.Applied && typeof response.Applied === 'object'
    ? response.Applied
    : {};

  return {
    Running: Array.isArray(response.Running) ? response.Running : [],
    Pending: Array.isArray(response.Pending) ? response.Pending : [],
    Applied: {
      MaxTasks: nonNegativeInteger(applied.MaxTasks, CLUSTER_TASK_DEFAULTS.maxTasks),
      MaxPending: nonNegativeInteger(applied.MaxPending, CLUSTER_TASK_DEFAULTS.maxPending),
      IncludeBackground: Boolean(applied.IncludeBackground),
      TaskName: typeof applied.TaskName === 'string' ? applied.TaskName : '',
    },
    RunningTotal: nonNegativeInteger(response.RunningTotal),
    PendingTotal: nonNegativeInteger(response.PendingTotal),
    TotalsAvailable: Boolean(response.TotalsAvailable),
    Partial: Boolean(response.Partial),
    ObservedAt: typeof response.ObservedAt === 'string' ? response.ObservedAt : '',
    TaskTypes: Array.isArray(response.TaskTypes)
      ? response.TaskTypes.filter((entry) => entry && typeof entry.Name === 'string')
      : [],
    TaskTypesAvailable: Boolean(response.TaskTypesAvailable),
    Warnings: Array.isArray(response.Warnings)
      ? response.Warnings.filter((entry) => entry && typeof entry.Message === 'string')
      : [],
  };
}

export function createClusterTaskViewState() {
  return {
    response: null,
    hasSuccessfulLoad: false,
    refreshing: false,
    stale: false,
    error: '',
    lastSuccessAt: null,
  };
}

export function beginClusterTaskRefresh(state) {
  return {
    ...state,
    refreshing: true,
    error: '',
  };
}

export function completeClusterTaskRefresh(state, value, completedAt) {
  return {
    ...state,
    response: normalizeClusterTaskResponse(value),
    hasSuccessfulLoad: true,
    refreshing: false,
    stale: false,
    error: '',
    lastSuccessAt: completedAt,
  };
}

export function failClusterTaskRefresh(state, error) {
  return {
    ...state,
    refreshing: false,
    stale: state.hasSuccessfulLoad,
    error: error?.message || String(error || 'Cluster task refresh failed'),
  };
}

export function cancelClusterTaskRefresh(state) {
  if (!state.refreshing) {
    return state;
  }
  return {
    ...state,
    refreshing: false,
  };
}

function groupsFor(entries, coalesceEntries) {
  return coalesceEntries
    ? groupConsecutiveTasks(entries)
    : entries.map((entry) => [entry]);
}

export function buildClusterTaskSections(response, coalesceEntries) {
  const normalized = normalizeClusterTaskResponse(response);
  return [
    {
      key: 'running',
      title: 'Running',
      ageLabel: 'Took',
      ageTooltip: `${RUNNING_AGE_TOOLTIP} ${INTERPOLATED_AGE_TOOLTIP}`,
      entries: normalized.Running,
      groups: groupsFor(normalized.Running, coalesceEntries),
      total: normalized.RunningTotal,
    },
    {
      key: 'pending',
      title: 'Pending preview',
      ageLabel: 'Waiting',
      ageTooltip: `${PENDING_AGE_TOOLTIP} ${INTERPOLATED_AGE_TOOLTIP}`,
      entries: normalized.Pending,
      groups: groupsFor(normalized.Pending, coalesceEntries),
      total: normalized.PendingTotal,
    },
  ];
}
