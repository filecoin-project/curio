import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
customElements.define('piece-info',class PieceInfoElement extends LitElement {
    constructor() {
        super();
        this.loadData();
    }
    async loadData() {
        try {
            const params = new URLSearchParams(window.location.search);
            this.data = await RPCCall('PieceInfo', [params.get('id')]);
            setTimeout(() => this.loadData(), 10000);
            this.requestUpdate();
        }catch (error) {
            console.error('Failed to load piece details:', error);
        }
    }
    render() {
        if (!this.data) {
            return html`<div>Loading...</div>`;
        }
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <h2>${this.data.piece_cid}</h2>
            <table class="table table-dark">
                <tr>
                    <td>Size</td>
                    <td>Created At</td>
                    <td>Indexed</td>
                    <td>Indexed At</td>
                    <td>IPNI AD</td>
                </tr>
                <tr>
                    <td>${this.toHumanBytes(this.data.size)}</td>
                    <td>${this.data.created_at}</td>
                    <td>${this.data.indexed}</td>
                    <td>${this.data.indexed_at}</td>
                    <td>${this.data.ipni_ad ? html`<a href="/pages/ipni/?ad_cid=${this.data.ipni_ad}">${this.data.ipni_ad}</a>` : 'No Ad Found'}</td>
                </tr>
            </table>
            <hr>
            <h2>Piece Deals</h2>
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>ID</th>
                    <th>Deal Type</th>
                    <th>Miner</th>
                    <th>Chain Deal ID</th>
                    <th>Sector</th>
                    <th>Offset</th>
                    <th>Length</th>
                    <th>Raw Size</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.deals.map((item, i) => html`
                    <tr>
                <td>${item.id}</td>
                <td>${item.boost_deal ? 'Boost' : (item.legacy_deal ? 'Legacy' : 'DDO')}</td>
                <td>${item.miner}</td>
                <td>${item.chain_deal_id}</td>
                <td>${item.sector}</td>
                <td>${item.offset}</td>
                <td>${item.length}</td>
                <td>${item.raw_size}</td>
            </tr>
                `)}
                </tbody>
            </table>
            <hr>
        `;
    }

    toHumanBytes(bytes) {
        const sizes = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB', 'ZiB'];
        let sizeIndex = 0;
        for (; bytes >= 1024 && sizeIndex < sizes.length - 1; sizeIndex++) {
            bytes /= 1024;
        }
        return bytes.toFixed(2) + ' ' + sizes[sizeIndex];
    }
} );
