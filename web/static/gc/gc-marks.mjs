import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class StorageGCStats extends LitElement {
    static properties = {
        data: { type: Array },
        pageSize: { type: Number }, // Make pageSize a reactive property
        currentPage: { type: Number },
        totalPages: { type: Number },
        totalCount: { type: Number },
    };

    constructor() {
        super();
        this.data = [];
        this.pageSize = 100; // Default value for page size
        this.currentPage = 1;
        this.totalPages = 0;
        this.totalCount = 0;
        this.loadData(); // Load the initial data for page 1
    }

    async loadData(page = 1) {
        const offset = (page - 1) * this.pageSize;

        // Fetch data from the backend with limit and offset
        const response = await RPCCall('StorageGCMarks', { limit: this.pageSize, offset });

        this.data = response.marks; // Data for the current page
        this.totalCount = response.total; // Total number of rows
        this.totalPages = Math.ceil(this.totalCount / this.pageSize); // Calculate total pages
        this.currentPage = page; // Update the current page
        this.requestUpdate();
    }


    async approveEntry(entry) {
        await RPCCall('StorageGCApprove', [entry.Actor, entry.SectorNum, entry.FileType, entry.StorageID]);
        this.loadData();
    }

    renderPagination() {
        return html`
            <nav>
                <ul class="pagination">
                    <li class="page-item ${this.currentPage === 1 ? 'disabled' : ''}">
                        <button class="page-link" @click="${() => this.loadData(this.currentPage - 1)}">Previous</button>
                    </li>
                    <li class="page-item disabled">
                        <span class="page-link">
                            Page ${this.currentPage} of ${this.totalPages}
                        </span>
                    </li>
                    <li class="page-item ${this.currentPage === this.totalPages ? 'disabled' : ''}">
                        <button class="page-link" @click="${() => this.loadData(this.currentPage + 1)}">Next</button>
                    </li>
                </ul>
            </nav>
        `;
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
                        <td>${entry.Miner}</td>
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
                            ${entry.Approved
                                    ? `Yes ${entry.ApprovedAt}`
                                    : html`No <button @click="${() => this.approveEntry(entry)}" class="btn btn-primary btn-sm">Approve</button>`}
                        </td>
                    </tr>
                `)}
                </tbody>
            </table>
            ${this.renderPagination()}
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