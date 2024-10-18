import {css, html, LitElement} from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class LegacyDealList extends LitElement {
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
            this.deals = await RPCCall('LegacyStorageDealList', params);
            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load legacy deals:', error);
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
        return html`
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        crossorigin="anonymous"
      />
      <link rel="stylesheet" href="/ux/main.css" />

      <div>
        <h2>Legacy Deal List</h2>
        <table class="table table-dark table-striped table-sm">
          <thead>
            <tr>
              <th>ID</th>
              <th>Provider</th>
              <th>Piece CID</th>
              <th>Piece Size</th>
              <th>Created At</th>
              <!-- Add more columns as needed -->
            </tr>
          </thead>
          <tbody>
            ${this.deals.map(
            (deal) => html`
                <tr>
                    <td><a href="/pages/mk12-deal/?id=${deal.id}">${deal.id}</a></td>
                  <td>${deal.miner}</td>
                  <td>${deal.piece_cid}</td>
                  <td>${this.formatBytes(deal.piece_size)}</td>
                  <td>${new Date(deal.created_at).toLocaleString()}</td>
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

    static styles = css`
    .pagination-controls {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 1rem;
    }
  `;
}

customElements.define('legacy-deal-list', LegacyDealList);