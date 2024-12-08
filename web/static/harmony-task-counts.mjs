import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs'
customElements.define('harmony-task-counts', class HarmonyTaskStatsTable extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('HarmonyTaskStats');
        this.calculatePercentages();
        this.requestUpdate();
    }

    calculatePercentages() {
        this.data = this.data.map(task => ({
            ...task,
            FailedPercentage: task.FalseCount > 0 ? `${((task.FalseCount / task.TotalCount) * 100).toFixed(2)}%` : '0%'
        }));
    }

    static get styles() {
        return [css`
        .row-error > td {
            color: red;
        }
    `];
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
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
                    <tr class="${task.FalseCount > task.TrueCount && task.TrueCount === 0 ? 'row-error' : ''}">
                        <td><a href="/pages/task/?name=${task.Name}">${task.Name}</a></td>
                        <td>${task.TrueCount}</td>
                        <td>${task.FalseCount} (${task.FailedPercentage})</td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
});
