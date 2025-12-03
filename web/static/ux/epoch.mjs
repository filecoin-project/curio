import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class EpochComponent extends LitElement {
    static properties = {
        epoch: { type: Number },
        output: { type: String }
    };

    constructor() {
        super();
        this.epoch = null;
        this.output = '';
        this._updateTimeout = null;
    }

    connectedCallback() {
        super.connectedCallback();
        this.loadEpochInfo(); // Initial load
        this.scheduleNextUpdate();
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this._updateTimeout) {
            clearTimeout(this._updateTimeout);
            this._updateTimeout = null;
        }
    }

    scheduleNextUpdate() {
        this._updateTimeout = setTimeout(() => {
            this.loadEpochInfo();
            this.scheduleNextUpdate();
        }, 15000);
    }

    async loadEpochInfo() {
        if (this.epoch !== null) {
            try {
                const result = await RPCCall('EpochPretty', [this.epoch]);
                this.output = result;
            } catch (error) {
                console.error('Error fetching epoch info:', error);
                this.output = 'Error fetching epoch info';
            }
            this.requestUpdate();
        }
    }

    render() {
        return html`
            <span>${this.output}</span>
        `;
    }
}

customElements.define('pretty-epoch', EpochComponent);
