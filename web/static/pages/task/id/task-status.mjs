import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class HarmonyTaskStatus extends LitElement {
    static properties = {
        taskId: { type: Number },
        status: { type: Object },
        loading: { type: Boolean }
    };

    constructor() {
        super();
        this.taskId = null;
        this.status = null;
        this.loading = true;
        this._refreshInterval = null;
    }

    connectedCallback() {
        super.connectedCallback();

        // read the ID from querystring
        this.taskId = parseInt(new URLSearchParams(window.location.search).get('id') || '0');
        this.loadStatus();
        this._refreshInterval = setInterval(() => this.loadStatus(), 2500);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this._refreshInterval) {
            clearInterval(this._refreshInterval);
            this._refreshInterval = null;
        }
    }

    async loadStatus() {
        try {
            const result = await RPCCall('GetTaskStatus', [this.taskId]);
            this.status = result;
        } catch (err) {
            console.error('Failed to load task status:', err);
            this.status = null;
        }
        this.loading = false;
        this.requestUpdate();
    }

    async restartTask() {
        try {
            await RPCCall('RestartFailedTask', [this.taskId]);
            await this.loadStatus();
        } catch (err) {
            console.error('Failed to restart task:', err);
            alert('Failed to restart task: ' + err.message);
        }
    }

    render() {
        if (this.loading) {
            return html`<div>Loading task status...</div>`;
        }

        if (!this.status) {
            return html`<div>Task not found or no status returned.</div>`;
        }

        let statusLabel;
        switch (this.status.status) {
            case 'pending':
                statusLabel = html`<span style="color: gray;">Pending</span>`;
                break;
            case 'running':
                statusLabel = html`<span style="color: var(--color-info-main);">Running</span>`;
                break;
            case 'done':
                statusLabel = html`<span style="color: var(--color-success-main);">Done</span>`;
                break;
            case 'failed':
                statusLabel = html`<span style="color: var(--color-danger-main);">Failed</span>`;
                break;
            default:
                statusLabel = html`<span>${this.status.status}</span>`;
        }

        const canRestart = this.status.status === 'failed';

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <div class="task-status">
                <h3>Task Status</h3>
                <p><strong>Task ID:</strong> ${this.taskId}</p>
                <p><strong>Status:</strong> ${statusLabel}</p>

                ${canRestart
            ? html`
                        <button
                            class="btn btn-warning"
                            @click=${this.restartTask}
                            title="Restart a failed task"
                        >
                            ⟳ Restart
                        </button>`
            : ''
        }
            </div>
        `;
    }

    static styles = css`
        :host {
            display: block;
            margin-bottom: 1em;
        }
        .task-status {
            padding: 1em;
            border: 1px solid var(--color-form-default);
            border-radius: 0.5rem;
            background-color: var(--color-fg);
        }
    `;
}

customElements.define('harmony-task-status', HarmonyTaskStatus);
