import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('cluster-machines', class ClusterMachines extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('ClusterMachines');
        setTimeout(() => this.loadData(), 5000);
        this.requestUpdate();
    }
    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <div class="row">
                <div class="col-md-auto" style="max-width: 1000px">
                    <div class="info-block">
                        <h2>Cluster Machines</h2>
                        <table class="table table-dark">
                            <thead>
                            <tr>
                                <th>Name</th>
                                <th>Host</th>
                                <th>ID</th>
                                <th>CPUs</th>
                                <th>RAM</th>
                                <th>GPUs</th>
                                <th>Last Contact</th>
                                <th>Tasks Supported</th>
                                <th>Layers Enabled</th>
                            </tr>
                            </thead>
                            <tbody>
                            ${this.data.map(item => html`
                                <tr>
                                    <td><a href="/pages/node_info/?id=${item.ID}">${item.Name}</a></td>
                                    <td><a href="/pages/node_info/?id=${item.ID}">${item.Address}</a></td>
                                    <td>${item.ID}</td>
                                    <td>${item.Cpu}</td>
                                    <td>${item.RamHumanized}</td>
                                    <td>${item.Gpu}</td>
                                    <td>${item.SinceContact}</td>
                                    <td>${item.Tasks.split(',').join(' ')}</td>
                                    <td>${item.Layers.split(',').join(' ')}</td>
                                </tr>
                            `)}
                            </tbody>
                        </table>
                    </div>
                </div>
                <div class="col-md-auto">
                </div>
            </div>
        `;
    }
});
