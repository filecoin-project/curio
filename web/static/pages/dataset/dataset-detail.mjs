import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'
import RPCCall from '/lib/jsonrpc.mjs'
import { formatDate } from '/lib/dateutil.mjs'
import '/ux/yesno.mjs'

function formatBytes(bytes) {
  const n = Number(bytes || 0)
  if (n === 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`
}

function formatLifespan(seconds) {
  if (seconds == null) return '—'
  const s = Number(seconds)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  if (d > 0) return `${d}d ${h}h`
  const m = Math.floor((s % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function shortHash(h) {
  if (!h || h.length < 14) return h || '—'
  return `${h.slice(0, 8)}…${h.slice(-6)}`
}

customElements.define('pdp-dataset-detail', class PdpDatasetDetail extends LitElement {
  static properties = {
    data: { type: Object },
    loadError: { type: String },
  }

  constructor() {
    super()
    this.data = null
    this.loadError = null
    this.load()
  }

  createRenderRoot() {
    return this
  }

  async load() {
    const params = new URLSearchParams(window.location.search)
    const id = params.get('id')
    if (!id) {
      this.loadError = 'Missing dataset id'
      this.requestUpdate()
      return
    }
    try {
      this.data = await RPCCall('PDPDataSetDetail', [Number(id)])
      this.loadError = null
    } catch (e) {
      console.error('Failed to load dataset:', e)
      this.loadError = e.message || String(e)
    }
    this.requestUpdate()
  }

  static styles = css``

  render() {
    if (this.loadError) {
      return html`<p style="color: var(--color-danger-fg, #f85149)">${this.loadError}</p>`
    }
    if (!this.data) {
      return html`<p style="color: var(--color-text-secondary, #8b949e)">Loading…</p>`
    }

    const d = this.data
    return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous" />
      <link rel="stylesheet" href="/ux/main.css" />
      <link rel="stylesheet" href="/ux/dark-table.css" />

      <style>
        .detail-grid {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
          gap: 16px;
          margin-bottom: 24px;
        }
        .info-block {
          background: var(--color-bg-subtle, #161b22);
          border: 1px solid var(--color-border-default, #30363d);
          border-radius: 8px;
          padding: 16px 20px;
        }
        .info-block h2 {
          font-size: 16px;
          font-weight: 600;
          margin: 0 0 12px;
          padding-bottom: 8px;
          border-bottom: 1px solid var(--color-border-default, #30363d);
        }
        .kv { display: grid; grid-template-columns: 140px 1fr; gap: 6px 12px; font-size: 14px; }
        .kv dt { color: var(--color-text-secondary, #8b949e); margin: 0; }
        .kv dd { margin: 0; word-break: break-all; }
        .mono { font-family: ui-monospace, monospace; font-size: 13px; }
        .metric {
          font-size: 28px;
          font-weight: 600;
          font-variant-numeric: tabular-nums;
          margin: 4px 0 8px;
        }
        .metric-label {
          font-size: 12px;
          text-transform: uppercase;
          letter-spacing: 0.04em;
          color: var(--color-text-secondary, #8b949e);
        }
        .back { margin-bottom: 16px; display: inline-block; }
        .err { color: var(--color-danger-fg, #f85149); }
        .ok { color: var(--color-success-fg, #3fb950); }
      </style>

      <a class="back" href="/pages/datasets/">← DataSets</a>

      <div class="detail-grid">
        <div class="info-block">
          <h2>Overview</h2>
          <div class="metric-label">Dataset</div>
          <div class="metric mono">${d.id}</div>
          <dl class="kv">
            <dt>Objects in store</dt><dd class="mono">${d.objectCount ?? 0}</dd>
            <dt>Size</dt><dd class="mono">${formatBytes(d.sizeBytes)}</dd>
            <dt>Lifespan</dt><dd>${formatLifespan(d.lifespanSeconds)}</dd>
            <dt>First upload</dt>
            <dd>${d.firstUploadAt ? `${formatDate(d.firstUploadAt)}` : '—'}</dd>
            <dt>Service</dt><dd class="mono">${d.service || '—'}</dd>
          </dl>
        </div>

        <div class="info-block">
          <h2>Wallet & payments</h2>
          <div class="metric-label">Payments remaining</div>
          <div class="metric">${d.paymentsRemaining != null ? d.paymentsRemaining : '—'}</div>
          <dl class="kv">
            <dt>Payer wallet</dt><dd class="mono">${d.payer || '—'}</dd>
            <dt>Payee</dt><dd class="mono">${d.payee || '—'}</dd>
            <dt>Available funds</dt><dd class="mono">${d.availableFunds || '—'} USDFC</dd>
            <dt>Funded until</dt><dd class="mono">${d.fundedUntilEpoch || '—'}</dd>
            <dt>Lockup period</dt><dd class="mono">${d.lockupPeriod || '—'} epochs</dd>
            <dt>Payment rate</dt><dd class="mono">${d.paymentRate || '—'}</dd>
            <dt>PDP rail</dt><dd class="mono">${d.pdpRailId || '—'}</dd>
            <dt>PDP end epoch</dt><dd class="mono">${d.pdpEndEpoch || '—'}</dd>
          </dl>
        </div>

        <div class="info-block">
          <h2>Proving</h2>
          <dl class="kv">
            <dt>Prove at epoch</dt><dd class="mono">${d.proveAtEpoch ?? '—'}</dd>
            <dt>Challenge window</dt><dd class="mono">${d.challengeWindow ?? '—'}</dd>
            <dt>Proving period</dt><dd class="mono">${d.provingPeriod ?? '—'}</dd>
            <dt>Proven this period</dt>
            <dd>${d.provenThisPeriod == null ? '—' : (d.provenThisPeriod ? html`<span class="ok">yes</span>` : html`<span class="err">no</span>`)}</dd>
            <dt>Consecutive failures</dt><dd class="mono">${d.consecutiveProveFailures ?? 0}</dd>
            <dt>Backoff until</dt><dd class="mono">${d.nextProveAttemptAt ?? '—'}</dd>
            <dt>Unrecoverable</dt>
            <dd class="${d.unrecoverableFailureEpoch ? 'err' : ''}">${d.unrecoverableFailureEpoch ?? '—'}</dd>
            <dt>Init ready</dt><dd><yes-no .value=${!!d.initReady}></yes-no></dd>
            <dt>Chain head</dt><dd class="mono">${d.headEpoch ?? '—'}</dd>
          </dl>
        </div>
      </div>

      <div class="info-block">
        <h2>Recent interactions</h2>
        ${!(d.interactions?.length)
          ? html`<p style="color: var(--color-text-secondary, #8b949e); margin:0">No recent interactions.</p>`
          : html`
            <table class="table table-dark table-striped table-sm">
              <thead>
                <tr>
                  <th>When</th>
                  <th>Kind</th>
                  <th>Result</th>
                  <th>Ref</th>
                  <th>Detail</th>
                </tr>
              </thead>
              <tbody>
                ${d.interactions.map((i) => html`
                  <tr>
                    <td>${i.timestamp ? formatDate(i.timestamp) : '—'}</td>
                    <td class="mono">${i.kind}</td>
                    <td>
                      ${i.success == null ? '—' : (i.success
                        ? html`<span class="ok">ok</span>`
                        : html`<span class="err">fail</span>`)}
                    </td>
                    <td class="mono">
                      ${i.taskId
                        ? html`<a href="/pages/task/id/?id=${i.taskId}">task ${i.taskId}</a>`
                        : (i.txHash ? shortHash(i.txHash) : '—')}
                    </td>
                    <td class="${i.err ? 'err' : ''}">${i.err || i.detail || '—'}</td>
                  </tr>
                `)}
              </tbody>
            </table>
          `}
      </div>
    `
  }
})
