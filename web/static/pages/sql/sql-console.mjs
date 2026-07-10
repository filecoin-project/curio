import { html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import { StyledLitElement } from '/ux/StyledLitElement.mjs';
import RPCCall from '/lib/jsonrpc.mjs';

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
        .table-wrap {
            overflow: auto;
            max-width: 100%;
        }
        table {
            width: max-content;
            min-width: 100%;
        }
        td, th {
            white-space: nowrap;
            max-width: 480px;
            overflow: hidden;
            text-overflow: ellipsis;
        }
    `;

    constructor() {
        super();
        this.query = 'SELECT 1 AS example';
        this.loading = false;
        this.error = null;
        this.result = null;
    }

    async runQuery() {
        const query = this.query.trim();
        if (!query) {
            this.error = 'Enter a SQL query';
            this.result = null;
            return;
        }

        this.loading = true;
        this.error = null;
        this.result = null;
        this.requestUpdate();

        try {
            this.result = await RPCCall('SQLQuery', [query]);
        } catch (e) {
            this.error = e?.message || String(e);
        } finally {
            this.loading = false;
        }
    }

    onKeyDown(e) {
        if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
            e.preventDefault();
            this.runQuery();
        }
    }

    renderTable() {
        if (!this.result?.Columns?.length) {
            return html``;
        }

        return html`
            <div class="table-wrap">
                <table class="table table-dark table-striped table-sm">
                    <thead>
                        <tr>
                            ${this.result.Columns.map((col) => html`<th>${col}</th>`)}
                        </tr>
                    </thead>
                    <tbody>
                        ${(this.result.Rows || []).map((row) => html`
                            <tr>
                                ${row.map((cell) => html`<td title=${cell}>${cell}</td>`)}
                            </tr>
                        `)}
                    </tbody>
                </table>
            </div>
        `;
    }

    render() {
        return html`
            <h2>SQL Console</h2>
            <p class="hint">Run read or write SQL against HarmonyDB. Results are limited to 1000 rows. Use Cmd/Ctrl+Enter to run.</p>

            <textarea
                .value=${this.query}
                @input=${(e) => { this.query = e.target.value; }}
                @keydown=${this.onKeyDown}
                ?disabled=${this.loading}
            ></textarea>

            <div class="actions">
                <button class="btn btn-primary" @click=${this.runQuery} ?disabled=${this.loading}>
                    ${this.loading ? 'Running…' : 'Run Query'}
                </button>
                <span class="hint">Read queries show a table; writes show affected row count.</span>
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

            ${this.renderTable()}
        `;
    }
}

customElements.define('sql-console', SQLConsole);
