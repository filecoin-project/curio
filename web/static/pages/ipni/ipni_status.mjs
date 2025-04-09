import { html, css, LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class IpniStatus extends LitElement {
    static properties = {
        ipniData: { type: Array },
        errorMessage: { type: String },
        expandedProviders: { type: Object },
    };

    constructor() {
        super();
        this.ipniData = [];
        this.errorMessage = '';
        this.expandedProviders = {};
    }

    connectedCallback() {
        super.connectedCallback();
        this.loadData();
    }

    async loadData() {
        try {
            this.ipniData = await RPCCall('IPNISummary', []);
        } catch (error) {
            console.error('Failed to load IPNI data:', error);
            this.errorMessage = 'Failed to load IPNI data. Please try again later.';
        }
    }

    toggleProvider(providerId) {
        this.expandedProviders = {
            ...this.expandedProviders,
            [providerId]: !this.expandedProviders[providerId],
        };
    }

    render() {
        return html`
      <!-- Bootstrap CSS -->
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        crossorigin="anonymous"
      />
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
      
      <style>
        .provider-row {
          cursor: pointer;
        }
        .provider-info {
          font-weight: bold;
        }
        .sync-status {
          margin-left: 20px;
        }
        .sync-status-details {
          font-size: 0.9em;
        }
        .icon {
          margin-right: 5px;
        }
        .accordion-button {
            background-color: var(--color-nav-button);
            color: var(--color-text-primary);
        }
        .accordion-button:not(.collapsed) {
            background-color: var(--color-nav-button);
            color: var(--color-text-primary);
        }
        .accordion-item {
            background-color: transparent;
            margin-top: 20px; 
            margin-bottom: 20px;
        }
        hr {
            border-color: #495057;
        }
      </style>

      <div>
        <h2 style="white-space: nowrap">Provider Status</h2>
        ${this.errorMessage
            ? html`<div class="alert alert-danger">${this.errorMessage}</div>`
            : html`
              <div class="accordion" id="ipniStatusAccordion">
                ${this.ipniData.map(
                (provider, index) => html`
                    <div class="accordion-item">
                      <h2 class="accordion-header" id="heading${index}">
                        <button
                          class="accordion-button ${!this.expandedProviders[provider.miner] ? '' : 'collapsed'}"
                          type="button"
                          @click="${() => this.toggleProvider(provider.miner)}"
                        >
                          <span class="provider-info">
                            Provider: ${provider.miner}
                          </span>
                        </button>
                      </h2>
                      <div
                        id="collapse${index}"
                        class="accordion-collapse collapse ${!this.expandedProviders[provider.miner] ? 'show' : ''}"
                        aria-labelledby="heading${index}"
                        data-bs-parent="#ipniStatusAccordion"
                      >
                        <div class="accordion-body">
                            <p><strong>PeerID:</strong>${provider.peer_id}></p>
                          <p><strong>Head:</strong> <a href="/pages/ipni/?ad_cid=${provider.head}">${provider.head}</a></p>
                          ${provider.sync_status && provider.sync_status.length > 0 ? provider.sync_status.map((status) => html`
                                  <div class="sync-status">
                                    <p><strong>Service:</strong> ${status.service}</p>
                                    <div class="sync-status-details">
                                      <p><strong>Remote Ad:</strong> ${status.remote_ad}</p>
                                      <p><strong>Publisher Address:</strong> ${status.publisher_address}</p>
                                      <p><strong>Address:</strong> ${status.address}</p>
                                      <p><strong>Last Advertisement Time:</strong>
                                        ${status.last_advertisement_time
                            ? new Date(
                                status.last_advertisement_time
                            ).toLocaleString()
                            : 'N/A'}
                                      </p>
                                      <p><strong>Error:</strong> ${status.error || 'N/A'}</p>
                                    </div>
                                  </div>
                                  <hr />
                                `
                    )
                    : html`<p>No Sync Status Available</p>`}
                        </div>
                      </div>
                    </div>
                  `
            )}
              </div>
            `}
      </div>
    `;
    }
}

customElements.define('ipni-status', IpniStatus);
