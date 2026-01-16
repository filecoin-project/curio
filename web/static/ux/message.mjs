import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/lib/cu-wallet.mjs';
import '/ux/epoch.mjs';

class FilMessage extends LitElement {
    static properties = {
        cid: { type: String },
        message: { type: Object },
        // Info-box state
        _showInfoBox: { state: true },
        _loading: { state: true },
        _notFound: { state: true }
    };

    static styles = css`
        :host {
            display: inline-block;
            position: relative;
        }
        .fil-message-parent-container {
            display: inline-block;
            white-space: nowrap;
        }
        .message-info-box-details {
            position: absolute;
            top: 100%;
            left: 0;
            background-color: var(--color-form-group-1, #1a2a3a);
            border: 1px solid #555;
            padding: 10px;
            z-index: 10000;
            min-width: 350px;
            max-width: 550px;
            box-shadow: 0 4px 12px rgba(0,0,0,0.5);
            font-size: 0.9em;
        }
        .message-info-box-details.wide {
            min-width: 450px;
        }
        .message-info-box-details p {
            margin: 4px 0;
            word-break: break-all;
        }
        .message-info-box-details .label {
            font-weight: bold;
        }
    `;

    constructor() {
        super();
        this.message = null;
        this._loading = true;
        this._notFound = false;
        // Info-box state handling
        this._showInfoBox = false;
        this._mouseOverComponent = false;
        this._mouseOverInfoBox = false;
        this._hideTimeout = null;
    }

    async loadMessage() {
        if (this.cid) {
            try {
                let message = await RPCCall('MessageByCid', [this.cid]);
                this.message = message;
                this._notFound = (message === null);
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
            } catch (err) {
                console.error('Failed to load message:', err);
                this._notFound = true;
            }
            this._loading = false;
            this.requestUpdate();
        }
    }

    // Hover helpers
    _handleComponentMouseEnter(e) {
        this._mouseOverComponent = true;
        if (this._hideTimeout) clearTimeout(this._hideTimeout);
        this._showInfoBox = true;
    }

    updated(changedProperties) {
        if (changedProperties.has('cid') && this.cid) {
            this._loading = true;
            this._notFound = false;
            this.loadMessage();
        }
    }

    connectedCallback() {
        super.connectedCallback();
        if (this.cid) {
            this.loadMessage();
        }
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
        if (this._loading) {
            return html`<span style="color: #888;">Loading ${this.cid ? this.formatCid(this.cid) : ''}...</span>`;
        }

        // Message not found in DB - show best-effort display with just the CID
        if (this._notFound || !this.message) {
            return html`
              <div class="fil-message-parent-container">
                <span @mouseenter=${this._handleComponentMouseEnter} @mouseleave=${this._handleComponentMouseLeave}>
                  <div>
                    <strong>MSG:</strong>${this.formatCid(this.cid)}
                    <span style="color: #888; font-size: 0.85em;">(not in local DB)</span>
                  </div>
                </span>
                ${this._showInfoBox ? html`
                  <div class="message-info-box-details" @mouseenter=${this._handleInfoBoxMouseEnter} @mouseleave=${this._handleInfoBoxMouseLeave}>
                    <p><span class="label">CID:</span> ${this.cid}</p>
                    <p><span class="label">FilFox:</span> <a href="https://filfox.info/en/message/${this.cid}" target="_blank">View on FilFox</a></p>
                    <p style="color: #888; font-size: 0.9em;">Message details not available in local database. This may be an old message that was pruned or from a different node.</p>
                  </div>
                ` : ''}
              </div>
            `;
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
          <div class="fil-message-parent-container">
            <span @mouseenter=${this._handleComponentMouseEnter} @mouseleave=${this._handleComponentMouseLeave}>
              <div><strong>MSG:</strong>${this.formatCid(this.cid)}</div>
              <div>
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
              <div class="message-info-box-details wide" @mouseenter=${this._handleInfoBoxMouseEnter} @mouseleave=${this._handleInfoBoxMouseLeave}>
                <p><span class="label">CID:</span> ${this.cid}</p>
                <p><span class="label">From:</span> <cu-wallet wallet_id="${from}"></cu-wallet></p>
                <p><span class="label">To:</span> <cu-wallet wallet_id="${to}"></cu-wallet></p>
                <p><span class="label">FilFox:</span> <a href="https://filfox.info/en/message/${this.cid}" target="_blank">View on FilFox</a></p>
                ${epoch ? html`<p><span class="label">Time:</span> <pretty-epoch .epoch=${epoch}></pretty-epoch></p>` : ''}
              </div>
            ` : ''}
          </div>
        `;
    }

    formatAddr(a) {
        if (!a) return '';
        if (a.length <= 9) {
            return a;
        }
        const start = a.substring(0, 4);
        const end = a.substring(a.length - 5);
        return `${start}..${end}`;
    }

    formatCid(cid) {
        if (!cid) return '';
        if (cid.length <= 20) return cid;
        return `${cid.substring(0, 10)}...${cid.substring(cid.length - 8)}`;
    }
}

customElements.define('fil-message', FilMessage);
