import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class ContentPage extends LitElement {
    static properties = {
        searchCid: { type: String },
        results: { type: Array },
        loading: { type: Boolean },
        error: { type: String }
    };

    constructor() {
        super();
        this.searchCid = '';
        this.results = [];
        this.loading = false;
        this.error = '';
    }

    handleInput(e) {
        this.searchCid = e.target.value;
    }

    async handleFind() {
        if (!this.searchCid.trim()) {
            this.error = 'Please enter a CID';
            return;
        }

        this.loading = true;
        this.error = '';
        this.results = [];

        try {
            const results = await RPCCall('FindContentByCID', [this.searchCid.trim()]);
            this.results = results || [];
            if (this.results.length === 0) {
                this.error = 'No content found for this CID';
            }
        } catch (err) {
            console.error('Error finding content:', err);
            this.error = `Error: ${err.message || err}`;
        } finally {
            this.loading = false;
        }
    }

    render() {
        return html`
            <link
                href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                rel="stylesheet"
                integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
                crossorigin="anonymous"
            />
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <div class="container">
                <h2>Find IPFS CID</h2>
                <div class="search-container">
                    <input
                        autofocus
                        type="text"
                        placeholder="Enter CID (baf...)"
                        .value="${this.searchCid}"
                        @input="${this.handleInput}"
                        @keypress="${(e) => e.key === 'Enter' && this.handleFind()}"
                    />
                    <button
                        class="btn btn-primary"
                        @click="${this.handleFind}"
                        ?disabled="${this.loading}"
                    >
                        ${this.loading ? 'Searching...' : 'Find'}
                    </button>
                </div>

                ${this.error ? html`
                    <div class="alert alert-danger">${this.error}</div>
                ` : ''}

                ${this.results.length > 0 ? html`
                    <h3>Results</h3>
                    <table class="table table-dark table-striped">
                        <thead>
                            <tr>
                                <th>Piece CID</th>
                                <th>Offset</th>
                                <th>Size</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${this.results.map(item => html`
                                <tr>
                                    <td><a href="/pages/piece/?id=${item.piece_cid}">${item.piece_cid}</a></td>
                                    <td>${item.err ? html`<span class="text-danger">${item.err}</span>` : item.offset}</td>
                                    <td>${this.formatBytes(item.size)}</td>
                                </tr>
                            `)}
                        </tbody>
                    </table>
                ` : ''}
            </div>
        `;
    }

    formatBytes(bytes) {
        if (!bytes) return '0 Bytes';
        const k = 1024;
        const sizes = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    static styles = css`
        .search-container {
            display: grid;
            grid-template-columns: 1fr auto;
            grid-column-gap: 0.75rem;
            margin-bottom: 1rem;
        }
    `;
}

customElements.define('content-page', ContentPage);
