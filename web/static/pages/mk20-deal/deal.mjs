import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { formatDate } from '/lib/dateutil.mjs';
import '/ux/epoch.mjs';
import '/lib/cu-wallet.mjs';
import '/ux/yesno.mjs';

class DealDetails extends LitElement {
    constructor() {
        super();
        this.loadData();
    }

    async loadData() {
        try {
            const params = new URLSearchParams(window.location.search);
            this.data = await RPCCall('MK20DDOStorageDeal', [params.get('id')]);
            setTimeout(() => this.loadData(), 10000);
            this.requestUpdate();
        } catch (error) {
            alert('Failed to load deal details: ' + error);
            console.error('Failed to load deal details:', error);
        }
    }

    render() {
        if (!this.data) return html`<p>No data.</p>`;

        const { Identifier, Data, Products } = this.data.deal;

        return html`
          <div class="table-container">
            <h5>Deal</h5>
            <table class="table table-bordered">
                <tr><th>Identifier</th><td>${Identifier}</td></tr>
                <tr><th>Error</th><td><error-or-not .value=${this.data.error}></error-or-not></td></tr>
                <tr><th>PieceCID</th><td>${Data?.piece_cid['/']}</td></tr>
                <tr><th>Size</th><td>${Data?.size}</td></tr>
            </table>

            ${this.renderPieceFormat(Data?.format)}
            ${Data?.source_http ? this.renderSourceHTTP(Data.source_http) : ''}
            ${Data?.source_aggregate ? this.renderSourceAggregate(Data.source_aggregate) : ''}
            ${Data?.source_offline ? this.renderSourceOffline(Data.source_offline) : ''}
            ${Data?.source_httpput ? this.renderSourceHttpPut(Data.source_httpput) : ''}
    
            ${Products?.ddo_v1 ? this.renderDDOV1(Products.ddo_v1) : ''}
          </div>
        `;
    }

    renderPieceFormat(format) {
        if (!format) return '';
        return html`
      <h6>Piece Format</h6>
      <table class="table table-sm nested-table">
        ${format.car ? html`<tr><th>Car</th><td>Yes</td></tr>` : ''}
        ${format.aggregate
            ? html`
              <tr><th>Aggregate Type</th><td>${format.aggregate.type}</td></tr>
              <tr><td colspan="2">${this.renderAggregateSubs(format.aggregate.sub)}</td></tr>
            `
            : ''}
        ${format.raw ? html`<tr><th>Raw</th><td>Yes</td></tr>` : ''}
      </table>
    `;
    }

    renderAggregateSubs(subs) {
        if (!subs?.length) return '';
        return html`
      <h6>Aggregate Sub Formats</h6>
      <table class="table table-sm nested-table">
        <thead><tr><th>#</th><th>Car</th><th>Raw</th><th>Aggregate</th></tr></thead>
        <tbody>
          ${subs.map((s, i) => html`
            <tr>
              <td>${i + 1}</td>
              <td>${s.car ? 'Yes' : ''}</td>
              <td>${s.raw ? 'Yes' : ''}</td>
              <td>${s.aggregate ? 'Yes' : ''}</td>
            </tr>
          `)}
        </tbody>
      </table>
    `;
    }

    renderSourceHTTP(src) {
        return html`
      <h6>Source HTTP</h6>
      <table class="table table-sm nested-table">
        <tr><th>Raw Size</th><td>${src.rawsize}</td></tr>
        <tr>
          <td colspan="2">
            <strong>URLs</strong>
            <table class="table table-sm">
              <thead><tr><th>URL</th><th>Priority</th><th>Fallback</th></tr></thead>
              <tbody>
                ${src.urls.map(u => html`
                  <tr>
                    <td>${u.url}</td>
                    <td>${u.priority}</td>
                    <td>${u.fallback ? 'Yes' : 'No'}</td>
                  </tr>
                `)}
              </tbody>
            </table>
          </td>
        </tr>
      </table>
    `;
    }

    renderSourceAggregate(src) {
        return html`
      <h6>Source Aggregate</h6>
      ${src.pieces.map((piece, i) => html`
        <div class="nested-table">
          <strong>Piece ${i + 1}</strong>
          <table class="table table-sm">
            <tr><th>PieceCID</th><td>${piece.piece_cid['/']}</td></tr>
            <tr><th>Size</th><td>${piece.size}</td></tr>
          </table>
        </div>
      `)}
    `;
    }

    renderSourceOffline(src) {
        return html`
      <h6>Source Offline</h6>
      <table class="table table-sm nested-table">
        <tr><th>Raw Size</th><td>${src.raw_size}</td></tr>
      </table>
    `;
    }

    renderSourceHttpPut(src) {
        return html`
      <h6>Source HTTP PUT</h6>
      <table class="table table-sm nested-table">
        <tr><th>Raw Size</th><td>${src.raw_size}</td></tr>
      </table>
    `;
    }

    renderDDOV1(ddo) {
        return html`
      <h6>DDO v1</h6>
      <table class="table table-sm nested-table">
        <tr><th>Provider</th><td>${ddo.provider}</td></tr>
        <tr><th>Client</th><td>${ddo.client}</td></tr>
        <tr><th>Piece Manager</th><td>${ddo.piece_manager}</td></tr>
        <tr><th>Duration</th><td>${ddo.duration}</td></tr>
        ${ddo.allocation_id ? html`<tr><th>Allocation ID</th><td>${ddo.allocation_id}</td></tr>` : ''}
        <tr><th>Contract</th><td>${ddo.contract_address}</td></tr>
        <tr><th>Verify Method</th><td>${ddo.contract_verify_method}</td></tr>
        <tr><th>Notify Address</th><td>${ddo.notification_address}</td></tr>
        <tr><th>Indexing</th><td>${ddo.indexing ? 'Yes' : 'No'}</td></tr>
        <tr><th>Announce to IPNI</th><td>${ddo.announce_to_ipni ? 'Yes' : 'No'}</td></tr>
      </table>
    `;
    }
}
customElements.define('deal-details', DealDetails);

// import { LitElement, html, css } from 'lit';
// import { customElement, property } from 'lit/decorators.js';
//
// @customElement('deal-view')
// export class DealView extends LitElement {
//     @property({ type: Object }) deal;
//
//     static styles = css`
//     table {
//       border-collapse: collapse;
//       width: 100%;
//       margin-bottom: 1rem;
//     }
//     th, td {
//       border: 1px solid #ddd;
//       padding: 0.5rem;
//       vertical-align: top;
//     }
//     th {
//       background-color: #f8f9fa;
//       text-align: left;
//     }
//     .nested-table {
//       margin-left: 1rem;
//       width: auto;
//     }
//   `;
//
//     renderNested(title, obj) {
//         if (!obj) return html``;
//         return html`
//       <tr>
//         <th colspan="2">${title}</th>
//       </tr>
//       ${Object.entries(obj).map(([key, value]) => html`
//         <tr>
//           <td>${key}</td>
//           <td>
//             ${typeof value === 'object' && value !== null
//             ? html`<table class="nested-table">${this.renderRows(value)}</table>`
//             : String(value)}
//           </td>
//         </tr>
//       `)}
//     `;
//     }
//
//     renderRows(data) {
//         return Object.entries(data).map(([key, value]) => {
//             if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
//                 return html`${this.renderNested(key, value)}`;
//             } else {
//                 return html`
//           <tr>
//             <td>${key}</td>
//             <td>${Array.isArray(value) ? html`<pre>${JSON.stringify(value, null, 2)}</pre>` : String(value)}</td>
//           </tr>
//         `;
//             }
//         });
//     }
//
//     render() {
//         if (!this.deal) return html`<p>No deal provided.</p>`;
//         return html`
//       <table>
//         <thead>
//           <tr><th colspan="2">Deal</th></tr>
//         </thead>
//         <tbody>
//           <tr><td>Identifier</td><td>${this.deal.identifier}</td></tr>
//           ${this.deal.data ? html`
//             <tr>
//               <th colspan="2">Data</th>
//             </tr>
//             ${this.renderNested('Data', this.deal.data)}
//           ` : null}
//           ${this.deal.products?.ddo_v1 ? html`
//             <tr>
//               <th colspan="2">DDOV1</th>
//             </tr>
//             ${this.renderNested('DDOV1', this.deal.products.ddo_v1)}
//           ` : null}
//         </tbody>
//       </table>
//     `;
//     }
// }

