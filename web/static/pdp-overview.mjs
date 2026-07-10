import { LitElement, html, css, render } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'
import RPCCall from '/lib/jsonrpc.mjs'
import { pollRPC } from '/lib/poll.mjs'
import { timeSince } from '/lib/dateutil.mjs'
import { walletAsideBadge, provingIssueReasons } from '/lib/pdp-proving-status.mjs'

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

function shortAddr(addr) {
  if (!addr || addr.length < 12) return addr || '—'
  return `${addr.slice(0, 6)}…${addr.slice(-4)}`
}

function hostFromUrl(url) {
  if (!url) return ''
  try {
    return new URL(url).host
  } catch (_) {
    return url.replace(/^https?:\/\//, '').replace(/\/.*$/, '')
  }
}

function groupAlertsByMessage(alerts, maxGroups = 5) {
  const byMsg = new Map()
  for (const a of alerts ?? []) {
    const key = a.Message ?? ''
    let g = byMsg.get(key)
    if (!g) {
      g = {
        message: key,
        alertName: a.AlertName,
        machineName: a.MachineName,
        count: 0,
        firstAt: a.CreatedAt,
        lastAt: a.CreatedAt,
      }
      byMsg.set(key, g)
    }
    g.count += 1
    const t = new Date(a.CreatedAt).getTime()
    if (t < new Date(g.firstAt).getTime()) g.firstAt = a.CreatedAt
    if (t > new Date(g.lastAt).getTime()) g.lastAt = a.CreatedAt
    // Prefer a non-empty machine name when present
    if (!g.machineName && a.MachineName) g.machineName = a.MachineName
  }
  return [...byMsg.values()]
    .sort((a, b) => new Date(b.lastAt) - new Date(a.lastAt))
    .slice(0, maxGroups)
}

function formatAlertRange(firstAt, lastAt, count) {
  const first = timeSince(new Date(firstAt))
  const last = timeSince(new Date(lastAt))
  if (count <= 1 || firstAt === lastAt) {
    return last
  }
  return `last ${last} · first ${first}`
}

customElements.define('pdp-overview', class PdpOverview extends LitElement {
  static properties = {
    summary: { type: Object },
    summaryStatus: { type: String },
    financial: { type: Object },
    alerts: { type: Array },
    alertTotal: { type: Number },
    alertsStatus: { type: String },
    keyStatus: { type: Object },
    keyStatusLoading: { type: Boolean },
    registry: { type: Object },
    financialLoading: { type: Boolean },
    financialError: { type: String },
    chartReady: { type: Boolean },
  }

  constructor() {
    super()
    this.summary = null
    this.summaryStatus = 'loading'
    this.financial = null
    this.alerts = []
    this.alertTotal = 0
    this.alertsStatus = 'loading'
    this.keyStatus = undefined
    this.keyStatusLoading = true
    this.registry = null
    this.financialLoading = true
    this.financialError = null
    this.chartReady = false
    this._chart = null
    this._loadPromise = null
    pollRPC(async () => {
      await this.load()
      await this.loadKeyStatus()
    }, 30000)
    this.loadKeyStatus()
    this.load()
  }

  connectedCallback() {
    super.connectedCallback()
    this.renderWalletAside()
  }

  async loadKeyStatus() {
    const firstLoad = this.keyStatus === undefined
    if (firstLoad) {
      this.keyStatusLoading = true
      this.renderWalletAside()
    }
    try {
      this.keyStatus = await RPCCall('PDPKeyStatus')
    } catch (e) {
      console.warn('PDP key status load failed:', e)
      if (this.keyStatus === undefined) {
        this.keyStatus = null
      }
    } finally {
      this.keyStatusLoading = false
      this.requestUpdate()
      this.renderWalletAside()
    }
  }

  async load() {
    if (this._loadPromise) {
      return this._loadPromise
    }
    this._loadPromise = this._doLoad().finally(() => {
      this._loadPromise = null
    })
    return this._loadPromise
  }

  async _doLoad() {
    this.loadFinancial()
    const summaryP = RPCCall('PDPDashboardSummary').then((summary) => {
      this.summary = summary
      this.summaryStatus = 'ok'
      this.requestUpdate()
      this.updateComplete.then(() => this.renderChart())
    }).catch((e) => {
      console.warn('PDPDashboardSummary failed:', e)
      if (!this.summary) this.summaryStatus = 'error'
      this.requestUpdate()
    })
    const alertsP = RPCCall('AlertHistoryListPaginated', [100, 0, false]).then((alertResult) => {
      this.alerts = alertResult?.Alerts ?? []
      this.alertTotal = alertResult?.Total ?? 0
      this.alertsStatus = 'ok'
      this.requestUpdate()
    }).catch((e) => {
      console.warn('AlertHistoryListPaginated failed:', e)
      if (this.alertsStatus !== 'ok') this.alertsStatus = 'error'
      this.requestUpdate()
    })
    const regP = RPCCall('FSRegistryStatus').catch(() => null).then((registry) => {
      this.registry = registry
      this.requestUpdate()
      this.renderWalletAside()
    })
    await Promise.all([summaryP, alertsP, regP])
  }

  async loadFinancial() {
    // Keep previous values visible while refreshing; only show loading on first fetch.
    if (!this.financial) {
      this.financialLoading = true
      this.requestUpdate()
    }
    try {
      this.financial = await RPCCall('PDPDashboardFinancial')
      this.financialError = null
    } catch (e) {
      console.warn('PDP financial load failed:', e)
      if (!this.financial) {
        this.financialError = e?.message || String(e)
      }
    } finally {
      this.financialLoading = false
      this.requestUpdate()
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback()
    const host = document.getElementById('pdp-wallet-aside')
    if (host) render(html``, host)
    if (this._chart) {
      this._chart.destroy()
      this._chart = null
    }
  }

  renderChart() {
    const canvas = this.renderRoot.querySelector('#proving-activity-chart')
    const activity = this.summary?.provingActivity ?? []
    const hasData = activity.some((d) => (d.success ?? 0) + (d.failed ?? 0) + (d.faulted ?? 0) > 0)
    if (!canvas || !hasData || typeof Chart === 'undefined') {
      if (this._chart) {
        this._chart.destroy()
        this._chart = null
      }
      return
    }
    const labels = activity.map((d) => d.date.slice(5))
    const success = activity.map((d) => d.success ?? 0)
    const failed = activity.map((d) => d.failed ?? 0)
    const faulted = activity.map((d) => d.faulted ?? 0)
    // Keep real counts for tooltips; pad non-zero minority series so they stay visible
    // when Proved dominates the stack height.
    const maxTotal = Math.max(
      1,
      ...activity.map((d) => (d.success ?? 0) + (d.failed ?? 0) + (d.faulted ?? 0)),
    )
    const minVisible = Math.max(1, Math.ceil(maxTotal * 0.06))
    const padVisible = (values) => values.map((v) => (v > 0 ? Math.max(v, minVisible) : 0))
    const failedDisplay = padVisible(failed)
    const faultedDisplay = padVisible(faulted)
    const realByDataset = [success, failed, faulted]
    const chartConfig = {
      type: 'bar',
      data: {
        labels,
        datasets: [
          {
            label: 'Proved',
            data: success,
            backgroundColor: 'rgba(63, 185, 80, 0.75)',
            stack: 'proving',
            order: 1,
          },
          {
            label: 'Task Failed',
            data: failedDisplay,
            backgroundColor: 'rgba(248, 81, 73, 0.8)',
            stack: 'proving',
            order: 2,
          },
          {
            label: 'Datasets Faulted',
            data: faultedDisplay,
            backgroundColor: 'rgba(210, 153, 34, 0.9)',
            stack: 'proving',
            order: 3,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        layout: {
          padding: { top: 0, right: 2, bottom: 0, left: 0 },
        },
        plugins: {
          legend: {
            display: false,
          },
          tooltip: {
            mode: 'index',
            intersect: false,
            filter: (item) => (realByDataset[item.datasetIndex]?.[item.dataIndex] ?? 0) > 0
              || item.datasetIndex === 0,
            callbacks: {
              label: (item) => {
                const real = realByDataset[item.datasetIndex]?.[item.dataIndex] ?? 0
                return `${item.dataset.label}: ${real}`
              },
            },
          },
        },
        scales: {
          x: {
            stacked: true,
            ticks: { color: '#8b949e', maxRotation: 0, autoSkip: true, maxTicksLimit: 10 },
            grid: { color: 'rgba(48, 54, 61, 0.5)' },
          },
          y: {
            stacked: true,
            beginAtZero: true,
            ticks: { color: '#8b949e', precision: 0 },
            grid: { color: 'rgba(48, 54, 61, 0.5)' },
          },
        },
      },
    }
    if (this._chart) {
      this._chart.destroy()
      this._chart = null
    }
    this._chart = new Chart(canvas, chartConfig)
  }

  static styles = css`
    :host {
      display: block;
    }
    .kpi-grid {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      margin-bottom: 0;
    }
    .kpi-card {
      flex: 1 1 140px;
      min-width: 0;
      max-width: 100%;
      padding: 6px 8px;
      background: var(--color-bg-subtle, #161b22);
      border: 1px solid var(--color-border-default, #30363d);
      border-radius: var(--radius-lg, 8px);
      box-sizing: border-box;
    }
    .kpi-label {
      font-size: 10px;
      font-weight: 600;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--color-text-secondary, #8b949e);
      margin-bottom: 2px;
    }
    .kpi-value {
      font-size: 20px;
      font-weight: 500;
      line-height: 1.1;
      color: var(--color-text-primary, #e6edf3);
    }
    .kpi-unit {
      font-size: 14px;
      font-weight: 500;
    }
    .kpi-usd {
      font-size: 14px;
      font-weight: 400;
      color: var(--color-text-secondary, #8b949e);
      margin-left: 6px;
    }
    .kpi-sub {
      margin-top: 2px;
      font-size: 10px;
      line-height: 1.25;
      color: var(--color-text-secondary, #8b949e);
    }
    .block-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      flex-wrap: wrap;
      margin: 0 0 6px;
      padding-bottom: 4px;
      border-bottom: 1px solid var(--color-border-default, #30363d);
    }
    .block-header h2 {
      margin: 0;
      font-size: 15px;
      font-weight: 600;
      color: var(--color-text-primary, #e6edf3);
    }
    .chart-legend {
      display: flex;
      align-items: center;
      gap: 10px;
      font-size: 11px;
      color: var(--color-text-secondary, #8b949e);
    }
    .legend-swatch {
      display: inline-block;
      width: 9px;
      height: 9px;
      border-radius: 2px;
      margin-right: 4px;
      vertical-align: -1px;
    }
    .legend-swatch.success {
      background: rgba(63, 185, 80, 0.75);
    }
    .legend-swatch.failed {
      background: rgba(248, 81, 73, 0.75);
    }
    .legend-swatch.faulted {
      background: rgba(210, 153, 34, 0.85);
    }
    .issues-list {
      list-style: none;
      padding: 0;
      margin: 0;
    }
    .issue-item {
      padding: 5px 0;
      border-bottom: 1px solid var(--color-border-muted, #21262d);
    }
    .issue-item:last-child {
      border-bottom: none;
      padding-bottom: 0;
    }
    .issue-msg {
      font-size: 13px;
      color: var(--color-text-primary, #e6edf3);
      margin-bottom: 2px;
    }
    .issue-count {
      display: inline-block;
      margin-left: 6px;
      padding: 0 5px;
      font-size: 11px;
      font-weight: 600;
      line-height: 1.4;
      color: var(--color-text-secondary, #8b949e);
      background: var(--color-bg-muted, #21262d);
      border-radius: 4px;
      vertical-align: 1px;
    }
    .issue-meta {
      font-size: 11px;
      color: var(--color-text-secondary, #8b949e);
    }
    .all-clear {
      padding: 0;
      margin: 0;
      color: var(--color-success-fg, #3fb950);
      font-size: 13px;
    }
    .status-muted {
      padding: 0;
      margin: 0;
      color: var(--color-text-secondary, #8b949e);
      font-size: 13px;
    }
    .status-error {
      padding: 0;
      margin: 0;
      color: var(--color-danger-fg, #f85149);
      font-size: 13px;
    }
    .chart-wrap {
      position: relative;
      height: 140px;
      max-height: 140px;
      overflow: hidden;
    }
    .split-row {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 6px;
      margin-bottom: 0;
      align-items: start;
    }
    .split-col {
      min-width: 0;
    }
    .info-block {
      background: var(--color-bg-subtle, #161b22);
      border: 1px solid var(--color-border-default, #30363d);
      border-radius: var(--radius-lg, 8px);
      padding: 10px 12px;
      margin-bottom: 0;
    }
    .split-col .info-block {
      margin-bottom: 0;
    }
    .dashboard-section {
      margin-bottom: 8px;
    }
    .dashboard-section:last-child {
      margin-bottom: 0;
    }
    @media (max-width: 960px) {
      .split-row {
        grid-template-columns: 1fr;
      }
    }
    .proving-alerts {
      margin-top: 8px;
      padding: 8px 10px;
      border-radius: 6px;
      background: rgba(248, 81, 73, 0.08);
      border: 1px solid rgba(248, 81, 73, 0.35);
    }
    .proving-alerts-title {
      margin: 0 0 4px;
      font-size: 11px;
      font-weight: 600;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--color-danger-fg, #f85149);
    }
    .proving-alerts-list {
      margin: 0;
      padding: 0 0 0 16px;
      font-size: 12px;
      line-height: 1.4;
      color: var(--color-text-primary, #e6edf3);
    }
    .proving-alerts-list li {
      margin: 0 0 2px;
    }
    .proving-alerts-list li:last-child {
      margin-bottom: 0;
    }
    .proving-alerts-foot {
      margin: 6px 0 0;
      font-size: 11px;
    }
    .proving-alerts-note {
      margin: 6px 0 0;
      font-size: 11px;
      line-height: 1.4;
      color: var(--color-text-secondary, #8b949e);
    }
    .proving-alerts-foot a {
      color: var(--color-accent-fg, #4493f8);
      text-decoration: none;
    }
    .proving-alerts-foot a:hover {
      text-decoration: underline;
    }
    a.view-all {
      font-size: 11px;
      color: var(--color-accent-fg, #4493f8);
      text-decoration: none;
    }
    a.view-all:hover {
      text-decoration: underline;
    }
  `

  updated(changed) {
    if (changed.has('keyStatus') || changed.has('keyStatusLoading') || changed.has('registry')) {
      this.renderWalletAside()
    }
  }

  financialDisplay(field) {
    if (this.financialLoading) return '…'
    if (this.financialError) return '—'
    if (!this.financial) return '…'
    const value = this.financial[field]
    if (value === '…') return '—'
    return value ?? '0'
  }

  expenseUsdHint() {
    if (this.financialLoading || this.financialError || !this.financial) return ''
    const usdfc = this.financial.expense30dUsdfc
    if (!usdfc || usdfc === '0' || usdfc === '…') return ''
    return `(± $${usdfc})`
  }

  financialSubline(kind) {
    if (this.financialLoading) return 'loading…'
    if (this.financialError) return 'financial metrics unavailable'
    if (!this.financial) return 'loading…'

    if (kind === 'income') {
      const parts = [this.financial.incomeSubtitle ?? 'settled · 30 d']
      if (this.financial.income30dUsdfc === '…') {
        parts.push('settlement query failed')
      }
      if (this.financial.accruedUnsettledUsdfc && this.financial.accruedUnsettledUsdfc !== '0') {
        parts.push(`${this.financial.accruedUnsettledUsdfc} accrued`)
      }
      return parts.join(' · ')
    }

    const fil = this.financial.expense30dFil
    const note = this.financial.expense30dUsdfcNote
    if (fil === '…') return 'gas query failed'
    if (note) return note
    return 'PDP + settlement txs'
  }

  bannerBadge() {
    return walletAsideBadge(this.keyStatus, {
      keyStatusLoading: this.keyStatusLoading,
    })
  }

  renderProvingAlerts() {
    const reasons = provingIssueReasons(this.summary)
    if (!reasons.length) return ''

    const s = this.summary
    const links = []
    if ((s?.faultedPeriods30d ?? 0) > 0) {
      links.push(html`<a href="/pages/pdp/">PDP datasets</a>`)
    }
    if ((s?.provingFailureCount ?? 0) > 0) {
      links.push(html`<a href="/pages/task/?name=PDPv0_Prove">Failed prove tasks</a>`)
    }
    if (this.alertTotal > 0) {
      links.push(html`<a href="/pages/alerts/">Alerts (${this.alertTotal})</a>`)
    }

    return html`
      <div class="proving-alerts">
        <p class="proving-alerts-title">Proving Issues</p>
        <ul class="proving-alerts-list">
          ${reasons.map((reason) => html`<li>${reason}</li>`)}
        </ul>
        ${(s?.faultedPeriods30d ?? 0) > 0 && (s?.provingFailureCount ?? 0) === 0 ? html`
          <p class="proving-alerts-note">
            Dataset faults are recorded when proving hits unrecoverable contract errors.
            Those runs often complete as successful tasks, so they may not appear under Task Failed.
          </p>
        ` : ''}
        ${links.length ? html`
          <p class="proving-alerts-foot">
            ${links.map((link, i) => html`${i > 0 ? ' · ' : ''}${link}`)}
          </p>
        ` : ''}
      </div>
    `
  }

  renderWalletAside() {
    const host = document.getElementById('pdp-wallet-aside')
    if (!host) return

    const ks = this.keyStatus
    const reg = this.registry
    const badge = this.bannerBadge()

    if (this.keyStatusLoading) {
      render(html`
        <div class="pdp-wallet-strip">
          <div class="pdp-wallet-strip-main">
            <span class="pdp-wallet-badge warn">Loading…</span>
          </div>
        </div>
      `, host)
      return
    }

    if (ks === null) {
      render(html`
        <div class="pdp-wallet-strip">
          <div class="pdp-wallet-strip-main">
            <span class="pdp-wallet-badge warn">Wallet unavailable</span>
            <a class="pdp-wallet-link" href="/pages/wallet/">Wallet →</a>
          </div>
        </div>
      `, host)
      return
    }

    if (!ks?.configured) {
      render(html`
        <div class="pdp-wallet-strip">
          <div class="pdp-wallet-strip-main">
            <span class="pdp-wallet-badge warn">No wallet</span>
            <a class="pdp-wallet-link" href="/pages/wallet/">Set up →</a>
          </div>
        </div>
      `, host)
      return
    }

    const balance = ks.balanceKnown
      ? (ks.balance || '0 FIL')
      : 'FIL …'
    const usdfc = ks.usdfcKnown
      ? (ks.usdfcBalance || '0 USDFC')
      : 'USDFC …'
    const subParts = []
    if (reg?.id) subParts.push(html`Provider #${reg.id}`)
    const hostName = hostFromUrl(reg?.pdp_service?.service_url)
    if (hostName) subParts.push(html`${hostName}`)

    render(html`
      <div class="pdp-wallet-strip">
        <div class="pdp-wallet-strip-main">
          ${badge ? html`<span class="pdp-wallet-badge ${badge.tone}">${badge.label}</span>` : ''}
          <span class="pdp-wallet-mono">${shortAddr(ks.address)}</span>
          <span class="pdp-wallet-sep">·</span>
          <span class="pdp-wallet-mono">${balance}</span>
          <span class="pdp-wallet-sep">·</span>
          <span class="pdp-wallet-mono">${usdfc}</span>
          <a class="pdp-wallet-link" href="/pages/wallet/">Wallet →</a>
        </div>
        ${subParts.length ? html`
          <div class="pdp-wallet-strip-sub">
            ${subParts.map((part, i) => html`${i > 0 ? ' · ' : ''}${part}`)}
          </div>
        ` : ''}
      </div>
    `, host)
  }

  renderKpis() {
    const s = this.summary
    const loading = this.summaryStatus === 'loading' && !s
    const errored = this.summaryStatus === 'error' && !s
    const rate = s?.provingSuccessRate
    const rateStr = loading ? '…' : errored ? '—' : (rate !== undefined && rate !== null
      ? `${rate.toFixed(rate >= 99 ? 2 : 1)}%`
      : '—')
    const faultNote = loading ? 'loading…'
      : errored ? 'summary unavailable'
      : (s?.faultedPeriods30d > 0
        ? `${s.faultedPeriods30d} faulted period${s.faultedPeriods30d === 1 ? '' : 's'}`
        : '0 faulted periods')
    const expenseUsd = this.expenseUsdHint()
    const dataBytes = loading ? '…' : errored ? '—' : formatBytes(s?.dataUnderProofBytes)
    const dataSub = loading ? 'loading…'
      : errored ? 'summary unavailable'
      : `${s?.dataSetCount ?? 0} datasets · ${s?.pieceCount ?? 0} pieces`

    return html`
      <div class="kpi-grid">
        <div class="kpi-card">
          <div class="kpi-label">Data under proof</div>
          <div class="kpi-value">${dataBytes}</div>
          <div class="kpi-sub">${dataSub}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Proving success · 30d</div>
          <div class="kpi-value">${rateStr}</div>
          <div class="kpi-sub">${faultNote}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Net income · 30d</div>
          <div class="kpi-value">${this.financialDisplay('income30dUsdfc')} <span style="font-size:14px">USDFC</span></div>
          <div class="kpi-sub">${this.financialSubline('income')}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">Gas spent · 30d</div>
          <div class="kpi-value">
            ${this.financialDisplay('expense30dFil')}
            <span class="kpi-unit">FIL</span>${expenseUsd ? html`<span class="kpi-usd">${expenseUsd}</span>` : ''}
          </div>
          <div class="kpi-sub">${this.financialSubline('expense')}</div>
        </div>
      </div>
    `
  }

  renderIssues() {
    const total = this.alertTotal
    const groups = groupAlertsByMessage(this.alerts, 5)
    let body
    if (this.alertsStatus === 'loading') {
      body = html`<p class="status-muted">Loading alerts…</p>`
    } else if (this.alertsStatus === 'error') {
      body = html`<p class="status-error">Alerts unavailable — connection or RPC error</p>`
    } else if (total === 0) {
      body = html`<p class="all-clear">All clear — no unacknowledged alerts</p>`
    } else {
      body = html`
        <ul class="issues-list">
          ${groups.map((g) => html`
            <li class="issue-item">
              <div class="issue-msg">
                ${g.message}
                ${g.count > 1 ? html`<span class="issue-count">×${g.count}</span>` : ''}
              </div>
              <div class="issue-meta">
                ${g.alertName}${g.machineName ? html` · ${g.machineName}` : ''}
                · ${formatAlertRange(g.firstAt, g.lastAt, g.count)}
              </div>
            </li>
          `)}
        </ul>
      `
    }
    return html`
      <div class="info-block dashboard-section">
        <div class="block-header">
          <h2>Open issues</h2>
          <a class="view-all" href="/pages/alerts/">${this.alertsStatus === 'ok' && total > 0 ? `${total} active · view all` : 'Alerts'}</a>
        </div>
        ${body}
      </div>
    `
  }

  renderActivityAndChain() {
    return html`
      <div class="split-row dashboard-section">
        <div class="split-col">
          <div class="info-block">
            <div class="block-header">
              <h2>Proving Activity · 30d</h2>
              <div class="chart-legend">
                <span><span class="legend-swatch success"></span>Proved</span>
                <span><span class="legend-swatch failed"></span>Task Failed</span>
                <span><span class="legend-swatch faulted"></span>Datasets Faulted</span>
              </div>
            </div>
            <div class="chart-wrap">
              <canvas id="proving-activity-chart"></canvas>
            </div>
            ${this.renderProvingAlerts()}
            ${!(this.summary?.provingActivity?.some((d) => (d.success ?? 0) + (d.failed ?? 0) + (d.faulted ?? 0) > 0)) ? html`
              <p class="kpi-sub" style="margin-top:6px">No proving activity in the last 30 days</p>
            ` : ''}
          </div>
        </div>
        <div class="split-col">
          <chain-status layout="panel"></chain-status>
        </div>
      </div>
    `
  }

  render() {
    return html`
      <div class="dashboard-section">${this.renderKpis()}</div>
      ${this.renderActivityAndChain()}
      ${this.renderIssues()}
    `
  }
})
