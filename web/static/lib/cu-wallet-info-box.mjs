import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class CuWalletInfoBox extends LitElement {
  static properties = {
    wallet_id: { type: String },
    active: { type: Boolean, reflect: true }, // Controls visibility and triggers data loading
    _walletInfo: { state: true },
    _isLoadingInfo: { state: true },
    _refreshInterval: { state: true },
  };

  constructor() {
    super();
    this.wallet_id = '';
    this.active = false;
    this._walletInfo = null;
    this._isLoadingInfo = false;
    this._refreshInterval = null;
  }

  connectedCallback() {
    super.connectedCallback();
    if (this.active) {
      this._startRefreshInterval();
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this._stopRefreshInterval();
  }

  updated(changedProperties) {
    if (changedProperties.has('active')) {
      if (this.active) {
        if (!this._walletInfo || 
            (this._walletInfo.id_address !== this.wallet_id && !this._walletInfo.error) ||
            (this._walletInfo.error)
           ) {
           this._loadWalletDetails();
        }
        this._startRefreshInterval();
      } else {
        this._stopRefreshInterval();
      }
    } else if (changedProperties.has('wallet_id') && this.active) {
        this._loadWalletDetails();
        this._startRefreshInterval(); 
    }
  }

  _startRefreshInterval() {
    this._stopRefreshInterval();
    if (this.active && this.wallet_id) {
      this._refreshInterval = setInterval(() => {
        console.log(`Refreshing wallet details for: ${this.wallet_id}`);
        this._loadWalletDetails();
      }, 10000);
    }
  }

  _stopRefreshInterval() {
    if (this._refreshInterval) {
      clearInterval(this._refreshInterval);
      this._refreshInterval = null;
      console.log(`Stopped wallet details refresh for: ${this.wallet_id || 'N/A'}`);
    }
  }

  async _loadWalletDetails() {
    if (!this.wallet_id) {
      this._walletInfo = { error: 'No wallet ID provided for info box.' };
      return;
    }

    this._isLoadingInfo = true;
    this._walletInfo = null;

    try {
      const result = await RPCCall('WalletInfoShort', [this.wallet_id]);
      console.log('WalletInfoShort result for info-box:', result);
      this._walletInfo = result;
    } catch (err) {
      console.error('Error during WalletInfoShort operation in info-box:', err);
      this._walletInfo = { error: 'Failed to load details.' };
    } finally {
      this._isLoadingInfo = false;
    }
  }

  _handleMouseEnter() {
    this.dispatchEvent(new CustomEvent('infoboxmouseenter', { bubbles: true, composed: true }));
  }

  _handleMouseLeave() {
    this.dispatchEvent(new CustomEvent('infoboxmouseleave', { bubbles: true, composed: true }));
  }

  createRenderRoot() {
    return this; // Light DOM
  }

  render() {
    if (!this.active) {
      return html``;
    }

    return html`
      <style>
        .wallet-info-box-details {
          position: absolute;
          top: 100%;
          left: 0;
          background-color: var(--color-form-group-1, #1a2a3a);
          border: 1px solid #555;
          padding: 10px;
          z-index: 10000; 
          min-width: 400px;
          max-width: 550px;
          box-shadow: 0 4px 12px rgba(0,0,0,0.5);
          font-size: 0.9em;
        }
        .wallet-info-box-details p {
          margin: 5px 0;
          word-break: break-all;
        }
        .wallet-info-box-details .label {
          font-weight: bold;
        }
      </style>
      <div 
        class="wallet-info-box-details"
        @mouseenter=${this._handleMouseEnter}
        @mouseleave=${this._handleMouseLeave}
      >
        ${this._isLoadingInfo ? html`<p>Loading details...</p>` : ''}
        ${!this._isLoadingInfo && this._walletInfo ? html`
          ${this._walletInfo.error ? html`<p style="color: red;">${this._walletInfo.error}</p>` : html`
            <p><span class="label">ID Address:</span> ${this._walletInfo.id_address}</p>
            <p><span class="label">Key Address:</span> ${this._walletInfo.key_address}</p>
            <p><span class="label">Balance:</span> ${this._walletInfo.balance}</p>
            <p><span class="label">Pending Msgs:</span> ${this._walletInfo.pending_messages}</p>
            <p><span class="label">FilFox:</span> <a href="https://filfox.info/en/address/${this._walletInfo.id_address}" target="_blank">${this._walletInfo.id_address}</a></p>
          `}
        ` : ''}
        ${!this._isLoadingInfo && !this._walletInfo && !this.wallet_id && this.active ? html`<p>No wallet ID provided.</p>` : ''}
      </div>
    `;
  }
}

customElements.define('cu-wallet-info-box', CuWalletInfoBox); 