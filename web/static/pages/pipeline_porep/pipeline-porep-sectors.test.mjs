import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const source = readFileSync(new URL('./pipeline-porep-sectors.mjs', import.meta.url), 'utf8');

function mount() {
    const timers = [];
    const requests = [];
    let Component;
    const template = (strings, ...values) => strings.reduce((s, part, i) => s + part + (values[i] ?? ''), '');
    // Run the actual component, replacing only browser imports, RPC and timers.
    vm.runInNewContext(source.replace(/^import .*;$/gm, '').replace(/^export /gm, ''), {
        LitElement: class { requestUpdate() { this.rendered = this.render(); } },
        html: template,
        css: template,
        formatDateTwo: () => ['', ''],
        RPCCall: (method) => {
            assert.equal(method, 'PipelinePorepSectors');
            return new Promise((resolve, reject) => requests.push({ resolve, reject }));
        },
        setTimeout: (callback, delay) => {
            assert.equal(delay, 3000);
            timers.push(callback);
        },
        customElements: { define: (_, component) => { Component = component; } },
    });
    const loadData = Component.prototype.loadData;
    Component.prototype.loadData = function () {
        this.pending = loadData.call(this);
        // Observe rejection on the unfixed component without an unhandled promise.
        this.pending.catch(() => {});
        return this.pending;
    };
    const page = new Component();
    return {
        page,
        async respond(value) {
            assert.equal(requests.length, 1, 'only one RPC in flight');
            assert.equal(timers.length, 0, 'no timer while RPC is pending');
            const request = requests.shift();
            if (value instanceof Error) request.reject(value);
            else request.resolve(value);
            await page.pending.catch(() => {});
            assert.equal(timers.length, 1, 'request completion must schedule exactly one refresh');
        },
        refresh() {
            assert.equal(timers.length, 1);
            timers.shift()();
            assert.equal(requests.length, 1);
            assert.equal(timers.length, 0);
        },
    };
}

for (const initialFailure of [false, true]) {
    test(initialFailure ? 'initial failure retries and recovers' : 'failed refresh preserves data and recovers', async () => {
        const h = mount();
        const previous = [{ Address: 'f01000', SectorNumber: 7 }];
        if (!initialFailure) {
            await h.respond(previous);
            h.refresh();
        }
        const saved = h.page.data;
        for (let i = 0; i < 2; i++) {
            await h.respond(new Error('RPC unavailable'));
            assert.equal(h.page.data, saved, 'failure must preserve the last successful data');
            assert.match(h.page.rendered, /Refresh failed; retrying/);
            if (initialFailure) assert.equal(h.page.data.length, 0);
            else assert.match(h.page.rendered, /f01000/);
            h.refresh();
        }
        const recovered = [{ Address: 'f02000', SectorNumber: 8 }];
        await h.respond(recovered);
        assert.equal(h.page.data, recovered);
        assert.match(h.page.rendered, /f02000/);
        assert.doesNotMatch(h.page.rendered, /Refresh failed/);
    });
}
