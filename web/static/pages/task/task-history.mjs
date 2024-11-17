import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class HarmonyTaskHistoryTable extends LitElement {
    constructor() {
        super();
        this.history = [];
        this.taskName = new URLSearchParams(window.location.search).get('name');
        this.loadHistory();
    }

    async loadHistory() {
        try {
            this.history = await RPCCall('HarmonyTaskHistory', [this.taskName, false]);
            this.requestUpdate();
        } catch (error) {
            console.error('Error fetching task history data:', error);
        }
    }

    static get styles() {
        return css`
        .error {
            color: red;
        }
        `;
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                    <tr>
                        <th>Task ID</th>
                        <th>Name</th>
                        <th>Work Start</th>
                        <th>Work End</th>
                        <th>Posted</th>
                        <th>Completed By</th>
                        <th>Result</th>
                        <th>Error</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.history.map(task => html`
                        <tr>
                            <td>${task.TaskID}</td>
                            <td>${task.Name}</td>
                            <td>${new Date(task.WorkStart).toLocaleString()}</td>
                            <td>${new Date(task.WorkEnd).toLocaleString()}</td>
                            <td>${new Date(task.Posted).toLocaleString()}</td>
                            <td>${task.CompletedById ? html`<a href="/pages/node_info/?id=${task.CompletedById}">${task.CompletedByName} (${task.CompletedBy})</a>` : task.CompletedBy}</td>
                            <td class="${task.Result ? '' : 'error'}">${task.Result ? 'Success' : 'Failed'}</td>
                            <td>${task.Err}</td>
                        </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
}

customElements.define('harmony-task-history', HarmonyTaskHistoryTable);

class HarmonyTaskFailsTable extends LitElement {
    constructor() {
        super();
        this.history = [];
        this.taskName = new URLSearchParams(window.location.search).get('name');
        this.loadHistory();
    }

    async loadHistory() {
        try {
            this.history = await RPCCall('HarmonyTaskHistory', [this.taskName, true]);
            this.requestUpdate();
        } catch (error) {
            console.error('Error fetching task history data:', error);
        }
    }

    static get styles() {
        return css`
        .error {
            color: red;
        }
        `;
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                    <tr>
                        <th>Task ID</th>
                        <th>Name</th>
                        <th>Work Start</th>
                        <th>Work End</th>
                        <th>Posted</th>
                        <th>Completed By</th>
                        <th>Result</th>
                        <th>Error</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.history.map(task => html`
                        <tr>
                            <td>${task.TaskID}</td>
                            <td>${task.Name}</td>
                            <td>${new Date(task.WorkStart).toLocaleString()}</td>
                            <td>${new Date(task.WorkEnd).toLocaleString()}</td>
                            <td>${new Date(task.Posted).toLocaleString()}</td>
                            <td>${task.CompletedById ? html`<a href="/pages/node_info/?id=${task.CompletedById}">${task.CompletedByName} (${task.CompletedBy})</a>` : task.CompletedBy}</td>
                            <td class="${task.Result ? '' : 'error'}">${task.Result ? 'Success' : 'Failed'}</td>
                            <td>${task.Err}</td>
                        </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
}

customElements.define('harmony-task-fails', HarmonyTaskFailsTable);

