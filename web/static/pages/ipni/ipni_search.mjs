import { html, css, LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class IpniSearch extends LitElement {
    static properties = {
        searchTerm: { type: String },
        adData: { type: Object },
        errorMessage: { type: String },
    };

    constructor() {
        super();
        this.searchTerm = '';
        this.adData = null;
        this.errorMessage = '';
    }

    handleInput(event) {
        this.searchTerm = event.target.value;
    }

    async handleSearch() {
        if (this.searchTerm.trim() !== '') {
            try {
                const params = [this.searchTerm.trim()];
                this.adData = await RPCCall('GetAd', params);
                this.errorMessage = '';
            } catch (error) {
                console.error('Error fetching ad data:', error);
                this.errorMessage = 'Failed to fetch ad data. Please check the Ad CID and try again.';
                this.adData = null;
            }
        } else {
            this.adData = null;
            this.errorMessage = '';
        }
    }

    render() {
        return html`
      <!-- Bootstrap CSS -->
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        crossorigin="anonymous"
      />

      <div>
        <h2>IPNI Ad Search</h2>
        <div class="search-container">
          <input
            type="text"
            class="form-control"
            placeholder="Enter Ad CID"
            @input="${this.handleInput}"
          />
          <button class="btn btn-primary" @click="${this.handleSearch}">
            Search
          </button>
        </div>
        ${this.errorMessage
            ? html`<div class="alert alert-danger">${this.errorMessage}</div>`
            : ''}
        ${this.adData
            ? html`
              <div class="ad-details">
                <h3>Ad Details</h3>
                <table class="table table-dark table-striped table-sm">
                  <tr>
                    <th>Ad CID</th>
                    <td>${this.adData.ad_cid}</td>
                  </tr>
                  <tr>
                    <th>Miner</th>
                    <td>${this.adData.miner}</td>
                  </tr>
                  <tr>
                    <th>Is Remove</th>
                    <td>${this.adData.is_rm}</td>
                  </tr>
                  <tr>
                    <th>Previous</th>
                    <td>${this.adData.previous}</td>
                  </tr>
                  <tr>
                    <th>Addresses</th>
                    <td>${this.adData.addresses}</td>
                  </tr>
                  <tr>
                    <th>Entries</th>
                    <td>${this.adData.entries}</td>
                  </tr>
                  <tr>
                    <th>Piece CID</th>
                    <td>${this.adData.piece_cid}</td>
                  </tr>
                  <tr>
                    <th>Piece Size</th>
                    <td>${this.adData.piece_size || 'N/A'}</td>
                  </tr>
                </table>
              </div>
            `
            : ''}
      </div>
    `;
    }

    static styles = css`
    .search-container {
      display: flex;
      align-items: center;
      margin-bottom: 1rem;
    }

    .search-container input {
      flex: 1;
      margin-right: 0.5rem;
    }
  `;
}

customElements.define('ipni-search', IpniSearch);
