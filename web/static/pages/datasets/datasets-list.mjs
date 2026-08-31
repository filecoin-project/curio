import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'
import RPCCall from '/lib/jsonrpc.mjs'
import { relativePhrase } from '/lib/dateutil.mjs'
import { loadingBlock, loadingSpinner, loadingCssText } from '/lib/loading.mjs'

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
      return 'bad'
    case 'in-window':
      return 'warn'
    case 'scheduled':
      return 'ok'
    default:
      return 'muted'
  }
}

const SEARCH_HINT = 'Search by dataset ID, payer wallet (0x…), PieceCID (baf…), or create tx hash.'

function classifySearch(q) {
  const s = (q || '').trim()
  if (!s) return { kind: 'empty' }
  if (/^\d+$/.test(s)) return { kind: 'dataset_id', value: s }
  if (/^0x[0-9a-fA-F]{64}$/.test(s)) return { kind: 'tx_hash', value: s }
  if (/^0x[0-9a-fA-F]{40}$/.test(s)) return { kind: 'wallet', value: s }
  // PieceCID / CIDv1 (bafk…, baga…, bafy…, etc.)
  if (/^b[a-z2-7]{10,}$/i.test(s)) return { kind: 'piece_cid', value: s }
  return { kind: 'unknown', value: s }
}

function reasonLabel(reason) {
  switch (reason) {
    case 'unpaid_grace': return 'Unpaid (grace period)'
    case 'payment_default': return 'Payment default'
    case 'client_requested': return 'Client requested termination'
    case 'proving_failure': return 'Proving failure'
    default: return reason || '—'
  }
}

function formatDeleteDate(item) {
  if (item?.projectedDeleteAt) {
    const d = new Date(item.projectedDeleteAt)
    const date = d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
    const epoch = item.projectedDeleteEpoch != null ? ` (epoch ${item.projectedDeleteEpoch})` : ''
    return `${date}${epoch}`
  }
  if (item?.deleteDatePending) return 'Pending confirmation'
  return '—'
}

function ownerContactLine(payer) {
  return payer ? `owner (payer ${payer})` : 'dataset owner (if known)'
}

customElements.define('pdp-datasets-list', class PdpDatasetsList extends LitElement {
  static properties = {
    items: { type: Array },
    total: { type: Number },
    limit: { type: Number },
    offset: { type: Number },
    filter: { type: String },
    filterInput: { type: String },
    sortBy: { type: String },
    sortAsc: { type: Boolean },
    loadError: { type: String },
    loading: { type: Boolean },
    scanning: { type: Boolean },
    scanProgress: { type: Object },
    viewMode: { type: String },
    allAtRiskItems: { type: Array },
    loadGeneration: { type: Number },
  }

  constructor() {
    super()
    this.items = []
    this.total = 0
    this.limit = 25
    this.offset = 0
    this.filter = ''
    this.filterInput = ''
    this.sortBy = 'id'
    this.sortAsc = false
    this.loadError = null
    this.loading = false
    this.scanning = false
    this.scanProgress = null
    this.viewMode = 'all'
    this.allAtRiskItems = []
    this.loadGeneration = 0

    const params = new URLSearchParams(window.location.search)
    if (params.get('risk') === 'payment') {
      this.viewMode = 'payment'
      this.sortBy = 'size_bytes'
      this.sortAsc = false
    }
    if (params.get('q')) {
      this.filterInput = params.get('q')
    }
    const sort = params.get('sort')
    if (sort === 'object_count' || sort === 'size_bytes' || sort === 'first_upload_at' || sort === 'id' || sort === 'projected_delete_epoch') {
      this.sortBy = sort
    }
    if (params.get('asc') === '1') {
      this.sortAsc = true
    } else if (params.get('asc') === '0') {
      this.sortAsc = false
    }

    // Deep-linked q= only stays as a list filter for wallets; other intents navigate away.
    const intent = classifySearch(this.filterInput)
    if (intent.kind === 'wallet') {
      this.filter = intent.value
      this.loadData()
    } else if (intent.kind === 'empty') {
      this.loadData()
    } else {
      // Reuse submit routing (dataset id / piece / tx / unknown).
      this.applySearch()
    }
  }

  createRenderRoot() {
    return this
  }

  syncUrl() {
    const url = new URL(window.location.href)
    if (this.viewMode === 'payment') url.searchParams.set('risk', 'payment')
    else url.searchParams.delete('risk')

    if (this.viewMode === 'payment') {
      if (this.filter) url.searchParams.set('q', this.filter)
      else url.searchParams.delete('q')
    } else if (this.filter) {
      url.searchParams.set('q', this.filter)
    } else {
      url.searchParams.delete('q')
    }

    const defaultSort = this.viewMode === 'payment'
      ? (this.sortBy === 'size_bytes' && !this.sortAsc)
      : (this.sortBy === 'id' && !this.sortAsc)
    if (defaultSort) {
      url.searchParams.delete('sort')
      url.searchParams.delete('asc')
    } else {
      url.searchParams.set('sort', this.sortBy)
      url.searchParams.set('asc', this.sortAsc ? '1' : '0')
    }
    window.history.replaceState({}, '', url)
  }

  setViewMode(mode) {
    if (this.viewMode === mode) return
    this.viewMode = mode
    this.offset = 0
    if (mode === 'payment') {
      this.sortBy = 'size_bytes'
      this.sortAsc = false
      this.filter = ''
      this.filterInput = ''
    } else {
      this.sortBy = 'id'
      this.sortAsc = false
    }
    this.syncUrl()
    this.loadData()
  }

  async loadData() {
    if (this.viewMode === 'payment') {
      return this.loadPaymentAtRiskProgressive()
    }

    this.loading = true
    this.scanning = false
    this.scanProgress = null
    try {
      const result = await RPCCall('PDPDataSetList', [
        this.limit,
        this.offset,
        this.filter || '',
        this.sortBy || 'id',
        !!this.sortAsc,
      ])
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

  sortAtRiskItems(items) {
    const col = this.sortBy || 'size_bytes'
    const asc = !!this.sortAsc
    const sorted = [...items]
    sorted.sort((a, b) => {
      let cmp = 0
      switch (col) {
        case 'id':
          cmp = a.id - b.id
          break
        case 'projected_delete_epoch': {
          const ae = a.projectedDeleteEpoch ?? (asc ? Number.MAX_SAFE_INTEGER : -1)
          const be = b.projectedDeleteEpoch ?? (asc ? Number.MAX_SAFE_INTEGER : -1)
          cmp = ae - be
          break
        }
        default:
          cmp = (a.sizeBytes ?? 0) - (b.sizeBytes ?? 0)
      }
      if (cmp === 0) cmp = a.id - b.id
      return asc ? cmp : -cmp
    })
    return sorted
  }

  scanProgressLabel() {
    const p = this.scanProgress
    if (!p) return 'Scanning datasets…'
    const total = p.datasetTotal > 0 ? p.datasetTotal : '?'
    const progress = `Scanning datasets (${p.scanned}/${total})`
    if (p.stopped) {
      return `Scan stopped (${p.scanned}/${total} scanned, ≥100 KiB only)`
    }
    if (p.awaiting) {
      return `${progress} — awaiting batch ${p.batchNumber}…`
    }
    return `${progress}…`
  }

  stopPaymentScan() {
    if (!this.scanning) return
    this.loadGeneration++
  }

  applyAtRiskPagination() {
    const sorted = this.sortAtRiskItems(this.allAtRiskItems)
    this.total = sorted.length
    const start = this.offset
    const end = start + this.limit
    this.items = sorted.slice(start, end)
  }

  async loadPaymentAtRiskProgressive() {
    const generation = ++this.loadGeneration
    this.loading = true
    this.scanning = true
    this.loadError = null
    this.allAtRiskItems = []
    this.items = []
    this.total = 0
    this.scanProgress = { scanned: 0, datasetTotal: 0, batchNumber: 1, awaiting: true }
    this.requestUpdate()

    let afterSize = 0
    let afterID = 0
    let scanned = 0
    let complete = false
    let chainRetries = 0
    let batchNumber = 1
    const maxChainRetries = 8
    const scanBatchSize = 20
    const minScanSizeBytes = 100 * 1024

    try {
      while (!complete) {
        if (generation !== this.loadGeneration) {
          this.scanProgress = {
            ...this.scanProgress,
            awaiting: false,
            stopped: true,
          }
          return
        }

        this.scanProgress = {
          scanned,
          datasetTotal: this.scanProgress?.datasetTotal ?? 0,
          batchNumber,
          awaiting: true,
        }
        this.requestUpdate()

        const batch = await RPCCall('PDPDataSetAtRiskScanBatch', [
          afterSize,
          afterID,
          scanned,
          scanBatchSize,
          minScanSizeBytes,
        ])
        if (generation !== this.loadGeneration) {
          this.scanProgress = {
            ...this.scanProgress,
            awaiting: false,
            stopped: true,
          }
          return
        }

        if (batch?.chainError) {
          chainRetries++
          const hint = batch.chainError.includes('context deadline exceeded')
            ? ' Chain RPC may be slow; retrying…'
            : ''
          if (chainRetries >= maxChainRetries) {
            this.loadError = `Payment status lookup failed: ${batch.chainError}.${hint}`
            break
          }
          this.loadError = `Chain lookup slow or unavailable: ${batch.chainError}. Retrying (${chainRetries}/${maxChainRetries})…${hint}`
          this.scanProgress = {
            scanned,
            datasetTotal: batch?.datasetTotal ?? this.scanProgress?.datasetTotal ?? 0,
            batchNumber,
            awaiting: true,
          }
          this.requestUpdate()
          await new Promise((r) => setTimeout(r, 2000))
          continue
        }
        chainRetries = 0
        this.loadError = null

        const found = batch?.items ?? []
        if (found.length > 0) {
          this.allAtRiskItems = this.allAtRiskItems.concat(found)
          this.applyAtRiskPagination()
        }

        scanned = batch?.scanned ?? scanned
        complete = !!batch?.complete
        afterSize = batch?.cursor?.afterSizeBytes ?? afterSize
        afterID = batch?.cursor?.afterId ?? afterID
        batchNumber++
        this.scanProgress = {
          scanned,
          datasetTotal: batch?.datasetTotal ?? this.scanProgress?.datasetTotal ?? 0,
          batchNumber,
          awaiting: !complete,
        }
        this.requestUpdate()
      }

      if (generation !== this.loadGeneration) return
      this.applyAtRiskPagination()
    } catch (e) {
      if (generation !== this.loadGeneration) return
      console.error('Failed to scan at-risk datasets:', e)
      this.loadError = e.message || String(e)
      if (this.allAtRiskItems.length === 0) {
        this.items = []
        this.total = 0
      }
    } finally {
      if (generation === this.loadGeneration) {
        this.loading = false
        this.scanning = false
        this.requestUpdate()
      }
    }
  }

  async applySearch(e) {
    e?.preventDefault?.()
    const q = (this.filterInput || '').trim()
    const intent = classifySearch(q)

    this.loadError = null

    switch (intent.kind) {
      case 'empty':
        this.filter = ''
        this.offset = 0
        this.syncUrl()
        this.loadData()
        return
      case 'dataset_id':
        window.location.href = `/pages/dataset/?id=${encodeURIComponent(intent.value)}`
        return
      case 'piece_cid':
        window.location.href = `/pages/piece/?id=${encodeURIComponent(intent.value)}`
        return
      case 'tx_hash': {
        this.loading = true
        this.requestUpdate()
        try {
          const id = await RPCCall('PDPDataSetFindByTxHash', [intent.value])
          window.location.href = `/pages/dataset/?id=${encodeURIComponent(String(id))}`
        } catch (err) {
          console.error('Tx hash lookup failed:', err)
          this.loadError = err.message || String(err)
          this.items = []
          this.total = 0
          this.loading = false
          this.requestUpdate()
        }
        return
      }
      case 'wallet':
        this.filter = intent.value
        this.offset = 0
        this.syncUrl()
        this.loadData()
        return
      default:
        this.filter = ''
        this.items = []
        this.total = 0
        this.loadError = SEARCH_HINT
        this.syncUrl()
        this.requestUpdate()
    }
  }

  clearSearch() {
    this.filterInput = ''
    this.filter = ''
    this.offset = 0
    this.loadError = null
    this.syncUrl()
    this.loadData()
  }

  setSort(column) {
    if (this.sortBy === column) {
      this.sortAsc = !this.sortAsc
    } else {
      this.sortBy = column
      this.sortAsc = column === 'projected_delete_epoch'
    }
    this.offset = 0
    this.syncUrl()
    if (this.viewMode === 'payment' && this.allAtRiskItems.length > 0) {
      this.applyAtRiskPagination()
      this.requestUpdate()
      return
    }
    this.loadData()
  }

  renderSortIndicator(column) {
    if (this.sortBy !== column) return ''
    return html`<span class="sort-indicator">${this.sortAsc ? '▲' : '▼'}</span>`
  }

  nextPage() {
    if (this.offset + this.limit >= this.total) return
    this.offset += this.limit
    if (this.viewMode === 'payment' && this.allAtRiskItems.length > 0) {
      this.applyAtRiskPagination()
      this.requestUpdate()
      return
    }
    this.loadData()
  }

  prevPage() {
    this.offset = Math.max(0, this.offset - this.limit)
    if (this.viewMode === 'payment' && this.allAtRiskItems.length > 0) {
      this.applyAtRiskPagination()
      this.requestUpdate()
      return
    }
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
        ${loadingCssText}
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
        .sortable { cursor: pointer; user-select: none; white-space: nowrap; }
        .sortable:hover { color: var(--color-accent-fg, #58a6ff); }
        .sort-indicator { margin-left: 4px; font-size: 11px; }
        .view-tabs { display: flex; gap: 8px; margin-bottom: 16px; }
        .view-tab {
          border: 1px solid var(--color-border-default, #30363d);
          background: var(--color-bg-subtle, #161b22);
          color: var(--color-text-primary, #e6edf3);
          border-radius: 6px;
          padding: 6px 12px;
          font-size: 13px;
          cursor: pointer;
        }
        .view-tab.active {
          border-color: var(--color-accent-fg, #58a6ff);
          color: var(--color-accent-fg, #58a6ff);
        }
        .at-risk-note {
          font-size: 12px;
          color: var(--color-text-secondary, #8b949e);
          margin: 4px 0 0;
          max-width: 420px;
        }
        .scan-progress-row {
          display: flex;
          align-items: center;
          gap: 12px;
          flex-wrap: wrap;
          margin: 8px 0 0;
        }
      </style>

      <div class="view-tabs">
        <button type="button" class="view-tab ${this.viewMode === 'all' ? 'active' : ''}" @click=${() => this.setViewMode('all')}>All</button>
        <button type="button" class="view-tab ${this.viewMode === 'payment' ? 'active' : ''}" @click=${() => this.setViewMode('payment')}>Payment grace</button>
      </div>

      ${this.viewMode === 'all' ? html`<p class="hint">${SEARCH_HINT}</p>` : html`
        <p class="hint">Datasets ≥100 KiB with unpaid payment rails or pending deletion. Contact the dataset owner to bring payments current before the deletion date.</p>
      `}

      ${this.viewMode === 'all' ? html`
      <form class="datasets-search" @submit=${(e) => this.applySearch(e)}>
        <input
          type="search"
          placeholder="Dataset ID, wallet, PieceCID, or create tx"
          .value=${this.filterInput}
          @input=${(e) => { this.filterInput = e.target.value }}
        />
        <button type="submit" class="btn btn-primary btn-sm">Search</button>
        ${this.filter || this.filterInput ? html`<button type="button" class="btn btn-secondary btn-sm" @click=${() => this.clearSearch()}>Clear</button>` : ''}
      </form>
      ` : ''}

      ${this.loadError ? html`<p class="load-error">${this.loadError}</p>` : ''}

      ${this.viewMode === 'payment' && this.scanning
        ? html`<div class="scan-progress-row hint scan-progress">
            ${loadingSpinner({ label: this.scanProgressLabel(), size: 'sm' })}
            <button type="button" class="btn btn-secondary btn-sm" @click=${() => this.stopPaymentScan()}>Stop</button>
          </div>`
        : ''}
      ${this.viewMode === 'payment' && !this.scanning && this.scanProgress?.stopped
        ? html`<p class="hint">${this.scanProgressLabel()}</p>`
        : ''}

      ${!this.loadError && this.items.length === 0 && !this.scanning && !this.loading
        ? html`<p class="hint">${this.viewMode === 'payment' ? 'No datasets in payment grace or deletion.' : 'No datasets found.'}</p>`
        : this.items.length > 0 ? html`
          <table class="table table-dark table-striped table-sm">
            <thead>
              <tr>
                ${this.viewMode === 'payment' ? html`
                  <th class="sortable" @click=${() => this.setSort('id')}>
                    Dataset${this.renderSortIndicator('id')}
                  </th>
                  <th>Owner</th>
                  <th class="sortable" @click=${() => this.setSort('size_bytes')}>
                    Size${this.renderSortIndicator('size_bytes')}
                  </th>
                  <th>Reason</th>
                  <th class="sortable" @click=${() => this.setSort('projected_delete_epoch')}>
                    Deletes on${this.renderSortIndicator('projected_delete_epoch')}
                  </th>
                ` : html`
                <th class="sortable" @click=${() => this.setSort('id')}>
                  Dataset${this.renderSortIndicator('id')}
                </th>
                <th class="sortable" @click=${() => this.setSort('object_count')}>
                  Objects in store${this.renderSortIndicator('object_count')}
                </th>
                <th class="sortable" @click=${() => this.setSort('size_bytes')}>
                  Size${this.renderSortIndicator('size_bytes')}
                </th>
                <th>Proving</th>
                <th class="sortable" @click=${() => this.setSort('first_upload_at')}>
                  First upload${this.renderSortIndicator('first_upload_at')}
                </th>
                `}
              </tr>
            </thead>
            <tbody>
              ${this.items.map((ds) => this.viewMode === 'payment' ? html`
                <tr>
                  <td class="mono"><a href="/pages/dataset/?id=${ds.id}">${ds.id}</a></td>
                  <td class="mono">${ds.payer || 'Unknown'}</td>
                  <td class="mono">${formatBytes(ds.sizeBytes)}</td>
                  <td>${reasonLabel(ds.reason)}</td>
                  <td>
                    <div>${formatDeleteDate(ds)}</div>
                    <p class="at-risk-note">Contact the ${ownerContactLine(ds.payer)} to bring it current before deletion on ${formatDeleteDate(ds)}.</p>
                  </td>
                </tr>
              ` : html`
                <tr>
                  <td class="mono"><a href="/pages/dataset/?id=${ds.id}">${ds.id}</a></td>
                  <td class="mono">${ds.objectCount ?? 0}</td>
                  <td class="mono">${formatBytes(ds.sizeBytes)}</td>
                  <td class="status-${statusTone(ds.provingStatus)}">${ds.provingStatus || '—'}</td>
                  <td>${ds.firstUploadAt ? relativePhrase(new Date(ds.firstUploadAt)) : '—'}</td>
                </tr>
              `)}
            </tbody>
          </table>
          ${this.scanning ? html`<div class="cu-loading-block">${loadingSpinner({ label: this.scanProgressLabel() })}</div>` : ''}
          ${this.viewMode === 'all' && this.loading ? loadingBlock('Loading datasets…') : ''}
          <div class="pager">
            <button class="btn btn-secondary btn-sm" ?disabled=${this.offset <= 0} @click=${() => this.prevPage()}>Prev</button>
            <span class="hint" style="margin:0">${from}–${to} of ${this.scanning ? `${this.total} found so far` : this.total}</span>
            <button class="btn btn-secondary btn-sm" ?disabled=${this.offset + this.limit >= this.total || this.scanning} @click=${() => this.nextPage()}>Next</button>
          </div>
        ` : this.viewMode === 'all' && this.loading ? loadingBlock('Loading datasets…') : ''}
    `
  }
})
