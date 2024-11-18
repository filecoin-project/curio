import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/yesno.mjs';

customElements.define('piece-info', class PieceInfoElement extends LitElement {
    static properties = {
        data: { type: Object },
        mk12DealData: { type: Array }, // Updated to be an array
    };

    constructor() {
        super();
        this.data = null;
        this.mk12DealData = []; // Initialize as an empty array
        this.loadData();
    }

    async loadData() {
        try {
            const params = new URLSearchParams(window.location.search);
            const pieceCid = params.get('id');

            // Fetch piece info
            this.data = await RPCCall('PieceInfo', [pieceCid]);
            this.mk12DealData = await RPCCall('MK12DealDetail', [pieceCid]);

            // TODO SNAP/POREP pipelines

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
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
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
                    <td><done-not-done .value=${this.data.indexed}></done-not-done></td>
                </tr>
                <tr>
                    <th>Indexed At</th>
                    <td>${new Date(this.data.indexed_at).toLocaleString()}</td>
                </tr>
                <tr>
                    <th>IPNI AD</th>
                    <td>
                        ${this.data.ipni_ad ? html`<a href="/pages/ipni/?ad_cid=${this.data.ipni_ad}">${this.data.ipni_ad}</a>` : 'No Ad Found'}
                    </td>
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
                        <td><a href="/pages/sector/?sp=${item.miner}&id=${item.sector}">${item.sector}</a></td>
                        <td>${item.offset}</td>
                        <td>${this.toHumanBytes(item.length)}</td>
                        <td>${this.toHumanBytes(item.raw_size)}</td>
                    </tr>
                `)}
                </tbody>
            </table>

            ${this.mk12DealData && this.mk12DealData.length > 0 ? html`
                <h2>MK12 Deal Details</h2>
                ${this.mk12DealData.map((entry) => html`
                    <h3>Deal Information (UUID: ${entry.deal.uuid})</h3>
                    <table class="table table-dark table-striped table-sm">
                        <tbody>
                            <tr><th>UUID</th><td><a href="/pages/mk12-deal/?id=${entry.deal.uuid}">${entry.deal.uuid}</a></td></tr>
                            <tr><th>Provider (sp_id)</th><td>${entry.deal.sp_id}</td></tr>
                            <tr><th>Created At</th><td>${new Date(entry.deal.created_at).toLocaleString()}</td></tr>
                            <tr><th>Signed Proposal CID</th><td>${entry.deal.signed_proposal_cid}</td></tr>
                            <tr><th>Proposal Signature</th><td><pre>${entry.deal.proposal_signature}</pre></td></tr>
                            <tr><th>Proposal</th><td><pre>${JSON.stringify(entry.deal.proposal, null, 2)}</pre></td></tr>
                            <tr><th>Offline</th><td><yes-no .value=${entry.deal.offline}></yes-no></td></tr>
                            <tr><th>Verified</th><td><yes-no .value=${entry.deal.verified}></yes-no></td></tr>
                            <tr><th>Start Epoch</th><td>${entry.deal.start_epoch}</td></tr>
                            <tr><th>End Epoch</th><td>${entry.deal.end_epoch}</td></tr>
                            <tr><th>Client Peer ID</th><td>${entry.deal.client_peer_id}</td></tr>
                            <tr><th>Chain Deal ID</th><td>${entry.deal.chain_deal_id.Valid ? entry.deal.chain_deal_id.Int64 : 'N/A'}</td></tr>
                            <tr><th>Publish CID</th><td>${entry.deal.publish_cid.Valid ? entry.deal.publish_cid.String : 'N/A'}</td></tr>
                            <tr><th>Piece CID</th><td>${entry.deal.piece_cid}</td></tr>
                            <tr><th>Piece Size</th><td>${this.toHumanBytes(entry.deal.piece_size)}</td></tr>
                            <tr><th>Fast Retrieval</th><td><yes-no .value=${entry.deal.fast_retrieval}></yes-no></td></tr>
                            <tr><th>Announce to IPNI</th><td><yes-no .value=${entry.deal.announce_to_ipni}></yes-no></td></tr>
                            <tr><th>URL</th><td>${entry.deal.url.Valid ? entry.deal.url.String : 'N/A'}</td></tr>
                            <tr><th>URL Headers</th><td><pre>${JSON.stringify(entry.deal.url_headers, null, 2)}</pre></td></tr>
                            <tr><th>Error</th><td>${entry.deal.error.Valid ? entry.deal.error.String : 'N/A'}</td></tr>
                            ${entry.pipeline ? html`
                                <tr><th colspan="2"><b>PIPELINE ACTIVE</b></th></tr>
                                <tr><th>Started</th><td>${this.renderNullableYesNo(entry.pipeline.started.Bool)}</td></tr>
                                <tr><th>Piece CID</th><td>${entry.pipeline.piece_cid}</td></tr>
                                <tr><th>Piece Size</th><td>${this.toHumanBytes(entry.pipeline.piece_size)}</td></tr>
                                <tr><th>Raw Size</th><td>${entry.pipeline.raw_size.Valid ? this.toHumanBytes(entry.pipeline.raw_size.Int64) : 'N/A'}</td></tr>
                                <tr><th>Offline</th><td><yes-no .value=${entry.pipeline.offline}></yes-no></td></tr>
                                <tr><th>URL</th><td>${entry.pipeline.url.Valid ? entry.pipeline.url.String : 'N/A'}</td></tr>
                                <tr><th>Headers</th><td><pre>${JSON.stringify(entry.pipeline.headers, null, 2)}</pre></td></tr>
                                <tr><th>Commp Task ID</th><td>${entry.pipeline.commp_task_id.Valid ? entry.pipeline.commp_task_id.Int64 : 'N/A'}</td></tr>
                                <tr><th>After Commp</th><td>${this.renderNullableDoneNotDone(entry.pipeline.after_commp.Bool)}</td></tr>
                                <tr><th>PSD Task ID</th><td>${entry.pipeline.psd_task_id.Valid ? entry.pipeline.psd_task_id.Int64 : 'N/A'}</td></tr>
                                <tr><th>After PSD</th><td>${this.renderNullableDoneNotDone(entry.pipeline.after_psd.Bool)}</td></tr>
                                <tr><th>PSD Wait Time</th><td>${entry.pipeline.psd_wait_time.Valid ? new Date(entry.pipeline.psd_wait_time.Time).toLocaleString() : 'N/A'}</td></tr>
                                <tr><th>Find Deal Task ID</th><td>${entry.pipeline.find_deal_task_id.Valid ? entry.pipeline.find_deal_task_id.Int64 : 'N/A'}</td></tr>
                                <tr><th>After Find Deal</th><td>${this.renderNullableDoneNotDone(entry.pipeline.after_find_deal.Bool)}</td></tr>
                                <tr><th>Sector</th><td>${entry.pipeline.sector.Valid ? html`<a href="/pages/sector/?sp=f0${entry.deal.sp_id}&id=${entry.pipeline.sector.Int64}">${entry.pipeline.sector.Int64}</a>` : 'N/A'}</td></tr>
                                <tr><th>Reg Seal Proof</th><td>${entry.pipeline.reg_seal_proof.Valid ? entry.pipeline.reg_seal_proof.Int64 : 'N/A'}</td></tr>
                                <tr><th>Sector Offset</th><td>${entry.pipeline.sector_offset.Valid ? entry.pipeline.sector_offset.Int64 : 'N/A'}</td></tr>
                                <tr><th>Sealed</th><td>${this.renderNullableDoneNotDone(entry.pipeline.sealed.Bool)}</td></tr>
                                <tr><th>Should Index</th><td>${this.renderNullableYesNo(entry.pipeline.should_index.Bool)}</td></tr>
                                <tr><th>Indexing Created At</th><td>${entry.pipeline.indexing_created_at.Valid ? new Date(entry.pipeline.indexing_created_at.Time).toLocaleString() : 'N/A'}</td></tr>
                                <tr><th>Indexing Task ID</th><td>${entry.pipeline.indexing_task_id.Valid ? entry.pipeline.indexing_task_id.Int64 : 'N/A'}</td></tr>
                                <tr><th>Indexed</th><td>${this.renderNullableDoneNotDone(entry.pipeline.indexed.Bool)}</td></tr>
                                <tr><th>Announce</th><td>${this.renderNullableDoneNotDone(entry.pipeline.announce.Bool)}</td></tr>
                                <tr><th>Complete</th><td><yes-no .value=${entry.pipeline.complete}></yes-no></td></tr>
                                <tr><th>Created At</th><td>${new Date(entry.pipeline.created_at).toLocaleString()}</td></tr>
                        ` : html`
                                <tr><th>No Pipeline Data</th><td></td></tr> 
                        `}
                        </tbody>
                    </table>
                `)}
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

    renderNullableYesNo(value) {
        if (value === null || value === undefined) {
            return 'N/A';
        }
        return html`<yes-no .value=${value}></yes-no>`;
    }

    renderNullableDoneNotDone(value) {
        if (value === null || value === undefined) {
            return 'N/A';
        }
        return html`<done-not-done .value=${value}></done-not-done>`;
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
        h3 {
            margin-top: 20px;
        }
    `;
});
