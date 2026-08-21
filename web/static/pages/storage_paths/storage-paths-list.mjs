import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { loadingBlock, loadingStyles } from '/lib/loading.mjs';
import { getUIVariant } from '/lib/ui-variant.mjs';

function formatBytes(n) {
    const v = Number(n) || 0;
    if (v <= 0) return '0 B';
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
    let x = v;
    let i = 0;
    while (x >= 1024 && i < units.length - 1) {
        x /= 1024;
        i++;
    }
    return `${x < 10 && i > 0 ? x.toFixed(1) : Math.round(x)} ${units[i]}`;
}

customElements.define('storage-paths-list', class StoragePathsList extends LitElement {
    static properties = {
        paths: { type: Array },
        candidates: { type: Array },
        loading: { type: Boolean },
        error: { type: String },
        sortBy: { type: String },
        sortAsc: { type: Boolean },
        filterType: { type: String },
        isSkiff: { type: Boolean },
        busyPath: { type: String },
        customPath: { type: String },
        actionMessage: { type: String },
        actionError: { type: String },
    };

    static styles = [loadingStyles, css`
        .health-ok {
            color: #4BB543;
        }
        .health-error {
            color: #B63333;
        }
        .tag {
            display: inline-block;
            padding: 2px 8px;
            margin: 2px;
            border-radius: 4px;
            font-size: 0.85em;
            background: rgba(255,255,255,0.1);
        }
        .tag-seal {
            background: rgba(75, 181, 67, 0.3);
            color: #4BB543;
        }
        .tag-store {
            background: rgba(59, 130, 246, 0.3);
            color: #3B82F6;
        }
        .tag-readonly {
            background: rgba(255, 214, 0, 0.3);
            color: #FFD600;
        }
        .tag-attached {
            background: rgba(75, 181, 67, 0.25);
            color: #4BB543;
        }
        .usage-bar {
            display: inline-block;
            width: 150px;
            height: 16px;
            border: 3px solid #3f3f3f;
        }
        .usage-used {
            height: 10px;
            background-color: green;
            float: left;
        }
        .usage-reserved {
            height: 10px;
            background-color: #b8860b;
            float: left;
        }
        .type-tags {
            font-size: 0.8em;
        }
        .type-tags .tag {
            padding: 1px 5px;
            margin: 1px;
        }
        .tag-allow {
            background: rgba(75, 181, 67, 0.2);
            color: #4BB543;
        }
        .tag-deny {
            background: rgba(182, 51, 51, 0.2);
            color: #B63333;
        }
        .filters {
            margin-bottom: 20px;
            display: flex;
            gap: 15px;
            align-items: center;
        }
        .sortable {
            cursor: pointer;
            user-select: none;
        }
        .sortable:hover {
            color: #3B82F6;
        }
        .sort-indicator {
            margin-left: 5px;
        }
        .mount-path {
            display: block;
            font-size: 0.95em;
            color: var(--color-text-primary);
            word-break: break-all;
        }
        .mount-id {
            margin-top: 2px;
            font-size: 0.75em;
            color: var(--color-text-secondary);
            font-family: var(--font-mono);
        }
        .picker {
            background: var(--color-bg-subtle, #161b22);
            border: 1px solid var(--color-border-default, #30363d);
            border-radius: 8px;
            padding: 16px 20px;
            margin-bottom: 24px;
        }
        .picker h2 {
            font-size: 1.1rem;
            margin: 0 0 8px;
        }
        .picker p {
            color: var(--color-text-secondary, #8b949e);
            margin: 0 0 12px;
        }
        .picker .mono {
            font-family: var(--font-mono, monospace);
        }
        .picker-actions {
            display: flex;
            gap: 8px;
            margin-bottom: 12px;
        }
        .banner {
            border-radius: 6px;
            padding: 8px 12px;
            margin-bottom: 12px;
            font-size: 0.9em;
        }
        .banner.ok {
            background: rgba(75, 181, 67, 0.15);
            color: #4BB543;
        }
        .banner.err {
            background: rgba(182, 51, 51, 0.2);
            color: #ff7b72;
        }
        .cand-path {
            font-family: var(--font-mono, monospace);
            word-break: break-all;
        }
        .custom-path-row {
            display: flex;
            flex-wrap: wrap;
            gap: 8px;
            align-items: center;
            margin-bottom: 16px;
        }
        .custom-path-row input {
            flex: 1 1 280px;
            min-width: 200px;
            font-family: var(--font-mono, monospace);
        }
    `];

    constructor() {
        super();
        this.paths = [];
        this.candidates = [];
        this.loading = true;
        this.error = null;
        this.sortBy = 'path';
        this.sortAsc = true;
        this.filterType = 'all';
        this.isSkiff = false;
        this.busyPath = '';
        this.customPath = '';
        this.actionMessage = '';
        this.actionError = '';
    }

    connectedCallback() {
        super.connectedCallback();
        this.loadData();
    }

    async loadData() {
        try {
            this.isSkiff = (await getUIVariant()) === 'skiff';
            const tasks = [RPCCall('StoragePathList')];
            if (this.isSkiff) {
                tasks.push(RPCCall('StorageCandidates'));
            }
            const [paths, candidates] = await Promise.all(tasks);
            this.paths = paths || [];
            this.candidates = candidates || [];
            this.loading = false;
        } catch (e) {
            this.error = e.message || 'Failed to load storage paths';
            this.loading = false;
        }
    }

    async attachPath(path) {
        const trimmed = (path || '').trim();
        if (!trimmed) {
            this.actionError = 'Enter a directory path to attach';
            return;
        }
        this.busyPath = trimmed;
        this.actionMessage = '';
        this.actionError = '';
        try {
            await RPCCall('StorageAttachLocal', [trimmed]);
            this.actionMessage = `Attached ${trimmed}`;
            this.customPath = '';
            await this.loadData();
        } catch (e) {
            this.actionError = e.message || String(e);
        } finally {
            this.busyPath = '';
        }
    }

    attachCustomPath(e) {
        e?.preventDefault?.();
        return this.attachPath(this.customPath);
    }

    async detachPath(path) {
        if (!confirm(`Detach storage path ${path}?`)) {
            return;
        }
        this.busyPath = path;
        this.actionMessage = '';
        this.actionError = '';
        try {
            await RPCCall('StorageDetachLocal', [path]);
            this.actionMessage = `Detached ${path}`;
            await this.loadData();
        } catch (e) {
            this.actionError = e.message || String(e);
        } finally {
            this.busyPath = '';
        }
    }

    get filteredPaths() {
        let filtered = [...this.paths];

        if (this.filterType !== 'all') {
            filtered = filtered.filter(p => {
                const canSeal = p.CanSeal;
                const canStore = p.CanStore;
                switch (this.filterType) {
                    case 'seal': return canSeal && !canStore;
                    case 'store': return !canSeal && canStore;
                    case 'both': return canSeal && canStore;
                    case 'readonly': return !canSeal && !canStore;
                    default: return true;
                }
            });
        }

        filtered.sort((a, b) => {
            let valA, valB;
            switch (this.sortBy) {
                case 'path':
                    valA = a.LocalPath || a.StorageID || '';
                    valB = b.LocalPath || b.StorageID || '';
                    break;
                case 'capacity':
                    valA = a.Capacity || 0;
                    valB = b.Capacity || 0;
                    break;
                case 'available':
                    valA = a.Available || 0;
                    valB = b.Available || 0;
                    break;
                case 'used':
                    valA = (a.Capacity || 0) - (a.FSAvailable || 0);
                    valB = (b.Capacity || 0) - (b.FSAvailable || 0);
                    break;
                case 'health':
                    valA = a.HealthOK ? 1 : 0;
                    valB = b.HealthOK ? 1 : 0;
                    break;
                case 'type':
                    valA = a.PathType || '';
                    valB = b.PathType || '';
                    break;
                default:
                    valA = a.LocalPath || a.StorageID || '';
                    valB = b.LocalPath || b.StorageID || '';
            }

            if (typeof valA === 'string') {
                return this.sortAsc ? valA.localeCompare(valB) : valB.localeCompare(valA);
            }
            return this.sortAsc ? valA - valB : valB - valA;
        });

        return filtered;
    }

    setSort(column) {
        if (this.sortBy === column) {
            this.sortAsc = !this.sortAsc;
        } else {
            this.sortBy = column;
            this.sortAsc = false;
        }
    }

    renderSortIndicator(column) {
        if (this.sortBy !== column) return '';
        return html`<span class="sort-indicator">${this.sortAsc ? '▲' : '▼'}</span>`;
    }

    renderPicker() {
        if (!this.isSkiff) {
            return html``;
        }
        return html`
            <div class="picker">
                <h2>Select storage folders</h2>
                <p>
                    Attach any existing directory Curio-PDP should use. Suggested candidates under
                    <span class="mono">/data</span> (or <span class="mono">DATA_STORAGE</span>) appear below.
                    Paths are not auto-registered.
                </p>
                ${this.actionMessage ? html`<div class="banner ok">${this.actionMessage}</div>` : ''}
                ${this.actionError ? html`<div class="banner err">${this.actionError}</div>` : ''}
                <form class="custom-path-row" @submit=${(e) => this.attachCustomPath(e)}>
                    <input class="form-control form-control-sm" type="text"
                           placeholder="/var/lib/curio-data or /mnt/nvme0"
                           .value=${this.customPath}
                           ?disabled=${!!this.busyPath}
                           @input=${(e) => { this.customPath = e.target.value; }}>
                    <button class="btn btn-primary btn-sm" type="submit"
                            ?disabled=${!!this.busyPath || !(this.customPath || '').trim()}>
                        ${this.busyPath && this.busyPath === (this.customPath || '').trim() ? '…' : 'Attach path'}
                    </button>
                    <button class="btn btn-secondary btn-sm" type="button" ?disabled=${!!this.busyPath}
                            @click=${() => this.loadData()}>Refresh</button>
                </form>
                ${!this.candidates?.length ? html`
                    <p>No suggested folders under the data root. Enter a path above, or mount directories under <span class="mono">/data</span>.</p>
                ` : html`
                    <table class="table table-dark">
                        <thead>
                            <tr>
                                <th>Folder</th>
                                <th>Available</th>
                                <th>Capacity</th>
                                <th>Status</th>
                                <th></th>
                            </tr>
                        </thead>
                        <tbody>
                            ${this.candidates.map(c => html`
                                <tr>
                                    <td class="cand-path">${c.Path}</td>
                                    <td>${formatBytes(c.Available)}</td>
                                    <td>${formatBytes(c.Capacity)}</td>
                                    <td>
                                        ${c.Attached
                                            ? html`<span class="tag tag-attached">Attached</span>`
                                            : html`<span class="tag">Available</span>`}
                                    </td>
                                    <td>
                                        ${c.Attached
                                            ? html`<button class="btn btn-secondary btn-sm"
                                                    ?disabled=${this.busyPath === c.Path}
                                                    @click=${() => this.detachPath(c.Path)}>
                                                    ${this.busyPath === c.Path ? '…' : 'Detach'}
                                                </button>`
                                            : html`<button class="btn btn-primary btn-sm"
                                                    ?disabled=${this.busyPath === c.Path}
                                                    @click=${() => this.attachPath(c.Path)}>
                                                    ${this.busyPath === c.Path ? '…' : 'Attach'}
                                                </button>`}
                                    </td>
                                </tr>
                            `)}
                        </tbody>
                    </table>
                `}
            </div>
        `;
    }

    render() {
        if (this.loading) {
            return html`
                <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
                <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
                ${loadingBlock('Loading…')}
            `;
        }

        if (this.error) {
            return html`
                <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
                <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
                <div style="color: var(--color-danger-fg);">Error: ${this.error}</div>
            `;
        }

        const filtered = this.filteredPaths;

        return html`
            <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <div style="max-width: 1600px;">
                <h1 style="margin-bottom: 8px;">Storage Mounts</h1>
                <p style="color: var(--color-text-secondary); margin-bottom: 20px;">Each mount with capacity, usage, and health.</p>

                ${this.renderPicker()}

                <div class="filters">
                    <label>
                        Filter by type:
                        <select class="form-select form-select-sm" style="width: auto; display: inline-block; margin-left: 5px;"
                                @change="${e => this.filterType = e.target.value}">
                            <option value="all" ?selected="${this.filterType === 'all'}">All</option>
                            <option value="seal" ?selected="${this.filterType === 'seal'}">Seal Only</option>
                            <option value="store" ?selected="${this.filterType === 'store'}">Store Only</option>
                            <option value="both" ?selected="${this.filterType === 'both'}">Seal + Store</option>
                            <option value="readonly" ?selected="${this.filterType === 'readonly'}">Read-Only</option>
                        </select>
                    </label>
                    <span style="color: #aaa;">Showing ${filtered.length} of ${this.paths.length} paths</span>
                </div>

                <table class="table table-dark">
                    <thead>
                        <tr>
                            <th class="sortable" @click="${() => this.setSort('path')}">
                                Path ${this.renderSortIndicator('path')}
                            </th>
                            <th class="sortable" @click="${() => this.setSort('type')}">
                                Type ${this.renderSortIndicator('type')}
                            </th>
                            <th>Hosts</th>
                            <th class="sortable" @click="${() => this.setSort('capacity')}">
                                Capacity ${this.renderSortIndicator('capacity')}
                            </th>
                            <th class="sortable" @click="${() => this.setSort('available')}">
                                Available ${this.renderSortIndicator('available')}
                            </th>
                            <th>Usage</th>
                            <th>Type Filters</th>
                            <th class="sortable" @click="${() => this.setSort('health')}">
                                Health ${this.renderSortIndicator('health')}
                            </th>
                        </tr>
                    </thead>
                    <tbody>
                        ${filtered.length === 0 ? html`
                            <tr><td colspan="8" style="color: var(--color-text-secondary);">
                                No storage paths attached yet.
                                ${this.isSkiff ? 'Use Select storage folders above.' : ''}
                            </td></tr>
                        ` : filtered.map(path => html`
                            <tr>
                                <td>
                                    <code class="mount-path">${path.LocalPath || '—'}</code>
                                    <div class="mount-id">${path.StorageID?.substring(0, 8)}…</div>
                                </td>
                                <td>
                                    <span class="tag ${this.getTypeClass(path)}">${path.PathType}</span>
                                </td>
                                <td>
                                    ${path.HostList?.slice(0, 2).map(h => html`<span class="tag">${h}</span>`)}
                                    ${path.HostList?.length > 2 ? html`<span class="tag">+${path.HostList.length - 2}</span>` : ''}
                                </td>
                                <td>${path.CapacityStr}</td>
                                <td>${path.AvailableStr}</td>
                                <td>
                                    <div class="usage-bar">
                                        <div class="usage-used" style="width: ${path.UsedPercent || 0}%;"></div>
                                        <div class="usage-reserved" style="width: ${path.ReservedPercent || 0}%;"></div>
                                    </div>
                                    <span style="margin-left: 8px; font-size: 0.85em;">${(path.UsedPercent || 0).toFixed(0)}%</span>
                                </td>
                                <td class="type-tags">
                                    ${path.AllowTypesList?.length ? path.AllowTypesList.map(t => html`<span class="tag tag-allow">+${t}</span>`) : ''}
                                    ${path.DenyTypesList?.length ? path.DenyTypesList.map(t => html`<span class="tag tag-deny">-${t}</span>`) : ''}
                                    ${!path.AllowTypesList?.length && !path.DenyTypesList?.length ? html`<span style="color: #666;">-</span>` : ''}
                                </td>
                                <td class="${path.HealthOK ? 'health-ok' : 'health-error'}">
                                    ${path.HealthOK ? '● OK' : '● ' + (path.HealthStatus || 'Error')}
                                </td>
                            </tr>
                        `)}
                    </tbody>
                </table>
            </div>
        `;
    }

    getTypeClass(path) {
        const canSeal = path.CanSeal;
        const canStore = path.CanStore;
        if (canSeal && canStore) return 'tag-seal';
        if (canSeal) return 'tag-seal';
        if (canStore) return 'tag-store';
        return 'tag-readonly';
    }
});
