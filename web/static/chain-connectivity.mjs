import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

window.customElements.define('chain-connectivity', class MyElement extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.summary = null;
        this.loadData();
        this.pollHandle = setInterval(() => this.loadData(), 4000);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this.pollHandle) {
            clearInterval(this.pollHandle);
        }
    }

    async loadData() {
        try {
            await Promise.all([
                this.updateData(),
                this.updateSummary(),
            ]);
        } catch (err) {
            console.error('failed to refresh chain connectivity data', err);
        }
    };

    async updateData() {
        this.data = await RPCCall('SyncerState');
    }

    async updateSummary() {
        const resp = await fetch('/api/net/summary');
        if (!resp.ok) {
            throw new Error(`failed /api/net/summary (${resp.status})`);
        }
        this.summary = await resp.json();
        this.requestUpdate();
    }

    formatRate(v) {
        if (v === undefined || v === null) return '—';
        const units = ['B/s', 'KiB/s', 'MiB/s', 'GiB/s'];
        let val = Number(v);
        let i = 0;
        while (val >= 1024 && i < units.length - 1) {
            val /= 1024;
            i++;
        }
        return `${val.toFixed(val >= 100 ? 0 : 1)} ${units[i]}`;
    }

    renderReachability() {
        const status = this.summary?.reachability?.status;
        if (!status) return html`<span class="warning">unknown</span>`;

        const s = String(status).toLowerCase();
        if (s.includes('public')) return html`<span class="success">${status}</span>`;
        if (s.includes('private')) return html`<span class="warning">${status}</span>`;
        return html`<span class="warning">${status}</span>`;
    }

    static get styles() {
        return [css`
        :host {
            box-sizing: border-box;
        }
        .summary {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
            gap: 8px;
            margin-bottom: 12px;
        }
        .summary-item {
            background: rgba(255,255,255,0.03);
            border: 1px solid rgba(255,255,255,0.08);
            border-radius: 8px;
            padding: 8px 10px;
        }
        .summary-label {
            font-size: 12px;
            opacity: 0.8;
            margin-bottom: 2px;
        }
        .summary-value {
            font-size: 14px;
            font-weight: 600;
        }
    `];
    }
    render() {
        return html`
  <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
    <link rel="stylesheet" href="/ux/main.css">
  <link href="https://fonts.cdnfonts.com/css/metropolis-2" rel="stylesheet" crossorigin="anonymous">

  <div class="summary">
      <div class="summary-item">
          <div class="summary-label">Epoch</div>
          <div class="summary-value">${this.summary?.epoch ?? '—'}</div>
      </div>
      <div class="summary-item">
          <div class="summary-label">Peers</div>
          <div class="summary-value">${this.summary?.peerCount ?? '—'}</div>
      </div>
      <div class="summary-item">
          <div class="summary-label">In rate</div>
          <div class="summary-value">${this.formatRate(this.summary?.bandwidth?.rateIn)}</div>
      </div>
      <div class="summary-item">
          <div class="summary-label">Out rate</div>
          <div class="summary-value">${this.formatRate(this.summary?.bandwidth?.rateOut)}</div>
      </div>
      <div class="summary-item">
          <div class="summary-label">Reachability</div>
          <div class="summary-value">${this.renderReachability()}</div>
      </div>
  </div>

  <table class="table table-dark">
    <thead>
        <tr>
            <th>RPC Address</th>
            <th>Reachability</th>
            <th>Sync Status</th>
            <th>Version</th>
        </tr>
    </thead>
    <tbody>
        ${this.data.map(item => html`
        <tr>
            <td>${item.Address}</td>
            <td>${item.Reachable ? html`<span class="success">ok</span>` : html`<span class="error">FAIL</span>`}</td>
            <td>${item.SyncState === "ok" ? html`<span class="success">ok</span>` : html`<span class="warning">No${item.SyncState? ', '+item.SyncState:''}</span>`}</td>
            <td>${item.Version}</td>
        </tr>
        `)}
    </tbody>
  </table>`
    }
});
