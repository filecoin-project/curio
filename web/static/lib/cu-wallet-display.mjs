import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import '/lib/clipboard-copy.mjs'; // Ensure clipboard-copy is available

class CuWalletDisplay extends LitElement {
  static properties = {
    wallet_id: { type: String },
    name: { type: String },
    havename: { type: Boolean }
  };

  constructor() {
    super();
    this.wallet_id = '';
    this.name = '';
    this.havename = false;
  }

  _handleMouseEnter() {
    this.dispatchEvent(new CustomEvent('componentmouseenter', { bubbles: true, composed: true }));
  }

  _handleMouseLeave() {
    this.dispatchEvent(new CustomEvent('componentmouseleave', { bubbles: true, composed: true }));
  }

  createRenderRoot() {
    // Render in light DOM for easier styling from parent
    return this;
  }

  render() {
    let content;
    if (!this.havename || !this.name || this.name === this.wallet_id) {
      const shortened = this.wallet_id ? `${this.wallet_id.slice(0, 6)}...${this.wallet_id.slice(-6)}` : 'No ID';
      content = html`
        <span title="${this.wallet_id}">${shortened}</span>
        ${this.wallet_id ? html`<clipboard-copy .text=${this.wallet_id}></clipboard-copy>` : ''}
      `;
    } else {
      content = html`
        <a href="/pages/wallet/?id=${this.wallet_id}" title="${this.wallet_id}">${this.name}</a>
      `;
    }
    return html`
      <span @mouseenter=${this._handleMouseEnter} @mouseleave=${this._handleMouseLeave}>
        ${content}
      </span>
    `;
  }
}

customElements.define('cu-wallet-display', CuWalletDisplay); 