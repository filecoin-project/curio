import { html, css, LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import './entry_grid.mjs';

class IpniSearch extends LitElement {
    static properties = {
        searchTerm: { type: String },
        adData: { type: Object },
        errorMessage: { type: String },
        showEntryGrid: { type: Boolean },
    };

    constructor() {
        super();
        this.searchTerm = '';
        this.adData = null;
        this.errorMessage = '';
        this.showEntryGrid = false;
    }

    connectedCallback() {
        super.connectedCallback();
        // Get the search term from the URL parameter (if available)
        const urlParams = new URLSearchParams(window.location.search);
        const adCid = urlParams.get('ad_cid');
        if (adCid) {
            this.searchTerm = adCid;
            this.handleSearch();
        }
    }

    handleInput(event) {
        this.searchTerm = event.target.value;
    }

    async handleSearch() {
        if (this.searchTerm.trim() !== '') {
            // Update the URL with the search term
            window.history.pushState({}, '', `?ad_cid=${encodeURIComponent(this.searchTerm.trim())}`);
            try {
                const params = [this.searchTerm.trim()];
                this.adData = await RPCCall('GetAd', params);
                this.errorMessage = '';
                this.showEntryGrid = false; // Reset when new search is performed
            } catch (error) {
                console.error('Error fetching ad data:', error);
                this.errorMessage = 'Failed to fetch ad data. Please check the Ad CID and try again.';
                this.adData = null;
                this.showEntryGrid = false;
            }
        } else {
            this.adData = null;
            this.errorMessage = '';
            this.showEntryGrid = false;
        }
    }

    async handleSetSkip(ad, skipValue) {
        try {
            await RPCCall('IPNISetSkip', [{"/": ad}, skipValue]);
            await this.handleSearch(); // Reload data after setting skip
            this.errorMessage = '';
        } catch (error) {
            console.error('Error setting skip:', error);
            this.errorMessage = 'Failed to set skip value.';
        }
    }

    handleScanClick() {
        this.showEntryGrid = true;
    }

    render() {
        return html`
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        crossorigin="anonymous"
      />
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

      <div>
        <h2>Ad Search</h2>
        <div class="search-container">
          <input
            type="text"
            placeholder="Enter Ad/Entry CID"
            .value="${this.searchTerm}"
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
                        <td>${this.adData.ad_cids.map(c => html`
                            <div>${c}</div>
                        `)}</td>
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
                          <th>Should Skip</th>
                          <td>
                              <span>${this.adData.is_skip}</span>
                              <details style="display: inline-block">
                                  <summary style="display: inline-block" class="btn btn-secondary btn-sm">Set</summary>
                                  ${this.adData.is_skip
                                          ? html`
                                              <button
                                                      class="btn btn-secondary btn-sm"
                                                      @click="${() => this.handleSetSkip(this.adData.ad_cid, false)}"
                                              >
                                                  Disable Skip
                                              </button>
                                          `
                                          : html`
                                              <button
                                                      class="btn btn-danger btn-sm"
                                                      @click="${() => this.handleSetSkip(this.adData.ad_cid, true)}"
                                              >
                                                  Enable Skip
                                              </button>
                                          `}
                              </details>
                          </td>
                      </tr>
                      <tr>
                        <th>Previous</th>
                          <td><a href="/pages/ipni/?ad_cid=${this.adData.previous}">${this.adData.previous}</a></td>
                      </tr>
                      <tr>
                        <th>Addresses</th>
                        <td>${this.adData.addresses}</td>
                      </tr>
                      <tr>
                        <th>Entries Head</th>
                        <td>
                          ${this.adData.entries}
                          <button class="btn btn-secondary btn-sm" @click="${this.handleScanClick}">
                            SCAN
                          </button>
                        </td>
                      </tr>
                      <tr>
                        <th>Entry Count</th>
                        <td>${this.adData.entry_count}</td>
                      </tr>
                      <tr>
                          <th>CID Count</th>
                          <td>${this.adData.cid_count}</td>
                      </tr>
                      <tr>
                        <th>Piece CID</th>
                        <td><a href="/pages/piece/?id=${this.adData.piece_cid}">${this.adData.piece_cid}</a></td>
                      </tr>
                      <tr>
                        <th>Piece Size</th>
                        <td>${this.adData.piece_size || 'N/A'}</td>
                      </tr>
                    </table>
                  </div>

                  ${this.showEntryGrid
                ? html`
                      <entry-grid
                              .entriesHead="${this.adData.entries}"
                              .entryCount="${this.adData.entry_count}"
                      ></entry-grid>
                        `
                : ''}
                `
            : ''}
          </div>
        `;
    }

    static styles = css`
        .search-container {
          display: grid;
          grid-template-columns: 1fr max-content;
          grid-column-gap: 0.75rem;
          margin-bottom: 1rem;
        }
        
        .btn {
          padding: 0.4rem 1rem;
          border: none;
          border-radius: 0;
          background-color: var(--color-form-default);
          color: var(--color-text-primary);
        }
        
        .btn-danger {
          background-color: var(--color-danger-main);
        }
    
        .btn:hover,
        .btn:focus,
        .btn:focus-visible {
          background-color: var(--color-form-default-pressed);
          color: var(--color-text-secondary);
        }
      `;
}

customElements.define('ipni-search', IpniSearch);
