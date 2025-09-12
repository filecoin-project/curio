import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { formatDate } from '/lib/dateutil.mjs';
import '/ux/epoch.mjs';
import '/lib/cu-wallet.mjs';
import '/ux/yesno.mjs';

class DealDetails extends LitElement {
    constructor() {
        super();
        this.loaddata();
    }

    createRenderRoot() {
        return this; // Render into light DOM instead of shadow DOM
    }


    async loaddata() {
        try {
            const params = new URLSearchParams(window.location.search);
            this.data = await RPCCall('MK20DDOStorageDeal', [params.get('id')]);
            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load deal details:', error);
            alert(`Failed to load deal details: ${error.message}`);
        }
    }

    render() {
        if (!this.data) return html`<p>No data.</p>`;

        const { identifier, client, data, products, error } = this.data.deal;


        return html`
            <link
                    href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                    rel="stylesheet"
                    crossorigin="anonymous"
            />
            <link rel="stylesheet" href="/ux/main.css" />
            
            <table class="table table-dark table-striped table-sm">
                <tr><th>Identifier</th><td>${identifier}</td></tr>
                <tr><th>Client</th><td><cu-wallet wallet_id=${client}></td></tr>
                <tr><th>Error</th><td><error-or-not .value=${this.data.error}></error-or-not></td></tr>
                <tr>
                    <th>PieceCID</th>
                    <td>
                        ${data
                                ? html`<a href="/pages/piece/?id=${data.piece_cid['/']}">${data.piece_cid['/']}</a>`
                                : "Not Available"}
                    </td>
                </tr>
            </table>
            
            <h4>Piece Format</h4>
            ${this.renderPieceFormat(data?.format)}
              
              <h4>Data Source</h4>
              <table class="table table-dark table-striped table-sm">
                  <thead>
                      <th><strong>Name</strong></th>
                      <th><strong>Details</strong></th>
                  </thead>
                  <tbody>
                    ${this.renderDataSource(data, identifier)}
                  </tbody>
              </table>
    
            ${products?.ddo_v1 ? this.renderDDOV1(products.ddo_v1) : ''}
            ${products?.pdp_v1 ? this.renderPDPV1(products.pdp_v1) : ''}
            ${products?.retrieval_v1 ? this.renderRetV1(products.retrieval_v1) : ''}
        `;
    }

    renderDataSource(data, id){
        if (!data) return '';
        if (data.source_http) {
            return html`
                <tr>
                    <td><strong>HTTP</strong></td>
                    <td>${data?.source_http ? this.renderSourceHTTP(data.source_http) : ''}</td>
                </tr>
            `
        }
        if (data.source_aggregate) {
            return html`
                <tr>
                    <td><strong>Aggregate</strong></td>
                    <td>${data?.source_aggregate ? this.renderSourceAggregate(data.source_aggregate) : ''}</td>
                </tr>
            `
        }
        if (data.source_offline) {
            return html`
                <tr>
                    <td><strong>Offline</strong></td>
                    <td>${data?.source_offline ? this.renderSourceOffline(data.source_offline) : ''}</td>
                </tr>
            `
        }
        if (data.source_httpput) {
            return html`
                <tr>
                    <td><strong><a href="/pages/upload-status/?id=${id}">HTTP Put</a></strong></td>
                    <td>${data?.source_httpput ? this.renderSourceHttpPut(data.source_httpput) : ''}</td>
                </tr>
            `
        }
    }

    renderPieceFormat(format) {
        if (!format) return '';
        return html`
      <table class="table table-dark table-striped table-sm">
          <thead>
            <th>Format Name</th>
            <th>Details</th>
          </thead>
          <tbody>
          ${format.car ? html`<tr><td>Car</td><td></td></tr>` : ''}
          ${format.aggregate
                  ? html`
              <tr><td>Aggregate</td><td>Type ${format.aggregate.type}</td></tr>
            `
                  : ''}
          ${format.raw ? html`<tr><td>Raw</td><td></td></tr>` : ''}
          </tbody>
      </table>
    `;
    }

    renderSourceHTTP(src) {
        return html`
      <table class="table table-dark table-striped table-sm">
        <tr><td>Raw Size</td><td>${src.rawsize}</td></tr>
        <tr><td>${src.urls ? this.renderUrls(src.urls) : ''}</td></tr>
      </table>
    `;
    }

    renderUrls(urls) {
        if (!urls?.length) return '';
        return html`
            <table class="table table-dark table-striped table-sm">
                <thead>
                    <th>URL</th>
                    <th>Headers</th>
                    <th>Priority</th>
                    <th>Fallback</th>
                </thead>
                <tbody>
                    ${urls.map(u => html`
                        <tr>
                            <td>${u.url}</td>
                            <td>
                                <details>
                                    <summary>[SHOW]</summary>
                                    <pre>${JSON.stringify(u.headers, null, 2)}</pre>
                                </details>
                            </td
                            <td>${u.priority}</td>
                            <td>${u.fallback}</td>
                        </tr>
                    `)}
                </tbody>
            </table>
        `
    }

    renderSourceAggregate(src) {
        return html`
        <details>
            <summary>[Aggregate Details]</summary>
            <div class="accordion" id="aggregatePieces">
                ${src.pieces.map((piece, i) => html`
                    <div class="accordion-item bg-dark text-white">
                        <h2 class="accordion-header" id="heading${i}">
                            <button class="accordion-button collapsed bg-dark text-white" type="button" data-bs-toggle="collapse" data-bs-target="#collapse${i}">
                                Piece ${i + 1}
                            </button>
                        </h2>
                        <div id="collapse${i}" class="accordion-collapse collapse" data-bs-parent="#aggregatePieces">
                            <div class="accordion-body">
                                <ul class="list-group list-group-flush">
                                    <li class="list-group-item bg-dark text-white"><strong>PieceCID:</strong> ${piece.piece_cid['/']}</li>
                                    <li class="list-group-item bg-dark text-white">${this.renderPieceFormat(piece.format)}</li>
                                    <li class="list-group-item bg-dark text-white">${this.renderDataSource(piece)}</li>
                                </ul>
                            </div>
                        </div>
                    </div>
                `)}
            </div>
        </details>
    `;
    }

    renderSourceOffline(src) {
        return html`
      <table class="table table-dark table-striped table-sm">
        <tr><th>Raw Size</th><td>${src.raw_size}</td></tr>
      </table>
    `;
    }

    renderSourceHttpPut(src) {
        return html`
      <table class="table table-dark table-striped table-sm">
        <tr><th>Raw Size</th><td>${src.raw_size}</td></tr>
      </table>
    `;
    }

    renderDDOV1(ddo) {
        if (!ddo) return '';
        return html`
      <h6>DDO v1</h6>
      <table class="table table-dark table-striped table-sm">
        <tr><th>Provider</th><td>${ddo.provider}</td></tr>
        <tr><th>Piece Manager</th><td><cu-wallet wallet_id=${ddo.piece_manager}></td></tr>
        <tr><th>Duration</th><td>${ddo.duration}</td></tr>
        ${ddo.allocation_id ? html`<tr><th>Allocation ID</th><td>${ddo.allocation_id}</td></tr>` : ''}
        <tr><th>Contract</th><td>${ddo.contract_address}</td></tr>
        <tr><th>Verify Method</th><td>${ddo.contract_verify_method}</td></tr>
        <tr><th>Notify Address</th><td>${ddo.notification_address}</td></tr>
      </table>
    `;
    }

    renderPDPV1(pdp) {
        if (!pdp) return '';
        return html`
            <h6>PDP V1</h6>
            <table class="table table-dark table-striped table-sm">
                <tr><th>Create DataSet</th><td><yes-no .value="${pdp.create_data_set}"></td></tr>
                <tr><th>Create Piece</th><td><yes-no .value="${pdp.add_piece}"></td></tr>
                <tr><th>Remove Piece</th><td><yes-no .value="${pdp.delete_piece}"></td></tr>
                <tr><th>Remove DataSet</th><td><yes-no .value="${pdp.delete_data_set}"></td></tr>
                <tr><th>Record Keeper</th><td>${pdp.record_keeper}></td></tr>
                ${pdp.data_set_id ? html`<tr><th>DataSet ID</th><td>${pdp.data_set_id}</td></tr>` : ``}
                ${pdp.piece_ids ? html`<tr><th>Piece IDs</th><td>${pdp.piece_ids}</td></tr>` : ``}
            </table>
        `;
    }

    renderRetV1(ret) {
        if (!ret) return '';
        return html`
            <h6>Retrieval v1</h6>
            <table class="table table-dark table-striped table-sm">
                <tr><th>Indexing</th><td>${ret.indexing ? 'Yes' : 'No'}</td></tr>
                <tr><th>Announce Piece to IPNI</th><td>${ret.announce_payload ? 'Yes' : 'No'}</td></tr>
                <tr><th>Announce Payload to IPNI</th><td>${ret.announce_payload ? 'Yes' : 'No'}</td></tr>
            </table>
    `;
    }
}
customElements.define('deal-details', DealDetails);

