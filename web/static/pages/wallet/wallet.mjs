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
        let res = await RPCCall('WalletName', [this.wallet_id]); // TODO API call
        this.name = res.name;
        this.requestUpdate();
    } catch (error) {
        console.error('Error during WalletName operation:', error);
    } finally {
        this.isProcessing = false;
    }
  }
  
  nice_id() {
    if (!this.wallet_id) return 'Not Set';
    if (this.name != '') return this.name;
    return this.wallet_id.substring(0, 6) + '...' + this.wallet_id.substring(this.wallet_id.length - 6);
  }
  async handleNameChange(new_nice_id) {
    this.isProcessing = true;
    this.name = new_nice_id;
    this.requestUpdate();
    try {
      await RPCCall('WalletNameChange', [this.wallet_id, new_nice_id]); // TODO API call
    } catch (error) {
        console.error('Error during WalletName operation:', error);
    } finally {
        this.isProcessing = false;
    }  
    this.requestUpdate();
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
        Full ID: <span>${this.wallet_id}</span><br>
        <form @submit=${(e) => {
          e.preventDefault()
          this.handleNameChange(e.target.walletName.value)
        }}>
          <input type="text" name="walletName" .value=${this.nice_id()}>
          <button type="submit">Change Name</button>
        </form>
      </div>    `;
  }
}

customElements.define('wallet-component', CuWallet);
