import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@2/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class TaskStatusElement extends LitElement {
    static properties= {
        taskId: { type: Number },
        status: { type: Object },
        loading: { type: Boolean }
    }

    constructor() {
        super();
        this.taskId = null;
        this.status = null;
        this.loading = true;
        this._refreshInterval = null;
    }

    connectedCallback() {
        super.connectedCallback();
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
            this.loading = false;
        } catch (err) {
            console.error('Failed to load task status:', err);
            this.status = null;
            this.loading = false;
        }
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
            return html`<div>Loading...</div>`;
        }

        if (!this.status) {
            return html`<div>Task not found</div>`;
        }

        let statusLabel = '';
        switch (this.status.status) {
            case 'pending':
                statusLabel = html`<span style="color: gray;">pending</span>`;
                break;
            case 'running':
                statusLabel = html`<span style="color: var(--color-info-main);">running</span>`;
                break;
            case 'done':
                statusLabel = html`<span style="color: var(--color-success-main);">done</span>`;
                break;
            case 'failed':
                statusLabel = html`<span style="color: var(--color-danger-main);">failed</span>`;
                break;
            default:
                statusLabel = this.status.status;
        }

        const canRestart = this.status.status === 'failed';

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            
      [<a href="/pages/task/id/?id=${this.taskId}">${this.taskId}</a> ${statusLabel}
      ${canRestart
            ? html`<button @click=${this.restartTask} title="Restart Task">⟳ RESTART</button>`
            : ''}]
    `;
    }

    static get styles() {
        return css`
      :host {
        display: inline-block;
      }
      button {
        background: #264000;
        border: none;
        cursor: pointer;
        font-size: 1em;
        margin-left: 5px;
      }
    `;
    }
}

customElements.define('task-status', TaskStatusElement);
