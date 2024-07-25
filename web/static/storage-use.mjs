import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
customElements.define('storage-use',class StorageUse extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('StorageUseStats');
        this.requestUpdate();
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Type</th>
                    <th>Storage</th>
                    <th></th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>${entry.Type}</td>
                        <td>${entry.UseStr} / ${entry.CapStr} (${(100-100*entry.Available/entry.Capacity).toFixed(2)}%)</td>
                        <td>
                            <div style="display: inline-block; width: 250px; height: 16px; border: #3f3f3f 3px solid;">
                                <div style="width: ${(100-100*entry.Available/entry.Capacity).toFixed(2)}%; height: 10px; background-color: green"></div>
                            </div>
                        </td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
} );
