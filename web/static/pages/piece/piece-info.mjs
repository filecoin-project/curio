import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/yesno.mjs';
import '/ux/task.mjs';
import { formatDate } from '/lib/dateutil.mjs';

customElements.define('piece-info', class PieceInfoElement extends LitElement {
    static properties = {
        data: { type: Object },
        mk12DealData: { type: Array },
        pieceParkStates: { type: Object },
    };

    constructor() {
        super();
        this.data = null;
        this.mk12DealData = [];
        this.pieceParkStates = null;
        this.loadData();
    }

    async loadData() {
        try {
            const params = new URLSearchParams(window.location.search);
            const pieceCid = params.get('id');

            // Fetch piece info
            this.data = await RPCCall('PieceInfo', [pieceCid]);
            this.mk12DealData = await RPCCall('MK12DealDetail', [pieceCid]);
            this.pieceParkStates = await RPCCall('PieceParkStates', [pieceCid]);

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
                    <td>Piece CID</td>
                    <td>${this.data.piece_cid}</td>
                </tr>
                <tr>
                    <td>Size</td>
                    <td>${this.toHumanBytes(this.data.size)}</td>
                </tr>
                <tr>
                    <td>Created At</td>
                    <td>${formatDate(this.data.created_at)}</td>
                </tr>
                <tr>
                    <td>Indexed</td>
                    <td><done-not-done .value=${this.data.indexed}></done-not-done></td>
                </tr>
                <tr>
                    <td>Indexed At</td>
                    <td>${formatDate(this.data.indexed_at)}</td>
                </tr>
                <tr>
                    <td>IPNI AD</td>
                    <td>
                        ${this.data.ipni_ad ? html`<a href="/pages/ipni/?ad_cid=${this.data.ipni_ad}">${this.data.ipni_ad}</a>` : 'No Ad Found'}
                    </td>
                </tr>
            </table>

            <h2>Active Piece Deals</h2>
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

            ${this.pieceParkStates ? this.renderPieceParkStates() : ''}

            ${this.mk12DealData && this.mk12DealData.length > 0 ? html`
                <h2>Related Deals</h2>
                ${this.mk12DealData.map((entry) => html`
                    <h3>Deal ${entry.deal.uuid}</h3>
                    <table class="table table-dark table-striped table-sm">
                        <tr><th colspan="2"><h5>Top Level Info 📋</h5></th></tr>
                        <tr><td>Created At</td><td>${formatDate(entry.deal.created_at)}</td></tr>
                        <tr><td>UUID</td><td><a href="/pages/mk12-deal/?id=${entry.deal.uuid}">${entry.deal.uuid}</a></td></tr>
                        <tr><td>Provider (sp_id)</td><td>f0${entry.deal.sp_id}</td></tr>
                        <tr><td>Signed Proposal CID</td><td>${entry.deal.signed_proposal_cid}</td></tr>
                        <tr><td>Proposal CID</td><td>${entry.deal.proposal_cid}</td></tr>

                        <tr><th colspan="2"><h5>Proposal 📝</h5></th></tr>
                        <tr><td>Proposal</td><td><pre>${JSON.stringify(entry.deal.proposal, null, 2)}</pre></td></tr>
                        <tr><td>Proposal Signature</td><td><pre>${entry.deal.proposal_signature}</pre></td></tr>

                        <tr><th colspan="2"><h5>Deal Parameters ⚙️</h5></th></tr>
                        <tr><td>Piece CID</td><td>${entry.deal.piece_cid}</td></tr>
                        <tr><td>Piece Size</td><td>${this.toHumanBytes(entry.deal.piece_size)}</td></tr>
                        <tr><td>Start Epoch</td><td>${entry.deal.start_epoch}</td></tr>
                        <tr><td>End Epoch</td><td>${entry.deal.end_epoch}</td></tr>
                        <tr><td>Verified</td><td><yes-no .value=${entry.deal.verified}></yes-no></td></tr>
                        <tr><td>Fast Retrieval</td><td><yes-no .value=${entry.deal.fast_retrieval}></yes-no></td></tr>
                        <tr><td>Announce to IPNI</td><td><yes-no .value=${entry.deal.announce_to_ipni}></yes-no></td></tr>

                        <tr><th colspan="2"><h5>Data Source 📥️</h5></th></tr>
                        <tr><td>Client Peer ID</td><td>${entry.deal.client_peer_id}</td></tr>
                        <tr><td>Offline</td><td><yes-no .value=${entry.deal.offline}></yes-no></td></tr>
                        <tr><td>URL</td><td>${entry.deal.url.Valid ? entry.deal.url.String : 'N/A'}</td></tr>
                        <tr>
                            <td>URL Headers</td>
                            <td>
                                <details>
                                    <summary>[SHOW]</summary>
                                    <pre>${JSON.stringify(entry.deal.url_headers, null, 2)}</pre>
                                </details>
                            </td>
                        </tr>

                        <tr><th colspan="2"><h5>Status 🟢️🔴</h5></th></tr>
                        <tr><td>Publish CID</td><td>${entry.deal.publish_cid.Valid ? entry.deal.publish_cid.String : 'N/A'}</td></tr>
                        <tr><td>Chain Deal ID</td><td>${entry.deal.chain_deal_id.Valid ? entry.deal.chain_deal_id.Int64 : 'N/A'}</td></tr>
                        <tr><td>Error</td><td>${entry.deal.error.Valid ? entry.deal.error.String : 'N/A'}</td></tr>
                        ${(() => {
                            const matchingPieceDeals = this.data.deals.filter(deal => deal.id === entry.deal.uuid);
                            if (matchingPieceDeals.length > 0) {
                                return html`
                                <tr><th colspan="2"><h5>Associated Piece Deals 🔗️</h5></th></tr>
                                <tr><td colspan="2" style="padding-left: 32px">
                                <table class="table table-dark table-striped table-sm">
                                        <thead>
                                            <tr>
                                                <th>ID</th>
                                                <th>Miner</th>
                                                <th>Chain Deal ID</th>
                                                <th>Sector</th>
                                                <th>Offset</th>
                                                <th>Length</th>
                                                <th>Raw Size</th>
                                            </tr>
                                        </thead>
                                        <tbody>
                                            ${matchingPieceDeals.map((item) => html`
                                                <tr>
                                                    <td><a href="/pages/mk12-deal/?id=${item.id}">${item.id}</a></td>
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
                                </td></tr>
                            `;
                            }
                        })()}
                        ${entry.pipeline ? html`
                            <tr><th colspan="2"><h5 style="color: var(--color-warning-main)">PIPELINE ACTIVE</h5></th></tr>
                            <tr><td>Created At</td><td>${formatDate(entry.pipeline.created_at)}</td></tr>
                            <tr><td>Piece CID</td><td>${entry.pipeline.piece_cid}</td></tr>
                            <tr><td>Piece Size</td><td>${this.toHumanBytes(entry.pipeline.piece_size)}</td></tr>
                            <tr><td>Raw Size</td><td>${entry.pipeline.raw_size.Valid ? this.toHumanBytes(entry.pipeline.raw_size.Int64) : 'N/A'}</td></tr>
                            <tr><td>Offline</td><td><yes-no .value=${entry.pipeline.offline}></yes-no></td></tr>
                            <tr><td>URL</td><td>${entry.pipeline.url.Valid ? entry.pipeline.url.String : 'N/A'}</td></tr>
                            <tr><td>Headers</td><td><pre>${JSON.stringify(entry.pipeline.headers, null, 2)}</pre></td></tr>
                            <tr><td>Should Index</td><td>${this.renderNullableYesNo(entry.pipeline.should_index.Bool)}</td></tr>
                            <tr>
                                <td>Announce</td>
                                <td>${this.renderNullableYesNo(entry.pipeline.announce.Bool)}</td>
                            </tr>

                            <tr><th colspan="2"><h5>Progress 🛠️</h5></th></tr>
                            <tr>
                                <td>Data Fetched</td>
                                <td>${this.renderNullableDoneNotDone(entry.pipeline.started.Bool)}</td>
                            </tr>
                            <tr>
                                <td>After Commp</td>
                                <td>${this.renderNullableDoneNotDone(entry.pipeline.after_commp.Bool)}</td>
                            </tr>
                            <tr>
                                <td>After PSD</td>
                                <td>${this.renderNullableDoneNotDone(entry.pipeline.after_psd.Bool)}</td>
                            </tr>
                            <tr>
                                <td>After Find Deal</td>
                                <td>${this.renderNullableDoneNotDone(entry.pipeline.after_find_deal.Bool)}</td>
                            </tr>
                            <tr>
                                <td>Sealed</td>
                                <td>${this.renderNullableDoneNotDone(entry.pipeline.sealed.Bool)}</td>
                            </tr>
                            <tr>
                                <td>Indexed</td>
                                <td>${this.renderNullableDoneNotDone(entry.pipeline.indexed.Bool)}</td>
                            </tr>
                            <tr>
                                <td>Announced</td>
                                <td><done-not-done .value=${entry.pipeline.complete}></done-not-done></td>
                            </tr>
                            
                            <tr><th colspan="2"><h5>Early States 🌿</h5></th></tr>
                            <tr>
                                <td>Commp Task ID</td>
                                <td>
                                    ${entry.pipeline.commp_task_id.Valid
                                            ? html`<task-status .taskId=${entry.pipeline.commp_task_id.Int64}></task-status>`
                                            : 'N/A'}
                                </td>
                            </tr>
                            <tr>
                                <td>PSD Task ID</td>
                                <td>
                                    ${entry.pipeline.psd_task_id.Valid
                                            ? html`<task-status .taskId=${entry.pipeline.psd_task_id.Int64}></task-status>`
                                            : 'N/A'}
                                </td>
                            </tr>
                            <tr><td>PSD Wait Time</td><td>${entry.pipeline.psd_wait_time.Valid ? formatDate(entry.pipeline.psd_wait_time.Time) : 'N/A'}</td></tr>
                            <tr>
                                <td>Find Deal Task ID</td>
                                <td>
                                    ${entry.pipeline.find_deal_task_id.Valid
                                            ? html`<task-status .taskId=${entry.pipeline.find_deal_task_id.Int64}></task-status>`
                                            : 'N/A'}
                                </td>
                            </tr>

                            <tr><th colspan="2"><h5>Sealing 📦</h5></th></tr>
                            <tr><td>Sector</td><td>${entry.pipeline.sector.Valid ? html`<a href="/pages/sector/?sp=f0${entry.deal.sp_id}&id=${entry.pipeline.sector.Int64}">${entry.pipeline.sector.Int64}</a>` : 'N/A'}</td></tr>
                            <tr><td>Reg Seal Proof</td><td>${entry.pipeline.reg_seal_proof.Valid ? entry.pipeline.reg_seal_proof.Int64 : 'N/A'}</td></tr>
                            <tr><td>Sector Offset</td><td>${entry.pipeline.sector_offset.Valid ? entry.pipeline.sector_offset.Int64 : 'N/A'}</td></tr>
                            
                            <tr><th colspan="2"><h5>Indexing 🔍</h5></th></tr>
                            <tr><td>Indexing Created At</td><td>${entry.pipeline.indexing_created_at.Valid ? formatDate(entry.pipeline.indexing_created_at.Time) : 'N/A'}</td></tr>
                            <tr>
                                <td>Indexing Task ID</td>
                                <td>
                                    ${entry.pipeline.indexing_task_id.Valid
                                            ? html`<task-status .taskId=${entry.pipeline.indexing_task_id.Int64}></task-status>`
                                            : 'N/A'}
                                </td>
                            </tr>
                        ` : html`
                            <tr><td>No Pipeline Data</td><td></td></tr>
                        `}
                        </tbody>
                    </table>
                `)}
            ` : ''}
        `;
    }

    renderPieceParkStates() {
        return html`
        <h2>Staged Piece States</h2>
            <table class="table table-dark table-striped table-sm">
                <tr>
                    <td>ID</td>
                    <td>${this.pieceParkStates.id}</td>
                </tr>
                <tr>
                    <td>Piece CID</td>
                    <td>${this.pieceParkStates.piece_cid}</td>
                </tr>
                <tr>
                    <td>Padded Size</td>
                    <td>${this.toHumanBytes(this.pieceParkStates.piece_padded_size)}</td>
                </tr>
                <tr>
                    <td>Raw Size</td>
                    <td>${this.toHumanBytes(this.pieceParkStates.piece_raw_size)}</td>
                </tr>
                <tr>
                    <td>Complete</td>
                    <td>${this.renderNullableDoneNotDone(this.pieceParkStates.complete)}</td>
                </tr>
                <tr>
                    <td>Created At</td>
                    <td>${new Date(this.pieceParkStates.created_at).toLocaleString()}</td>
                </tr>
                <tr>
                    <td>Download Task</td>
                    <td>
                        ${this.pieceParkStates.task_id.Valid
                            ? html`<task-status .taskId=${this.pieceParkStates.task_id.Int64}></task-status>`
                            : 'N/A'}
                    </td>
                </tr>
                <tr>
                    <td>Cleanup Task</td>
                    <td>
                        ${this.pieceParkStates.cleanup_task_id.Valid
                            ? html`<task-status .taskId=${this.pieceParkStates.cleanup_task_id.Int64}></task-status>`
                            : 'N/A'}
                    </td>
                </tr>
            </table>

            <h3>Staged Piece References</h3>
            <table class="table table-dark table-striped table-sm">
                <thead>
                    <tr>
                        <td>Ref ID</td>
                        <td>Data URL</td>
                    </tr>
                </thead>
                <tbody>
                    ${this.pieceParkStates.refs.map((ref) => html`
                        <tr>
                            <td>${ref.ref_id}</td>
                            <td>
                                <p>${ref.data_url.Valid && ref.data_url.String || 'N/A'}</p>
                                <details>
                                    <summary>[SHOW HEADERS]</summary>
                                    <p><pre>${JSON.stringify(ref.data_headers, null, 2)}</pre></p>
                                </details>
                            </td>
                        </tr>
                    `)}
                </tbody>
            </table>
        `
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
            color: var(--color-text-dense);
        }
        .table-dark th {
            padding-top: 20px;
            border-bottom: dashed 1px #aaaa44;
            text-align: center;
        }
        h2 {
            margin-top: 20px;
        }
        h3 {
            margin-top: 20px;
        }
    `;
});
