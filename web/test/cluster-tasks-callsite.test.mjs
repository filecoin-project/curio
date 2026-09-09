import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

test('production component uses the bounded controller and display clock', () => {
  const source = readFileSync(new URL('../static/cluster-tasks.mjs', import.meta.url), 'utf8');
  for (const token of ['new ClusterTaskPollController(', "'ClusterTaskSummaryLimited'", 'new ClusterTaskFreshnessTicker(', 'resetClusterTaskDisplayClock(this.monotonicNow())', 'freezeClusterTaskDisplayClock(this.displayClock)', 'interpolateClusterTaskAgeSeconds(']) {
    assert.ok(source.includes(token), `production component missing ${token}`);
  }
  assert.ok(!source.includes("RPCCall('ClusterTaskSummary')"));
});
