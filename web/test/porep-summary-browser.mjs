// Real Chromium + Lit, offline RPC fixtures only. No chain/Curio/DB access.
// POREP_BASELINE renders the same fixture with a preserved Git source revision.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
import {sectorCountKeys} from '../static/lib/porep-summary.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const {chromium} = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const baseline = process.env.POREP_BASELINE;
function source(name) {
    return baseline ? execFileSync('git', ['show', `${baseline}:web/static${name}`], {cwd:root,encoding:'utf8'})
        : readFileSync(path.join(root,'web/static',name),'utf8');
}
function fixture(running = 4) {
    const c = {...Object.fromEntries(sectorCountKeys.map(k => [k,0])), ObservedAt:'2026-09-13T01:00:00Z',
        Total:32124, Complete:1, Remaining:32123, Failed:3, PostSDR:8,
        SDRTotal:32113, SDRRunning:running, SDRPreparing:2, SDRWaitingTask:10,
        SDRWaitingCreate:32092+(4-running), SDRFailed:1, SDRMissingTask:1, SDROtherTask:1, SDRUnknown:2};
    // CountSDR=32113 includes queue/failure/linkage categories, not 4 running.
    return [{Actor:'f01000',CountSDR:32113,CountTrees:5,CountPrecommitMsg:2,CountWaitSeed:1,
        CountPoRep:3,CountCommitMsg:2,CountDone:4,CountFailed:3,SectorCounts:c}];
}

const browser = await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_EXECUTABLE});
try {
    const page = await browser.newPage({viewport:{width:1540,height:1250}});
    const errors=[], summaryCalls=[], calls=[], held=[];
    page.on('pageerror', e=>errors.push(e.message));
    await page.clock.install({time:new Date('2026-09-13T01:00:01Z')});
    let mode = 'hold';
    function rpc(req, finish) {
        const method=req.method.replace('CurioWeb.',''); calls.push(method);
        if (method==='PorepPipelineSummary') {
            summaryCalls.push(method);
            if(mode==='hold') { held.push(finish); return; }
            return finish(fixture());
        }
        const stubs={Version:'offline fixture',ActorSummary:[],AlertHistoryListPaginated:{Alerts:[],Total:0},
            MessageQueueSummary:{filPendingCount:0,ethPendingCount:0},ChainStatus:{syncStatus:'ok',epoch:0},
            MarketBalance:[],DealsPending:[],ClusterMachines:[]};
        assert.ok(Object.hasOwn(stubs,method),`Unexpected RPC ${method}`);
        return finish(stubs[method]);
    }
    await page.route('**/*',async route=>{
        const u=new URL(route.request().url());
        if(u.href.includes('/lit/dist@3/')) return route.fulfill({contentType:'text/javascript',body:readFileSync(process.env.LIT3_FIXTURE,'utf8')});
        if(u.origin!=='http://summary.invalid') return route.abort();
        if(u.pathname==='/') return route.fulfill({contentType:'text/html',body:`<!doctype html><meta charset="utf-8"><body style="background:#171c24;color:#e6edf3;font:14px sans-serif"><h2>PoRep Overview — offline synthetic snapshot</h2><p>32,108 waiting + other states; no production connection.</p><porep-overview></porep-overview><script type="module">import '/pipeline-porep.mjs'; import '/porep-overview.mjs';</script></body>`});
        if(u.pathname==='/api/webrpc/v0') {
            const req=route.request().postDataJSON();
            return rpc(req,async(result,error)=>{
                try { await route.fulfill({contentType:'application/json',body:JSON.stringify({jsonrpc:'2.0',id:req.id,...(error?{error:{code:-1,message:error}}:{result})})}); }
                catch { /* An invalidated/hidden snapshot may have aborted its HTTP request. */ }
            });
        }
        try { return route.fulfill({contentType:u.pathname.endsWith('.css')?'text/css':'text/javascript',body:source(u.pathname)}); }
        catch { return route.abort(); }
    });
    await page.routeWebSocket('**/*',ws=>ws.onMessage(message=>{
        const req=JSON.parse(message);
        rpc(req,(result,error)=>ws.send(JSON.stringify({jsonrpc:'2.0',id:req.id,...(error?{error}:{result})})));
    }));
    await page.goto('http://summary.invalid');
    await page.waitForFunction(()=>document.querySelector('porep-overview')?.shadowRoot?.querySelector('pipeline-porep')?.shadowRoot?.querySelector('table'));
    await page.waitForFunction(()=>document.querySelector('porep-overview').actorsStatus==='ok');
    const pipe=page.locator('pipeline-porep');
    const pipeText=()=>pipe.evaluate(el=>el.shadowRoot.textContent);
    if(baseline) {
        await page.waitForTimeout(50);
        assert.equal(summaryCalls.length,2,'Old top + table perform independent summary calls');
        for(const finish of held.splice(0)) await finish(fixture());
        await page.waitForFunction(()=>document.querySelector('porep-overview').pipelineStatus==='ok');
        await page.locator('pipeline-porep td').first().waitFor();
        assert.match(await pipeText(),/32113/);
        await page.screenshot({path:process.env.POREP_SCREENSHOT,fullPage:true});
    } else {
        assert.equal(summaryCalls.length,1,'Only table owns the shared snapshot request');
        assert.match(await pipeText(),/Loading summary/);
        assert.equal(await pipe.locator('.sdr-running').count(),0);
        await held.shift()(fixture());
        await page.waitForFunction(()=>document.querySelector('porep-overview').pipelineStatus==='ok');
        assert.equal(await pipe.locator('.sdr-running').innerText(),'4 running');
        assert.match(await pipeText(),/2 preparing · 32102 waiting/);
        assert.match(await pipeText(),/32113 SDR incomplete/);
        const top=page.locator('porep-overview .data-strip');
        assert.match(await top.innerText(),/32123 remaining/);
        assert.match(await top.innerText(),/4 SDR running.*8 post-SDR incomplete/s);
        assert.doesNotMatch(await top.innerText(),/in flight/);
        await page.screenshot({path:process.env.POREP_SCREENSHOT,fullPage:true});
        // Display limits/coalescing on another surface cannot change this RPC.
        await page.evaluate(()=>{window.clusterTaskDisplayLimit=1;window.pendingPreview=0;window.coalescing=true;});
        const refresh=()=>page.evaluate(()=>document.querySelector('porep-overview').shadowRoot.querySelector('pipeline-porep').poller.refresh());
        await refresh();
        await page.waitForFunction(()=>document.querySelector('porep-overview').pipelineStatus==='loading');
        assert.match(await pipeText(),/Refreshing — previous/);
        await page.waitForTimeout(30);
        await held.shift()(null,'synthetic summary failure');
        await page.waitForFunction(()=>document.querySelector('porep-overview').pipelineStatus==='error');
        assert.equal(await pipe.locator('.sdr-running').innerText(),'4 running');
        assert.match(await pipeText(),/Stale — previous snapshot/);
        await refresh(); await page.waitForTimeout(30);
        const missing=fixture(); delete missing[0].SectorCounts;
        await held.shift()(missing);
        await page.waitForFunction(()=>document.querySelector('porep-overview').pipelineStatus==='error');
        assert.match(await pipeText(),/compatible backend required/);
        assert.equal(await pipe.locator('.sdr-running').innerText(),'4 running');
        await refresh(); await page.waitForTimeout(30);
        await held.shift()(fixture(2));
        await page.waitForFunction(()=>document.querySelector('porep-overview').pipelineStatus==='ok');
        assert.equal(await pipe.locator('.sdr-running').innerText(),'2 running');
        assert.match(await top.innerText(),/2 SDR running/);
        // Stop a pending response when hidden; do not accept its late contents.
        await refresh(); await page.waitForTimeout(30);
        const late=held.shift();
        await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:true});document.dispatchEvent(new Event('visibilitychange'));});
        await late(fixture(4));
        assert.match(await pipeText(),/Paused — previous snapshot/);
        assert.equal(await pipe.locator('.sdr-running').innerText(),'2 running');
        await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:false});document.dispatchEvent(new Event('visibilitychange'));});
        await page.waitForTimeout(30); await held.shift()([]);
        await page.waitForFunction(()=>document.querySelector('porep-overview').pipelineStatus==='ok');
        assert.match(await pipeText(),/No pipeline sectors/);
        assert.match(await top.innerText(),/0 remaining/);
        assert.ok(calls.every(m=>!['PipelinePorepSectors','PipelinePorepPage','GetTaskStatus'].includes(m)));
        // No leaked poll on removal; the established page-poller tests cover
        // deterministic timeout/invalidation/backoff interleavings as well.
        await page.evaluate(()=>document.querySelector('porep-overview').remove());
    }
    assert.deepEqual(errors,[]);
    console.log(JSON.stringify({result:'PASS',baseline:baseline||null,summaryCalls:summaryCalls.length,
        calls,screenshot:process.env.POREP_SCREENSHOT,scope:'real Chromium, local intercepted fixtures; SQL verified separately'},null,2));
} finally { await browser.close(); }
