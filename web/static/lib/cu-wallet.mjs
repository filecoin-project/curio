import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import './cu-wallet-display.mjs';
import './cu-wallet-info-box.mjs';

class CuWallet extends LitElement {
  static properties = {
    wallet_id: { type: String },
    _name: { state: true },
    _havename: { state: true },
    _showInfoBox: { state: true },
    _mouseOverComponent: { state: false }, // Internal tracking, not a reactive property
    _mouseOverInfoBox: { state: false }   // Internal tracking, not a reactive property
  };

  constructor() {
    super();
    this.wallet_id = '';
    this._name = '';
    this._havename = false;
    this._showInfoBox = false;
    this._mouseOverComponent = false;
    this._mouseOverInfoBox = false;
    this._hideTimeout = null;
  }

  connectedCallback() {
    super.connectedCallback();
    this._loadWalletName();
    this.addEventListener('componentmouseenter', this._handleComponentMouseEnter);
    this.addEventListener('componentmouseleave', this._handleComponentMouseLeave);
    this.addEventListener('infoboxmouseenter', this._handleInfoBoxMouseEnter);
    this.addEventListener('infoboxmouseleave', this._handleInfoBoxMouseLeave);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.removeEventListener('componentmouseenter', this._handleComponentMouseEnter);
    this.removeEventListener('componentmouseleave', this._handleComponentMouseLeave);
    this.removeEventListener('infoboxmouseenter', this._handleInfoBoxMouseEnter);
    this.removeEventListener('infoboxmouseleave', this._handleInfoBoxMouseLeave);
    if (this._hideTimeout) clearTimeout(this._hideTimeout);
  }

  async _loadWalletName() {
    if (!this.wallet_id) {
        this._name = this.wallet_id; // Fallback if no id
        this._havename = false;
        return;
    }

    try {
      const result = await RPCCall('WalletName', [this.wallet_id]);
      console.log('Parent CuWallet - WalletName result:', result);
      this._name = result || this.wallet_id;
      this._havename = (this._name !== this.wallet_id && !!result); // Ensure result is not empty for havename to be true
    } catch (err) {
      console.error('Parent CuWallet - Error during WalletName operation:', err);
      this._name = this.wallet_id; // fallback
      this._havename = false;
    }
  }

  _handleComponentMouseEnter() {
    this._mouseOverComponent = true;
    if (this._hideTimeout) clearTimeout(this._hideTimeout);
    this._showInfoBox = true;
  }

  _handleComponentMouseLeave() {
    this._mouseOverComponent = false;
    this._scheduleHideInfoBox();
  }

  _handleInfoBoxMouseEnter() {
    this._mouseOverInfoBox = true;
    if (this._hideTimeout) clearTimeout(this._hideTimeout);
  }

  _handleInfoBoxMouseLeave() {
    this._mouseOverInfoBox = false;
    this._scheduleHideInfoBox();
  }

  _scheduleHideInfoBox() {
    if (this._hideTimeout) clearTimeout(this._hideTimeout);
    this._hideTimeout = setTimeout(() => {
      if (!this._mouseOverComponent && !this._mouseOverInfoBox) {
        this._showInfoBox = false;
      }
    }, 100); // Small delay
  }

  createRenderRoot() {
    // Render in light DOM so the text can be styled normally and picked up by parent CSS
    return this;
  }

  render() {
    return html`
      <style>
        .cu-wallet-parent-container {
          position: relative; /* Needed for absolute positioning of info-box */
          display: inline-block;
          white-space: nowrap;
        }
      </style>
      <div class="cu-wallet-parent-container">
        <cu-wallet-display
          .wallet_id=${this.wallet_id}
          .name=${this._name}
          .havename=${this._havename}
        ></cu-wallet-display>
        <cu-wallet-info-box
          .wallet_id=${this.wallet_id}
          .active=${this._showInfoBox}
        ></cu-wallet-info-box>
      </div>
    `;
  }
}

customElements.define('cu-wallet', CuWallet);
