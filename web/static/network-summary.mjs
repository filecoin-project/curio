import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

window.customElements.define('network-summary', class NetworkSummary extends LitElement {
    constructor() {
        super();
        this.summary = null;
        this.nodeCount = 0;
        this.loadData();
        this.pollHandle = setInterval(() => this.loadData(), 1000);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this.pollHandle) clearInterval(this.pollHandle);
    }

    async loadData() {
        try {
            const [resp, syncState] = await Promise.all([
                fetch('/api/net/summary'),
                RPCCall('SyncerState'),
            ]);
            if (!resp.ok) throw new Error(`failed /api/net/summary (${resp.status})`);
            this.summary = await resp.json();
            this.nodeCount = Array.isArray(syncState) ? syncState.length : 0;
            this.requestUpdate();
        } catch (err) {
            console.error('failed to refresh network summary', err);
        }
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

    renderFailoverNote() {
        if (this.nodeCount > 1) return 'Multi-node failover enabled';
        if (this.nodeCount === 1) return 'Single node configured';
        return 'Node configuration unknown';
    }

    static get styles() {
        return [css`
        :host {
            display: block;
            box-sizing: border-box;
        }
        .summary {
            display: flex;
            flex-wrap: wrap;
            gap: 8px;
        }
        .note {
            margin-top: 8px;
            font-size: 12px;
            opacity: 0.8;
        }
        .summary-item {
            flex: 0 0 170px;
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
  <div class="note">${this.renderFailoverNote()}</div>`;
    }
});
