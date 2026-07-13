import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'
import RPCCall from '/lib/jsonrpc.mjs'
import { timeSince } from '/lib/dateutil.mjs'

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

customElements.define('pdp-datasets-list', class PdpDatasetsList extends LitElement {
  static properties = {
    items: { type: Array },
    total: { type: Number },
    limit: { type: Number },
    offset: { type: Number },
    filter: { type: String },
    filterInput: { type: String },
    loadError: { type: String },
    loading: { type: Boolean },
  }

  constructor() {
    super()
    this.items = []
    this.total = 0
    this.limit = 25
    this.offset = 0
    this.filter = ''
    this.filterInput = ''
    this.loadError = null
    this.loading = false

    const params = new URLSearchParams(window.location.search)
    if (params.get('q')) {
      this.filter = params.get('q')
      this.filterInput = this.filter
    }
    this.loadData()
  }

  createRenderRoot() {
    return this
  }

  async loadData() {
    this.loading = true
    try {
      const result = await RPCCall('PDPDataSetList', [this.limit, this.offset, this.filter || ''])
      this.items = result?.items ?? []
      this.total = result?.total ?? 0
      this.loadError = null
    } catch (e) {
      console.error('Failed to load datasets:', e)
      this.loadError = e.message || String(e)
      this.items = []
      this.total = 0
    } finally {
      this.loading = false
      this.requestUpdate()
    }
  }

  applySearch(e) {
    e?.preventDefault?.()
    this.filter = (this.filterInput || '').trim()
    this.offset = 0
    const url = new URL(window.location.href)
    if (this.filter) url.searchParams.set('q', this.filter)
    else url.searchParams.delete('q')
    window.history.replaceState({}, '', url)
    this.loadData()
  }

  clearSearch() {
    this.filterInput = ''
    this.filter = ''
    this.offset = 0
    const url = new URL(window.location.href)
    url.searchParams.delete('q')
    window.history.replaceState({}, '', url)
    this.loadData()
  }

  nextPage() {
    if (this.offset + this.limit >= this.total) return
    this.offset += this.limit
    this.loadData()
  }

  prevPage() {
    this.offset = Math.max(0, this.offset - this.limit)
    this.loadData()
  }

  static styles = css``

  render() {
    const from = this.total === 0 ? 0 : this.offset + 1
    const to = Math.min(this.offset + this.limit, this.total)

    return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous" />
      <link rel="stylesheet" href="/ux/main.css" />
      <link rel="stylesheet" href="/ux/dark-table.css" />

      <style>
        .datasets-search { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 16px; align-items: center; }
        .datasets-search input {
          min-width: 280px; flex: 1 1 280px;
          background: var(--color-bg-elevated, #21262d);
          border: 1px solid var(--color-border-default, #30363d);
          color: var(--color-text-primary, #e6edf3);
          border-radius: 6px; padding: 8px 12px;
        }
        .status-ok { color: var(--color-success-fg, #3fb950); }
        .status-warn { color: var(--color-warning-fg, #d29922); }
        .status-bad { color: var(--color-danger-fg, #f85149); }
        .status-muted { color: var(--color-text-secondary, #8b949e); }
        .mono { font-family: ui-monospace, monospace; font-size: 13px; }
        .pager { display: flex; gap: 12px; align-items: center; margin-top: 12px; }
        .hint { color: var(--color-text-secondary, #8b949e); font-size: 13px; margin-bottom: 12px; }
        .load-error { color: var(--color-danger-fg, #f85149); }
      </style>

      <p class="hint">Search by dataset ID or payer wallet (0x…).</p>

      <form class="datasets-search" @submit=${(e) => this.applySearch(e)}>
        <input
          type="search"
          placeholder="Dataset ID or wallet address"
          .value=${this.filterInput}
          @input=${(e) => { this.filterInput = e.target.value }}
        />
        <button type="submit" class="btn btn-primary btn-sm">Search</button>
        ${this.filter ? html`<button type="button" class="btn btn-secondary btn-sm" @click=${() => this.clearSearch()}>Clear</button>` : ''}
      </form>

      ${this.loadError ? html`<p class="load-error">${this.loadError}</p>` : ''}
      ${this.loading ? html`<p class="hint">Loading…</p>` : ''}

      ${!this.loading && this.items.length === 0
        ? html`<p class="hint">No datasets found.</p>`
        : html`
          <table class="table table-dark table-striped table-sm">
            <thead>
              <tr>
                <th>Dataset</th>
                <th>Objects in store</th>
                <th>Size</th>
                <th>Proving</th>
                <th>First upload</th>
              </tr>
            </thead>
            <tbody>
              ${this.items.map((ds) => html`
                <tr>
                  <td class="mono"><a href="/pages/dataset/?id=${ds.id}">${ds.id}</a></td>
                  <td class="mono">${ds.objectCount ?? 0}</td>
                  <td class="mono">${formatBytes(ds.sizeBytes)}</td>
                  <td class="status-${statusTone(ds.provingStatus)}">${ds.provingStatus || '—'}</td>
                  <td>${ds.firstUploadAt ? timeSince(new Date(ds.firstUploadAt)) + ' ago' : '—'}</td>
                </tr>
              `)}
            </tbody>
          </table>
          <div class="pager">
            <button class="btn btn-secondary btn-sm" ?disabled=${this.offset <= 0} @click=${() => this.prevPage()}>Prev</button>
            <span class="hint" style="margin:0">${from}–${to} of ${this.total}</span>
            <button class="btn btn-secondary btn-sm" ?disabled=${this.offset + this.limit >= this.total} @click=${() => this.nextPage()}>Next</button>
          </div>
        `}
    `
  }
})
