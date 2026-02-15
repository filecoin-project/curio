import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class PipelineFailedSectors extends LitElement {
    static properties = {
        data: { type: Array },
        loading: { type: Boolean },
    };

    constructor() {
        super();
        this.data = [];
        this.loading = true;
        this.loadData();
    }

    async loadData() {
        try {
            const result = await RPCCall('PipelineFailedSectors');
            this.data = result || [];
        } catch (e) {
            console.error('Failed to load failed sectors:', e);
            this.data = [];
        }
        this.loading = false;
        this.requestUpdate();
        setTimeout(() => this.loadData(), 10000);
    }

    static styles = css`
        :host {
            color: #d0d0d0;
        }
        .count-header {
            font-size: 1.2em;
            margin-bottom: 1em;
        }
        .count-header .count {
            font-weight: bold;
            color: #ff6b6b;
        }
        .success-message {
            background: rgba(75, 181, 67, 0.15);
            border: 1px solid rgba(75, 181, 67, 0.4);
            border-radius: 8px;
            padding: 2em;
            text-align: center;
            font-size: 1.3em;
            color: #4BB543;
        }
        .row-recent {
            background-color: rgba(180, 40, 40, 0.3) !important;
        }
        .row-day {
            background-color: rgba(180, 120, 40, 0.2) !important;
        }
        .stage-badge {
            display: inline-block;
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 0.85em;
            font-weight: 600;
            background: #444;
            color: #eee;
        }
        .details-cell {
            max-width: 200px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            cursor: help;
        }
        .reason-cell {
            font-family: monospace;
            font-size: 0.9em;
        }
    `;

    computeStage(sector) {
        // Return the LAST completed stage
        if (sector.AfterCommitMsgSuccess) return 'CommitMsgSuccess';
        if (sector.AfterCommitMsg) return 'CommitMsg';
        if (sector.AfterMoveStorage) return 'MoveStorage';
        if (sector.AfterFinalize) return 'Finalize';
        if (sector.AfterPorep) return 'PoRep';
        if (sector.AfterPrecommitMsgSuccess) return 'PrecommitMsgSuccess';
        if (sector.AfterPrecommitMsg) return 'PrecommitMsg';
        if (sector.AfterTreeR) return 'TreeR';
        if (sector.AfterTreeC) return 'TreeC';
        if (sector.AfterTreeD) return 'TreeD';
        if (sector.AfterSDR) return 'SDR';
        return 'New';
    }

    formatAge(dateStr) {
        if (!dateStr) return '--';
        const date = new Date(dateStr);
        if (isNaN(date.getTime()) || date.getFullYear() < 2000) return '--';
        const now = Date.now();
        const diff = now - date.getTime();
        if (diff < 0) return 'just now';

        const minutes = Math.floor(diff / 60000);
        const hours = Math.floor(diff / 3600000);
        const days = Math.floor(diff / 86400000);

        if (minutes < 1) return 'just now';
        if (minutes < 60) return `${minutes}m ago`;
        if (hours < 24) return `${hours}h ago`;
        if (days < 30) return `${days}d ago`;
        return `${Math.floor(days / 30)}mo ago`;
    }

    formatTimestamp(dateStr) {
        if (!dateStr) return '--';
        const date = new Date(dateStr);
        if (isNaN(date.getTime()) || date.getFullYear() < 2000) return '--';
        return date.toLocaleString();
    }

    getRowClass(sector) {
        const failedAt = new Date(sector.FailedAt);
        if (isNaN(failedAt.getTime()) || failedAt.getFullYear() < 2000) return '';
        const diff = Date.now() - failedAt.getTime();
        if (diff < 3600000) return 'row-recent';     // <1h
        if (diff < 86400000) return 'row-day';       // <24h
        return '';
    }

    truncate(str, len) {
        if (!str) return '';
        if (str.length <= len) return str;
        return str.substring(0, len) + '…';
    }

    render() {
        if (this.loading) {
            return html`<div>Loading...</div>`;
        }

        if (!this.data || this.data.length === 0) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
                <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
                <div class="success-message">No failed sectors 🎉</div>
            `;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <div class="count-header">
                <span class="count">${this.data.length}</span> Failed Sector${this.data.length !== 1 ? 's' : ''}
            </div>

            <table class="table table-dark table-hover">
                <thead>
                    <tr>
                        <th>Miner</th>
                        <th>Sector #</th>
                        <th>Failed At</th>
                        <th>Stage</th>
                        <th>Reason</th>
                        <th>Details</th>
                        <th>Age</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.data.map(sector => this.renderRow(sector))}
                </tbody>
            </table>
        `;
    }

    renderRow(sector) {
        return html`
            <tr class="${this.getRowClass(sector)}">
                <td>f0${sector.SpID}</td>
                <td>${sector.SectorNumber}</td>
                <td style="white-space: nowrap">${this.formatTimestamp(sector.FailedAt)}</td>
                <td><span class="stage-badge">${this.computeStage(sector)}</span></td>
                <td class="reason-cell">${sector.FailedReason || '--'}</td>
                <td class="details-cell" title="${sector.FailedReasonMsg || ''}">${this.truncate(sector.FailedReasonMsg, 100) || '--'}</td>
                <td style="white-space: nowrap">${this.formatAge(sector.CreateTime)}</td>
            </tr>
        `;
    }
}

customElements.define('pipeline-failed-sectors', PipelineFailedSectors);
