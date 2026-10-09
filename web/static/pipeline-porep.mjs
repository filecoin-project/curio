import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import { RPCCallHTTP } from '/lib/jsonrpc.mjs';
import { PoRepPagePoller } from '/pages/pipeline_porep/page-poller.mjs';
import { summaryTotals, summaryStatus } from '/lib/porep-summary.mjs';
customElements.define('pipeline-porep',class PipelinePorep extends LitElement {
    static properties = {
        data: { type: Array },
        status: { type: String },
        error: { type: String },
    };

    constructor() {
        super();
        this.data = null;
        this.status = 'loading';
        this.error = '';
        this.poller = new PoRepPagePoller({
            request: signal => RPCCallHTTP('PorepPipelineSummary', [], {signal}),
            timeoutMs: 20000,
            onStart: () => this.publish('loading'),
            onSuccess: rows => {
                if (!summaryTotals(rows)) throw new Error('Sector counts missing or inconsistent; compatible backend required');
                this.data = rows;
                this.error = '';
                this.publish('ok');
            },
            onError: error => {
                this.error = error?.message || String(error);
                this.publish('error');
            },
        });
        this.visibilityChanged = () => this.updateActivity();
    }
    connectedCallback() {
        super.connectedCallback();
        document.addEventListener('visibilitychange', this.visibilityChanged);
        this.updateActivity();
    }
    disconnectedCallback() {
        this.poller.setActive(false);
        document.removeEventListener('visibilitychange', this.visibilityChanged);
        this.publish('paused');
        super.disconnectedCallback();
    }
    updateActivity() {
        const active = this.isConnected && !document.hidden;
        this.poller.setActive(active);
        if (!active) this.publish('paused');
    }
    publish(status) {
        this.status = status;
        // Overview and the miner table consume exactly the same response.
        this.dispatchEvent(new CustomEvent('porep-summary', {
            detail: {rows: this.data, status, error: this.error}, bubbles: true, composed: true,
        }));
    }
    renderSDR(c) {
        return html`
          <strong class="sdr-running ${c.SDRRunning ? 'success' : ''}" title="Current SDR Do-entry record and recent owner heartbeat; not proof of native progress">${c.SDRRunning} running</strong>
          <span class="sdr-detail">${c.SDRPreparing} preparing · ${c.SDRWaitingTask + c.SDRWaitingCreate} waiting</span>
          <span class="sdr-detail dim" title="Unowned SDR tasks include retry waits; no task means not yet created">${c.SDRWaitingTask} queued task · ${c.SDRWaitingCreate} no task yet</span>
          <span class="sdr-detail">${c.SDRTotal} SDR incomplete</span>
          ${c.SDRFailed ? html`<span class="sdr-detail warning">${c.SDRFailed} failed</span>` : ''}
          ${c.SDRMissingTask ? html`<span class="sdr-detail warning">${c.SDRMissingTask} missing task reference</span>` : ''}
          ${c.SDROtherTask ? html`<span class="sdr-detail warning" title="Linked task is not SDR, including SDRKeyRegen and SupraSeal Batch tasks">${c.SDROtherTask} other task type</span>` : ''}
          ${c.SDRUnknown ? html`<span class="sdr-detail warning" title="Stale owner, missing current attempt provenance, future/conflicting timestamps or inconsistent stage state">${c.SDRUnknown} unknown</span>` : ''}
        `;
    }
    render() {
        return html`
            <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <div class="row">
            <div class="col-md-auto" style="max-width: 1000px">
                <div class="info-block">
                    <h2>PoRep Pipeline</h2>
                    <p role="status" class="summary-status">${summaryStatus(this.status, this.data !== null, this.error)}${this.data?.[0] ? html` · observed ${new Date(this.data[0].SectorCounts.ObservedAt).toLocaleTimeString()}` : ''}</p>
                    <p class="dim">SDR counts are sectors. Running is recorded Do entry, not a native progress check. Later stage columns are legacy counts and may overlap or include failures; do not sum them.</p>
                    <style>
                      .sdr-running { display: block; font-size: 1.25rem; white-space: nowrap; }
                      .sdr-detail { display: block; font-size: .8rem; white-space: nowrap; }
                      .dim { opacity: .7; font-size: .8rem; }
                      .warning { color: #ffd28a; }
                      .summary-status { font-size: .85rem; }
                      td, th { vertical-align: top; }
                    </style>
                    <table class="table table-dark">
                        <thead>
                        <tr>
                            <th>Address</th>
                            <th>SDR</th>
                            <th>Trees</th>
                            <th>Precommit Msg</th>
                            <th>Wait Seed</th>
                            <th>PoRep</th>
                            <th>Commit Msg</th>
                            <th title="Commit confirmed; Finalize/MoveStorage can still be pending">Commit confirmed</th>
                            <th>Failed</th>
                        </tr>
                        </thead>
                        <tbody>
                        ${(this.data ?? []).map(
                            item => html`
                            
                                <tr>
                                    <td><b>${item.Actor}</b></td>
                                    <td class="sdr-cell">${this.renderSDR(item.SectorCounts)}</td>
                                    <td class=${item.CountTrees !== 0 ? 'success' : ''}>${item.CountTrees}</td>
                                    <td class=${item.CountPrecommitMsg !== 0 ? 'success' : ''}>${item.CountPrecommitMsg}</td>
                                    <td class=${item.CountWaitSeed !== 0 ? 'success' : ''}>${item.CountWaitSeed}</td>
                                    <td class=${item.CountPoRep !== 0 ? 'success' : ''}>${item.CountPoRep}</td>
                                    <td class=${item.CountCommitMsg !== 0 ? 'success' : ''}>${item.CountCommitMsg}</td>
                                    <td>${item.CountDone}</td>
                                    <td>${item.CountFailed}</td>
                                </tr>
                            `
                        )}
                        ${this.data?.length === 0 ? html`<tr><td colspan="9">No pipeline sectors in this snapshot.</td></tr>` : ''}
                        </tbody>
                    </table>
                </div>
            </div>
            <div class="col-md-auto">
                <div class="info-block">
                    <cc-scheduler></cc-scheduler>
                </div>
            </div>
        </div>
        `;
    }
} );
