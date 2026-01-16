import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import '/ux/epoch.mjs';

class CompactPrettyEpoch extends LitElement {
  static properties = {
    epoch: { type: Number },
    _showInfoBox: { state: true }
  };

  constructor() {
    super();
    this.epoch = null;
    this._showInfoBox = false;
    this._mouseOverComponent = false;
    this._mouseOverInfoBox = false;
    this._hideTimeout = null;
  }

  createRenderRoot() {
    return this; // light DOM for easier styling
  }

  _handleComponentMouseEnter(e) {
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
    }, 100);
  }

  render() {
    const epochStr = this.epoch !== null && this.epoch !== undefined ? this.epoch : '--';

    return html`
      <style>
        .epoch-parent {
          position: relative;
          display: inline-block;
        }
        .epoch-info-box {
          position: absolute;
          top: 100%;
          left: 0;
          background-color: var(--color-form-group-1, #1a2a3a);
          border: 1px solid #555;
          padding: 6px 8px;
          z-index: 10000;
          white-space: nowrap;
          box-shadow: 0 4px 12px rgba(0,0,0,0.5);
          font-size: 0.9em;
        }
      </style>
      <span class="epoch-parent">
        <span @mouseenter=${this._handleComponentMouseEnter} @mouseleave=${this._handleComponentMouseLeave}>${epochStr}</span>
        ${this._showInfoBox && this.epoch !== null && this.epoch !== undefined ? html`
          <div class="epoch-info-box" @mouseenter=${this._handleInfoBoxMouseEnter} @mouseleave=${this._handleInfoBoxMouseLeave}>
            <pretty-epoch .epoch=${this.epoch}></pretty-epoch>
          </div>` : ''}
      </span>
    `;
  }
}

customElements.define('compact-pretty-epoch', CompactPrettyEpoch); 