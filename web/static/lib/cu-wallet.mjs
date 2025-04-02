import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/lib/clipboard-copy.mjs';

class CuWallet extends LitElement {
  static properties = {
    wallet_id: { type: String },
    name: { state: true },
    havername: { state: true }
  };

  constructor() {
    super();
    this.wallet_id = '';
    this.name = '';
    this.havename = false;
  }

  connectedCallback() {
    super.connectedCallback();
    this.loadWallet();
  }

  async loadWallet() {
    if (!this.wallet_id) return;

    try {
      const result = await RPCCall('WalletName', [this.wallet_id]);
      console.log('WalletName result:', result);
      this.name = result || this.wallet_id;
      this.havename = (this.name !== this.wallet_id);
    } catch (err) {
      console.error('Error during WalletName operation:', err);
      this.name = this.wallet_id; // fallback
    }
  }

  createRenderRoot() {
    // Render in light DOM so the text can be styled normally and picked up by parent CSS
    return this;
  }

  render() {
    if (!this.havename){
      const shortened = `${this.wallet_id.slice(0, 6)}...${this.wallet_id.slice(-6)}`;
      return html`
        <span title="${this.wallet_id}">${shortened}</span>
        <clipboard-copy .text=${this.wallet_id}></clipboard-copy>
      </span>
    `;
    }
    return html`
      <a href="/pages/wallet/?id=${this.wallet_id}"><span title="${this.wallet_id}">${this.name}</span></a>
    `;
  }
}

customElements.define('cu-wallet', CuWallet);
