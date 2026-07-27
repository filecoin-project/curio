import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'
import RPCCall from '/lib/jsonrpc.mjs'
import { pollRPC } from '/lib/poll.mjs'

customElements.define('chain-status', class ChainStatus extends LitElement {
  static properties = {
    data: { type: Object },
    layout: { type: String, reflect: true },
  }

  constructor() {
    super()
    this.data = null
    this.layout = 'strip'
    pollRPC(async () => {
      this.data = await RPCCall('ChainStatus')
    }, 15000)
  }

  static styles = css`
    :host {
      display: block;
    }
    .strip {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 16px 24px;
      padding: 12px 16px;
      background: var(--color-bg-subtle, #161b22);
      border: 1px solid var(--color-border-default, #30363d);
      border-radius: var(--radius-lg, 8px);
    }
    .label {
      font-size: 11px;
      font-weight: 600;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: var(--color-text-secondary, #8b949e);
    }
    .value {
      font-size: 14px;
      font-weight: 500;
      color: var(--color-text-primary, #e6edf3);
    }
    .mono {
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    }
    .status-dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      flex-shrink: 0;
      display: inline-block;
    }
    .status-dot.ok { background: var(--color-success-fg, #3fb950); }
    .status-dot.warn { background: var(--color-warning-fg, #d29922); }
    .status-dot.bad { background: var(--color-danger-fg, #f85149); }
    .status-ok { color: var(--color-success-fg, #3fb950); }
    .status-warn { color: var(--color-warning-fg, #d29922); }
    .status-bad { color: var(--color-danger-fg, #f85149); }
    .detail-link {
      margin-left: auto;
      font-size: 12px;
      color: var(--color-accent-fg, #4493f8);
      text-decoration: none;
    }
    .detail-link:hover {
      text-decoration: underline;
    }
    .item {
      display: flex;
      flex-direction: column;
      gap: 2px;
      min-width: 72px;
    }

    /* Compact panel for dashboard column */
    .panel {
      padding: 10px 12px;
      background: var(--color-bg-subtle, #161b22);
      border: 1px solid var(--color-border-default, #30363d);
      border-radius: var(--radius-lg, 8px);
      box-sizing: border-box;
    }
    .panel-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      margin: 0 0 6px;
      padding-bottom: 4px;
      border-bottom: 1px solid var(--color-border-default, #30363d);
    }
    .panel-header h2 {
      margin: 0;
      font-size: 15px;
      font-weight: 600;
      color: var(--color-text-primary, #e6edf3);
    }
    .panel-rows {
      display: flex;
      flex-direction: column;
      gap: 4px;
    }
    .panel-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      font-size: 12px;
      line-height: 1.25;
    }
    .panel-row .label {
      font-size: 11px;
      font-weight: 600;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--color-text-secondary, #8b949e);
      flex-shrink: 0;
    }
    .panel-row .value {
      font-size: 13px;
      font-weight: 500;
      color: var(--color-text-primary, #e6edf3);
      text-align: right;
    }
    .panel-row .value.mono {
      font-size: 12px;
    }
    .sync-value {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      justify-content: flex-end;
    }
    .panel-header .detail-link {
      margin-left: 0;
      font-size: 12px;
    }
  `

  statusClass(status) {
    const s = String(status || '').toLowerCase()
    if (s === 'ok') return 'ok'
    if (s === 'slow' || s === 'degraded') return 'warn'
    return 'bad'
  }

  syncLabel(status) {
    const s = String(status || '').toLowerCase()
    if (s === 'ok') return ''
    return status ?? '…'
  }

  renderSync(status) {
    if (status === undefined || status === null) {
      return html`…`
    }
    const tone = this.statusClass(status)
    const label = this.syncLabel(status)
    return html`
      <span class="sync-value">
        <span class="status-dot ${tone}" title="${label || 'OK'}"></span>
        ${label ? html`<span class="status-${tone}">${label}</span>` : ''}
      </span>
    `
  }

  formatEpoch(epoch) {
    if (epoch === undefined || epoch === null) return '—'
    return Number(epoch).toLocaleString()
  }

  render() {
    const d = this.data
    if (this.layout === 'panel') {
      return html`
        <div class="panel">
          <div class="panel-header">
            <h2>Chain status</h2>
            <a class="detail-link" href="/pages/chain/">Details →</a>
          </div>
          <div class="panel-rows">
            <div class="panel-row">
              <span class="label">Network</span>
              <span class="value">${d?.networkName ?? '…'}</span>
            </div>
            <div class="panel-row">
              <span class="label">Epoch</span>
              <span class="value mono">${this.formatEpoch(d?.epoch)}</span>
            </div>
            <div class="panel-row">
              <span class="label">Sync</span>
              <span class="value">${this.renderSync(d?.syncStatus)}</span>
            </div>
            ${d?.totalNodes > 0 ? html`
              <div class="panel-row">
                <span class="label">RPC</span>
                <span class="value mono">${d.reachableNodes}/${d.totalNodes}</span>
              </div>
            ` : ''}
          </div>
        </div>
      `
    }

    return html`
      <div class="strip">
        <div class="item">
          <span class="label">Chain</span>
          <span class="value">${d?.networkName ?? '…'}</span>
        </div>
        <div class="item">
          <span class="label">Epoch</span>
          <span class="value mono">${this.formatEpoch(d?.epoch)}</span>
        </div>
        <div class="item">
          <span class="label">Status</span>
          <span class="value">${this.renderSync(d?.syncStatus)}</span>
        </div>
        ${d?.totalNodes > 0 ? html`
          <div class="item">
            <span class="label">RPC nodes</span>
            <span class="value mono">${d.reachableNodes}/${d.totalNodes}</span>
          </div>
        ` : ''}
        <a class="detail-link" href="/pages/chain/">details →</a>
      </div>
    `
  }
})
