import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs'
import { pollRPC } from '/lib/poll.mjs';
customElements.define('harmony-task-counts', class HarmonyTaskStatsTable extends LitElement {
    static properties = {
        data: { type: Array },
    };

    constructor() {
        super();
        this.data = [];
        pollRPC(async () => {
            this.data = await RPCCall('HarmonyTaskStats');
            this.calculatePercentages();
        }, 10000);
    }

    calculatePercentages() {
        this.data = this.data.map(task => ({
            ...task,
            FailedPercentage: task.failure > 0 ? `${((task.failure / task.total) * 100).toFixed(2)}%` : '0%'
        }));
    }

    static get styles() {
        return [css`
        .row-error > td {
            color: var(--color-danger-fg, #f85149);
        }
    `];
    }

    render() {
        return html`
            <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                    <tr>
                        <th>Task</th>
                        <th>Successful</th>
                        <th>Failed</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.data.map(task => html`
                    <tr class="${task.failure > 0 && task.success === 0 ? 'row-error' : ''}">
                        <td><a href="/pages/task/?name=${task.name}">${task.name}</a></td>
                        <td>${task.success}</td>
                        <td>${task.failure} (${task.FailedPercentage})</td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
});
