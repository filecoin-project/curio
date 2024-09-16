import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class StorageGCStats extends LitElement {
    static properties = {
        data: { type: Array }
    };

    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('StorageGCMarks');
        this.requestUpdate();
    }

    async approveEntry(entry) {
        await RPCCall('StorageGCApprove', [entry.Actor, entry.SectorNum, entry.FileType, entry.StorageID]);
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
                    <th>Storage Path</th>
                    <th>Storage Type</th>
                    <th>File Type</th>
                    <th>Marked At</th>
                    <th>Approved</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>f0${entry.Actor}</td>
                        <td>${entry.SectorNum}</td>
                        <td>
                            <div>
                                ${entry.StorageID}
                            </div>
                            <div>
                                ${entry.Urls}
                            </div>
                        </td>
                        <td>${entry.PathType}</td>
                        <td>${entry.TypeName}</td>
                        <td>${entry.CreatedAt}</td>
                        <td>
                            ${entry.Approved ?
            "Yes " + entry.ApprovedAt :
            html`No <button @click="${() => this.approveEntry(entry)}" class="btn btn-primary btn-sm">Approve</button>`
        }
                        </td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
}
customElements.define('gc-marks', StorageGCStats);

class ApproveAllButton extends LitElement {
    static properties = {
        unapprove: { type: Boolean }
    };

    constructor() {
        super();
        this.unapprove = false; // default is false, meaning "Approve All"
    }

    async handleClick() {
        if (this.unapprove) {
            await RPCCall('StorageGCUnapproveAll'); // Call the UnapproveAll RPC method
        } else {
            await RPCCall('StorageGCApproveAll');
        }
        window.location.reload();
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <button @click="${this.handleClick}" class="btn ${this.unapprove ? 'btn-warning' : 'btn-danger'}">
                ${this.unapprove ? 'Unapprove All' : 'Approve All'}
            </button>
        `;
    }
}
customElements.define('approve-all-button', ApproveAllButton);