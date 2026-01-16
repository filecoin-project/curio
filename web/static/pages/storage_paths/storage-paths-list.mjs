import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('storage-paths-list', class StoragePathsList extends LitElement {
    static properties = {
        paths: { type: Array },
        loading: { type: Boolean },
        error: { type: String },
        sortBy: { type: String },
        sortAsc: { type: Boolean },
        filterType: { type: String },
    };

    static styles = css`
        .path-row {
            cursor: pointer;
        }
        .path-row:hover {
            background: rgba(255,255,255,0.05);
        }
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
    `;

    constructor() {
        super();
        this.paths = [];
        this.loading = true;
        this.error = null;
        this.sortBy = 'capacity';
        this.sortAsc = false;
        this.filterType = 'all';
    }

    connectedCallback() {
        super.connectedCallback();
        this.loadData();
    }

    async loadData() {
        try {
            this.paths = await RPCCall('StoragePathList') || [];
            this.loading = false;
        } catch (e) {
            this.error = e.message || 'Failed to load storage paths';
            this.loading = false;
        }
    }

    get filteredPaths() {
        let filtered = [...this.paths];
        
        // Apply type filter
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
        
        // Apply sorting
        filtered.sort((a, b) => {
            let valA, valB;
            switch (this.sortBy) {
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
                    valA = a.StorageID || '';
                    valB = b.StorageID || '';
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

    navigateToPath(id) {
        window.location.href = `/pages/storage_path/?id=${id}`;
    }

    render() {
        if (this.loading) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous">
                <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
                <div style="padding: 20px;">Loading...</div>
            `;
        }

        if (this.error) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous">
                <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
                <div style="padding: 20px; color: #B63333;">Error: ${this.error}</div>
            `;
        }

        const filtered = this.filteredPaths;

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            
            <div style="padding: 20px; max-width: 1600px; margin: 0 auto;">
                <h1 style="margin-bottom: 20px;">Storage Paths</h1>
                
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
                            <th class="sortable" @click="${() => this.setSort('id')}">
                                ID ${this.renderSortIndicator('id')}
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
                        ${filtered.map(path => html`
                            <tr class="path-row" @click="${() => this.navigateToPath(path.StorageID)}">
                                <td><code>${path.StorageID?.substring(0, 8)}...</code></td>
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
