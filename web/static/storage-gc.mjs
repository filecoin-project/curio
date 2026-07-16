import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { pollRPC } from '/lib/poll.mjs';
customElements.define('storage-gc-stats',class StorageGCStats extends LitElement {
    static properties = {
        data: { type: Array },
    };

    constructor() {
        super();
        this.data = [];
        pollRPC(async () => {
            this.data = await RPCCall('StorageGCStats') || [];
        }, 10000);
    }

    render() {
        return html`
            <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Address</th>
                    <th>Marked For GC</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>${entry.Miner}</td>
                        <td>${entry.Count} files</td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
} );
