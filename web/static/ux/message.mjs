import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/lib/cu-wallet.mjs';
import '/ux/epoch.mjs';

class FilMessage extends LitElement {
    static properties = {
        cid: { type: String },
        message: { type: Object },
        // Info-box state
        _showInfoBox: { state: true }
    };

    constructor() {
        super();
        this.message = null;
        // Info-box state handling
        this._showInfoBox = false;
        this._mouseOverComponent = false;
        this._mouseOverInfoBox = false;
        this._hideTimeout = null;
    }

    updated(changedProperties) {
        if (changedProperties.has('cid')) {
            this.loadMessage();
        }
    }

    async loadMessage() {
        if (this.cid) {
            let message = await RPCCall('MessageByCid', [this.cid]);
            this.message = message;
            // Fetch pretty epoch string if available
            if (message && message.executed_tsk_epoch !== null && message.executed_tsk_epoch !== undefined) {
                try {
                    this._epochPretty = await RPCCall('EpochPretty', [message.executed_tsk_epoch]);
                } catch (err) {
                    console.error('Failed to fetch pretty epoch:', err);
                    this._epochPretty = message.executed_tsk_epoch?.toString() || '';
                }
            } else {
                this._epochPretty = '';
            }
            this.requestUpdate();
        }
    }

    // Hover helpers
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
        }, 100);
    }

    render() {
        if (!this.message) {
            return html`<p>Loading...</p>`;
        }

        const msg = this.message;
        const from = msg.from_key || msg.FromKey;
        const to = msg.to_addr || msg.ToAddr;
        const md = msg.signed_json.Message || {};
        const method = md.Method || '';
        const value = msg.value_str || '';
        const fee = msg.fee_str || '';
        const gasLimitValue = md.GasLimit;
        const gas = (typeof gasLimitValue === 'number' && gasLimitValue > 1000000)
            ? `${Math.round(gasLimitValue / 1000000)}M`
            : (gasLimitValue || '');
        const status = msg.executed_rcpt_exitcode !== null
            ? (msg.executed_rcpt_exitcode === 0
                ? html`<span style="color: var(--color-success-main);">Executed</span>(<abbr title="Exit Code">E:${msg.executed_rcpt_exitcode}</abbr> ✅)`
                : html`<span style="color: var(--color-danger-main);">Executed</span>(<abbr title="Exit Code">E:${msg.executed_rcpt_exitcode}</abbr> ❌)`)
            : html`<span style="color: var(--color-info-main);">Pending</span>`;
        const epoch = msg.executed_tsk_epoch || '';

        return html`
          <style>
            .fil-message-parent-container {
              position: relative;
              display: inline-block;
              white-space: nowrap;
            }
            .message-info-box-details {
              position: absolute;
              top: 100%;
              left: 0;
              background-color: var(--color-form-group-1);
              border: 1px solid #ccc;
              padding: 10px;
              z-index: 1000;
              min-width: 450px;
              box-shadow: 0 2px 5px rgba(0,0,0,0.15);
              font-size: 0.9em;
            }
            .message-info-box-details p {
              margin: 4px 0;
            }
            .message-info-box-details .label {
              font-weight: bold;
            }
          </style>
          <div class="fil-message-parent-container">
            <span @mouseenter=${this._handleComponentMouseEnter} @mouseleave=${this._handleComponentMouseLeave}>
              <div style="white-space: nowrap;"><strong>MSG:</strong>${this.cid}</div>
              <div style="white-space: nowrap;">
                  <span style="font-size: 0.8em;">
                    <abbr title=${from}>${this.formatAddr(from)}</abbr>→<abbr title=${to}>${this.formatAddr(to)}</abbr>
                    M${method}
                    <abbr title="Value"><strong>${value}</strong></abbr>
                    <span style="color: var(--color-form-default);"><abbr title="Max Fee">F:${fee}</abbr></span>
                    <abbr title="Gas">G:${gas}</abbr>
                    ${status}${epoch ? ' @' + epoch : ''}
                  </span>
              </div>
            </span>
            ${this._showInfoBox ? html`
              <div class="message-info-box-details" @mouseenter=${this._handleInfoBoxMouseEnter} @mouseleave=${this._handleInfoBoxMouseLeave}>
                <p><span class="label">From:</span> <cu-wallet wallet_id="${from}"></cu-wallet></p>
                <p><span class="label">To:</span> <cu-wallet wallet_id="${to}"></cu-wallet></p>
                <p><span class="label">FilFox:</span> <a href="https://filfox.info/en/message/${this.cid}" target="_blank">${this.cid}</a></p>
                ${epoch ? html`<p><span class="label">Time:</span> <pretty-epoch .epoch=${epoch}></pretty-epoch></p>` : ''}
              </div>
            ` : ''}
          </div>
        `;
    }

    formatAddr(a) {
        if (a.length <= 9) {
            return a;
        }
        const start = a.substring(0, 4);
        const end = a.substring(a.length - 5);
        return `${start}..${end}`;
    }
}

customElements.define('fil-message', FilMessage);
