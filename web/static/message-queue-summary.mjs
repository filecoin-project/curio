import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'
import RPCCall from '/lib/jsonrpc.mjs'
import { pollRPC } from '/lib/poll.mjs'
import { timeSince } from '/lib/dateutil.mjs'

customElements.define('message-queue-summary', class MessageQueueSummary extends LitElement {
  static properties = {
    data: { type: Object },
  }

  constructor() {
    super()
    this.data = null
    pollRPC(async () => {
      this.data = await RPCCall('MessageQueueSummary')
    }, 15000)
  }

  static styles = css`
    .counts {
      display: flex;
      gap: 24px;
      margin-bottom: 16px;
      flex-wrap: wrap;
    }
    .count-item {
      display: flex;
      flex-direction: column;
      gap: 4px;
    }
    .count-value {
      font-size: 24px;
      font-weight: 500;
      color: var(--color-text-primary, #e6edf3);
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    }
    .count-label {
      font-size: 12px;
      color: var(--color-text-secondary, #8b949e);
    }
    table {
      width: 100%;
    }
    .mono {
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 12px;
      word-break: break-all;
    }
    .empty {
      color: var(--color-text-secondary, #8b949e);
      font-size: 13px;
    }
    h3 {
      font-size: 14px;
      font-weight: 600;
      margin: 16px 0 8px;
      color: var(--color-text-primary, #e6edf3);
    }
  `

  renderTable(rows, columns) {
    if (!rows?.length) {
      return html`<p class="empty">None waiting</p>`
    }
    return html`
      <table class="table table-dark table-striped">
        <thead><tr>${columns.map((c) => html`<th>${c.label}</th>`)}</tr></thead>
        <tbody>
          ${rows.map((row) => html`
            <tr>${columns.map((c) => html`<td class="${c.mono ? 'mono' : ''}">${c.render(row)}</td>`)}</tr>
          `)}
        </tbody>
      </table>
    `
  }

  render() {
    const d = this.data
    return html`
      <div class="counts">
        <div class="count-item">
          <span class="count-value">${d?.filPendingCount ?? '…'}</span>
          <span class="count-label">FIL messages queued</span>
        </div>
        <div class="count-item">
          <span class="count-value">${d?.ethPendingCount ?? '…'}</span>
          <span class="count-label">ETH txs awaiting receipt</span>
        </div>
      </div>

      <h3>FIL messages waiting for receipt</h3>
      ${this.renderTable(d?.filPending, [
        { label: 'CID', mono: true, render: (r) => r.messageCid },
        { label: 'Queued', render: (r) => timeSince(r.addedAt) },
      ])}

      <h3>ETH transactions pending confirmation</h3>
      ${this.renderTable(d?.ethPending, [
        { label: 'Tx hash', mono: true, render: (r) => r.txHash },
        { label: 'Reason', render: (r) => r.sendReason || '—' },
        { label: 'Sent', render: (r) => r.sendTime ? timeSince(r.sendTime) : '—' },
      ])}
    `
  }
})
