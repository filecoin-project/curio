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

function formatAtRiskDeleteDate(atRisk) {
  if (!atRisk) return 'the scheduled deletion date'
  if (atRisk.projectedDeleteAt) {
    const d = new Date(atRisk.projectedDeleteAt)
    const date = d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
    if (atRisk.projectedDeleteEpoch != null) {
      return `${date} (epoch ${atRisk.projectedDeleteEpoch})`
    }
    return date
  }
  if (atRisk.deleteDatePending) return 'pending confirmation'
  return 'the scheduled deletion date'
}

function atRiskBannerTitle(atRisk) {
  switch (atRisk?.status) {
    case 'grace': return 'Payment grace'
    case 'terminated': return atRisk?.reason === 'proving_failure' ? 'Proving stopped' : 'Service terminated'
    case 'terminating': return 'Service termination'
    case 'pending_delete': return 'Pending deletion'
    default: return 'Payment status'
  }
}

function atRiskBannerMessage(atRisk) {
  const owner = atRisk?.payer ? `owner (payer ${atRisk.payer})` : 'dataset owner (if known)'
  const deleteDate = formatAtRiskDeleteDate(atRisk)
  const contactOwner = atRisk?.reason === 'operator_requested' ? '' : ` Contact the ${owner}.`
  if (atRisk?.status === 'grace') {
    return `Contact the ${owner} to bring payments current.`
  }
  if (atRisk?.status === 'terminated') {
    if (atRisk?.reason === 'proving_failure') {
      return 'The service for this dataset ended or the dataset was removed on chain. Curio keeps the data until its deletion is requested.'
    }
    return 'Service for this dataset was terminated on chain. Curio keeps the data until its deletion is requested.'
  }
  if (atRisk?.status === 'pending_delete') {
    return `This dataset is pending deletion on ${deleteDate}.${contactOwner}`
  }
  if (atRisk?.status === 'terminating') {
    return `Service termination is confirmed. Scheduled for deletion on ${deleteDate}.${contactOwner} Payment will not stop that.`
  }
  return `Contact the ${owner}.`
}

const EPOCHS_PER_DAY = 2880

function daysLabel(days) {
  return `${days} day${days === 1 ? '' : 's'}`
}

function deletionInProgress(atRisk) {
  return atRisk?.status === 'terminating' || atRisk?.status === 'pending_delete'
}

function shortDate(value) {
  return new Date(value).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

function deleteConsequence(atRisk, payments) {
  if (payments?.removedOnChain) {
    return 'Curio deletes its data from this node. If cleanup on chain is unfinished, Curio finishes it first and pays the gas, which can take a while for a large dataset.'
  }
  if (atRisk?.status === 'terminated') {
    return 'Curio deletes the data once the service period is over and the last payment is settled.'
  }
  if (!payments || payments.error) {
    return 'Curio ends the service on chain if it is still running, then deletes the data once the service period is over.'
  }
  const lockupDays = Math.round(Number(payments.lockupPeriod) / EPOCHS_PER_DAY)
  const period = lockupDays > 0 ? `, up to ${daysLabel(lockupDays)} from now` : ''
  return `Curio ends the service on chain right away, then deletes the data once the service period is over${period}.`
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
    income: { type: Object },
    incomeStatus: { type: String },
    confirmOpen: { type: Boolean },
    deleteBusy: { type: Boolean },
    deleteError: { type: String },
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
    this.income = null
    this.incomeStatus = 'idle'
    this.confirmOpen = false
    this.deleteBusy = false
    this.deleteError = null
    this.load()
  }

  createRenderRoot() {
    return this
  }

  openDeleteConfirm() {
    this.deleteError = null
    this.confirmOpen = true
  }

  closeDeleteConfirm() {
    if (this.deleteBusy) return
    this.confirmOpen = false
  }

  async confirmDelete() {
    this.deleteBusy = true
    this.deleteError = null
    try {
      await RPCCall('PDPDataSetRequestDeletion', [this.data.id])
    } catch (e) {
      // A deletion requested earlier needs no second request: the reload shows it.
      if (!/already requested/.test(e?.message || '')) {
        console.error('Dataset deletion request failed:', e)
        this.deleteError = "Couldn't start the deletion. Try again."
        this.deleteBusy = false
        return
      }
    }
    this.deleteBusy = false
    this.confirmOpen = false
    await this.load()
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

    // Late-populate chain payments, income + interactions in parallel.
    this.paymentsStatus = 'loading'
    this.interactionsStatus = 'loading'
    this.incomeStatus = 'loading'
    this.requestUpdate()

    // Without income records there is nothing to show, whatever the cause.
    const incomeP = RPCCall('PDPDataSetIncome', [numId])
      .then((income) => { this.income = income })
      .catch(() => { this.income = null })
      .finally(() => { this.incomeStatus = 'ready' })

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

    await Promise.allSettled([paymentsP, interactionsP, incomeP])
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
    const income = this.income
    const incomeLoading = this.incomeStatus === 'loading'
    const recorded = !!income?.recordedSince
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
        .payment-at-risk-banner {
          border: 1px solid rgba(248, 81, 73, 0.45);
          background: rgba(248, 81, 73, 0.12);
          color: var(--color-text-primary, #e6edf3);
          border-radius: 8px;
          padding: 12px 16px;
          margin-bottom: 16px;
          font-size: 14px;
          line-height: 1.45;
        }
        .payment-at-risk-banner strong {
          display: block;
          margin-bottom: 4px;
          color: var(--color-danger-fg, #f85149);
        }
        .income-block { margin-bottom: 24px; }
        .income-metrics {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
          gap: 16px;
        }
        .income-actions { display: flex; justify-content: flex-end; }
        .metric-unit { font-size: 14px; font-weight: 500; margin-left: 6px; }
        .confirm-overlay {
          position: fixed;
          inset: 0;
          z-index: 1050;
          display: flex;
          align-items: center;
          justify-content: center;
          padding: 16px;
          background: var(--color-shadow-main, rgba(1, 4, 9, 0.8));
        }
        .confirm-dialog {
          width: 100%;
          max-width: 480px;
          background: var(--color-bg-subtle, #161b22);
          border: 1px solid var(--color-border-default, #30363d);
          border-radius: var(--radius-lg, 8px);
          box-shadow: var(--shadow-overlay, 0 8px 24px rgba(1, 4, 9, 0.5));
          padding: 20px 24px;
          font-size: 14px;
          line-height: 1.5;
        }
        .confirm-dialog h3 { font-size: 16px; font-weight: 600; margin: 0 0 12px; }
        .confirm-dialog p { margin: 0 0 8px; }
        .confirm-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
      </style>

      ${d.paymentAtRisk?.atRisk ? html`
        <div class="payment-at-risk-banner">
          <strong>${atRiskBannerTitle(d.paymentAtRisk)}</strong>
          ${atRiskBannerMessage(d.paymentAtRisk)}
        </div>
      ` : ''}

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

      <div class="info-block income-block">
        <h2>Income</h2>
        <div class="income-metrics">
          <div>
            <div class="metric-label">Recorded income</div>
            <div class="metric">${maybeLoading(incomeLoading, recorded
              ? html`${income.recordedIncome}<span class="metric-unit">USDFC</span>`
              : '—')}</div>
            ${incomeLoading ? '' : html`
              <p class="section-note muted">${recorded ? `Since ${shortDate(income.recordedSince)}` : 'Not recorded yet'}</p>
            `}
            ${recorded && income.missingPeriods > 0 ? html`
              <p class="section-note muted">${income.missingPeriods} period${income.missingPeriods === 1 ? '' : 's'} not recorded</p>
            ` : ''}
          </div>
          <div>
            <div class="metric-label">Income per month</div>
            <div class="metric">${maybeLoading(paymentsLoading, p?.monthlyIncome
              ? html`${p.monthlyIncome}<span class="metric-unit">USDFC</span>`
              : '—')}</div>
            ${p?.monthlyIncome ? html`<p class="section-note muted">Before fees</p>` : ''}
          </div>
          <div>
            <div class="metric-label">Payer funded for</div>
            <div class="metric">${maybeLoading(paymentsLoading, p?.fundedDays != null ? daysLabel(Number(p.fundedDays)) : '—')}</div>
            ${p?.fundedUntilAt ? html`<p class="section-note muted">Until ${shortDate(p.fundedUntilAt)}</p>` : ''}
          </div>
        </div>
        ${p?.removedOnChain ? html`<p class="section-note muted">No longer on chain</p>` : ''}
        <div class="income-actions">
          ${deletionInProgress(d.paymentAtRisk)
            ? html`<p class="section-note muted">Deletion in progress</p>`
            : html`
              <button type="button" class="btn btn-danger btn-sm" ?disabled=${paymentsLoading}
                @click=${() => this.openDeleteConfirm()}>Delete dataset</button>
            `}
        </div>
      </div>

      ${this.confirmOpen ? html`
        <div class="confirm-overlay" @click=${() => this.closeDeleteConfirm()}>
          <div class="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="delete-dataset-title"
            @click=${(e) => e.stopPropagation()}>
            <h3 id="delete-dataset-title">Delete dataset ${d.id}?</h3>
            <p>${deleteConsequence(d.paymentAtRisk, p)}</p>
            <p class="err">This can't be undone.</p>
            ${this.deleteError ? html`<p class="err">${this.deleteError}</p>` : ''}
            <div class="confirm-actions">
              <button type="button" class="btn btn-secondary btn-sm" ?disabled=${this.deleteBusy}
                @click=${() => this.closeDeleteConfirm()}>Cancel</button>
              <button type="button" class="btn btn-danger btn-sm" ?disabled=${this.deleteBusy}
                @click=${() => this.confirmDelete()}>Delete dataset</button>
            </div>
          </div>
        </div>
      ` : ''}

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
