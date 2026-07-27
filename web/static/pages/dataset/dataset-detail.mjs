import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'
import RPCCall from '/lib/jsonrpc.mjs'
import { formatDate } from '/lib/dateutil.mjs'
import { loadingSpinner, loadingBlock, loadingCssText } from '/lib/loading.mjs'
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

function maybeLoading(loading, value) {
  return loading ? loadingSpinner() : value
}

function statusTone(status) {
  switch (status) {
    case 'unrecoverable':
    case 'overdue':
    case 'failing':
      return 'bad'
    case 'in-window':
      return 'warn'
    case 'scheduled':
      return 'ok'
    default:
      return 'muted'
  }
}

function statusTitle(status) {
  switch (status) {
    case 'unrecoverable': return 'Unrecoverable failure'
    case 'failing': return 'Proving failures'
    case 'overdue': return 'Proving overdue'
    case 'in-window': return 'In proving window'
    case 'scheduled': return 'Scheduled'
    case 'uninit': return 'Not initialized'
    default: return status || 'Unknown'
  }
}

function statusSummary(d, proven) {
  switch (d.provingStatus) {
    case 'unrecoverable':
      return 'Proving has stopped. This dataset will not schedule further prove attempts until recovered.'
    case 'failing':
      return d.nextProveAttemptAt != null
        ? `Waiting to retry after ${d.consecutiveProveFailures} consecutive failure(s).`
        : `${d.consecutiveProveFailures} consecutive prove failure(s).`
    case 'overdue':
      return 'The prove window closed without a successful proof.'
    case 'in-window':
      return proven === true
        ? 'Currently in window and already proven this period.'
        : 'Currently in the challenge window — proof should be submitted.'
    case 'scheduled':
      return 'Waiting for the next prove window to open.'
    case 'uninit':
      return 'No prove schedule yet (init not ready or next period not set).'
    default:
      return ''
  }
}

function formatEpoch(epoch, head) {
  if (epoch == null) return null
  const e = Number(epoch)
  if (!Number.isFinite(e)) return String(epoch)
  if (head == null || !Number.isFinite(Number(head))) {
    return html`<span class="mono">epoch ${e}</span>`
  }
  const h = Number(head)
  const delta = e - h
  let rel
  if (delta === 0) rel = 'now'
  else if (delta > 0) rel = `in ${delta} epoch${delta === 1 ? '' : 's'}`
  else rel = `${-delta} epoch${delta === -1 ? '' : 's'} ago`
  return html`<span class="mono">epoch ${e}</span> <span class="muted">(${rel})</span>`
}

function epochsLabel(n) {
  if (n == null) return '—'
  return html`<span class="mono">${n}</span> <span class="muted">epochs</span>`
}

customElements.define('pdp-dataset-detail', class PdpDatasetDetail extends LitElement {
  static properties = {
    data: { type: Object },
    payments: { type: Object },
    interactions: { type: Array },
    loadError: { type: String },
    paymentsStatus: { type: String },
    paymentsError: { type: String },
    interactionsStatus: { type: String },
    interactionsError: { type: String },
  }

  constructor() {
    super()
    this.data = null
    this.payments = null
    this.interactions = null
    this.loadError = null
    this.paymentsStatus = 'idle'
    this.paymentsError = null
    this.interactionsStatus = 'idle'
    this.interactionsError = null
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
    const numId = Number(id)

    try {
      this.data = await RPCCall('PDPDataSetDetail', [numId])
      this.loadError = null
    } catch (e) {
      console.error('Failed to load dataset:', e)
      this.loadError = e.message || String(e)
      this.requestUpdate()
      return
    }
    this.requestUpdate()

    // Late-populate chain payments + interactions in parallel.
    this.paymentsStatus = 'loading'
    this.interactionsStatus = 'loading'
    this.requestUpdate()

    const paymentsP = RPCCall('PDPDataSetPayments', [numId])
      .then((payments) => {
        this.payments = payments
        this.paymentsError = payments?.error || null
        this.paymentsStatus = 'ready'
      })
      .catch((e) => {
        console.warn('Dataset payments load failed:', e)
        this.paymentsError = e.message || String(e)
        this.paymentsStatus = 'error'
      })
      .finally(() => this.requestUpdate())

    const interactionsP = RPCCall('PDPDataSetInteractions', [numId])
      .then((interactions) => {
        this.interactions = interactions ?? []
        this.interactionsError = null
        this.interactionsStatus = 'ready'
      })
      .catch((e) => {
        console.warn('Dataset interactions load failed:', e)
        this.interactions = []
        this.interactionsError = e.message || String(e)
        this.interactionsStatus = 'error'
      })
      .finally(() => this.requestUpdate())

    await Promise.allSettled([paymentsP, interactionsP])
  }

  static styles = css``

  render() {
    if (this.loadError) {
      return html`<p style="color: var(--color-danger-fg, #f85149)">${this.loadError}</p>`
    }
    if (!this.data) {
      return html`
        <style>${loadingCssText}</style>
        ${loadingBlock('Loading dataset…')}
      `
    }

    const d = this.data
    const p = this.payments
    const proven = p?.provenThisPeriod
    const paymentsLoading = this.paymentsStatus === 'loading'
    const interactionsLoading = this.interactionsStatus === 'loading'
    const head = d.headEpoch ?? p?.headEpoch
    const provingNote = statusSummary(d, proven)

    return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous" />
      <link rel="stylesheet" href="/ux/main.css" />
      <link rel="stylesheet" href="/ux/dark-table.css" />

      <style>
        ${loadingCssText}
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
          min-height: 34px;
          display: flex;
          align-items: center;
        }
        .metric-label {
          font-size: 12px;
          text-transform: uppercase;
          letter-spacing: 0.04em;
          color: var(--color-text-secondary, #8b949e);
        }
        .back { margin-bottom: 16px; display: inline-block; }
        .top-line {
          display: flex;
          align-items: center;
          gap: 16px;
          margin-bottom: 16px;
          flex-wrap: nowrap;
        }
        .top-line .back { margin-bottom: 0; }
        .top-line .title {
          margin: 0;
          font-size: 1.75rem;
          font-weight: 600;
          line-height: 1.2;
        }
        .err { color: var(--color-danger-fg, #f85149); }
        .ok { color: var(--color-success-fg, #3fb950); }
        .muted { color: var(--color-text-secondary, #8b949e); }
        .section-note { font-size: 12px; margin: 0 0 8px; }
        .status-pill {
          display: inline-block;
          padding: 2px 8px;
          border-radius: 4px;
          font-size: 12px;
          font-weight: 600;
          text-transform: uppercase;
          letter-spacing: 0.03em;
        }
        .status-pill.ok { color: var(--color-success-fg, #3fb950); background: rgba(63, 185, 80, 0.15); }
        .status-pill.warn { color: var(--color-warning-fg, #d29922); background: rgba(210, 153, 34, 0.15); }
        .status-pill.bad { color: var(--color-danger-fg, #f85149); background: rgba(248, 81, 73, 0.15); }
        .status-pill.muted { color: var(--color-text-secondary, #8b949e); background: rgba(139, 148, 158, 0.15); }
        .proving-summary {
          font-size: 13px;
          color: var(--color-text-secondary, #8b949e);
          margin: 8px 0 14px;
          line-height: 1.4;
        }
      </style>

      <div class="top-line">
        <a class="back" href="/pages/datasets/">← DataSets</a>
        <h1 class="title">DataSet</h1>
        ${d.exploreUrl ? html`<a class="back" href="${d.exploreUrl}" target="_blank" rel="noopener noreferrer">Explore pieces ↗</a>` : ''}
      </div>

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
          ${paymentsLoading ? html`<p class="section-note">${loadingSpinner({ label: 'Loading from chain', size: 'sm' })}</p>` : ''}
          ${this.paymentsError && !paymentsLoading ? html`<p class="section-note err">${this.paymentsError}</p>` : ''}
          <div class="metric-label">Payments remaining</div>
          <div class="metric">${maybeLoading(paymentsLoading, p?.paymentsRemaining != null ? p.paymentsRemaining : '—')}</div>
          <dl class="kv">
            <dt>Payer wallet</dt><dd class="mono">${maybeLoading(paymentsLoading, p?.payer || '—')}</dd>
            <dt>Payee</dt><dd class="mono">${maybeLoading(paymentsLoading, p?.payee || '—')}</dd>
            <dt>Available funds</dt><dd class="mono">${maybeLoading(paymentsLoading, `${p?.availableFunds || '—'} USDFC`)}</dd>
            <dt>Funded until</dt><dd class="mono">${maybeLoading(paymentsLoading, p?.fundedUntilEpoch || '—')}</dd>
            <dt>Lockup period</dt><dd class="mono">${maybeLoading(paymentsLoading, `${p?.lockupPeriod || '—'} epochs`)}</dd>
            <dt>Payment rate</dt><dd class="mono">${maybeLoading(paymentsLoading, p?.paymentRate || '—')}</dd>
            <dt>PDP rail</dt><dd class="mono">${maybeLoading(paymentsLoading, p?.pdpRailId || '—')}</dd>
            <dt>PDP end epoch</dt><dd class="mono">${maybeLoading(paymentsLoading, p?.pdpEndEpoch || '—')}</dd>
          </dl>
        </div>

        <div class="info-block">
          <h2>Proving</h2>
          <div>
            <span class="status-pill ${statusTone(d.provingStatus)}">${statusTitle(d.provingStatus)}</span>
          </div>
          ${provingNote ? html`<p class="proving-summary">${provingNote}</p>` : ''}
          <dl class="kv">
            ${d.unrecoverableFailureEpoch != null ? html`
              <dt>Failed at</dt>
              <dd class="err">${formatEpoch(d.unrecoverableFailureEpoch, head)}</dd>
            ` : ''}
            ${(d.consecutiveProveFailures ?? 0) > 0 ? html`
              <dt>Consecutive failures</dt>
              <dd class="mono ${d.provingStatus === 'unrecoverable' || d.provingStatus === 'failing' ? 'err' : ''}">${d.consecutiveProveFailures}</dd>
            ` : ''}
            ${d.nextProveAttemptAt != null && d.provingStatus !== 'unrecoverable' ? html`
              <dt>Retry after</dt>
              <dd>${formatEpoch(d.nextProveAttemptAt, head)}</dd>
            ` : ''}
            ${d.provingStatus !== 'unrecoverable' ? html`
              <dt>Next window opens</dt>
              <dd>${d.proveAtEpoch != null ? formatEpoch(d.proveAtEpoch, head) : '—'}</dd>
              ${d.proveAtEpoch != null && d.challengeWindow != null ? html`
                <dt>Next window closes</dt>
                <dd>${formatEpoch(Number(d.proveAtEpoch) + Number(d.challengeWindow), head)}</dd>
              ` : ''}
              <dt>Proven this period</dt>
              <dd>${paymentsLoading
                ? loadingSpinner()
                : (proven == null
                  ? '—'
                  : (proven ? html`<span class="ok">yes</span>` : html`<span class="err">no</span>`))}</dd>
            ` : ''}
            <dt>Challenge window</dt><dd>${epochsLabel(d.challengeWindow)}</dd>
            <dt>Proving period</dt><dd>${epochsLabel(d.provingPeriod)}</dd>
            <dt>Init ready</dt><dd><yes-no .value=${!!d.initReady}></yes-no></dd>
            <dt>Chain head</dt><dd class="mono">epoch ${head ?? '—'}</dd>
          </dl>
        </div>
      </div>

      <div class="info-block">
        <h2>Recent interactions</h2>
        ${this.interactionsError && !interactionsLoading ? html`<p class="section-note err">${this.interactionsError}</p>` : ''}
        ${interactionsLoading
          ? loadingBlock('Loading interactions…')
          : (!(this.interactions?.length)
            ? html`<p class="muted" style="margin:0">No recent interactions.</p>`
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
                ${this.interactions.map((i) => html`
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
          `)}
      </div>
    `
  }
})
