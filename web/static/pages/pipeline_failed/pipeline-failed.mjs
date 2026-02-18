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
        .count-header .count-stuck {
            font-weight: bold;
            color: #e0a040;
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
        .section-label {
            font-size: 1.1em;
            font-weight: 600;
            margin: 1.5em 0 0.5em 0;
            padding: 4px 0;
        }
        .section-label.terminal {
            color: #ff6b6b;
            border-bottom: 1px solid rgba(255, 107, 107, 0.3);
        }
        .section-label.stuck {
            color: #e0a040;
            border-bottom: 1px solid rgba(224, 160, 64, 0.3);
        }
        .row-terminal {
            background-color: rgba(180, 40, 40, 0.25) !important;
        }
        .row-stuck {
            background-color: rgba(180, 120, 40, 0.15) !important;
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
        .status-badge {
            display: inline-block;
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 0.85em;
            font-weight: 600;
        }
        .status-badge.terminal {
            background: rgba(255, 107, 107, 0.3);
            color: #ff6b6b;
        }
        .status-badge.stuck {
            background: rgba(224, 160, 64, 0.3);
            color: #e0a040;
        }
        .details-cell {
            max-width: 250px;
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

    stageNames() {
        return [
            ['AfterSDR', 'SDR'],
            ['AfterTreeD', 'TreeD'],
            ['AfterTreeC', 'TreeC'],
            ['AfterTreeR', 'TreeR'],
            ['AfterSynth', 'Synth'],
            ['AfterPrecommitMsg', 'PrecommitMsg'],
            ['AfterPrecommitMsgSuccess', 'PrecommitMsgSuccess'],
            ['AfterPorep', 'PoRep'],
            ['AfterFinalize', 'Finalize'],
            ['AfterMoveStorage', 'MoveStorage'],
            ['AfterCommitMsg', 'CommitMsg'],
            ['AfterCommitMsgSuccess', 'CommitMsgSuccess'],
        ];
    }

    computeStage(sector) {
        const stages = this.stageNames();
        for (let i = stages.length - 1; i >= 0; i--) {
            if (sector[stages[i][0]]) return stages[i][1];
        }
        return 'New';
    }

    isStuck(sector) {
        return !sector.Failed && sector.MissingTasks && sector.MissingTasks.length > 0;
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

        const terminal = this.data.filter(s => s.Failed);
        const stuck = this.data.filter(s => this.isStuck(s));
        const totalCount = this.data.length;

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <div class="count-header">
                ${terminal.length > 0 ? html`<span class="count">${terminal.length}</span> failed` : ''}
                ${terminal.length > 0 && stuck.length > 0 ? html` · ` : ''}
                ${stuck.length > 0 ? html`<span class="count-stuck">${stuck.length}</span> stuck (missing tasks)` : ''}
                <span style="color: #888; font-size: 0.85em; margin-left: 0.5em">${totalCount} total</span>
            </div>

            ${terminal.length > 0 ? html`
                <div class="section-label terminal">⛔ Terminally Failed</div>
                ${this.renderTable(terminal, true)}
            ` : ''}

            ${stuck.length > 0 ? html`
                <div class="section-label stuck">⚠️ Stuck — Missing Tasks</div>
                ${this.renderTable(stuck, false)}
            ` : ''}
        `;
    }

    renderTable(sectors, isTerminal) {
        return html`
            <table class="table table-dark table-hover">
                <thead>
                    <tr>
                        <th>Miner</th>
                        <th>Sector #</th>
                        <th>Status</th>
                        <th>Last Stage</th>
                        ${isTerminal ? html`
                            <th>Failed At</th>
                            <th>Reason</th>
                            <th>Details</th>
                        ` : html`
                            <th>Details</th>
                        `}
                        <th>Created</th>
                    </tr>
                </thead>
                <tbody>
                    ${sectors.map(s => isTerminal ? this.renderTerminalRow(s) : this.renderStuckRow(s))}
                </tbody>
            </table>
        `;
    }

    renderTerminalRow(sector) {
        return html`
            <tr class="row-terminal">
                <td>f0${sector.SpID}</td>
                <td>${sector.SectorNumber}</td>
                <td><span class="status-badge terminal">FAILED</span></td>
                <td><span class="stage-badge">${this.computeStage(sector)}</span></td>
                <td style="white-space: nowrap">${this.formatTimestamp(sector.FailedAt)}</td>
                <td class="reason-cell">${sector.FailedReason || '--'}</td>
                <td class="details-cell" title="${sector.FailedReasonMsg || ''}">${this.truncate(sector.FailedReasonMsg, 100) || '--'}</td>
                <td style="white-space: nowrap">${this.formatAge(sector.CreateTime)}</td>
            </tr>
        `;
    }

    renderStuckRow(sector) {
        const missingCount = sector.MissingTasks ? sector.MissingTasks.length : 0;
        return html`
            <tr class="row-stuck">
                <td>f0${sector.SpID}</td>
                <td>${sector.SectorNumber}</td>
                <td><span class="status-badge stuck">STUCK</span></td>
                <td><span class="stage-badge">${this.computeStage(sector)}</span></td>
                <td class="details-cell" title="Task IDs: ${(sector.MissingTasks || []).join(', ')}">
                    ${missingCount} missing task${missingCount !== 1 ? 's' : ''} — sector needs restart
                </td>
                <td style="white-space: nowrap">${this.formatAge(sector.CreateTime)}</td>
            </tr>
        `;
    }
}

customElements.define('pipeline-failed-sectors', PipelineFailedSectors);
