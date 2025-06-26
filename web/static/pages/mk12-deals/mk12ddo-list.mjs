import {css, html, LitElement} from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { formatDate } from '/lib/dateutil.mjs';
import '/ux/yesno.mjs';

class MK12DDODealList extends LitElement {
    static properties = {
        deals: { type: Array },
        limit: { type: Number },
        offset: { type: Number },
        totalCount: { type: Number },
    };

    constructor() {
        super();
        this.deals = [];
        this.limit = 25;
        this.offset = 0;
        this.totalCount = 0;
        this.loadData();
    }

    async loadData() {
        try {
            const params = [this.limit, this.offset];
            this.deals = await RPCCall('MK12DDOStorageDealList', params);
            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load ddo deals:', error);
        }
    }

    nextPage() {
        this.offset += this.limit;
        this.loadData();
    }

    prevPage() {
        if (this.offset >= this.limit) {
            this.offset -= this.limit;
        } else {
            this.offset = 0;
        }
        this.loadData();
    }

    render() {
        // Check if there's an error or if the deals array is empty
        if (!this.deals || this.deals.length === 0) {
            return html``; // Return an empty template if there's no data to render
        }

        return html`
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        crossorigin="anonymous"
      />
      <link rel="stylesheet" href="/ux/main.css" />

      <div>
        <h2>DDO Deal List
                <button class="info-btn">
                    <!-- Inline SVG icon for the info button -->
                    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-info-circle" viewBox="0 0 16 16">
                        <path d="M8 15A7 7 0 1 1 8 1a7 7 0 0 1 0 14m0 1A8 8 0 1 0 8 0a8 8 0 0 0 0 16"/>
                        <path d="m8.93 6.588-2.29.287-.082.38.45.083c.294.07.352.176.288.469l-.738 3.468c-.194.897.105 1.319.808 1.319.545 0 1.178-.252 1.465-.598l.088-.416c-.2.176-.492.246-.686.246-.275 0-.375-.193-.304-.533zM9 4.5a1 1 0 1 1-2 0 1 1 0 0 1 2 0"/>
                    </svg>
                    <span class="tooltip-text">
              List of all DDO deals including the deals currently in pipeline. Use the pagination controls to navigate through the list.
            </span>
                </button>
            </h2>
        </h2>
        <table class="table table-dark table-striped table-sm">
          <thead>
            <tr>
                <th>Created At</th>
                <th>ID</th>
                <th>Provider</th>
                <th>Piece CID</th>
                <th>Piece Size</th>
                <th>Processed</th>
                <th>Error</th>
            </tr>
          </thead>
          <tbody>
            ${this.deals.map(
            (deal) => html`
                <tr>
                    <td>${formatDate(deal.created_at)}</td>
                    <td><a href="/pages/mk12-deal/?id=${deal.id}">${deal.id}</a></td>
                    <td>${deal.miner}</td>
                    <td>
                        ${deal.piece_cid_v2 && deal.piece_cid_v2 !== ""
                                ? html`<a href="/pages/piece/?id=${deal.piece_cid_v2}">${this.formatPieceCid(deal.piece_cid_v2)}</a>`
                                : html`${this.formatPieceCid(deal.piece_cid)}`
                        }
                    </td>
                    <td>${this.formatBytes(deal.piece_size)}</td>
                    <td><done-not-done .value=${deal.processed}></yes-no></td>
                    <td><error-or-not .value=${deal.error}></error-or-not></td>
                </tr>
              `
        )}
          </tbody>
        </table>
        <div class="pagination-controls">
          <button class="btn btn-secondary" @click="${this.prevPage}" ?disabled="${this.offset === 0}">
            Previous
          </button>
          <span>Page ${(this.offset / this.limit) + 1}</span>
          <button
            class="btn btn-secondary"
            @click="${this.nextPage}"
            ?disabled="${this.deals.length < this.limit}"
          >
            Next
          </button>
        </div>
      </div>
    `;
    }

    formatBytes(bytes) {
        const units = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB'];
        let i = 0;
        let size = bytes;
        while (size >= 1024 && i < units.length - 1) {
            size /= 1024;
            i++;
        }
        if (i === 0) {
            return `${size} ${units[i]}`;
        } else {
            return `${size.toFixed(2)} ${units[i]}`;
        }
    }

    formatPieceCid(pieceCid) {
        if (!pieceCid) return '';
        if (pieceCid.length <= 24) {
            return pieceCid;
        }
        const start = pieceCid.substring(0, 16);
        const end = pieceCid.substring(pieceCid.length - 8);
        return `${start}...${end}`;
    }

    static styles = css`
    .pagination-controls {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 1rem;
    }
    
    .info-btn {
        position: relative;
        border: none;
        background: transparent;
        cursor: pointer;
        color: #17a2b8;
        font-size: 1em;
        margin-left: 8px;
    }

    .tooltip-text {
        display: none;
        position: absolute;
        top: 50%;
        left: 120%; /* Position the tooltip to the right of the button */
        transform: translateY(-50%); /* Center the tooltip vertically */
        min-width: 440px;
        max-width: 600px;
        background-color: #333;
        color: #fff;
        padding: 8px;
        border-radius: 4px;
        font-size: 0.8em;
        text-align: left;
        white-space: normal;
        z-index: 10;
    }

    .info-btn:hover .tooltip-text {
        display: block;
    }
  `;
}

customElements.define('mk12ddo-deal-list', MK12DDODealList);