import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class PendingDeals extends LitElement {
    static properties = {
        data: { type: Array }
    };

    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('DealsPending');
        super.requestUpdate();
    }

    async sealNow(entry) {
        await RPCCall('DealsSealNow', [entry.Actor, entry.SectorNumber]);
        this.loadData();
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Address</th>
                    <th>Sector Number</th>
                    <th>Piece CID</th>
                    <th>Piece Size</th>
                    <th>Created At</th>
                    <th>SnapDeals</th>
                    <th>Control</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>f0${entry.Actor}</td>
                        <td>${entry.SectorNumber}</td>
                        <td>${entry.PieceCID}</td>
                        <td>${entry.PieceSizeStr}</td>
                        <td>${entry.CreatedAtStr}</td>
                        <td>
                            ${entry.SnapDeals ? "Yes" : "No"}
                        </td>
                        <td><button @click="${() => this.sealNow(entry)}" class="btn btn-primary btn-sm">Seal Now</button></td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
}
customElements.define('pending-deals', PendingDeals);
