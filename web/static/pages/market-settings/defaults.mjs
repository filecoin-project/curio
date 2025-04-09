import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/yesno.mjs';

class DefaultMarketFilters extends LitElement {
    static properties = {
        data: { type: Object },
    };

    constructor() {
        super();
        this.data = null;
        this.loadData();
    }

    async loadData() {
        try {
            this.data = await RPCCall('DefaultFilterBehaviour', []);
        } catch (error) {
            alert(`Failed to load default allow behavior: ${error.message}`);
            console.error('Failed to load default allow behavior:', error);
        }
    }

    render() {
        if (!this.data) {
            return html`<div>Loading...</div>`;
        }

        return html`
            <link
                    href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                    rel="stylesheet"
                    crossorigin="anonymous"
            />
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'" />
            <div class="container">
                <h2>Filter Settings</h2>
            <table class="table table-dark table-striped">
                <thead>
                    <tr>
                        <th>Parameter</th>
                        <th>Status</th>
                    </tr>
                </thead>
                <tbody>
                    <tr>
                        <td>Unknown client allowed</td>
                        <td><yes-no .value=${this.data.allow_deals_from_unknown_clients}></yes-no></td>
                    </tr>
                    <tr>
                        <td>Deal Rejected When CIDGravity unavailable</td>
                        <td><yes-no .value=${this.data.is_deal_rejected_when_cid_gravity_not_reachable}></yes-no></td>
                    </tr>
                    ${Object.entries(this.data.cid_gravity_status).map(([miner, status]) => html`
                        <tr>
                            <td>${miner} CIDGravity Enabled</td>
                            <td><yes-no .value=${status}></yes-no></td>
                        </tr>
                    `)}
                </tbody>
            </table>
            </div>
        `;
    }
}

customElements.define('default-market-filters', DefaultMarketFilters);
