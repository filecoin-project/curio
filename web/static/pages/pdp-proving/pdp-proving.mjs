import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'
import RPCCall from '/lib/jsonrpc.mjs'
import { pollRPC } from '/lib/poll.mjs'
import { formatDate } from '/lib/dateutil.mjs'
import { loadingSpinner, loadingBlock, loadingCssText } from '/lib/loading.mjs'

function formatDuration(seconds) {
  if (seconds == null) return '—'
  const s = Number(seconds)
  if (s <= 0) return 'now'
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  const parts = []
  if (d > 0) parts.push(`${d}d`)
  if (h > 0) parts.push(`${h}h`)
  if (m > 0 || parts.length === 0) parts.push(`${m}m`)
  return parts.join(' ')
}

function isZeroDate(value) {
  if (!value) return true
  const t = new Date(value).getTime()
  return Number.isNaN(t) || t <= 0
}

function formatFailureWhen(f) {
  if (!isZeroDate(f?.workEnd)) {
    return formatDate(f.workEnd)
  }
  if (f?.unrecoverableFailureEpoch != null) {
    return `epoch ${f.unrecoverableFailureEpoch}`
  }
  if (f?.nextProveAttemptAt != null) {
    return `backoff until epoch ${f.nextProveAttemptAt}`
  }
  return '—'
}

customElements.define('pdp-proving', class PdpProving extends LitElement {
  static properties = {
    status: { type: Object },
    timeline: { type: Array },
    failures: { type: Array },
    loadError: { type: String },
  }

  constructor() {
    super()
    this.status = null
    this.timeline = []
    this.failures = []
    this.loadError = null
    this._chart = null
    pollRPC(() => this.load(), 30000)
    this.load()
  }

  disconnectedCallback() {
    super.disconnectedCallback()
    if (this._chart) {
      this._chart.destroy()
      this._chart = null
    }
  }

  async load() {
    try {
      const [status, timeline, failures] = await Promise.all([
        RPCCall('PDPProvingStatus'),
        RPCCall('PDPProvingTimeline24h'),
        RPCCall('PDPProvingFailures'),
      ])
      this.status = status
      this.timeline = timeline ?? []
      this.failures = failures ?? []
      this.loadError = null
      await this.updateComplete
      this.renderChart()
    } catch (e) {
      console.error('PDP proving load failed:', e)
      this.loadError = e.message || String(e)
    }
  }

  renderChart() {
    const canvas = this.renderRoot?.querySelector('#proving-timeline-chart')
    if (!canvas || typeof Chart === 'undefined') return

    const events = this.timeline ?? []
    const now = Date.now()
    const dayAgo = now - 24 * 60 * 60 * 1000

    const successPts = []
    const failPts = []
    for (const ev of events) {
      const t = new Date(ev.workEnd).getTime()
      if (Number.isNaN(t)) continue
      const pt = {
        x: t,
        y: ev.success ? 1 : 0,
        taskId: ev.taskId,
        dataSetId: ev.dataSetId,
        err: ev.err,
      }
      if (ev.success) successPts.push(pt)
      else failPts.push(pt)
    }

    const chartConfig = {
      type: 'scatter',
      data: {
        datasets: [
          {
            label: 'Success',
            data: successPts,
            backgroundColor: 'rgba(63, 185, 80, 0.85)',
            pointRadius: 5,
            pointHoverRadius: 7,
          },
          {
            label: 'Failure',
            data: failPts,
            backgroundColor: 'rgba(248, 81, 73, 0.9)',
            pointRadius: 6,
            pointHoverRadius: 8,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        parsing: false,
        plugins: {
          legend: { display: false },
          tooltip: {
            callbacks: {
              label: (item) => {
                const raw = item.raw || {}
                const kind = item.dataset.label
                const ds = raw.dataSetId ? `dataset ${raw.dataSetId}` : 'dataset ?'
                const task = raw.taskId ? `task ${raw.taskId}` : ''
                const err = raw.err ? ` — ${raw.err}` : ''
                return `${kind}: ${ds} ${task}${err}`
              },
            },
          },
        },
        scales: {
          x: {
            type: 'linear',
            min: dayAgo,
            max: now,
            ticks: {
              color: '#8b949e',
              callback: (v) => {
                const d = new Date(v)
                return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
              },
              maxTicksLimit: 12,
            },
            grid: { color: 'rgba(48, 54, 61, 0.5)' },
            title: { display: true, text: 'Last 24 hours', color: '#8b949e' },
          },
          y: {
            min: -0.5,
            max: 1.5,
            ticks: {
              color: '#8b949e',
              callback: (v) => (v === 1 ? 'OK' : v === 0 ? 'Fail' : ''),
              stepSize: 1,
            },
            grid: { color: 'rgba(48, 54, 61, 0.5)' },
          },
        },
        onClick: (_evt, elements) => {
          if (!elements?.length) return
          const el = elements[0]
          const raw = chartConfig.data.datasets[el.datasetIndex]?.data?.[el.index]
          if (raw?.dataSetId) {
            window.location.href = `/pages/dataset/?id=${raw.dataSetId}`
          } else if (raw?.taskId) {
            window.location.href = `/pages/task/id/?id=${raw.taskId}`
          }
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
    :host { display: block; }
    .top-row {
      display: grid;
      grid-template-columns: 220px 1fr;
      gap: 16px;
      margin-bottom: 24px;
    }
    @media (max-width: 900px) {
      .top-row { grid-template-columns: 1fr; }
    }
    .countdown {
      background: var(--color-bg-subtle, #161b22);
      border: 1px solid var(--color-border-default, #30363d);
      border-radius: 8px;
      padding: 20px;
      display: flex;
      flex-direction: column;
      justify-content: center;
      min-height: 200px;
    }
    .countdown-label {
      font-size: 12px;
      font-weight: 500;
      text-transform: uppercase;
      letter-spacing: 0.04em;
      color: var(--color-text-secondary, #8b949e);
      margin-bottom: 8px;
    }
    .countdown-value {
      font-size: 36px;
      font-weight: 600;
      color: var(--color-text-primary, #e6edf3);
      font-variant-numeric: tabular-nums;
      line-height: 1.1;
    }
    .countdown-meta {
      margin-top: 12px;
      font-size: 13px;
      color: var(--color-text-secondary, #8b949e);
      font-family: ui-monospace, monospace;
    }
    .countdown-meta .meta-row {
      display: flex;
      justify-content: space-between;
      gap: 8px;
      line-height: 1.45;
    }
    .countdown-meta .meta-label {
      color: var(--color-text-secondary, #8b949e);
      font-family: inherit;
      font-size: 12px;
    }
    .countdown-value.warn { color: var(--color-warning-fg, #d29922); }
    .countdown-value.danger { color: var(--color-danger-fg, #f85149); }
    .timeline-panel {
      background: var(--color-bg-subtle, #161b22);
      border: 1px solid var(--color-border-default, #30363d);
      border-radius: 8px;
      padding: 16px 20px;
      min-height: 200px;
    }
    .panel-title {
      font-size: 16px;
      font-weight: 600;
      margin: 0 0 12px;
      padding-bottom: 8px;
      border-bottom: 1px solid var(--color-border-default, #30363d);
    }
    .panel-title-row {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: 12px;
      margin: 0 0 12px;
      padding-bottom: 8px;
      border-bottom: 1px solid var(--color-border-default, #30363d);
    }
    .panel-title-row .panel-title {
      margin: 0;
      padding: 0;
      border: none;
    }
    .title-stats {
      display: flex;
      gap: 12px;
      flex-shrink: 0;
      font-size: 12px;
      font-weight: 500;
      color: var(--color-text-secondary, #8b949e);
    }
    .chart-wrap { height: 220px; position: relative; }
    .empty-chart {
      height: 220px;
      display: flex;
      align-items: center;
      justify-content: center;
      color: var(--color-text-secondary, #8b949e);
    }
    .stats-row {
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
      margin-top: 8px;
      font-size: 12px;
      color: var(--color-text-secondary, #8b949e);
    }
    .failures-panel {
      background: var(--color-bg-subtle, #161b22);
      border: 1px solid var(--color-border-default, #30363d);
      border-radius: 8px;
      padding: 16px 20px;
    }
    .hint {
      font-size: 13px;
      color: var(--color-text-secondary, #8b949e);
      margin: 0 0 12px;
    }
    table { width: 100%; margin-bottom: 0; }
    .mono { font-family: ui-monospace, monospace; font-size: 13px; }
    .badge {
      display: inline-block;
      padding: 2px 8px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 500;
    }
    .badge-danger {
      color: var(--color-danger-fg, #f85149);
      background: rgba(248, 81, 73, 0.15);
    }
    .badge-warn {
      color: var(--color-warning-fg, #d29922);
      background: rgba(210, 153, 34, 0.15);
    }
    .err { color: var(--color-danger-fg, #f85149); max-width: 420px; word-break: break-word; }
    .load-error { color: var(--color-danger-fg, #f85149); margin-bottom: 12px; }
  `

  render() {
    const st = this.status
    const stillLoading = !st && !this.loadError
    const secs = st?.secondsUntilNextSession
    const state = st?.sessionState || 'idle'
    const successCount = (this.timeline ?? []).filter((e) => e.success).length
    const failCount = (this.timeline ?? []).filter((e) => !e.success).length
    const hasTimeline = (this.timeline ?? []).length > 0

    let label = 'Next proving window'
    let value = formatDuration(secs)
    let valueClass = 'countdown-value'
    if (stillLoading) {
      value = loadingSpinner({ size: 'md' })
    } else if (state === 'in-window') {
      label = 'Proving window open'
      value = 'now'
    } else if (state === 'overdue') {
      label = 'Proving overdue'
      value = 'attention'
      valueClass += ' danger'
    } else if (state === 'idle') {
      label = 'Next proving window'
      value = '—'
    } else if (state === 'upcoming') {
      valueClass += secs != null && secs < 3600 ? ' warn' : ''
    }

    return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous" />
      <link rel="stylesheet" href="/ux/main.css" />
      <link rel="stylesheet" href="/ux/dark-table.css" />

      <style>${loadingCssText}</style>

      ${this.loadError ? html`<p class="load-error">${this.loadError}</p>` : ''}
      ${stillLoading ? loadingBlock('Loading proving status…') : ''}

      <div class="top-row" style="${stillLoading ? 'opacity:0.55;pointer-events:none' : ''}">
        <div class="countdown">
          <div class="countdown-label">${label}</div>
          <div class="${valueClass}">${value}</div>
          <div class="countdown-meta">
            ${st?.nextProveAtEpoch != null ? html`
              <div class="meta-row"><span class="meta-label">opens</span><span>epoch ${st.nextProveAtEpoch}</span></div>
            ` : ''}
            ${st?.nextDeadlineEpoch != null ? html`
              <div class="meta-row"><span class="meta-label">closes</span><span>epoch ${st.nextDeadlineEpoch}</span></div>
            ` : ''}
            ${st?.headEpoch != null ? html`
              <div class="meta-row"><span class="meta-label">chain head</span><span>epoch ${st.headEpoch}</span></div>
            ` : ''}
            ${state === 'overdue' && st?.nextProveAtEpoch == null ? html`
              <div class="meta-row"><span class="meta-label">note</span><span>no upcoming window scheduled</span></div>
            ` : ''}
          </div>
          <div class="stats-row">
            <span>${st?.activeDataSetCount ?? 0} active</span>
            <span>${st?.inWindowCount ?? 0} in window</span>
            <span style="${(st?.overdueCount ?? 0) > 0 ? 'color: var(--color-danger-fg, #f85149)' : ''}">${st?.overdueCount ?? 0} overdue</span>
          </div>
        </div>

        <div class="timeline-panel">
          <div class="panel-title-row">
            <h2 class="panel-title">Proving window · last 24h</h2>
            <div class="title-stats">
              <span style="color: var(--color-success-fg, #3fb950)">${successCount} success</span>
              <span style="color: var(--color-danger-fg, #f85149)">${failCount} failed</span>
            </div>
          </div>
          ${hasTimeline
            ? html`<div class="chart-wrap"><canvas id="proving-timeline-chart"></canvas></div>`
            : html`<div class="empty-chart">No prove tasks in the last 24 hours</div>`}
        </div>
      </div>

      <div class="failures-panel">
        <h2 class="panel-title">Proving failures</h2>
        <p class="hint">Investigate these first — link through to the dataset or failed task to see what needs fixing.</p>
        ${this.renderFailures()}
      </div>
    `
  }

  renderFailures() {
    const failures = this.failures ?? []
    if (failures.length === 0) {
      return html`<p class="hint" style="margin:0">No recent prove failures or unhealthy datasets.</p>`
    }
    return html`
      <table class="table table-dark table-striped table-sm">
        <thead>
          <tr>
            <th>Reason</th>
            <th>Dataset</th>
            <th>Task</th>
            <th>Failures</th>
            <th>When</th>
            <th>Error</th>
          </tr>
        </thead>
        <tbody>
          ${failures.map((f) => html`
            <tr>
              <td>
                <span class="badge ${f.unrecoverableFailureEpoch ? 'badge-danger' : 'badge-warn'}">${f.reason || 'attention'}</span>
              </td>
              <td class="mono">
                ${f.dataSetId
                  ? html`<a href="/pages/dataset/?id=${f.dataSetId}">${f.dataSetId}</a>`
                  : '—'}
              </td>
              <td class="mono">
                ${f.taskId
                  ? html`<a href="/pages/task/id/?id=${f.taskId}">${f.taskId}</a>`
                  : '—'}
              </td>
              <td class="mono">${f.consecutiveProveFailures ?? 0}</td>
              <td class="mono">${formatFailureWhen(f)}</td>
              <td class="err">${f.err || '—'}</td>
            </tr>
          `)}
        </tbody>
      </table>
    `
  }

  updated(changed) {
    if (changed.has('timeline')) {
      this.renderChart()
    }
  }
})
