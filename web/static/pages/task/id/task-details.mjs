import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class HarmonyTaskDetails extends LitElement {
    constructor() {
        super();
        this.task = null;
        this.taskId = new URLSearchParams(window.location.search).get('id');
        this.loadTaskDetails();
    }

    async loadTaskDetails() {
        try {
            this.task = await RPCCall('HarmonyTaskDetails', [parseInt(this.taskId)]);
            this.requestUpdate();
        } catch (error) {
            console.error('Error fetching task details:', error);
        }
    }

    render() {
        if (!this.task) {
            return html`<p>Not running</p>`;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <tbody>
                    <tr>
                        <th>Task ID</th>
                        <td>${this.task.ID}</td>
                    </tr>
                    <tr>
                        <th>Name</th>
                        <td><a href="/pages/task/?name=${this.task.Name}">${this.task.Name}</a></td>
                    </tr>
                    <tr>
                        <th>Update Time</th>
                        <td>${new Date(this.task.UpdateTime).toLocaleString()}</td>
                    </tr>
                    <tr>
                        <th>Posted Time</th>
                        <td>${new Date(this.task.PostedTime).toLocaleString()}</td>
                    </tr>
                    <tr>
                        <th>Owner</th>
                        <td>${this.task.OwnerID ? html`<a href="/pages/node_info/?id=${this.task.OwnerID}">${this.task.OwnerAddr} ${this.task.OwnerName}</a>` : 'N/A'}</td>
                    </tr>
                </tbody>
            </table>
        `;
    }
}

customElements.define('harmony-task-details', HarmonyTaskDetails);
