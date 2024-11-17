import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('piece-info', class PieceInfoElement extends LitElement {
    static properties = {
        data: { type: Object },
        mk12DealData: { type: Object },
    };

    constructor() {
        super();
        this.data = null;
        this.mk12DealData = null;
        this.loadData();
    }

    async loadData() {
        try {
            const params = new URLSearchParams(window.location.search);
            const pieceCid = params.get('id');

            // Fetch piece info
            this.data = await RPCCall('PieceInfo', [pieceCid]);
            this.mk12DealData = await RPCCall('MK12DealDetail', [pieceCid]);

            setTimeout(() => this.loadData(), 10000);
            this.requestUpdate();
        } catch (error) {
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

            <h2>Piece Information</h2>
            <table class="table table-dark table-striped table-sm">
                <tr>
                    <th>Piece CID</th>
                    <td>${this.data.piece_cid}</td>
                </tr>
                <tr>
                    <th>Size</th>
                    <td>${this.toHumanBytes(this.data.size)}</td>
                </tr>
                <tr>
                    <th>Created At</th>
                    <td>${new Date(this.data.created_at).toLocaleString()}</td>
                </tr>
                <tr>
                    <th>Indexed</th>
                    <td>${this.data.indexed ? 'Yes' : 'No'}</td>
                </tr>
                <tr>
                    <th>Indexed At</th>
                    <td>${new Date(this.data.indexed_at).toLocaleString()}</td>
                </tr>
                <tr>
                    <th>IPNI AD</th>
                    <td>${this.data.ipni_ad ? html`<a href="/pages/ipni/?ad_cid=${this.data.ipni_ad}">${this.data.ipni_ad}</a>` : 'No Ad Found'}</td>
                </tr>
            </table>

            <h2>Piece Deals</h2>
            <table class="table table-dark table-striped table-sm">
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
                    ${this.data.deals.map((item) => html`
                        <tr>
                            <td><a href="/pages/mk12-deal/?id=${item.id}">${item.id}</a></td>
                            <td>${item.boost_deal ? 'Boost' : (item.legacy_deal ? 'Legacy' : 'DDO')}</td>
                            <td>${item.miner}</td>
                            <td>${item.chain_deal_id}</td>
                            <td>${item.sector}</td>
                            <td>${item.offset}</td>
                            <td>${this.toHumanBytes(item.length)}</td>
                            <td>${this.toHumanBytes(item.raw_size)}</td>
                        </tr>
                    `)}
                </tbody>
            </table>

            ${this.mk12DealData ? html`
                <h2>MK12 Deal Details</h2>
                <h3>Deal Information</h3>
                <table class="table table-dark table-striped table-sm">
                    <tbody>
                        <tr><th>UUID</th><td><a href="/pages/mk12-deal/?id=${this.mk12DealData.deal.uuid}">${this.mk12DealData.deal.uuid}</a></td></tr>
                        <tr><th>Provider (sp_id)</th><td>${this.mk12DealData.deal.sp_id}</td></tr>
                        <tr><th>Created At</th><td>${new Date(this.mk12DealData.deal.created_at).toLocaleString()}</td></tr>
                        <tr><th>Signed Proposal CID</th><td>${this.mk12DealData.deal.signed_proposal_cid}</td></tr>
                        <tr><th>Proposal Signature</th><td><pre>${this.mk12DealData.deal.proposal_signature}</pre></td></tr>
                        <tr><th>Proposal</th><td><pre>${JSON.stringify(this.mk12DealData.deal.proposal, null, 2)}</pre></td></tr>
                        <tr><th>Offline</th><td>${this.mk12DealData.deal.offline ? 'Yes' : 'No'}</td></tr>
                        <tr><th>Verified</th><td>${this.mk12DealData.deal.verified ? 'Yes' : 'No'}</td></tr>
                        <tr><th>Start Epoch</th><td>${this.mk12DealData.deal.start_epoch}</td></tr>
                        <tr><th>End Epoch</th><td>${this.mk12DealData.deal.end_epoch}</td></tr>
                        <tr><th>Client Peer ID</th><td>${this.mk12DealData.deal.client_peer_id}</td></tr>
                        <tr><th>Chain Deal ID</th><td>${this.mk12DealData.deal.chain_deal_id.Valid ? this.mk12DealData.deal.chain_deal_id.Int64 : 'N/A'}</td></tr>
                        <tr><th>Publish CID</th><td>${this.mk12DealData.deal.publish_cid.Valid ? this.mk12DealData.deal.publish_cid.String : 'N/A'}</td></tr>
                        <tr><th>Piece CID</th><td>${this.mk12DealData.deal.piece_cid}</td></tr>
                        <tr><th>Piece Size</th><td>${this.toHumanBytes(this.mk12DealData.deal.piece_size)}</td></tr>
                        <tr><th>Fast Retrieval</th><td>${this.mk12DealData.deal.fast_retrieval ? 'Yes' : 'No'}</td></tr>
                        <tr><th>Announce to IPNI</th><td>${this.mk12DealData.deal.announce_to_ipni ? 'Yes' : 'No'}</td></tr>
                        <tr><th>URL</th><td>${this.mk12DealData.deal.url.Valid ? this.mk12DealData.deal.url.String : 'N/A'}</td></tr>
                        <tr><th>URL Headers</th><td><pre>${JSON.stringify(this.mk12DealData.deal.url_headers, null, 2)}</pre></td></tr>
                        <tr><th>Error</th><td>${this.mk12DealData.deal.error.Valid ? this.mk12DealData.deal.error.String : 'N/A'}</td></tr>
                    </tbody>
                </table>

                <h3>Pipeline States</h3>
                <table class="table table-dark table-striped table-sm">
                    <tbody>
                        <tr><th>Started</th><td>${this.formatNullableBoolean(this.mk12DealData.pipeline.started)}</td></tr>
                        <tr><th>Piece CID</th><td>${this.mk12DealData.pipeline.piece_cid}</td></tr>
                        <tr><th>Piece Size</th><td>${this.toHumanBytes(this.mk12DealData.pipeline.piece_size)}</td></tr>
                        <tr><th>Raw Size</th><td>${this.mk12DealData.pipeline.raw_size.Valid ? this.toHumanBytes(this.mk12DealData.pipeline.raw_size.Int64) : 'N/A'}</td></tr>
                        <tr><th>Offline</th><td>${this.mk12DealData.pipeline.offline ? 'Yes' : 'No'}</td></tr>
                        <tr><th>URL</th><td>${this.mk12DealData.pipeline.url.Valid ? this.mk12DealData.pipeline.url.String : 'N/A'}</td></tr>
                        <tr><th>Headers</th><td><pre>${JSON.stringify(this.mk12DealData.pipeline.headers, null, 2)}</pre></td></tr>
                        <tr><th>Commp Task ID</th><td>${this.mk12DealData.pipeline.commp_task_id.Valid ? this.mk12DealData.pipeline.commp_task_id.Int64 : 'N/A'}</td></tr>
                        <tr><th>After Commp</th><td>${this.formatNullableBoolean(this.mk12DealData.pipeline.after_commp)}</td></tr>
                        <tr><th>PSD Task ID</th><td>${this.mk12DealData.pipeline.psd_task_id.Valid ? this.mk12DealData.pipeline.psd_task_id.Int64 : 'N/A'}</td></tr>
                        <tr><th>After PSD</th><td>${this.formatNullableBoolean(this.mk12DealData.pipeline.after_psd)}</td></tr>
                        <tr><th>PSD Wait Time</th><td>${this.mk12DealData.pipeline.psd_wait_time.Valid ? new Date(this.mk12DealData.pipeline.psd_wait_time.Time).toLocaleString() : 'N/A'}</td></tr>
                        <tr><th>Find Deal Task ID</th><td>${this.mk12DealData.pipeline.find_deal_task_id.Valid ? this.mk12DealData.pipeline.find_deal_task_id.Int64 : 'N/A'}</td></tr>
                        <tr><th>After Find Deal</th><td>${this.formatNullableBoolean(this.mk12DealData.pipeline.after_find_deal)}</td></tr>
                        <tr><th>Sector</th><td>${this.mk12DealData.pipeline.sector.Valid ? this.mk12DealData.pipeline.sector.Int64 : 'N/A'}</td></tr>
                        <tr><th>Reg Seal Proof</th><td>${this.mk12DealData.pipeline.reg_seal_proof.Valid ? this.mk12DealData.pipeline.reg_seal_proof.Int64 : 'N/A'}</td></tr>
                        <tr><th>Sector Offset</th><td>${this.mk12DealData.pipeline.sector_offset.Valid ? this.mk12DealData.pipeline.sector_offset.Int64 : 'N/A'}</td></tr>
                        <tr><th>Sealed</th><td>${this.formatNullableBoolean(this.mk12DealData.pipeline.sealed)}</td></tr>
                        <tr><th>Should Index</th><td>${this.formatNullableBoolean(this.mk12DealData.pipeline.should_index)}</td></tr>
                        <tr><th>Indexing Created At</th><td>${this.mk12DealData.pipeline.indexing_created_at.Valid ? new Date(this.mk12DealData.pipeline.indexing_created_at.Time).toLocaleString() : 'N/A'}</td></tr>
                        <tr><th>Indexing Task ID</th><td>${this.mk12DealData.pipeline.indexing_task_id.Valid ? this.mk12DealData.pipeline.indexing_task_id.Int64 : 'N/A'}</td></tr>
                        <tr><th>Indexed</th><td>${this.formatNullableBoolean(this.mk12DealData.pipeline.indexed)}</td></tr>
                        <tr><th>Announce</th><td>${this.formatNullableBoolean(this.mk12DealData.pipeline.announce)}</td></tr>
                        <tr><th>Complete</th><td>${this.mk12DealData.pipeline.complete ? 'Yes' : 'No'}</td></tr>
                        <tr><th>Created At</th><td>${new Date(this.mk12DealData.pipeline.created_at).toLocaleString()}</td></tr>
                    </tbody>
                </table>
            ` : ''}
        `;
    }

    toHumanBytes(bytes) {
        if (typeof bytes !== 'number') {
            return 'N/A';
        }
        const sizes = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB', 'ZiB'];
        let sizeIndex = 0;
        for (; bytes >= 1024 && sizeIndex < sizes.length - 1; sizeIndex++) {
            bytes /= 1024;
        }
        return bytes.toFixed(2) + ' ' + sizes[sizeIndex];
    }

    formatNullableBoolean(value) {
        if (value === null || value === undefined) {
            return 'N/A';
        }
        return value ? 'Yes' : 'No';
    }

    static styles = css`
        .table-dark {
            background-color: #343a40;
        }
        .table-dark th, .table-dark td {
            color: #fff;
        }
        h2 {
            margin-top: 20px;
        }
    `;
});
