// deal-pipelines.mjs

import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class DealPipelines extends LitElement {
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
            const deals = await RPCCall('GetDealPipelines', params);
            this.deals = deals;

            // Optionally, get the total count of deals for pagination (if your API supports it)
            // For this example, we'll assume we have a way to get the total count
            // this.totalCount = await RPCCall('GetDealPipelinesCount', []);

            // If total count is not available, we can infer whether there are more pages based on the number of deals fetched
            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load deal pipelines:', error);
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
        <h2>Deal Pipelines</h2>
        <table class="table table-dark table-striped table-sm">
          <thead>
            <tr>
              <th>UUID</th>
              <th>SP ID</th>
              <th>Piece CID</th>
              <th>Piece Size</th>
              <th>Created At</th>
              <th>Status</th>
              <!-- Add more columns as needed -->
            </tr>
          </thead>
          <tbody>
            ${this.deals.map(
            (deal) => html`
                <tr>
                  <td>${deal.uuid}</td>
                  <td>f0${deal.sp_id}</td>
                  <td>${deal.piece_cid}</td>
                  <td>${this.formatBytes(deal.piece_size)}</td>
                  <td>${new Date(deal.created_at).toLocaleString()}</td>
                  <td>${this.getDealStatus(deal)}</td>
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

    getDealStatus(deal) {
        if (deal.complete) {
            return 'Complete';
        } else if (deal.sector) {
            return 'Sealed';
        } else if (deal.after_find_deal) {
            return 'On Chain';
        } else if (deal.after_psd) {
            return 'Piece Added';
        } else if (deal.after_commp) {
            return 'CommP Calculated';
        } else if (deal.started) {
            return 'Started';
        } else {
            return 'Pending';
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

customElements.define('deal-pipelines', DealPipelines);