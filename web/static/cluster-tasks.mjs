import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class ClusterTasks extends LitElement {
    static properties = {
        data: { type: Array }
    };

    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('ClusterTaskSummary');
        setTimeout(() => this.loadData(), 1000);
        super.requestUpdate();
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Miner ID</th>
                    <th style="min-width: 128px">Task</th>
                    <th>ID</th>
                    <th>Posted</th>
                    <th>Owner</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>${entry.MinerID? entry.MinerID: ''}</td>
                        <td>${entry.Name}</td>
                        <td>${entry.ID}</td>
                        <td>${entry.SincePosted}</td>
                        <td>${entry.OwnerID ? html`<a href="/hapi/node/${entry.OwnerID}">${entry.Owner}</a>`:''}</td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
}

customElements.define('cluster-tasks', ClusterTasks);
