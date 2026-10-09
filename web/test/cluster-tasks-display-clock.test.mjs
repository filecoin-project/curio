import assert from 'node:assert/strict';
import test from 'node:test';

import {
  advanceClusterTaskDisplayClock,
  beginClusterTaskRefresh,
  buildClusterTaskSections,
  completeClusterTaskRefresh,
  createClusterTaskDisplayClock,
  createClusterTaskViewState,
  failClusterTaskRefresh,
  formatClusterTaskFreshness,
  freezeClusterTaskDisplayClock,
  interpolateClusterTaskAgeSeconds,
  resetClusterTaskDisplayClock,
} from '../static/cluster-tasks-model.mjs';

test('display ages derive whole seconds from one monotonic receipt anchor', () => {
  let clock = resetClusterTaskDisplayClock(10_000);
  const baselineAge = 90;

  clock = advanceClusterTaskDisplayClock(clock, 10_999);
  assert.equal(interpolateClusterTaskAgeSeconds(baselineAge, clock), 90);

  clock = advanceClusterTaskDisplayClock(clock, 11_001);
  assert.equal(interpolateClusterTaskAgeSeconds(baselineAge, clock), 91);

  clock = advanceClusterTaskDisplayClock(clock, 13_900);
  assert.equal(interpolateClusterTaskAgeSeconds(baselineAge, clock), 93);

  for (const repeatedCallback of [13_901, 13_999, 14_001]) {
    clock = advanceClusterTaskDisplayClock(clock, repeatedCallback);
  }
  assert.equal(clock.elapsedSeconds, 4, 'callback frequency must not accumulate drift');
  assert.equal(interpolateClusterTaskAgeSeconds(baselineAge, clock), 94);
});

test('unknown ages stay unknown and server/client wall clocks do not affect interpolation', () => {
  let clock = resetClusterTaskDisplayClock(500);
  clock = advanceClusterTaskDisplayClock(clock, 2_750);

  for (const unavailable of [null, undefined, Number.NaN, -1]) {
    assert.equal(interpolateClusterTaskAgeSeconds(unavailable, clock), null);
  }

  const snapshotsWithOppositeWallClockSkew = [
    {ObservedAt: '1970-01-01T00:00:00Z', AgeSeconds: 12},
    {ObservedAt: '2099-01-01T00:00:00Z', AgeSeconds: 12},
  ];
  assert.deepEqual(
      snapshotsWithOppositeWallClockSkew.map((entry) =>
        interpolateClusterTaskAgeSeconds(entry.AgeSeconds, clock)),
      [14, 14],
  );
});

test('pause or failure freezes the last displayed age until a new snapshot', () => {
  let clock = resetClusterTaskDisplayClock(1_000);
  clock = advanceClusterTaskDisplayClock(clock, 3_100);
  const displayedBeforeFreeze = interpolateClusterTaskAgeSeconds(20, clock);

  clock = freezeClusterTaskDisplayClock(clock);
  assert.equal(interpolateClusterTaskAgeSeconds(20, clock), displayedBeforeFreeze);

  clock = advanceClusterTaskDisplayClock(clock, 100_000);
  assert.equal(
      interpolateClusterTaskAgeSeconds(20, clock),
      displayedBeforeFreeze,
      'inactive time must not be extrapolated before a fresh response',
  );

  let viewState = completeClusterTaskRefresh(
      createClusterTaskViewState(),
      {Running: [{ID: 9, AgeSeconds: 20}]},
      1_000,
  );
  viewState = beginClusterTaskRefresh(viewState);
  viewState = failClusterTaskRefresh(viewState, new Error('timeout'));
  assert.equal(viewState.stale, true);
  assert.equal(formatClusterTaskFreshness(11_000, viewState.lastSuccessAt), '10s ago');
  assert.equal(interpolateClusterTaskAgeSeconds(20, clock), displayedBeforeFreeze);
});

test('a successful snapshot resets the baseline and accepts a lower runtime', () => {
  const previousTask = {ID: 44, OwnerID: 7, AgeSeconds: 100};
  let clock = resetClusterTaskDisplayClock(1_000);
  clock = advanceClusterTaskDisplayClock(clock, 5_100);
  assert.equal(interpolateClusterTaskAgeSeconds(previousTask.AgeSeconds, clock), 104);

  const restartedTask = {...previousTask, AgeSeconds: 3};
  clock = resetClusterTaskDisplayClock(10_000);
  assert.equal(
      interpolateClusterTaskAgeSeconds(restartedTask.AgeSeconds, clock),
      3,
      'the new server value is authoritative even for the same task and owner',
  );

  clock = advanceClusterTaskDisplayClock(clock, 11_050);
  assert.equal(interpolateClusterTaskAgeSeconds(restartedTask.AgeSeconds, clock), 4);
});

test('a successful partial-enrichment snapshot still advances task ages', () => {
  const state = completeClusterTaskRefresh(createClusterTaskViewState(), {
    Running: [{ID: 4, AgeSeconds: 7}],
    Partial: true,
    Warnings: [{Code: 'spid', Message: 'SpID unavailable'}],
  }, 1_000);
  let clock = resetClusterTaskDisplayClock(20_000);
  clock = advanceClusterTaskDisplayClock(clock, 22_100);

  assert.equal(state.response.Partial, true);
  assert.equal(
      interpolateClusterTaskAgeSeconds(state.response.Running[0].AgeSeconds, clock),
      9,
  );
});

test('display-clock updates do not mutate or reorder snapshot rows', () => {
  const response = {
    Running: [
      {ID: 8, AgeSeconds: 80},
      {ID: 3, AgeSeconds: 20},
    ],
    Pending: [
      {ID: 12, AgeSeconds: 60},
      {ID: 4, AgeSeconds: 10},
    ],
  };
  const original = structuredClone(response);
  const before = buildClusterTaskSections(response, false)
      .flatMap((section) => section.entries.map((entry) => entry.ID));

  let clock = createClusterTaskDisplayClock();
  clock = resetClusterTaskDisplayClock(50);
  clock = advanceClusterTaskDisplayClock(clock, 8_050);
  response.Running.map((entry) => interpolateClusterTaskAgeSeconds(entry.AgeSeconds, clock));
  response.Pending.map((entry) => interpolateClusterTaskAgeSeconds(entry.AgeSeconds, clock));

  const after = buildClusterTaskSections(response, false)
      .flatMap((section) => section.entries.map((entry) => entry.ID));
  assert.deepEqual(after, before);
  assert.deepEqual(response, original);
});
