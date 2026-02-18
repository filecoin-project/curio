import { LitElement, html, css, nothing } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class PipelineFailedSectors extends LitElement {
    static properties = {
        groups: { type: Array },
        loading: { type: Boolean },
        expanded: { type: Object },   // { groupKey: { sectors: [], loading, offset, hasMore } }
        pageSize: { type: Number },
    };

    constructor() {
        super();
        this.groups = [];
        this.loading = true;
        this.expanded = {};
        this.pageSize = 100;
        this.loadGroups();
    }

    async loadGroups() {
        try {
            const result = await RPCCall('PipelineFailedSectors');
            this.groups = result || [];
        } catch (e) {
            console.error('Failed to load failed sectors:', e);
            this.groups = [];
        }
        this.loading = false;
        this.requestUpdate();
        setTimeout(() => this.loadGroups(), 30000);
    }

    groupKey(g) {
        return `${g.Status}|${g.LastStage}|${g.FailedReason}`;
    }

    async toggleGroup(group) {
        const key = this.groupKey(group);
        if (this.expanded[key]) {
            const next = { ...this.expanded };
            delete next[key];
            this.expanded = next;
            return;
        }
        // Load first page
        this.expanded = {
            ...this.expanded,
            [key]: { sectors: [], loading: true, offset: 0, hasMore: true },
        };
        await this.loadPage(group, 0);
    }

    async loadPage(group, offset) {
        const key = this.groupKey(group);
        const state = this.expanded[key];
        if (!state) return;

        state.loading = true;
        this.requestUpdate();

        try {
            const rows = await RPCCall('PipelineFailedSectorDetails', [
                group.Status, group.LastStage, group.FailedReason,
                offset, this.pageSize,
            ]);
            const sectors = offset === 0 ? (rows || []) : [...state.sectors, ...(rows || [])];
            this.expanded = {
                ...this.expanded,
                [key]: {
                    sectors,
                    loading: false,
                    offset: offset + this.pageSize,
                    hasMore: (rows || []).length === this.pageSize,
                },
            };
        } catch (e) {
            console.error('Failed to load sector details:', e);
            state.loading = false;
            this.requestUpdate();
        }
    }

    async loadMore(group) {
        const key = this.groupKey(group);
        const state = this.expanded[key];
        if (!state || state.loading) return;
        await this.loadPage(group, state.offset);
    }

    static styles = css`
        :host {
            color: #d0d0d0;
        }
        .summary-bar {
            display: flex;
            gap: 1.5em;
            flex-wrap: wrap;
            margin-bottom: 1.2em;
            font-size: 1.1em;
        }
        .summary-bar .terminal { color: #ff6b6b; font-weight: bold; }
        .summary-bar .stuck { color: #e0a040; font-weight: bold; }
        .summary-bar .total { color: #888; }
        .success-message {
            background: rgba(75, 181, 67, 0.15);
            border: 1px solid rgba(75, 181, 67, 0.4);
            border-radius: 8px;
            padding: 2em;
            text-align: center;
            font-size: 1.3em;
            color: #4BB543;
        }
        .group-row {
            cursor: pointer;
            user-select: none;
            transition: background 0.15s;
        }
        .group-row:hover {
            background: rgba(255, 255, 255, 0.05) !important;
        }
        .group-row.terminal {
            background: rgba(180, 40, 40, 0.2) !important;
        }
        .group-row.stuck {
            background: rgba(180, 120, 40, 0.12) !important;
        }
        .expand-icon {
            display: inline-block;
            width: 1.2em;
            text-align: center;
            transition: transform 0.2s;
        }
        .expand-icon.open {
            transform: rotate(90deg);
        }
        .status-badge {
            display: inline-block;
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 0.85em;
            font-weight: 600;
        }
        .status-badge.terminal { background: rgba(255, 107, 107, 0.3); color: #ff6b6b; }
        .status-badge.stuck { background: rgba(224, 160, 64, 0.3); color: #e0a040; }
        .stage-badge {
            display: inline-block;
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 0.85em;
            font-weight: 600;
            background: #444;
            color: #eee;
        }
        .count-badge {
            font-weight: bold;
            font-size: 1.05em;
        }
        .count-badge.terminal { color: #ff6b6b; }
        .count-badge.stuck { color: #e0a040; }
        .detail-container {
            background: rgba(0, 0, 0, 0.2);
            border-left: 3px solid #555;
            margin: 0;
        }
        .detail-row td {
            font-size: 0.9em;
            padding: 4px 8px !important;
            border-top: 1px solid rgba(255,255,255,0.04) !important;
        }
        .detail-row td:first-child {
            padding-left: 2.5em !important;
        }
        .load-more-row td {
            text-align: center;
            padding: 8px !important;
        }
        .load-more-btn {
            background: rgba(255,255,255,0.08);
            border: 1px solid rgba(255,255,255,0.15);
            color: #ccc;
            padding: 4px 16px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 0.9em;
        }
        .load-more-btn:hover { background: rgba(255,255,255,0.15); }
        .details-cell {
            max-width: 300px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            cursor: help;
        }
        .page-size-select {
            float: right;
            font-size: 0.85em;
            color: #888;
        }
        .page-size-select select {
            background: #333;
            color: #ccc;
            border: 1px solid #555;
            border-radius: 3px;
            padding: 2px 4px;
            margin-left: 4px;
        }
    `;

    formatAge(dateStr) {
        if (!dateStr) return '--';
        const date = new Date(dateStr);
        if (isNaN(date.getTime()) || date.getFullYear() < 2000) return '--';
        const diff = Date.now() - date.getTime();
        if (diff < 0) return 'just now';
        const m = Math.floor(diff / 60000);
        const h = Math.floor(diff / 3600000);
        const d = Math.floor(diff / 86400000);
        if (m < 1) return 'just now';
        if (m < 60) return `${m}m ago`;
        if (h < 24) return `${h}h ago`;
        if (d < 30) return `${d}d ago`;
        return `${Math.floor(d / 30)}mo ago`;
    }

    formatTimestamp(dateStr) {
        if (!dateStr) return '--';
        const date = new Date(dateStr);
        if (isNaN(date.getTime()) || date.getFullYear() < 2000) return '--';
        return date.toLocaleString();
    }

    truncate(str, len) {
        if (!str) return '';
        return str.length <= len ? str : str.substring(0, len) + '…';
    }

    render() {
        if (this.loading) return html`<div>Loading...</div>`;

        if (!this.groups || this.groups.length === 0) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
                <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
                <div class="success-message">No failed sectors 🎉</div>
            `;
        }

        const terminalCount = this.groups.filter(g => g.Status === 'terminal').reduce((s, g) => s + g.Count, 0);
        const stuckCount = this.groups.filter(g => g.Status === 'stuck').reduce((s, g) => s + g.Count, 0);
        const total = terminalCount + stuckCount;

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <div class="summary-bar">
                ${terminalCount > 0 ? html`<span>⛔ <span class="terminal">${terminalCount}</span> failed</span>` : ''}
                ${stuckCount > 0 ? html`<span>⚠️ <span class="stuck">${stuckCount}</span> stuck</span>` : ''}
                <span class="total">${total} total across ${this.groups.length} group${this.groups.length !== 1 ? 's' : ''}</span>
                <span class="page-size-select">
                    Page size:
                    <select @change=${e => { this.pageSize = parseInt(e.target.value); }}>
                        ${[100, 250, 500].map(n => html`<option value=${n} ?selected=${this.pageSize === n}>${n}</option>`)}
                    </select>
                </span>
            </div>

            <table class="table table-dark">
                <thead>
                    <tr>
                        <th style="width:2em"></th>
                        <th>Status</th>
                        <th>Stage</th>
                        <th>Reason</th>
                        <th style="text-align:right">Sectors</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.groups.map(g => this.renderGroup(g))}
                </tbody>
            </table>
        `;
    }

    renderGroup(group) {
        const key = this.groupKey(group);
        const state = this.expanded[key];
        const isOpen = !!state;
        const cls = group.Status === 'terminal' ? 'terminal' : 'stuck';

        return html`
            <tr class="group-row ${cls}" @click=${() => this.toggleGroup(group)}>
                <td><span class="expand-icon ${isOpen ? 'open' : ''}">▶</span></td>
                <td><span class="status-badge ${cls}">${group.Status === 'terminal' ? 'FAILED' : 'STUCK'}</span></td>
                <td><span class="stage-badge">${group.LastStage}</span></td>
                <td>${group.FailedReason || (group.Status === 'stuck' ? 'missing tasks' : '--')}</td>
                <td style="text-align:right"><span class="count-badge ${cls}">${group.Count}</span></td>
            </tr>
            ${isOpen ? this.renderDetails(group, state) : nothing}
        `;
    }

    renderDetails(group, state) {
        if (state.loading && state.sectors.length === 0) {
            return html`<tr class="detail-row"><td colspan="5" style="text-align:center;color:#888">Loading...</td></tr>`;
        }
        const isTerminal = group.Status === 'terminal';
        const rows = state.sectors.map(s => html`
            <tr class="detail-row">
                <td></td>
                <td>f0${s.SpID}</td>
                <td>${s.SectorNumber}</td>
                <td class="details-cell" title="${s.FailedReasonMsg || (s.MissingTasks || []).join(', ')}">
                    ${isTerminal
                        ? this.truncate(s.FailedReasonMsg, 120) || '--'
                        : `${(s.MissingTasks || []).length} missing task(s)`}
                </td>
                <td style="text-align:right;white-space:nowrap">${this.formatAge(s.CreateTime)}</td>
            </tr>
        `);

        const loadMoreRow = state.hasMore ? html`
            <tr class="load-more-row">
                <td colspan="5">
                    <button class="load-more-btn"
                        ?disabled=${state.loading}
                        @click=${(e) => { e.stopPropagation(); this.loadMore(group); }}>
                        ${state.loading ? 'Loading...' : `Load more (showing ${state.sectors.length} of ${group.Count})`}
                    </button>
                </td>
            </tr>
        ` : nothing;

        return html`${rows}${loadMoreRow}`;
    }
}

customElements.define('pipeline-failed-sectors', PipelineFailedSectors);
