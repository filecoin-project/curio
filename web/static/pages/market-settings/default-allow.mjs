import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class DefaultAllow extends LitElement {
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
            this.data = await RPCCall('DefaultAllowBehaviour', []);
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
                <h4 class=${this.data ? 'text-success' : 'text-danger'}>
                    Deals from unknown clients are
                    ${this.data ? 'Allowed' : 'Denied'}
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

customElements.define('default-allow', DefaultAllow);
