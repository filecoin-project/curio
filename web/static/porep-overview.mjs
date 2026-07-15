import { LitElement, html, css, render } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'
import RPCCall from '/lib/jsonrpc.mjs'
import { pollRPC } from '/lib/poll.mjs'
import { timeSince } from '/lib/dateutil.mjs'

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
    if (!g.machineName && a.MachineName) g.machineName = a.MachineName
  }
  return [...byMsg.values()]
    .sort((a, b) => new Date(b.lastAt) - new Date(a.lastAt))
    .slice(0, maxGroups)
}

function formatAlertRange(firstAt, lastAt, count) {
  const first = timeSince(new Date(firstAt))
  const last = timeSince(new Date(lastAt))
  if (count <= 1 || firstAt === lastAt) return last
  return `last ${last} · first ${first}`
}

/** Parse FIL display strings like "1.23 FIL" / "0 FIL" into a number (atto omitted). */
function parseFilRough(s) {
  if (s == null || s === '') return null
  const m = String(s).replace(/,/g, '').match(/-?[\d.]+/)
  if (!m) return null
  const n = Number(m[0])
  return Number.isFinite(n) ? n : null
}

/** Parse Go time.Duration strings like "5s", "2m0s", "1h2m3s" into milliseconds. */
function parseGoDuration(s) {
  if (s == null || s === '') return null
  const re = /(-?\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g
  let ms = 0
  let matched = false
  let m
  while ((m = re.exec(String(s))) !== null) {
    matched = true
    const n = Number(m[1])
    switch (m[2]) {
      case 'ns': ms += n / 1e6; break
      case 'us':
      case 'µs': ms += n / 1e3; break
      case 'ms': ms += n; break
      case 's': ms += n * 1000; break
      case 'm': ms += n * 60_000; break
      case 'h': ms += n * 3_600_000; break
    }
  }
  return matched ? ms : null
}

/** Heartbeats are ~1m; treat >2m since last_contact as offline while the row still exists. */
const MACHINE_OFFLINE_AFTER_MS = 2 * 60_000

function isMachineOffline(m) {
  const since = parseGoDuration(m?.SinceContact)
  return since != null && since >= MACHINE_OFFLINE_AFTER_MS
}

function sumPipeline(rows) {
  const zero = {
    sdr: 0, trees: 0, precommit: 0, waitSeed: 0, porep: 0, commit: 0, done: 0, failed: 0, inFlight: 0,
  }
  for (const r of rows ?? []) {
    zero.sdr += r.CountSDR ?? 0
    zero.trees += r.CountTrees ?? 0
    zero.precommit += r.CountPrecommitMsg ?? 0
    zero.waitSeed += r.CountWaitSeed ?? 0
    zero.porep += r.CountPoRep ?? 0
    zero.commit += r.CountCommitMsg ?? 0
    zero.done += r.CountDone ?? 0
    zero.failed += r.CountFailed ?? 0
  }
  zero.inFlight = zero.sdr + zero.trees + zero.precommit + zero.waitSeed + zero.porep + zero.commit
  return zero
}

function postStats(actors) {
  let currentPending = 0
  let currentTotal = 0
  let faulted = 0
  let recovering = 0
  for (const a of actors ?? []) {
    for (const d of a.Deadlines ?? []) {
      if (d.Count) {
        faulted += d.Count.Fault ?? 0
        recovering += d.Count.Recovering ?? 0
      }
      if (d.Current && d.PartitionCount > 0) {
        currentTotal += d.PartitionCount
        if (!d.PartitionsProven) {
          currentPending += Math.max(0, (d.PartitionCount ?? 0) - (d.PartitionsPosted ?? 0))
        }
      }
    }
  }
  return { currentPending, currentTotal, faulted, recovering }
}

customElements.define('porep-overview', class PorepOverview extends LitElement {
  static properties = {
    actors: { type: Array },
    actorsStatus: { type: String },
    pipeline: { type: Array },
    pipelineStatus: { type: String },
    market: { type: Array },
    msgs: { type: Object },
    chain: { type: Object },
    alerts: { type: Array },
    alertTotal: { type: Number },
    alertsStatus: { type: String },
    dealsPending: { type: Number },
    machines: { type: Array },
    machinesStatus: { type: String },
  }

  constructor() {
    super()
    this.actors = []
    this.actorsStatus = 'loading'
    this.pipeline = []
    this.pipelineStatus = 'loading'
    this.market = []
    this.msgs = null
    this.chain = null
    this.alerts = []
    this.alertTotal = 0
    this.alertsStatus = 'loading'
    this.dealsPending = null
    this.machines = []
    this.machinesStatus = 'loading'
    this._loadPromise = null
    pollRPC(() => this.load(), 30000)
    this.load()
  }

  connectedCallback() {
    super.connectedCallback()
    this.renderAside()
  }

  disconnectedCallback() {
    super.disconnectedCallback()
    const host = document.getElementById('porep-status-aside')
    if (host) render(html``, host)
  }

  async load() {
    if (this._loadPromise) return this._loadPromise
    this._loadPromise = this._doLoad().finally(() => {
      this._loadPromise = null
    })
    return this._loadPromise
  }

  async _doLoad() {
    const actorsP = RPCCall('ActorSummary').then((actors) => {
      this.actors = actors ?? []
      this.actorsStatus = 'ok'
      this.requestUpdate()
      this.renderAside()
    }).catch((e) => {
      console.warn('ActorSummary failed:', e)
      if (this.actorsStatus !== 'ok') this.actorsStatus = 'error'
      this.requestUpdate()
      this.renderAside()
    })

    const pipeP = RPCCall('PorepPipelineSummary').then((rows) => {
      this.pipeline = rows ?? []
      this.pipelineStatus = 'ok'
      this.requestUpdate()
      this.renderAside()
    }).catch((e) => {
      console.warn('PorepPipelineSummary failed:', e)
      if (this.pipelineStatus !== 'ok') this.pipelineStatus = 'error'
      this.requestUpdate()
      this.renderAside()
    })

    const alertsP = RPCCall('AlertHistoryListPaginated', [100, 0, false]).then((alertResult) => {
      this.alerts = alertResult?.Alerts ?? []
      this.alertTotal = alertResult?.Total ?? 0
      this.alertsStatus = 'ok'
      this.requestUpdate()
      this.renderAside()
    }).catch((e) => {
      console.warn('AlertHistoryListPaginated failed:', e)
      if (this.alertsStatus !== 'ok') this.alertsStatus = 'error'
      this.requestUpdate()
      this.renderAside()
    })

    const msgsP = RPCCall('MessageQueueSummary').then((msgs) => {
      this.msgs = msgs
      this.requestUpdate()
      this.renderAside()
    }).catch((e) => {
      console.warn('MessageQueueSummary failed:', e)
    })

    const chainP = RPCCall('ChainStatus').then((chain) => {
      this.chain = chain
      this.requestUpdate()
      this.renderAside()
    }).catch((e) => {
      console.warn('ChainStatus failed:', e)
    })

    const marketP = RPCCall('MarketBalance').then((market) => {
      this.market = market ?? []
      this.requestUpdate()
      this.renderAside()
    }).catch((e) => {
      console.warn('MarketBalance failed:', e)
    })

    const dealsP = RPCCall('DealsPending').then((deals) => {
      this.dealsPending = Array.isArray(deals) ? deals.length : 0
      this.requestUpdate()
      this.renderAside()
    }).catch((e) => {
      console.warn('DealsPending failed:', e)
      this.dealsPending = null
    })

    const machinesP = RPCCall('ClusterMachines').then((machines) => {
      this.machines = machines ?? []
      this.machinesStatus = 'ok'
      this.requestUpdate()
    }).catch((e) => {
      console.warn('ClusterMachines failed:', e)
      if (this.machinesStatus !== 'ok') this.machinesStatus = 'error'
      this.requestUpdate()
    })

    await Promise.all([actorsP, pipeP, alertsP, msgsP, chainP, marketP, dealsP, machinesP])
  }

  updated() {
    this.renderAside()
    const el = this.renderRoot?.querySelector('actor-summary')
    if (el && this.actorsStatus === 'ok' && Array.isArray(this.actors)) {
      el.data = this.actors
    }
  }

  health() {
    const pipe = sumPipeline(this.pipeline)
    const post = postStats(this.actors)
    const filPending = this.msgs?.filPendingCount ?? 0
    const lowWallets = (this.actors ?? []).filter((a) => {
      const n = parseFilRough(a.ActorAvailable)
      return n !== null && n <= 0
    })
    const chainOk = !this.chain || this.chain.syncStatus === 'ok'
    const machines = this.machines ?? []
    const offline = machines.filter(isMachineOffline).length
    const cordoned = machines.filter((m) => m.Unschedulable).length
    const restarting = machines.filter((m) => m.Restarting).length
    return {
      pipe,
      post,
      filPending,
      ethPending: this.msgs?.ethPendingCount ?? 0,
      lowWallets,
      walletsOk: this.actorsStatus === 'ok' && lowWallets.length === 0,
      postOk: this.actorsStatus === 'ok' && post.currentPending === 0 && post.faulted === 0,
      pipelineOk: this.pipelineStatus === 'ok' && pipe.failed === 0,
      msgsOk: this.msgs != null && filPending === 0,
      alertsOk: this.alertsStatus === 'ok' && this.alertTotal === 0,
      chainOk,
      machineCount: machines.length,
      offline,
      cordoned,
      restarting,
      machinesOk: this.machinesStatus === 'ok' && offline === 0,
    }
  }

  toneIcon(ok, loading) {
    if (loading) return html`<span class="tone muted">…</span>`
    if (ok) {
      return html`<span class="tone ok" title="OK">✓</span>`
    }
    return html`<span class="tone bad" title="Attention">●</span>`
  }

  renderAside() {
    const host = document.getElementById('porep-status-aside')
    if (!host) return

    const h = this.health()
    const actorsLoading = this.actorsStatus === 'loading'
    const availParts = (this.actors ?? []).map((a) => {
      const addr = a.Address || ''
      const short = addr.length > 12 ? `${addr.slice(0, 4)}…${addr.slice(-4)}` : addr
      return html`<a class="aside-link" href="/pages/actor/?id=${a.Address}">${short}</a><span class="mono">${a.ActorAvailable ?? '—'}</span>`
    })

    const market = (this.market ?? []).map((m) => html`
      <span class="mono">${m.miner}: ${m.market_balance ?? '—'}</span>
    `)

    render(html`
      <div class="porep-aside-strip">
        <div class="porep-aside-main">
          ${this.toneIcon(h.walletsOk, actorsLoading)}
          <span class="aside-label">Available</span>
          ${actorsLoading ? html`<span class="mono">…</span>` : availParts.length
            ? availParts.map((p, i) => html`${i > 0 ? html`<span class="sep">·</span>` : ''}${p}`)
            : html`<span class="mono">—</span>`}
          <a class="aside-more" href="/pages/wallet/">Wallets →</a>
        </div>
        ${market.length ? html`
          <div class="porep-aside-sub">
            Escrow ${market.map((m, i) => html`${i > 0 ? ' · ' : ''}${m}`)}
          </div>
        ` : ''}
      </div>
    `, host)
  }

  renderDataStrip() {
    const h = this.health()
    const actorsLoading = this.actorsStatus === 'loading'
    const pipeLoading = this.pipelineStatus === 'loading'
    const alertsLoading = this.alertsStatus === 'loading'
    const qap = (this.actors ?? []).map((a) => a.QualityAdjustedPower).filter(Boolean)
    const minerCount = (this.actors ?? []).length

    return html`
      <div class="data-strip">
        <div class="cell">
          <span class="cell-label">Capacity</span>
          <span class="cell-value">
            ${actorsLoading ? '…' : html`${minerCount} SP · ${qap[0] ?? '—'}${qap.length > 1 ? html` +${qap.length - 1}` : ''}`}
          </span>
        </div>
        <div class="cell">
          <span class="cell-label">Machines</span>
          <span class="cell-value">
            ${this.toneIcon(h.machinesOk, this.machinesStatus === 'loading')}
            ${this.machinesStatus === 'loading' ? '…' : html`
              <a href="/pages/tasks/">${h.machineCount}</a>
              ${h.offline > 0
                ? html` · <span class="emph">${h.offline} offline</span>`
                : html` online`}
            `}
          </span>
        </div>
        <div class="cell">
          <span class="cell-label">WindowPoSt</span>
          <span class="cell-value">
            ${this.toneIcon(h.postOk, actorsLoading)}
            ${actorsLoading ? '…' : html`
              ${h.post.currentPending}/${h.post.currentTotal || 0} pending
              ${h.post.faulted > 0 ? html` · <span class="emph">${h.post.faulted} fault</span>` : ''}
              ${h.post.recovering > 0 ? html` · ${h.post.recovering} rec` : ''}
            `}
          </span>
        </div>
        <div class="cell">
          <span class="cell-label">Pipeline</span>
          <span class="cell-value">
            ${this.toneIcon(h.pipelineOk, pipeLoading)}
            ${pipeLoading ? '…' : html`
              ${h.pipe.inFlight} in flight ·
              ${h.pipe.failed > 0
                ? html`<a class="emph" href="/pages/pipeline_porep/">${h.pipe.failed} failed</a>`
                : html`${h.pipe.failed} failed`}
            `}
          </span>
        </div>
        <div class="cell">
          <span class="cell-label">Msgs</span>
          <span class="cell-value">
            ${!this.msgs ? '…' : html`
              ${h.filPending === 0 && h.ethPending === 0 ? this.toneIcon(true, false) : this.toneIcon(h.filPending < 50, false)}
              <a href="/pages/chain/">${h.filPending} FIL</a>
              ${h.ethPending > 0 ? html` · ${h.ethPending} ETH` : ''}
            `}
          </span>
        </div>
        <div class="cell">
          <span class="cell-label">Deals</span>
          <span class="cell-value">
            ${this.dealsPending == null ? '…' : html`
              ${this.dealsPending === 0 ? this.toneIcon(true, false) : ''}
              <a href="/pages/market/">${this.dealsPending} pending</a>
            `}
          </span>
        </div>
        <div class="cell">
          <span class="cell-label">Alerts</span>
          <span class="cell-value">
            ${this.toneIcon(h.alertsOk, alertsLoading)}
            ${alertsLoading ? '…' : html`<a href="/pages/alerts/">${this.alertTotal}</a>`}
          </span>
        </div>
        <div class="cell">
          <span class="cell-label">Chain</span>
          <span class="cell-value">
            ${this.toneIcon(h.chainOk, !this.chain)}
            ${!this.chain ? '…' : html`
              <a href="/pages/chain/">${this.chain.syncStatus ?? '—'}</a>
              <span class="mono dim">@${this.chain.epoch ?? '—'}</span>
            `}
          </span>
        </div>
      </div>
    `
  }

  renderPipelineTable() {
    const rows = this.pipeline ?? []
    const totals = sumPipeline(rows)
    const loading = this.pipelineStatus === 'loading'
    const errored = this.pipelineStatus === 'error'

    return html`
      <div class="info-block dashboard-section">
        <div class="block-header">
          <h2>PoRep pipeline</h2>
          <a class="view-all" href="/pages/pipeline_porep/">Detail →</a>
        </div>
        ${loading ? html`<p class="status-muted">Loading…</p>` : ''}
        ${errored && !rows.length ? html`<p class="status-error">Pipeline unavailable</p>` : ''}
        ${!loading && rows.length === 0 && !errored ? html`
          <p class="all-clear">${this.toneIcon(true, false)} No sectors in pipeline</p>
        ` : ''}
        ${rows.length ? html`
          <table class="tight-table">
            <thead>
              <tr>
                <th>SP</th>
                <th>SDR</th>
                <th>Trees</th>
                <th>Precommit</th>
                <th>Seed</th>
                <th>PoRep</th>
                <th>Commit</th>
                <th>Done</th>
                <th>Failed</th>
              </tr>
            </thead>
            <tbody>
              ${rows.map((item) => html`
                <tr>
                  <td class="mono"><a href="/pages/pipeline_porep/">${item.Actor}</a></td>
                  <td class=${item.CountSDR ? 'hot' : ''}>${item.CountSDR}</td>
                  <td class=${item.CountTrees ? 'hot' : ''}>${item.CountTrees}</td>
                  <td class=${item.CountPrecommitMsg ? 'hot' : ''}>${item.CountPrecommitMsg}</td>
                  <td class=${item.CountWaitSeed ? 'hot' : ''}>${item.CountWaitSeed}</td>
                  <td class=${item.CountPoRep ? 'hot' : ''}>${item.CountPoRep}</td>
                  <td class=${item.CountCommitMsg ? 'hot' : ''}>${item.CountCommitMsg}</td>
                  <td>${item.CountDone}</td>
                  <td class=${item.CountFailed ? 'bad' : 'ok-num'}>${item.CountFailed}</td>
                </tr>
              `)}
              ${rows.length > 1 ? html`
                <tr class="total-row">
                  <td>Total</td>
                  <td>${totals.sdr}</td>
                  <td>${totals.trees}</td>
                  <td>${totals.precommit}</td>
                  <td>${totals.waitSeed}</td>
                  <td>${totals.porep}</td>
                  <td>${totals.commit}</td>
                  <td>${totals.done}</td>
                  <td class=${totals.failed ? 'bad' : 'ok-num'}>${totals.failed}</td>
                </tr>
              ` : ''}
            </tbody>
          </table>
        ` : ''}
      </div>
    `
  }

  renderActors() {
    return html`
      <div class="info-block dashboard-section">
        <div class="block-header">
          <h2>Miners &amp; deadlines</h2>
          <a class="view-all" href="/sector/">Sectors →</a>
        </div>
        <actor-summary></actor-summary>
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
      body = html`<p class="status-error">Alerts unavailable</p>`
    } else if (total === 0) {
      body = html`<p class="all-clear">${this.toneIcon(true, false)} 0 open alerts</p>`
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

  renderMachines() {
    const machines = this.machines ?? []
    let body
    if (this.machinesStatus === 'loading') {
      body = html`<p class="status-muted">Loading machines…</p>`
    } else if (this.machinesStatus === 'error') {
      body = html`<p class="status-error">Machines unavailable</p>`
    } else if (machines.length === 0) {
      body = html`<p class="status-muted">No machines reported</p>`
    } else {
      body = html`
        <ul class="machine-list">
          ${machines.map((m) => {
            const offline = isMachineOffline(m)
            return html`
              <li class="machine-item">
                <a class="machine-name" href="/pages/node_info/?id=${m.ID}">${m.Name || m.Address || m.ID}</a>
                <span class="machine-meta">
                  ${offline
                    ? html`<span class="emph">offline</span>`
                    : m.Unschedulable
                      ? html`<span class="emph">${m.RunningTasks > 0 ? `cordoned (${m.RunningTasks} tasks)` : 'cordoned'}</span>`
                      : html`<span class="ok-text">online</span>`}
                  ${m.Restarting ? html` · <span class="emph">restarting</span>` : ''}
                  · ${m.SinceContact ?? '—'}
                </span>
              </li>
            `
          })}
        </ul>
      `
    }
    return html`
      <div class="info-block dashboard-section">
        <div class="block-header">
          <h2>Machines</h2>
          <a class="view-all" href="/pages/tasks/">Tasks →</a>
        </div>
        ${body}
      </div>
    `
  }

  static styles = css`
    :host { display: block; }
    .dashboard-section { margin-bottom: 14px; }
    .dashboard-cols {
      display: grid;
      grid-template-columns: 1fr;
      gap: 14px;
      margin-bottom: 14px;
    }
    .dashboard-cols > .dashboard-section { margin-bottom: 0; }
    @media (min-width: 900px) {
      .dashboard-cols { grid-template-columns: 1fr 1fr; }
    }
    .block-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      margin-bottom: 6px;
    }
    .block-header h2 {
      margin: 0;
      font-size: 15px;
      font-weight: 600;
      color: var(--color-text-primary, #e6edf3);
    }
    .view-all {
      font-size: 12px;
      color: var(--color-accent-fg, #4493f8);
      text-decoration: none;
      white-space: nowrap;
    }
    .view-all:hover { text-decoration: underline; }

    .data-strip {
      display: flex;
      flex-wrap: wrap;
      gap: 2px 0;
      margin-bottom: 14px;
      background: var(--color-bg-subtle, #161b22);
      border: 1px solid var(--color-border-default, #30363d);
      border-radius: var(--radius-lg, 8px);
      overflow: hidden;
    }
    .cell {
      flex: 1 1 120px;
      min-width: 110px;
      padding: 6px 10px;
      border-right: 1px solid var(--color-border-default, #30363d);
      box-sizing: border-box;
    }
    .cell:last-child { border-right: none; }
    .cell-label {
      display: block;
      font-size: 10px;
      font-weight: 600;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--color-text-secondary, #8b949e);
      margin-bottom: 2px;
    }
    .cell-value {
      font-size: 13px;
      font-weight: 500;
      color: var(--color-text-primary, #e6edf3);
      line-height: 1.3;
    }
    .cell-value a {
      color: inherit;
      text-decoration: none;
    }
    .cell-value a:hover { text-decoration: underline; color: var(--color-accent-fg, #4493f8); }
    .mono {
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 12px;
    }
    .dim { color: var(--color-text-secondary, #8b949e); margin-left: 4px; }
    .emph { color: var(--color-danger-fg, #f85149); font-weight: 600; }
    .tone {
      display: inline-block;
      width: 1em;
      text-align: center;
      margin-right: 3px;
      font-weight: 700;
    }
    .tone.ok { color: var(--color-success-fg, #3fb950); }
    .tone.bad { color: var(--color-danger-fg, #f85149); font-size: 10px; vertical-align: middle; }
    .tone.muted { color: var(--color-text-secondary, #8b949e); }

    .tight-table {
      width: 100%;
      border-collapse: collapse;
      font-size: 12px;
    }
    .tight-table th, .tight-table td {
      padding: 4px 8px;
      text-align: right;
      border-bottom: 1px solid var(--color-border-default, #30363d);
    }
    .tight-table th:first-child, .tight-table td:first-child { text-align: left; }
    .tight-table th {
      font-size: 10px;
      font-weight: 600;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--color-text-secondary, #8b949e);
    }
    .tight-table a { color: var(--color-accent-fg, #4493f8); text-decoration: none; }
    .tight-table a:hover { text-decoration: underline; }
    .tight-table .hot { color: var(--color-success-fg, #3fb950); }
    .tight-table .bad { color: var(--color-danger-fg, #f85149); font-weight: 600; }
    .tight-table .ok-num { color: var(--color-success-fg, #3fb950); }
    .total-row td { font-weight: 600; border-top: 1px solid var(--color-border-default, #30363d); }

    .status-muted, .all-clear {
      margin: 0;
      font-size: 13px;
      color: var(--color-text-secondary, #8b949e);
    }
    .all-clear { color: var(--color-success-fg, #3fb950); }
    .status-error {
      margin: 0;
      font-size: 13px;
      color: var(--color-danger-fg, #f85149);
    }
    .issues-list {
      list-style: none;
      margin: 0;
      padding: 0;
    }
    .issue-item {
      padding: 6px 0;
      border-bottom: 1px solid var(--color-border-default, #30363d);
    }
    .issue-item:last-child { border-bottom: none; }
    .issue-msg {
      font-size: 13px;
      color: var(--color-text-primary, #e6edf3);
      word-break: break-word;
    }
    .issue-count {
      margin-left: 6px;
      font-size: 11px;
      color: var(--color-warning-fg, #d29922);
    }
    .issue-meta {
      margin-top: 2px;
      font-size: 11px;
      color: var(--color-text-secondary, #8b949e);
    }

    .machine-list {
      list-style: none;
      margin: 0;
      padding: 0;
    }
    .machine-item {
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      gap: 4px 10px;
      padding: 5px 0;
      border-bottom: 1px solid var(--color-border-default, #30363d);
      font-size: 13px;
    }
    .machine-item:last-child { border-bottom: none; }
    .machine-name {
      color: var(--color-accent-fg, #4493f8);
      text-decoration: none;
      font-weight: 500;
    }
    .machine-name:hover { text-decoration: underline; }
    .machine-meta {
      font-size: 11px;
      color: var(--color-text-secondary, #8b949e);
    }
    .ok-text { color: var(--color-success-fg, #3fb950); }
  `

  render() {
    return html`
      ${this.renderDataStrip()}
      ${this.renderActors()}
      ${this.renderPipelineTable()}
      <div class="dashboard-cols">
        ${this.renderMachines()}
        ${this.renderIssues()}
      </div>
    `
  }
})
