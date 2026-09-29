import test from 'node:test';
import assert from 'node:assert/strict';
import {PoRepPagePoller} from '../static/pages/pipeline_porep/page-poller.mjs';

class Clock {
    now = 0; id = 0; timers = new Map();
    set = (fn, ms) => { const id = ++this.id; this.timers.set(id, {fn, at: this.now + ms}); return id; };
    clear = (id) => this.timers.delete(id);
    advance(ms) {
        const end = this.now + ms;
        while (true) {
            const entry = [...this.timers].filter(([, t]) => t.at <= end).sort((a,b) => a[1].at-b[1].at)[0];
            if (!entry) break;
            this.timers.delete(entry[0]); this.now = entry[1].at; entry[1].fn();
        }
        this.now = end;
    }
}
const flush = async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); };
function fixture(honorAbort = true) {
    const clock = new Clock(), calls = [], success = [], errors = [];
    let starts = 0, active = 0, peak = 0;
    const poller = new PoRepPagePoller({
        request(signal) {
            active++; peak = Math.max(peak, active);
            return new Promise((resolve, reject) => {
                const finish = (callback) => (value) => { active--; callback(value); };
                const call = {signal, resolve: finish(resolve), reject: finish(reject)};
                calls.push(call);
                if (honorAbort) signal.addEventListener('abort', () => call.reject(new Error('aborted')), {once:true});
            });
        },
        onStart: () => starts++, onSuccess: value => success.push(value), onError: error => errors.push(error.message),
        setTimer: clock.set, clearTimer: clock.clear,
    });
    return {clock, calls, success, errors, poller, stats: () => ({starts, active, peak})};
}

test('slow success is not empty and does not overlap; successful empty is delivered', async () => {
    const f = fixture(); f.poller.setActive(true);
    f.clock.advance(40000); await flush();
    assert.equal(f.calls.length, 1); assert.deepEqual(f.success, []); assert.deepEqual(f.errors, []);
    f.calls[0].resolve({Sectors:[{SectorNumber:1}]}); await flush();
    assert.equal(f.success.length, 1);
    f.clock.advance(4999); assert.equal(f.calls.length, 1);
    f.clock.advance(1); f.calls[1].resolve({Sectors:[]}); await flush();
    assert.deepEqual(f.success[1], {Sectors:[]}); assert.equal(f.stats().peak, 1);
    f.poller.setActive(false); assert.equal(f.clock.timers.size, 0);
});

test('first and later errors retry with bounded backoff and preserve last success', async () => {
    const f = fixture(); f.poller.setActive(true);
    f.calls[0].reject(new Error('first failure')); await flush();
    assert.equal(f.success.length, 0); f.clock.advance(9999); assert.equal(f.calls.length, 1);
    f.clock.advance(1); f.calls[1].reject(new Error('second failure')); await flush();
    f.clock.advance(20000); f.calls[2].resolve({Sectors:[1]}); await flush();
    f.clock.advance(5000); f.calls[3].reject(new Error('later failure')); await flush();
    assert.deepEqual(f.success, [{Sectors:[1]}]);
    f.clock.advance(10000); f.calls[4].resolve({Sectors:[2]}); await flush();
    assert.equal(f.success.length, 2); assert.equal(f.stats().peak, 1);
    f.poller.setActive(false); assert.equal(f.clock.timers.size, 0);
});

test('timeout aborts transport, then retries; not a promise-only timeout', async () => {
    const f = fixture(); f.poller.setActive(true);
    f.clock.advance(60000); await flush();
    assert.equal(f.calls[0].signal.aborted, true); assert.equal(f.stats().active, 0);
    assert.match(f.errors[0], /timed out/); assert.equal(f.success.length, 0);
    f.clock.advance(10000); assert.equal(f.calls.length, 2);
    f.poller.setActive(false); await flush(); assert.equal(f.clock.timers.size, 0);
});

test('disconnect/reconnect/refresh reject late data and wait for the previous transport', async () => {
    const f = fixture(false); f.poller.setActive(true);
    f.poller.setActive(false); assert.equal(f.calls[0].signal.aborted, true);
    assert.equal(f.clock.timers.size, 0);
    f.poller.setActive(true); f.poller.setActive(true); f.poller.refresh();
    assert.equal(f.calls.length, 1, 'no overlapping transport even if abort is ignored');
    f.calls[0].resolve('old page'); await flush(); assert.deepEqual(f.success, []);
    assert.equal(f.calls.length, 2); f.calls[1].resolve('new page'); await flush();
    assert.deepEqual(f.success, ['new page']); assert.equal(f.stats().peak, 1);
    f.poller.setActive(false); assert.equal(f.clock.timers.size, 0);
});

test('repeated failures reach a finite 30s retry delay; inactive mount has no request', async () => {
    const f = fixture(); f.clock.advance(300000); assert.equal(f.calls.length, 0);
    f.poller.setActive(true);
    for (const delay of [10000,20000,30000,30000]) {
        f.calls.at(-1).reject(new Error('offline')); await flush();
        const count = f.calls.length; f.clock.advance(delay-1); assert.equal(f.calls.length,count);
        f.clock.advance(1); assert.equal(f.calls.length,count+1);
    }
    f.poller.setActive(false); await flush(); assert.equal(f.clock.timers.size, 0);
});
