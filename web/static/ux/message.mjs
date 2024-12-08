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
        const gas = md.GasLimit || '';
        const status = msg.executed_rcpt_exitcode !== null ? html`Executed(<abbr title="Exit Code">E:${msg.executed_rcpt_exitcode}</abbr>${msg.executed_rcpt_exitcode===0?' ✅':' ❌'})` : 'Pending';
        const epoch = msg.executed_tsk_epoch || '';

        return html`
          <div>
            <div><strong>MSG:</strong>${this.cid}</div>
            <div>
                <abbr title=${from}>${this.formatAddr(from)}</abbr>→${to} M${method} <abbr title="Value">${value}</abbr> <abbr title="Max Fee">F:${fee}</abbr> G:${gas} ${status}${epoch ? ' @' + epoch : ''}
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
