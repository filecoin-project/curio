import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class FilMessage extends LitElement {
    static properties = {
        cid: { type: String },
        message: { type: Object }
    };

    constructor() {
        super();
        this.message = null;
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
            this.requestUpdate();
        }
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
          <div>
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
