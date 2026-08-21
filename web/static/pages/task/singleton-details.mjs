import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class SingletonTaskDetails extends LitElement {
    constructor() {
        super();
        this.info = null;
        this.loading = true;
        this.taskName = new URLSearchParams(window.location.search).get('name');
        this.loadInfo();
        this._interval = setInterval(() => this.loadInfo(), 5000);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this._interval) {
            clearInterval(this._interval);
        }
    }

    async loadInfo() {
        try {
            this.info = await RPCCall('SingletonTaskInfo', [this.taskName]);
            this.loading = false;
            this.requestUpdate();
        } catch (error) {
            console.error('Error fetching singleton info:', error);
            this.loading = false;
            this.requestUpdate();
        }
    }

    async runNow() {
        try {
            await RPCCall('SingletonRunNow', [this.taskName]);
            await this.loadInfo();
        } catch (error) {
            console.error('Error requesting run now:', error);
        }
    }

    async disable() {
        const reason = window.prompt(`Disable scheduling for "${this.taskName}"? This stops it fleet-wide until re-enabled.\n\nOptional reason:`, '');
        if (reason === null) {
            return;
        }
        try {
            await RPCCall('SingletonTaskDisable', [this.taskName, reason]);
            await this.loadInfo();
        } catch (error) {
            console.error('Error disabling singleton task:', error);
        }
    }

    async enable() {
        if (!confirm(`Re-enable scheduling for "${this.taskName}"?`)) {
            return;
        }
        try {
            await RPCCall('SingletonTaskEnable', [this.taskName]);
            await this.loadInfo();
        } catch (error) {
            console.error('Error enabling singleton task:', error);
        }
    }

    static get styles() {
        return css`
            .singleton-card {
                border: 1px solid #444;
                border-radius: 8px;
                padding: 16px;
                margin-bottom: 16px;
                background: #1a1d21;
            }
            .status-row {
                display: flex;
                align-items: center;
                gap: 16px;
                margin-bottom: 8px;
            }
            .status-badge {
                display: inline-block;
                padding: 4px 10px;
                border-radius: 4px;
                font-size: 0.85em;
                font-weight: bold;
            }
            .status-running {
                background: #0d6efd;
                color: white;
            }
            .status-idle {
                background: #495057;
                color: white;
            }
            .status-pending {
                background: #ffc107;
                color: black;
            }
            .status-disabled {
                background: #6c757d;
                color: white;
            }
            .detail {
                color: #adb5bd;
                margin: 4px 0;
            }
            .detail strong {
                color: #dee2e6;
            }
            .run-btn {
                margin-top: 8px;
                margin-right: 8px;
            }
        `;
    }

    render() {
        if (this.loading) {
            return html``;
        }
        if (!this.info) {
            return html``;
        }

        const isRunning = this.info.TaskID && this.info.TaskID !== 0;
        const isDisabled = this.info.Disabled;
        const statusText = isDisabled ? 'Disabled' : (isRunning ? 'Running' : 'Idle');
        const statusClass = isDisabled ? 'status-disabled' : (isRunning ? 'status-running' : 'status-idle');

        const lastRun = this.info.LastRunTime
            ? new Date(this.info.LastRunTime).toLocaleString()
            : 'Never';

        const runNowPending = this.info.RunNowRequest;
        const runBtnDisabled = runNowPending || isRunning || isDisabled;

        return html`
            <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <div class="singleton-card">
                <div class="status-row">
                    <span class="status-badge ${statusClass}">${statusText}</span>
                    ${isRunning ? html`<span class="detail">Task ID: <a href="/pages/task/id/?id=${this.info.TaskID}">${this.info.TaskID}</a></span>` : ''}
                    ${runNowPending ? html`<span class="status-badge status-pending">Run Requested</span>` : ''}
                </div>
                ${isDisabled && this.info.DisabledReason ? html`<div class="detail"><strong>Disabled reason:</strong> ${this.info.DisabledReason}</div>` : ''}
                <div class="detail"><strong>Last Run:</strong> ${lastRun}</div>
                <button class="btn btn-primary btn-sm run-btn"
                    ?disabled=${runBtnDisabled}
                    @click=${() => this.runNow()}>
                    ${runNowPending ? 'Run Requested...' : 'Run Now'}
                </button>
                ${isDisabled
                    ? html`<button class="btn btn-outline-success btn-sm run-btn" @click=${() => this.enable()}>Enable</button>`
                    : this.info.CanDisable
                        ? html`<button class="btn btn-outline-danger btn-sm run-btn" @click=${() => this.disable()}>Disable</button>`
                        : ''}
            </div>
        `;
    }
}

customElements.define('singleton-task-details', SingletonTaskDetails);
