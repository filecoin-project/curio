import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class DefaultMarketFilters extends LitElement {
    static properties = {
        data: { type: Boolean },
    };

    constructor() {
        super();
        this.data = null; // Initialize data to null
        this.loadData();
    }

    async loadData() {
        try {
            // Load default allow behavior using the correct RPC method name
            this.data = await RPCCall('DefaultFilterBehaviour', []);
        } catch (error) {
            console.error('Failed to load default allow behavior:', error);
        }
    }

    render() {
        if (this.data === null) {
            return html`<div>Loading...</div>`;
        }
        return html`
            <div>
                <h4 class=${this.data.allow_deals_from_unknown_clients ? 'text-success' : 'text-danger'}>
                    Deals from unknown clients are
                    ${this.data.allow_deals_from_unknown_clients ? 'Allowed' : 'Denied'}
                </h4>
                <h4 class=${this.data.is_cid_gravity_enabled ? 'text-success' : 'text-danger'}>
                    CID Gravity is
                    ${this.data.is_cid_gravity_enabled ? 'Enabled' : 'Disabled'}
                </h4>
                <h4 class=${this.data.is_deal_rejected_when_cid_gravity_not_reachable ? 'text-success' : 'text-danger'}>
                    When CID Gravity is not reachable, deals are
                    ${this.data.is_deal_rejected_when_cid_gravity_not_reachable ? 'Rejected' : 'Accepted'}
                </h4>
            </div>
        `;
    }

    static styles = css`
        /* Include any component-specific styles here */
        .text-success {
            color: #28a745;
        }
        .text-danger {
            color: #dc3545;
        }
    `;
}

customElements.define('default-market-filters', DefaultMarketFilters);
