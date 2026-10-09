// Offline Chromium fixture. All routes are fulfilled locally; no Curio/DB or
// chain server is started. Supply existing Playwright/Chromium/Lit artifacts.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../../', import.meta.url));
const {chromium} = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const baseline = process.env.POREP_BASELINE;
const negative = process.env.POREP_NEGATIVE;
const files = new Map();
function source(name) {
    if (!files.has(name)) files.set(name, baseline
        ? execFileSync('git', ['show', `${baseline}:web/static${name}`], {cwd:root, encoding:'utf8', maxBuffer:8*1024*1024})
        : readFileSync(path.join(root, 'web/static', name), 'utf8'));
    let value = files.get(name);
    // Local negative control only; never edits a source file or commit.
    if (negative === 'loading' && name.endsWith('/pipeline-porep-sectors.mjs')) {
        value = value.replace('this.snapshot = null;', 'this.snapshot = {Sectors: [], Total: 0, Matching: 0, Offset: 0, Limit: 100};');
    }
    return value;
}

// 31,183 pending rows precede 294 active/failed/post-SDR rows in source order.
// This is a fake SQL result planner, NOT an execution test of porepPageQuery.
const rows = Array.from({length:31477}, (_, i) => ({
    SpID:1000, Address:'f01000', SectorNumber:i+1, CreateTime:'2026-01-01T00:00:00Z',
    TaskSDR:i+100, SDROwned:i>=31183, StartedSDR:i>=31183,
    AfterSDR:false, Failed:false, MissingTasks:[],
    ChainSector:null, ChainActive:null, ChainAlloc:null, AfterSeed:null,
    fixtureOwner:i>=31183 ? (i-31183)%44 : null,
}));
rows[31184].Failed = true; rows[31184].SDROwned = false;
rows[31185].AfterSDR = true; rows[31185].SDROwned = false;
const useful = r => r.Failed || r.AfterSDR || r.SDROwned;
const ordered = [...rows].sort((a,b) => Number(!useful(a))-Number(!useful(b)) || a.SpID-b.SpID || a.SectorNumber-b.SectorNumber);
function snapshot(req, empty = false) {
    const filtered = empty ? [] : ordered.filter(r => !req.HidePendingSDR || useful(r));
    return {Sectors:filtered.slice(req.Offset,req.Offset+100), Total:empty?0:rows.length,
        Matching:filtered.length, WaitingForPrecommit:7, WaitingForCommit:9,
        ObservedAt:'2026-01-01T00:00:00Z', Offset:req.Offset, Limit:100, HidePendingSDR:req.HidePendingSDR};
}

const browser = await chromium.launch({headless:true, executablePath:process.env.CHROMIUM_EXECUTABLE});
const results = [];
try {
    const page = await browser.newPage({viewport:{width:1500,height:1100}});
    const errors = [], calls = [], held = [], inFlight = new Set();
    let peakHTTP = 0;
    page.on('pageerror', e => errors.push(e.message));
    page.on('request', req => {
        if(req.url().endsWith('/api/webrpc/v0')) { inFlight.add(req);peakHTTP=Math.max(peakHTTP,inFlight.size); }
    });
    page.on('requestfinished', req => inFlight.delete(req));
    page.on('requestfailed', req => inFlight.delete(req));
    let behavior = 'hold';
    await page.clock.install();
    await page.route('**/*', async route => {
        const url = new URL(route.request().url());
        if (url.href.includes('/lit/dist@3/')) return route.fulfill({contentType:'text/javascript',body:readFileSync(process.env.LIT3_FIXTURE,'utf8')});
        if (url.href.includes('/lit/dist@2/')) return route.fulfill({contentType:'text/javascript',body:readFileSync(process.env.LIT2_FIXTURE,'utf8')});
        if (url.origin !== 'http://porep.invalid') return route.abort();
        if (url.pathname === '/') return route.fulfill({contentType:'text/html',body:`<!doctype html><meta charset="utf-8"><body style="background:#171c24;color:#e6edf3;font:14px sans-serif"><h2>PoRep progress — offline synthetic fixture</h2><pipeline-porep-sectors></pipeline-porep-sectors><script type="module" src="/pages/pipeline_porep/pipeline-porep-sectors.mjs"></script></body>`});
        if (url.pathname === '/api/webrpc/v0') {
            const request = route.request().postDataJSON();
            assert.equal(request.method, 'CurioWeb.PipelinePorepPage');
            const entry = {request, aborted:false}; calls.push(entry);
            const finish = async (result, error) => {
                try { await route.fulfill({contentType:'application/json',body:JSON.stringify({jsonrpc:'2.0',id:request.id,...(error?{error:{code:-1,message:error}}:{result})})}); }
                catch { entry.aborted=true; }
            };
            if (behavior === 'hold') { held.push({entry,finish}); return; }
            if (behavior === 'fail') return finish(null,'synthetic query failure');
            return finish(snapshot(request.params[0], behavior==='empty'));
        }
        if (url.pathname === '/lib/jsonrpc.mjs' && baseline) return route.fulfill({contentType:'text/javascript',body:`
            export default async function(method) {
                window.rpcCounts ??= {}; window.rpcCounts[method]=(window.rpcCounts[method]||0)+1;
                if(method==='PipelinePorepSectors') return new Promise(()=>{});
                return new Promise(resolve=>{(window.pendingStatuses??=[]).push(resolve)});
            }`});
        try { return route.fulfill({contentType:url.pathname.endsWith('.css')?'text/css':'text/javascript',body:source(url.pathname)}); }
        catch { return route.abort(); }
    });
    // The unchanged JSON-RPC module's one-time version WS is satisfied locally.
    await page.routeWebSocket('**/*', ws => ws.onMessage(message => {
        const req=JSON.parse(message); ws.send(JSON.stringify({jsonrpc:'2.0',id:req.id,result:'synthetic fixture'}));
    }));
    await page.goto('http://porep.invalid');
    await page.waitForFunction(() => document.querySelector('pipeline-porep-sectors')?.shadowRoot?.querySelector('input'));
    const text = () => page.evaluate(() => document.querySelector('pipeline-porep-sectors').shadowRoot.textContent);

    if (baseline) {
        const state=await text();
        if (process.env.POREP_ASSERT_FIXED) assert.match(state,/Loading PoRep sectors/, 'pending first RPC must not look like a successful zero');
        assert.match(state,/Showing 0 of 0/);
        // Execute the real render functions for the complete fixture, without
        // attaching a million-cell table. Counts are TemplateResults, not DOM
        // timing/memory measurements. Then execute actual per-task lifecycle
        // hooks with rendering suppressed, isolating timer/RPC fanout safely.
        const renderStart = performance.now();
        const metrics = await page.evaluate(async fixtureRows => {
            const el=document.querySelector('pipeline-porep-sectors');
            el.requestUpdate=()=>{}; el.performUpdate=()=>{}; el.data=fixtureRows;
            let rowCount=0, taskCount=0;
            const original=el.renderSectorRow;
            el.renderSectorRow=function(row){rowCount++;return original.call(this,row)};
            const walk=value=>{
                if(Array.isArray(value)) { for(const item of value)walk(item); }
                else if(value?.strings) { taskCount+=(value.strings.join('').match(/<task-status\b/g)||[]).length;for(const item of value.values)walk(item); }
            };
            const reports=[];
            for(const hide of [false,true]) {
                el.hidePendingSDR=hide; rowCount=0;taskCount=0;
                walk(el.render());
                reports.push({hide,renderRows:rowCount,taskElements:taskCount});
            }
            const Task=customElements.get('task-status'), tasks=[];
            // Collect actual interval callbacks instead of exercising a fake
            // timer library's quadratic scheduling of 31k same-deadline timers.
            const intervals=new Map();let timerID=0;
            const setInterval=window.setInterval,clearInterval=window.clearInterval;
            window.setInterval=(callback,ms)=>{if(ms!==2500)throw new Error('unexpected task interval');const id=++timerID;intervals.set(id,callback);return id};
            window.clearInterval=id=>intervals.delete(id);
            for(let i=0;i<reports[0].taskElements;i++) {
                const task=new Task();task.performUpdate=()=>{};task.requestUpdate=()=>{};task.taskId=i+100;
                task.connectedCallback();tasks.push(task);
            }
            const initialCalls=window.rpcCounts.GetTaskStatus;
            for(const callback of intervals.values())callback();
            const secondTickCalls=window.rpcCounts.GetTaskStatus;
            const pending=window.pendingStatuses.length;
            for(const task of tasks)task.disconnectedCallback();
            const timersAfterDisconnect=intervals.size;
            const hiddenBefore=window.rpcCounts.GetTaskStatus;
            const hiddenTasks=[];
            for(let i=0;i<reports[1].taskElements;i++) {
                const task=new Task();task.performUpdate=()=>{};task.requestUpdate=()=>{};task.taskId=i+100;
                task.connectedCallback();hiddenTasks.push(task);
            }
            const hiddenInitialCalls=window.rpcCounts.GetTaskStatus-hiddenBefore;
            for(const callback of intervals.values())callback();
            const hiddenSecondTickCalls=window.rpcCounts.GetTaskStatus-hiddenBefore;
            for(const task of hiddenTasks)task.disconnectedCallback();
            window.setInterval=setInterval;window.clearInterval=clearInterval;
            return {reports,initialCalls,secondTickCalls,pending,timersAfterDisconnect,hiddenInitialCalls,hiddenSecondTickCalls,hiddenTimersAfterDisconnect:intervals.size};
        },rows);
        metrics.protocolRenderAndHookWallMs = performance.now() - renderStart;
        assert.equal(metrics.reports[0].renderRows,31477);assert.equal(metrics.reports[0].taskElements,31477);
        assert.equal(metrics.reports[1].renderRows,294);assert.equal(metrics.secondTickCalls,62954);assert.equal(metrics.pending,62954);
        assert.equal(metrics.timersAfterDisconnect,0);
        assert.equal(metrics.hiddenInitialCalls,294);assert.equal(metrics.hiddenSecondTickCalls,588);assert.equal(metrics.hiddenTimersAfterDisconnect,0);
        results.push({name:'baseline-first-pending-and-31k-real-render/lifecycle',metrics,
            limitation:'Full 31k DOM not mounted. Real TemplateResult traversal and task connected/poll callbacks with suppressed rendering. RPC boundary deferred; not network/DB RPS.'});
    } else {
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').poller.run !== null);
        const host=page.locator('pipeline-porep-sectors');
        await page.evaluate(async()=>await document.querySelector('pipeline-porep-sectors').updateComplete);
        assert.match(await text(),/Loading PoRep sectors/, 'pending first RPC must not look like a successful zero');
        assert.equal(await host.locator('.counts').count(),0);
        await page.clock.fastForward(40000);
        assert.equal(calls.length,1);assert.equal(await host.locator('.counts').count(),0);
        await held.shift().finish(snapshot({Offset:0,HidePendingSDR:false}));
        await host.locator('.counts').waitFor();
        const inspect=()=>page.evaluate(()=>{const el=document.querySelector('pipeline-porep-sectors');return {
            sectors:el.data.map(r=>r.SectorNumber), owners:[...new Set(el.data.map(r=>r.fixtureOwner))],
            rows:el.shadowRoot.querySelector('table.table > tbody').children.length,
            tasks:el.shadowRoot.querySelectorAll('task-status').length,
            count:el.shadowRoot.querySelector('.counts')?.textContent,
            status:el.shadowRoot.querySelector('[role=status]').textContent,
        }});
        const first=await inspect();assert.equal(first.rows,100);assert.equal(first.tasks,0);assert.equal(first.owners.length,44);
        assert.ok(first.sectors.every(n=>n>31183));assert.match(first.status,/Snapshot loaded/);
        assert.match(await host.locator('table.table > tbody > tr').nth(1).innerText(),/Failed/);
        assert.match(await host.locator('table.table > tbody > tr').nth(2).innerText(),/done/);
        assert.match(first.count,/31477 matching of 31477/);
        behavior='rows';await page.clock.fastForward(5000);await page.waitForFunction(()=>!document.querySelector('pipeline-porep-sectors').loading);
        assert.equal(calls.length,2);
        await host.getByRole('button',{name:'Next',exact:true}).click();
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').snapshot.Offset===100);
        const second=await inspect();assert.equal(second.rows,100);assert.ok(!second.sectors.some(n=>first.sectors.includes(n)));
        await host.locator('input').setChecked(true);
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').snapshot.HidePendingSDR);
        assert.match((await inspect()).count,/294 matching of 31477/);
        await host.getByRole('button',{name:'Next',exact:true}).click();
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').snapshot.Offset===100);
        await host.getByRole('button',{name:'Next',exact:true}).click();
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').snapshot.Offset===200);
        const lastActive=await inspect();assert.equal(lastActive.rows,94);assert.ok(lastActive.sectors.every(n=>n>31183));
        assert.equal(await host.getByRole('button',{name:'Next',exact:true}).isDisabled(),true);
        await host.locator('input').setChecked(false);
        await page.waitForFunction(()=>!document.querySelector('pipeline-porep-sectors').snapshot.HidePendingSDR);
        await page.evaluate(()=>document.querySelector('pipeline-porep-sectors').changePage(31400));
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').snapshot.Offset===31400);
        assert.equal((await inspect()).rows,77);
        behavior='fail';await host.getByRole('button',{name:'Refresh now'}).click();
        await page.waitForFunction(()=>!!document.querySelector('pipeline-porep-sectors').error);
        assert.equal((await inspect()).rows,77);assert.match((await inspect()).status,/Stale snapshot/);
        behavior='rows';await page.clock.fastForward(10000);
        await page.waitForFunction(()=>!document.querySelector('pipeline-porep-sectors').error);
        behavior='empty';await host.getByRole('button',{name:'Refresh now'}).click();
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').snapshot.Total===0);
        assert.equal((await inspect()).rows,0);assert.match(await text(),/No pipeline sectors/);
        behavior='hold';await host.getByRole('button',{name:'Refresh now'}).click();
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').poller.run !== null);
        await host.getByRole('button',{name:'Pause refresh'}).click();
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').poller.run === null);
        const pausedCalls=calls.length;await page.clock.fastForward(120000);assert.equal(calls.length,pausedCalls);
        behavior='rows';await host.getByRole('button',{name:'Resume refresh'}).click();
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').snapshot.Total===31477);
        for(const old of held.splice(0)) await old.finish(snapshot({Offset:0,HidePendingSDR:false},true));
        assert.equal(await page.evaluate(()=>document.querySelector('pipeline-porep-sectors').snapshot.Total),31477);
        await page.evaluate(()=>{window.saved=document.querySelector('pipeline-porep-sectors');window.saved.remove()});
        const detachedCalls=calls.length;await page.clock.fastForward(120000);assert.equal(calls.length,detachedCalls);
        behavior='fail';await page.evaluate(()=>document.body.append(window.saved));
        await page.waitForFunction(()=>!!document.querySelector('pipeline-porep-sectors').error);
        behavior='rows';await page.clock.fastForward(10000);
        await page.waitForFunction(()=>!document.querySelector('pipeline-porep-sectors').error);
        // A newly mounted component must distinguish first failure from zero,
        // then retry; exercise real HTTP cancellation at its 60s deadline.
        behavior='fail';
        await page.evaluate(()=>{
            document.querySelector('pipeline-porep-sectors').remove();
            document.body.append(document.createElement('pipeline-porep-sectors'));
        });
        await page.waitForFunction(()=>!!document.querySelector('pipeline-porep-sectors').error);
        assert.equal(await host.locator('.counts').count(),0);
        assert.match(await text(),/Unable to load/);
        behavior='rows';await page.clock.fastForward(10000);
        await page.waitForFunction(()=>!!document.querySelector('pipeline-porep-sectors').snapshot);
        behavior='hold';await host.getByRole('button',{name:'Refresh now'}).click();
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').poller.run !== null);
        await page.clock.fastForward(60000);
        await page.waitForFunction(()=>document.querySelector('pipeline-porep-sectors').poller.run === null);
        assert.match(await text(),/timed out/);
        behavior='rows';await page.clock.fastForward(10000);
        await page.waitForFunction(()=>!document.querySelector('pipeline-porep-sectors').error);
        await page.evaluate(()=>{
            Object.defineProperty(document,'hidden',{configurable:true,value:true});
            document.dispatchEvent(new Event('visibilitychange'));
        });
        const hiddenCalls=calls.length;await page.clock.fastForward(120000);assert.equal(calls.length,hiddenCalls);
        await page.evaluate(()=>{
            Object.defineProperty(document,'hidden',{configurable:true,value:false});
            document.dispatchEvent(new Event('visibilitychange'));
        });
        await page.waitForFunction(()=>!document.querySelector('pipeline-porep-sectors').loading);
        assert.equal(peakHTTP,1, 'one outstanding HTTP request across refresh/cancel/reconnect');
        results.push({name:'actual-component-http-page-lifecycle',first,second,lastActive,requests:calls.length,
            periodicPageRPCs:1,perTaskRPCs:0,peakHTTP,maxDOMRows:100,fixtureRows:rows.length,owners:44,
            limitation:'SQL/chain substituted; actual component/Lit DOM and HTTP AbortSignal path executed offline.'});
        if(process.env.POREP_SCREENSHOT)await page.screenshot({path:process.env.POREP_SCREENSHOT});
    }
    assert.deepEqual(errors,[]);
    console.log(JSON.stringify({status:'PASS',baseline:baseline||null,results},null,2));
} finally { await browser.close(); }
