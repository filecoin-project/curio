import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

window.customElements.define('network-summary', class NetworkSummary extends LitElement {
    constructor() {
        super();
        this.summary = null;
        this.pollHandle = setInterval(() => this.loadData(), 5000);
        this.loadData();
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this.pollHandle) clearInterval(this.pollHandle);
    }

    async loadData() {
        try {
            this.summary = await RPCCall('NetSummary');
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

    statusClass(status) {
        const s = String(status || 'unknown').toLowerCase();
        if (s.includes('public')) return 'reach-public';
        if (s.includes('private')) return 'reach-private';
        return 'reach-unknown';
    }

    statusLabel(status) {
        const s = String(status || 'unknown').toLowerCase();
        if (s.includes('public')) return 'Public';
        if (s.includes('private')) return 'Private';
        return 'Unknown';
    }

    rows() {
        const nodes = this.summary?.nodes;
        if (Array.isArray(nodes) && nodes.length > 0) return nodes;
        if (!this.summary) return [];

        return [{
            node: 'node-1',
            epoch: this.summary.epoch,
            peerCount: this.summary.peerCount,
            bandwidth: this.summary.bandwidth,
            reachability: this.summary.reachability,
        }];
    }

    renderFailoverNote(rows) {
        const nodeCount = Number(this.summary?.nodeCount || rows.length || 0);
        if (nodeCount > 1) return 'Multi-node failover enabled';
        if (nodeCount === 1) return 'Single node configured';
        return 'Node configuration unknown';
    }

    static get styles() {
        return [css`
        :host {
            display: block;
            box-sizing: border-box;
            width: 100%;
            max-width: 100%;
        }
        .table-wrap {
            width: 100%;
            overflow-x: auto;
            padding-bottom: 2px;
        }
        table {
            border-collapse: collapse;
            table-layout: fixed;
            width: 100%;
            min-width: 900px;
            max-width: 100%;
        }
        th, td {
            padding: 8px 10px;
            border: 1px solid rgba(255,255,255,0.08);
            background: rgba(255,255,255,0.03);
            font-size: 13px;
            vertical-align: middle;
            white-space: nowrap;
            font-variant-numeric: tabular-nums;
        }
        th {
            font-size: 12px;
            opacity: 0.8;
            font-weight: 600;
            text-align: left;
        }
        .node {
            font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
            font-size: 12px;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        th:nth-child(1), td:nth-child(1) { width: 30%; }
        th:nth-child(2), td:nth-child(2) { width: 12%; text-align: right; }
        th:nth-child(3), td:nth-child(3) { width: 10%; text-align: right; }
        th:nth-child(4), td:nth-child(4) { width: 16%; text-align: right; }
        th:nth-child(5), td:nth-child(5) { width: 16%; text-align: right; }
        th:nth-child(6), td:nth-child(6) { width: 16%; }
        .note {
            margin-top: 8px;
            margin-bottom: 10px;
            font-size: 12px;
            opacity: 0.8;
            padding-left: 2px;
        }
        .reach {
            font-weight: 600;
        }
        .reach-public { color: #2ecc71; }
        .reach-private { color: #f39c12; }
        .reach-unknown { color: #e74c3c; }
        .empty {
            opacity: 0.8;
            font-size: 13px;
            padding: 8px 0;
        }
        @media (max-width: 1100px) {
            table {
                min-width: 900px;
            }
            .node {
                max-width: 240px;
            }
        }
    `];
    }

    render() {
        const rows = this.rows();

        return html`
  <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
  <link rel="stylesheet" href="/ux/main.css">
  <link href="https://fonts.cdnfonts.com/css/metropolis-2" rel="stylesheet" crossorigin="anonymous">

  ${rows.length === 0 ? html`<div class="empty">No network data available yet.</div>` : html`
  <div class="table-wrap">
    <table>
        <thead>
        <tr>
            <th>Node</th>
            <th>Epoch</th>
            <th>Peers</th>
            <th>In rate</th>
            <th>Out rate</th>
            <th>Reachability</th>
        </tr>
        </thead>
        <tbody>
        ${rows.map((n) => html`
        <tr>
            <td class="node" title=${n.node || ''}>${n.node || '—'}</td>
            <td>${n.epoch ?? '—'}</td>
            <td>${n.peerCount ?? '—'}</td>
            <td>${this.formatRate(n?.bandwidth?.rateIn)}</td>
            <td>${this.formatRate(n?.bandwidth?.rateOut)}</td>
            <td class="reach ${this.statusClass(n?.reachability?.status)}">${this.statusLabel(n?.reachability?.status)}</td>
        </tr>`)}
        </tbody>
    </table>
  </div>
  <div class="note">${this.renderFailoverNote(rows)}</div>`}`;
    }
});
