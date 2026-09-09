import assert from 'node:assert/strict';
import test from 'node:test';

import {
  CLUSTER_TASK_FRESHNESS_TICK_MS,
  CLUSTER_TASK_SETTLE_MS,
  CLUSTER_TASK_TIMEOUT_MS,
  ClusterTaskFreshnessTicker,
  ClusterTaskPollController,
} from '../static/cluster-tasks-controller.mjs';
import {
  advanceClusterTaskDisplayClock,
  freezeClusterTaskDisplayClock,
  resetClusterTaskDisplayClock,
} from '../static/cluster-tasks-model.mjs';

class FakeClock {
  constructor() {
    this.now = 0;
    this.nextID = 1;
    this.timers = new Map();
  }

  setTimeout(callback, delay) {
    const id = this.nextID++;
    this.timers.set(id, {callback, at: this.now + delay});
    return id;
  }

  clearTimeout(id) {
    this.timers.delete(id);
  }

  advance(duration) {
    const target = this.now + duration;
    while (true) {
      const next = [...this.timers.entries()]
          .filter(([, timer]) => timer.at <= target)
          .sort((left, right) => left[1].at - right[1].at || left[0] - right[0])[0];
      if (!next) {
        break;
      }

      const [id, timer] = next;
      this.timers.delete(id);
      this.now = timer.at;
      timer.callback();
    }
    this.now = target;
  }
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return {promise, resolve, reject};
}

function abortError() {
  const error = new Error('aborted');
  error.name = 'AbortError';
  return error;
}

function requestHarness({honorAbort = true} = {}) {
  const calls = [];
  return {
    calls,
    request(signal) {
      const pending = deferred();
      const call = {signal, ...pending};
      calls.push(call);
      if (honorAbort) {
        signal.addEventListener('abort', () => pending.reject(abortError()), {once: true});
      }
      return pending.promise;
    },
  };
}

async function flushPromises() {
  for (let index = 0; index < 5; index++) {
    await Promise.resolve();
  }
}

function makeController({clock, harness, successes = [], errors = []}) {
  return new ClusterTaskPollController({
    request: (signal) => harness.request(signal),
    onSuccess: (value) => successes.push(value),
    onError: (error) => errors.push(error),
    setTimeoutFn: (callback, delay) => clock.setTimeout(callback, delay),
    clearTimeoutFn: (id) => clock.clearTimeout(id),
  });
}

function activate(controller) {
  controller.setActivity({
    mounted: true,
    viewVisible: true,
    documentVisible: true,
    paused: false,
  });
}

test('freshness ticker starts once, advances, and cleans up', () => {
  const clock = new FakeClock();
  const ticks = [];
  const ticker = new ClusterTaskFreshnessTicker({
    onTick: (now) => ticks.push(now),
    nowFn: () => clock.now,
    setTimeoutFn: (callback, delay) => clock.setTimeout(callback, delay),
    clearTimeoutFn: (id) => clock.clearTimeout(id),
  });

  assert.equal(ticker.start(), true);
  assert.equal(ticker.start(), false);
  assert.deepEqual(ticks, [0]);
  assert.equal(clock.timers.size, 1);

  clock.advance(CLUSTER_TASK_FRESHNESS_TICK_MS);
  assert.deepEqual(ticks, [0, CLUSTER_TASK_FRESHNESS_TICK_MS]);
  assert.equal(clock.timers.size, 1);

  ticker.stop();
  assert.equal(clock.timers.size, 0);
  clock.advance(CLUSTER_TASK_FRESHNESS_TICK_MS * 2);
  assert.deepEqual(ticks, [0, CLUSTER_TASK_FRESHNESS_TICK_MS]);

  assert.equal(ticker.start(), true);
  assert.deepEqual(ticks, [0, CLUSTER_TASK_FRESHNESS_TICK_MS, clock.now]);
  ticker.stop();
});

test('display-clock ticks do not issue monitoring requests', async () => {
  const clock = new FakeClock();
  const harness = requestHarness();
  const controller = makeController({clock, harness});
  let displayClock = resetClusterTaskDisplayClock(clock.now);
  const ticker = new ClusterTaskFreshnessTicker({
    onTick: () => {
      displayClock = advanceClusterTaskDisplayClock(displayClock, clock.now);
    },
    nowFn: () => clock.now,
    setTimeoutFn: (callback, delay) => clock.setTimeout(callback, delay),
    clearTimeoutFn: (id) => clock.clearTimeout(id),
  });

  activate(controller);
  ticker.start();
  assert.equal(harness.calls.length, 1);

  clock.advance(CLUSTER_TASK_FRESHNESS_TICK_MS * 4);
  assert.equal(displayClock.elapsedSeconds, 4);
  assert.equal(harness.calls.length, 1, 'display updates must not trigger RPCs');

  ticker.stop();
  controller.setActivity({mounted: false});
  await flushPromises();
});

test('inactive controllers do not issue requests', () => {
  const clock = new FakeClock();
  const harness = requestHarness();
  const controller = makeController({clock, harness});

  controller.setActivity({mounted: true, documentVisible: true});
  controller.refreshNow();
  clock.advance(CLUSTER_TASK_SETTLE_MS * 2);

  assert.equal(harness.calls.length, 0);
});

test('polling starts once, never overlaps, and waits 5s after settlement', async () => {
  const clock = new FakeClock();
  const harness = requestHarness();
  const successes = [];
  const controller = makeController({clock, harness, successes});

  activate(controller);
  controller.setActivity({viewVisible: true});
  assert.equal(harness.calls.length, 1);

  clock.advance(CLUSTER_TASK_SETTLE_MS * 2);
  assert.equal(harness.calls.length, 1, 'an unresolved request must not overlap another');

  harness.calls[0].resolve('first');
  await flushPromises();
  assert.deepEqual(successes, ['first']);

  clock.advance(CLUSTER_TASK_SETTLE_MS - 1);
  assert.equal(harness.calls.length, 1);
  clock.advance(1);
  assert.equal(harness.calls.length, 2);

  controller.setActivity({mounted: false});
  await flushPromises();
});

test('manual refresh replaces one running request without fan-out', async () => {
  const clock = new FakeClock();
  const harness = requestHarness({honorAbort: false});
  const successes = [];
  const controller = makeController({clock, harness, successes});

  activate(controller);
  controller.refreshNow();
  controller.refreshNow();
  assert.equal(harness.calls.length, 1);
  assert.equal(harness.calls[0].signal.aborted, true);

  await flushPromises();
  assert.equal(harness.calls.length, 2);
  assert.deepEqual(successes, [], 'the invalidated generation must not commit');

  harness.calls[1].resolve('current');
  await flushPromises();
  assert.deepEqual(successes, ['current']);

  harness.calls[0].resolve('obsolete');
  await flushPromises();
  assert.deepEqual(successes, ['current']);

  controller.setActivity({mounted: false});
});

test('disconnect, close, document hiding, and pause abort and resume once', async (t) => {
  const cases = [
    {name: 'disconnect', stop: {mounted: false}, resume: {mounted: true}},
    {name: 'drawer close', stop: {viewVisible: false}, resume: {viewVisible: true}},
    {name: 'document hidden', stop: {documentVisible: false}, resume: {documentVisible: true}},
    {name: 'pause', stop: {paused: true}, resume: {paused: false}},
  ];

  for (const testCase of cases) {
    await t.test(testCase.name, async () => {
      const clock = new FakeClock();
      const harness = requestHarness();
      const controller = makeController({clock, harness});

      activate(controller);
      assert.equal(harness.calls.length, 1);
      controller.setActivity(testCase.stop);
      assert.equal(harness.calls[0].signal.aborted, true);

      controller.setActivity(testCase.resume);
      await flushPromises();
      assert.equal(harness.calls.length, 2, 'resume must issue exactly one immediate request');

      controller.setActivity({mounted: false});
      await flushPromises();
      clock.advance(CLUSTER_TASK_SETTLE_MS * 2);
      assert.equal(harness.calls.length, 2, 'inactive controllers must not reschedule');
    });
  }
});

test('a stale completion after close and reopen cannot overwrite new data', async () => {
  const clock = new FakeClock();
  const harness = requestHarness({honorAbort: false});
  const successes = [];
  const controller = makeController({clock, harness, successes});

  activate(controller);
  controller.setActivity({viewVisible: false});
  controller.setActivity({viewVisible: true});
  await flushPromises();

  assert.equal(harness.calls.length, 2);
  assert.deepEqual(successes, []);
  harness.calls[1].resolve('fresh');
  await flushPromises();
  assert.deepEqual(successes, ['fresh']);

  harness.calls[0].resolve('stale');
  await flushPromises();
  assert.deepEqual(successes, ['fresh']);

  controller.setActivity({mounted: false});
});

test('request timeout settles at 15s and retries even when transport abort lags', async () => {
  const clock = new FakeClock();
  const harness = requestHarness({honorAbort: false});
  const errors = [];
  const controller = makeController({clock, harness, errors});

  activate(controller);
  clock.advance(CLUSTER_TASK_TIMEOUT_MS - 1);
  assert.equal(harness.calls[0].signal.aborted, false);
  clock.advance(1);
  assert.equal(harness.calls[0].signal.aborted, true);
  await flushPromises();

  assert.equal(errors.length, 1);
  assert.equal(errors[0].name, 'TimeoutError');
  clock.advance(CLUSTER_TASK_SETTLE_MS - 1);
  assert.equal(harness.calls.length, 1);
  clock.advance(1);
  assert.equal(harness.calls.length, 2);

  harness.calls[0].resolve('late');
  await flushPromises();
  assert.equal(errors.length, 1, 'late completion must not alter timeout state');

  controller.setActivity({mounted: false});
  await flushPromises();
});

test('timeout freezes display ages while the shared ticker can update freshness', async () => {
  const clock = new FakeClock();
  const harness = requestHarness({honorAbort: false});
  const errors = [];
  let freshnessTicks = 0;
  let displayClock = resetClusterTaskDisplayClock(clock.now);
  const ticker = new ClusterTaskFreshnessTicker({
    onTick: () => {
      freshnessTicks += 1;
      displayClock = advanceClusterTaskDisplayClock(displayClock, clock.now);
    },
    nowFn: () => clock.now,
    setTimeoutFn: (callback, delay) => clock.setTimeout(callback, delay),
    clearTimeoutFn: (id) => clock.clearTimeout(id),
  });
  const controller = new ClusterTaskPollController({
    request: (signal) => harness.request(signal),
    onError: (error) => {
      errors.push(error);
      displayClock = freezeClusterTaskDisplayClock(displayClock);
    },
    setTimeoutFn: (callback, delay) => clock.setTimeout(callback, delay),
    clearTimeoutFn: (id) => clock.clearTimeout(id),
  });

  activate(controller);
  ticker.start();
  clock.advance(CLUSTER_TASK_TIMEOUT_MS);
  await flushPromises();

  assert.equal(errors[0].name, 'TimeoutError');
  const frozenAge = displayClock.elapsedSeconds;
  const ticksAtFailure = freshnessTicks;
  clock.advance(CLUSTER_TASK_FRESHNESS_TICK_MS * 2);
  assert.equal(displayClock.elapsedSeconds, frozenAge);
  assert.ok(freshnessTicks > ticksAtFailure, 'freshness may keep updating while stale');

  ticker.stop();
  controller.setActivity({mounted: false});
  await flushPromises();
});

test('ordinary request failures retry 5s after settlement', async () => {
  const clock = new FakeClock();
  const harness = requestHarness();
  const errors = [];
  const controller = makeController({clock, harness, errors});

  activate(controller);
  harness.calls[0].reject(new Error('offline'));
  await flushPromises();

  assert.equal(errors.length, 1);
  assert.equal(errors[0].message, 'offline');
  clock.advance(CLUSTER_TASK_SETTLE_MS - 1);
  assert.equal(harness.calls.length, 1);
  clock.advance(1);
  assert.equal(harness.calls.length, 2);

  controller.setActivity({mounted: false});
  await flushPromises();
});
