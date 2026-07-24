import { html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import DataTable from 'https://esm.sh/datatables.net@2.3.2';
import { StyledLitElement } from '/ux/StyledLitElement.mjs';
import { RPCCallHTTP } from '/lib/jsonrpc.mjs';

class SQLConsole extends StyledLitElement {
    static properties = {
        query: { type: String },
        loading: { type: Boolean },
        error: { type: String },
        result: { type: Object },
    };

    static styles = css`
        :host {
            display: block;
            max-width: 100%;
        }
        h2 {
            margin-bottom: 1rem;
        }
        textarea {
            width: 100%;
            min-height: 160px;
            font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
            font-size: 14px;
            background: #1a1a1e;
            color: #e8e8ea;
            border: 1px solid #444;
            border-radius: 6px;
            padding: 12px;
            resize: vertical;
        }
        .actions {
            display: flex;
            gap: 12px;
            align-items: center;
            margin: 12px 0 20px;
        }
        .hint {
            color: #9aa0a6;
            font-size: 0.9rem;
        }
        .error {
            color: #ff8a80;
            margin-bottom: 16px;
            white-space: pre-wrap;
        }
        .meta {
            color: #9aa0a6;
            margin-bottom: 12px;
        }
        .table-host {
            overflow: auto;
            max-width: 100%;
        }
        .table-host table.dataTable {
            width: 100% !important;
            color: #e8e8ea;
        }
        .table-host table.dataTable thead th {
            background: #2a2a2e;
            color: #e8e8ea;
            border-bottom-color: #444;
        }
        .table-host table.dataTable tbody td {
            border-top-color: #333;
            white-space: pre-wrap;
            word-break: break-word;
            max-width: 64rem;
        }
        .table-host table.dataTable.stripe > tbody > tr:nth-child(odd) > * {
            background: #1a1a1e;
        }
        .table-host table.dataTable.stripe > tbody > tr:nth-child(even) > * {
            background: #222226;
        }
        .table-host table.dataTable.hover > tbody > tr:hover > * {
            background: #2c2c32 !important;
        }
        .dt-container .dt-search input,
        .dt-container .dt-length select {
            background: #1a1a1e;
            color: #e8e8ea;
            border: 1px solid #444;
            border-radius: 4px;
        }
        .dt-container .dt-info,
        .dt-container .dt-length,
        .dt-container .dt-search,
        .dt-container .dt-paging {
            color: #9aa0a6;
        }
        .dt-container .dt-paging .dt-paging-button {
            color: #e8e8ea !important;
        }
        .dt-container .dt-paging .dt-paging-button.current {
            background: #3a3a40 !important;
            border-color: #555 !important;
        }
    `;

    constructor() {
        super();
        this.query = `SELECT posted, err 
FROM harmony_task_history 
WHERE err != '' 
ORDER BY posted DESC
LIMIT 100`;
        this.loading = false;
        this.error = null;
        this.result = null;
        this._dataTable = null;
        this._abort = null;
        this._runGen = 0;
    }

    disconnectedCallback() {
        if (this._abort) {
            this._abort.abort();
            this._abort = null;
        }
        this.destroyDataTable();
        super.disconnectedCallback();
    }

    updated(changed) {
        const host = this.renderRoot?.querySelector('.table-host');
        const needsTable = Boolean(this.result?.Columns?.length);
        // Lit clears empty template nodes on re-render; rebuild when result
        // changes or when the host was wiped while results are still present.
        if (changed.has('result') || (needsTable && host && !host.querySelector('table'))) {
            this.syncDataTable();
        }
    }

    destroyDataTable() {
        if (this._dataTable) {
            this._dataTable.destroy(true);
            this._dataTable = null;
        }
    }

    syncDataTable() {
        const host = this.renderRoot?.querySelector('.table-host');
        if (!host) {
            return;
        }

        this.destroyDataTable();
        host.replaceChildren();

        const columns = this.result?.Columns;
        if (!columns?.length) {
            return;
        }

        const table = document.createElement('table');
        table.className = 'display stripe hover';
        table.style.width = '100%';

        const thead = document.createElement('thead');
        const headRow = document.createElement('tr');
        for (const col of columns) {
            const th = document.createElement('th');
            th.textContent = col;
            headRow.appendChild(th);
        }
        thead.appendChild(headRow);
        table.appendChild(thead);

        const tbody = document.createElement('tbody');
        for (const row of this.result.Rows || []) {
            const tr = document.createElement('tr');
            for (const cell of row) {
                const td = document.createElement('td');
                td.textContent = cell;
                td.title = cell;
                tr.appendChild(td);
            }
            tbody.appendChild(tr);
        }
        table.appendChild(tbody);
        host.appendChild(table);

        this._dataTable = new DataTable(table, {
            pageLength: 25,
            lengthMenu: [10, 25, 50, 100, 250],
            order: [],
            scrollX: true,
            autoWidth: false,
        });
    }

    async runQuery() {
        const wasLoading = this.loading;

        // Abort any in-flight query first (re-run or cancel).
        if (this._abort) {
            this._abort.abort();
            this._abort = null;
        }

        const query = this.query.trim();
        if (!query) {
            this.loading = false;
            this.error = wasLoading ? 'Query cancelled' : 'Enter a SQL query';
            this.result = null;
            return;
        }

        const ac = new AbortController();
        this._abort = ac;
        const generation = ++this._runGen;

        this.loading = true;
        this.error = null;
        this.result = null;
        this.requestUpdate();

        try {
            // HTTP (not WebSocket) so the browser sends Referer for server-side gating.
            this.result = await RPCCallHTTP('SQLQuery', [query], { signal: ac.signal });
        } catch (e) {
            if (generation !== this._runGen) {
                return;
            }
            if (ac.signal.aborted || e?.name === 'AbortError') {
                this.error = 'Query cancelled';
                return;
            }
            this.error = e?.message || String(e);
        } finally {
            if (generation === this._runGen) {
                this.loading = false;
                if (this._abort === ac) {
                    this._abort = null;
                }
            }
        }
    }

    onKeyDown(e) {
        if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
            e.preventDefault();
            this.runQuery();
        }
    }

    render() {
        return html`
            <link rel="stylesheet" href="https://cdn.datatables.net/2.3.2/css/dataTables.dataTables.min.css">

            <h2>SQL Console</h2>
            <p class="hint">Run read or write SQL against HarmonyDB. Results are limited to 1000 rows (10 minute timeout). Use Cmd/Ctrl+Enter to run; run again to cancel the previous query.</p>

            <textarea
                .value=${this.query}
                @input=${(e) => { this.query = e.target.value; }}
                @keydown=${this.onKeyDown}
            ></textarea>

            <div class="actions">
                <button class="btn btn-primary" @click=${this.runQuery}>
                    ${this.loading ? 'Cancel & Run' : 'Run Query'}
                </button>
                <span class="hint">${this.loading ? 'Query running… click again to cancel and start a new one.' : 'Read queries show a table; writes show affected row count.'}</span>
            </div>

            ${this.error ? html`<div class="error">${this.error}</div>` : ''}

            ${this.result?.CommandTag && !this.result?.Columns?.length ? html`
                <div class="meta">${this.result.CommandTag}</div>
            ` : ''}

            ${this.result?.Truncated ? html`
                <div class="meta">Showing first 1000 rows.</div>
            ` : ''}

            ${this.result?.Rows?.length === 0 && this.result?.Columns?.length ? html`
                <div class="meta">Query returned no rows.</div>
            ` : ''}

            <div class="table-host"></div>
        `;
    }
}

customElements.define('sql-console', SQLConsole);
