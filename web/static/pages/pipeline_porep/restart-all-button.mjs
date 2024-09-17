import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class RestartAllButton extends LitElement {
    static properties = {
        isProcessing: { type: Boolean },
    };

    constructor() {
        super();
        this.isProcessing = false;
    }

    render() {
        return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
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
            await RPCCall('PipelinePorepRestartAll', []);
            console.log('Resume All operation completed successfully');
        } catch (error) {
            console.error('Error during Resume All operation:', error);
        } finally {
            this.isProcessing = false;
        }
    }
}

customElements.define('restart-all-button', RestartAllButton);
