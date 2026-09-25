import test from 'node:test';
import assert from 'node:assert/strict';
import {clusterTaskAge} from '../static/cluster-tasks-age.mjs';
import {resetClusterTaskDisplayClock, advanceClusterTaskDisplayClock, freezeClusterTaskDisplayClock} from '../static/cluster-tasks-model.mjs';

test('Took uses only confirmed current-attempt seconds, never Posted or ownership age', () => {
  const task={ID:1,OwnerID:7,AgeSeconds:10800,TookSeconds:600,TookState:'running',AttemptID:'one'};
  const clock=advanceClusterTaskDisplayClock(resetClusterTaskDisplayClock(100),2600);
  assert.equal(clusterTaskAge(task,clock).seconds,602);
  for(const state of ['unknown','awaiting-start','future-start']) {
    const value=clusterTaskAge({...task,TookState:state},clock);
    assert.equal(value.seconds,null);
    assert.ok(value.text==='unknown'||value.text==='—');
  }
  assert.equal(clusterTaskAge({...task,OwnerID:null},clock).seconds,10802);
});

test('new same-owner attempt replaces Took with a lower authoritative baseline', () => {
  const old={ID:1,OwnerID:7,TookState:'running',TookSeconds:600,AttemptID:'old'};
  const running=advanceClusterTaskDisplayClock(resetClusterTaskDisplayClock(100),2500);
  const frozen=freezeClusterTaskDisplayClock(running);
  assert.equal(clusterTaskAge(old,advanceClusterTaskDisplayClock(frozen,1e9)).seconds,602);
  assert.equal(clusterTaskAge({...old,TookSeconds:1,AttemptID:'new'},resetClusterTaskDisplayClock(1e9)).seconds,1);
});
