import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class RestartAllSnapButton extends LitElement {
    static properties = {
        isProcessing: { type: Boolean },
    };

    constructor() {
        super();
        this.isProcessing = false;
    }

    render() {
        return html`
      <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
      <button
        @click="${this.handleClick}"
        class="btn ${this.isProcessing ? 'btn-secondary' : 'btn-primary'}"
        ?disabled="${this.isProcessing}"
      >
        ${this.isProcessing ? 'Processing...' : 'Resume All'}
      </button>
    `;
    }

    async handleClick() {
        this.isProcessing = true;
        try {
            await RPCCall('PipelineSnapRestartAll', []);
            console.log('Resume All operation completed successfully');
        } catch (error) {
            console.error('Error during Resume All operation:', error);
        } finally {
            this.isProcessing = false;
        }
    }
}

customElements.define('restart-all-snap-button', RestartAllSnapButton);
