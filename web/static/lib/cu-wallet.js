import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class CuWallet extends LitElement {
  static get properties() {
    return {
      wallet_id: { type: String }
    };
  }

  constructor() {
    super();
    this.wallet_id = '';
  }

  updated(changedProperties) {
    if (changedProperties.has('wallet_id') && this.wallet_id) {
      this.handleIdUpdate();
    }
  }

  async handleIdUpdate() {
    this.isProcessing = true;
    try {
        this.name = await RPCCall('WalletName', [this.wallet_id]);
    } catch (error) {
        console.error('Error during WalletName operation:', error);
    } finally {
        this.isProcessing = false;
    }
    this.requestUpdate();
  }
  
  nice_id() {
    if (!this.wallet_id) return 'Not Set';
    if (this.name != '') return this.name;
    return this.wallet_id.substring(0, 6) + '...' + this.wallet_id.substring(this.wallet_id.length - 6);
  }
  static styles = css`
    @keyframes spinner {
      to {transform: rotate(360deg);}
    }
    .spinner:before {
      content: url('/favicon.svg');
      display: inline-block;      
      animation: spinner 1s linear infinite;
      height: 12px;
    }
  `;

  render() {
    return html`
      <div class="wallet">
        ${this.isProcessing ? html`<span class="spinner"></span>` : ''}
        <a href="/pages/wallet/?id=${this.wallet_id}">📛</a>
        <span>${this.nice_id()}</span>
        <a @click=${() => navigator.clipboard.writeText(this.wallet_id)}>📋</a>
      </div>
    `;
  }
}

customElements.define('cu-wallet', CuWallet);
