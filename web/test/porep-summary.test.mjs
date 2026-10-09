import test from 'node:test';
import assert from 'node:assert/strict';
import {sectorCountKeys, summaryTotals, summaryStatus} from '../static/lib/porep-summary.mjs';

const counts = overrides => ({...Object.fromEntries(sectorCountKeys.map(k => [k, 0])),
    ObservedAt: '2026-09-13T00:00:00Z', ...overrides});
const row = (overrides, Actor = 'f01000') => ({Actor, CountSDR: 99999, CountTrees: 88888,
    SectorCounts: counts(overrides)});

test('sum disjoint sectors across miners, never legacy stage sums or display limits', () => {
    const rows = [row({Total:32110, Remaining:32109, Complete:1, PostSDR:1, SDRTotal:32108,
        SDRRunning:4, SDRPreparing:2, SDRWaitingTask:10, SDRWaitingCreate:32092}),
    row({Total:3, Remaining:3, Failed:2, PostSDR:1, SDRTotal:1, SDRFailed:1}, 'f01001')];
    for (const limit of [0, 30, 100, 500]) {
        // These unrelated display controls are not inputs to the aggregator.
        const out = summaryTotals(rows.map(r => ({...r, displayLimit:limit, coalescing:!!limit})));
        assert.equal(out.Total,32113);
        assert.equal(out.Remaining,32112);
        assert.equal(out.SDRRunning,4);
        assert.equal(out.PostSDR,2);
        assert.equal(out.Failed,2);
    }
});

test('missing new fields, malformed and non-snapshot rows are unavailable, not zero', () => {
    assert.equal(summaryTotals(null),null);
    assert.equal(summaryTotals([{Actor:'f01000',CountSDR:30}]),null);
    for (const key of sectorCountKeys) {
        const r = row({});
        delete r.SectorCounts[key];
        assert.equal(summaryTotals([r]),null,key);
    }
    for (const value of [-1, NaN, '0', Infinity, Number.MAX_SAFE_INTEGER+1]) {
        assert.equal(summaryTotals([row({SDRTotal:value})]),null);
    }
    assert.equal(summaryTotals([row({Remaining:1})]),null);
    assert.equal(summaryTotals([row({SDRTotal:1})]),null);
    assert.equal(summaryTotals([row({}),row({})]),null);
    assert.equal(summaryTotals([row({}),row({ObservedAt:'2026-09-13T00:00:01Z'},'f01001')]),null);
    assert.equal(summaryTotals([row({ObservedAt:'invalid'})]),null);
    assert.equal(summaryTotals([]).Total,0); // Successful, explicitly empty result only.
});

test('refresh/failure/hidden states do not turn previous data into current zero', () => {
    assert.match(summaryStatus('loading',false),/Loading/);
    assert.match(summaryStatus('loading',true),/previous/);
    assert.match(summaryStatus('error',false,'query failed'),/unavailable.*query failed/);
    assert.match(summaryStatus('error',true),/Stale.*previous/);
    assert.match(summaryStatus('paused',true),/Paused.*previous/);
    assert.equal(summaryStatus('ok',true),'Snapshot');
});
