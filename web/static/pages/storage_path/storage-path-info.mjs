import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('storage-path-info', class StoragePathInfo extends LitElement {
    static properties = {
        data: { type: Object },
        sectors: { type: Array },
        sectorsTotal: { type: Number },
        sectorsPage: { type: Number },
        sectorsLoading: { type: Boolean },
        loading: { type: Boolean },
        error: { type: String },
        hostMachineMap: { type: Object },
    };

    static styles = css`
        .info-grid {
            display: grid;
            grid-template-columns: auto 1fr;
            gap: 8px 20px;
            font-size: 0.95em;
        }
        .info-label {
            color: #aaa;
            font-weight: 500;
        }
        .info-value {
            color: #fff;
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
            width: 100%;
            height: 16px;
            border: 3px solid #3f3f3f;
            position: relative;
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
        .usage-text {
            margin-top: 5px;
            font-size: 0.85em;
            color: #aaa;
        }
        .summary-card {
            background: rgba(255,255,255,0.05);
            border-radius: 8px;
            padding: 15px;
            margin-bottom: 15px;
        }
        .summary-stat {
            text-align: center;
            padding: 10px;
        }
        .summary-stat-value {
            font-size: 1.5em;
            font-weight: 600;
            color: #3B82F6;
        }
        .summary-stat-label {
            font-size: 0.85em;
            color: #aaa;
        }
        .url-status {
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 8px 12px;
            background: rgba(255,255,255,0.03);
            border-radius: 4px;
            margin: 4px 0;
        }
        .url-status-dot {
            width: 10px;
            height: 10px;
            border-radius: 50%;
        }
        .url-status-dot.live {
            background: #4BB543;
        }
        .url-status-dot.dead {
            background: #B63333;
        }
        :host {
            display: block;
            width: 100%;
            flex: 1;
            min-width: 0;
        }
        .stats-row {
            display: grid;
            grid-template-columns: repeat(4, 1fr);
            gap: 15px;
            margin-bottom: 20px;
        }
        .two-col {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 20px;
        }
        @media (max-width: 1200px) {
            .two-col {
                grid-template-columns: 1fr;
            }
        }
        @media (max-width: 800px) {
            .stats-row {
                grid-template-columns: repeat(2, 1fr);
            }
        }
        .sector-table {
            width: 100%;
        }
        .sector-table th {
            text-align: left;
            padding: 8px;
            border-bottom: 1px solid rgba(255,255,255,0.1);
        }
        .sector-table td {
            padding: 8px;
            border-bottom: 1px solid rgba(255,255,255,0.05);
        }
        .pagination {
            display: flex;
            gap: 10px;
            align-items: center;
            margin-top: 15px;
        }
        .pagination button {
            padding: 5px 15px;
        }
        .gc-approved {
            color: #FFD600;
        }
        .gc-pending {
            color: #aaa;
        }
        .primary-badge {
            background: rgba(75, 181, 67, 0.3);
            color: #4BB543;
            padding: 2px 6px;
            border-radius: 3px;
            font-size: 0.8em;
        }
        .secondary-badge {
            background: rgba(170, 170, 170, 0.2);
            color: #aaa;
            padding: 2px 6px;
            border-radius: 3px;
            font-size: 0.8em;
        }
    `;

    constructor() {
        super();
        this.data = null;
        this.sectors = [];
        this.sectorsTotal = 0;
        this.sectorsPage = 0;
        this.sectorsLoading = false;
        this.loading = true;
        this.error = null;
        this.hostMachineMap = {};
    }

    connectedCallback() {
        super.connectedCallback();
        this.loadData();
    }

    async loadData() {
        const id = new URLSearchParams(window.location.search).get('id');
        if (!id) {
            this.error = 'No storage ID provided';
            this.loading = false;
            return;
        }

        try {
            this.data = await RPCCall('StoragePathDetail', [id]);
            this.loading = false;
            this.loadSectors();
            this.loadHostMachineMap();
        } catch (e) {
            this.error = e.message || 'Failed to load storage path details';
            this.loading = false;
        }
    }

    async loadHostMachineMap() {
        if (!this.data?.Info?.HostList?.length) return;
        try {
            this.hostMachineMap = await RPCCall('HostToMachineID', [this.data.Info.HostList]) || {};
        } catch (e) {
            console.error('Failed to load host machine map:', e);
        }
    }

    async loadSectors() {
        if (!this.data?.Info?.StorageID) return;

        this.sectorsLoading = true;
        try {
            const pageSize = 50;
            const result = await RPCCall('StoragePathSectors', [
                this.data.Info.StorageID,
                pageSize,
                this.sectorsPage * pageSize
            ]);
            this.sectors = result?.Sectors || [];
            this.sectorsTotal = result?.Total || 0;
        } catch (e) {
            console.error('Failed to load sectors:', e);
        }
        this.sectorsLoading = false;
    }

    prevPage() {
        if (this.sectorsPage > 0) {
            this.sectorsPage--;
            this.loadSectors();
        }
    }

    nextPage() {
        const pageSize = 50;
        if ((this.sectorsPage + 1) * pageSize < this.sectorsTotal) {
            this.sectorsPage++;
            this.loadSectors();
        }
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

        const info = this.data?.Info;
        if (!info) {
            return html`<div>No data</div>`;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            
            <div class="main-content">
                <h1 style="margin-bottom: 5px;">Storage Path</h1>
                <p style="color: #aaa; font-family: monospace; font-size: 0.9em; margin-bottom: 20px;">${info.StorageID}</p>
                
                <!-- Summary Cards -->
                <div class="stats-row">
                    <div class="summary-card">
                        <div class="summary-stat">
                            <div class="summary-stat-value">${this.data.TotalSectorEntries || 0}</div>
                            <div class="summary-stat-label">Sector Entries</div>
                        </div>
                    </div>
                    <div class="summary-card">
                        <div class="summary-stat">
                            <div class="summary-stat-value" style="color: #4BB543;">${this.data.PrimaryEntries || 0}</div>
                            <div class="summary-stat-label">Primary</div>
                        </div>
                    </div>
                    <div class="summary-card">
                        <div class="summary-stat">
                            <div class="summary-stat-value" style="color: #aaa;">${this.data.SecondaryEntries || 0}</div>
                            <div class="summary-stat-label">Secondary</div>
                        </div>
                    </div>
                    <div class="summary-card">
                        <div class="summary-stat">
                            <div class="summary-stat-value" style="color: ${this.data.PendingGC > 0 ? '#FFD600' : '#aaa'};">${this.data.PendingGC || 0}</div>
                            <div class="summary-stat-label">Pending GC</div>
                        </div>
                    </div>
                </div>

                <div class="two-col">
                    <!-- Left Column: Basic Info -->
                    <div>
                        <div class="summary-card">
                            <h3 style="margin-bottom: 15px;">Configuration</h3>
                            <div class="info-grid">
                                <span class="info-label">Type:</span>
                                <span class="info-value">
                                    <span class="tag ${this.getTypeClass(info)}">${info.PathType}</span>
                                </span>
                                
                                <span class="info-label">Weight:</span>
                                <span class="info-value">${info.Weight || 0}</span>
                                
                                <span class="info-label">Max Storage:</span>
                                <span class="info-value">${info.MaxStorageStr || 'Unlimited'}</span>
                                
                                <span class="info-label">Health:</span>
                                <span class="info-value ${info.HealthOK ? 'health-ok' : 'health-error'}">${info.HealthStatus}</span>
                                
                                ${info.GroupList?.length ? html`
                                    <span class="info-label">Groups:</span>
                                    <span class="info-value">${info.GroupList.map(g => html`<span class="tag">${g}</span>`)}</span>
                                ` : ''}
                                
                                ${info.AllowToList?.length ? html`
                                    <span class="info-label">Allow To:</span>
                                    <span class="info-value">${info.AllowToList.map(g => html`<span class="tag">${g}</span>`)}</span>
                                ` : ''}
                                
                                ${info.AllowTypesList?.length ? html`
                                    <span class="info-label">Allow Types:</span>
                                    <span class="info-value">${info.AllowTypesList.map(t => html`<span class="tag">${t}</span>`)}</span>
                                ` : ''}
                                
                                ${info.DenyTypesList?.length ? html`
                                    <span class="info-label">Deny Types:</span>
                                    <span class="info-value">${info.DenyTypesList.map(t => html`<span class="tag" style="background: rgba(182,51,51,0.3); color: #B63333;">${t}</span>`)}</span>
                                ` : ''}
                                
                                ${info.AllowMinersList?.length ? html`
                                    <span class="info-label">Allow Miners:</span>
                                    <span class="info-value">${info.AllowMinersList.map(m => html`<span class="tag">${m}</span>`)}</span>
                                ` : ''}
                                
                                ${info.DenyMinersList?.length ? html`
                                    <span class="info-label">Deny Miners:</span>
                                    <span class="info-value">${info.DenyMinersList.map(m => html`<span class="tag" style="background: rgba(182,51,51,0.3); color: #B63333;">${m}</span>`)}</span>
                                ` : ''}
                            </div>
                        </div>
                        
                        <!-- Storage Usage -->
                        <div class="summary-card">
                            <h3 style="margin-bottom: 15px;">Storage Usage</h3>
                            <div class="usage-bar">
                                <div class="usage-used" style="width: ${info.UsedPercent || 0}%;"></div>
                                <div class="usage-reserved" style="width: ${info.ReservedPercent || 0}%;"></div>
                            </div>
                            <div class="usage-text">${(info.UsedPercent || 0).toFixed(1)}% used + ${(info.ReservedPercent || 0).toFixed(1)}% reserved</div>
                            <div class="info-grid" style="margin-top: 15px;">
                                <span class="info-label">Capacity:</span>
                                <span class="info-value">${info.CapacityStr}</span>
                                
                                <span class="info-label">Used:</span>
                                <span class="info-value">${info.UsedStr}</span>
                                
                                <span class="info-label">Available:</span>
                                <span class="info-value">${info.AvailableStr}</span>
                                
                                <span class="info-label">FS Available:</span>
                                <span class="info-value">${info.FSAvailableStr}</span>
                                
                                <span class="info-label">Reserved:</span>
                                <span class="info-value">${info.ReservedStr}</span>
                            </div>
                        </div>
                    </div>
                    
                    <!-- Right Column: URLs and Stats -->
                    <div>
                        <!-- URL Status -->
                        <div class="summary-card">
                            <h3 style="margin-bottom: 15px;">Hosts (${info.HostList?.length || 0})</h3>
                            ${info.HostList?.length ? html`
                                ${info.HostList.map(host => {
                                    const urlInfo = this.data.URLs?.find(u => u.Host === host);
                                    const machineId = this.hostMachineMap[host];
                                    return html`
                                        <div class="url-status">
                                            <span class="url-status-dot ${urlInfo?.IsLive ? 'live' : 'dead'}"></span>
                                            <span style="flex: 1;">
                                                ${machineId ? html`<a href="/pages/node_info/?id=${machineId}">${host}</a>` : host}
                                            </span>
                                            ${urlInfo ? html`
                                                <span style="font-size: 0.85em; color: #aaa;">
                                                    ${urlInfo.IsLive ? `Live ${urlInfo.LastLiveStr}` : `Dead ${urlInfo.LastDeadStr}`}
                                                </span>
                                            ` : ''}
                                        </div>
                                    `;
                                })}
                            ` : html`<p style="color: #aaa;">No hosts configured</p>`}
                        </div>
                        
                        <!-- Sectors by Type -->
                        <div class="summary-card">
                            <h3 style="margin-bottom: 15px;">Sectors by Type</h3>
                            ${this.data.ByType?.length ? html`
                                <table class="table table-dark table-sm">
                                    <thead>
                                        <tr>
                                            <th>Type</th>
                                            <th style="text-align: right;">Count</th>
                                            <th style="text-align: right;">Primary</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        ${this.data.ByType.map(t => html`
                                            <tr>
                                                <td><span class="tag">${t.FileType}</span></td>
                                                <td style="text-align: right;">${t.Count}</td>
                                                <td style="text-align: right;">${t.Primary}</td>
                                            </tr>
                                        `)}
                                    </tbody>
                                </table>
                            ` : html`<p style="color: #aaa;">No sectors</p>`}
                        </div>
                        
                        <!-- Sectors by Miner -->
                        ${this.data.ByMiner?.length ? html`
                            <div class="summary-card">
                                <h3 style="margin-bottom: 15px;">Sectors by Miner</h3>
                                <table class="table table-dark table-sm">
                                    <thead>
                                        <tr>
                                            <th>Miner</th>
                                            <th style="text-align: right;">Count</th>
                                            <th style="text-align: right;">Primary</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        ${this.data.ByMiner.map(m => html`
                                            <tr>
                                                <td><a href="/pages/actor/?id=${m.Miner}">${m.Miner}</a></td>
                                                <td style="text-align: right;">${m.Count}</td>
                                                <td style="text-align: right;">${m.Primary}</td>
                                            </tr>
                                        `)}
                                    </tbody>
                                </table>
                            </div>
                        ` : ''}
                    </div>
                </div>
                
                <!-- Sectors List -->
                <div class="summary-card" style="margin-top: 20px;">
                    <h3 style="margin-bottom: 15px;">Sector Entries (${this.sectorsTotal})</h3>
                    ${this.sectorsLoading ? html`<p>Loading sectors...</p>` : html`
                        ${this.sectors.length ? html`
                            <table class="table table-dark">
                                <thead>
                                    <tr>
                                        <th>Miner</th>
                                        <th>Sector</th>
                                        <th>Type</th>
                                        <th>Copy</th>
                                        <th>Locks</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    ${this.sectors.map(s => html`
                                        <tr>
                                            <td><a href="/pages/actor/?id=${s.Miner}">${s.Miner}</a></td>
                                            <td><a href="/pages/sector/?sp=${s.Miner}&id=${s.SectorNum}">${s.SectorNum}</a></td>
                                            <td><span class="tag">${s.FileTypeStr}</span></td>
                                            <td>
                                                <span class="${s.IsPrimary ? 'primary-badge' : 'secondary-badge'}">
                                                    ${s.IsPrimary ? 'Primary' : 'Secondary'}
                                                </span>
                                            </td>
                                            <td>
                                                ${s.ReadRefs > 0 ? html`<span style="color: #3B82F6;">R:${s.ReadRefs}</span>` : ''}
                                                ${s.HasWriteLock ? html`<span style="color: #FFD600; margin-left: 5px;">W</span>` : ''}
                                                ${!s.ReadRefs && !s.HasWriteLock ? '-' : ''}
                                            </td>
                                        </tr>
                                    `)}
                                </tbody>
                            </table>
                            <div class="pagination">
                                <button class="btn btn-secondary btn-sm" @click="${this.prevPage}" ?disabled="${this.sectorsPage === 0}">Previous</button>
                                <span>Page ${this.sectorsPage + 1} of ${Math.ceil(this.sectorsTotal / 50) || 1}</span>
                                <button class="btn btn-secondary btn-sm" @click="${this.nextPage}" ?disabled="${(this.sectorsPage + 1) * 50 >= this.sectorsTotal}">Next</button>
                            </div>
                        ` : html`<p style="color: #aaa;">No sector entries on this path</p>`}
                    `}
                </div>
                
                <!-- GC Marks -->
                ${this.data.GCMarks?.length ? html`
                    <div class="summary-card" style="margin-top: 20px;">
                        <h3 style="margin-bottom: 15px;">Recent GC Marks</h3>
                        <table class="table table-dark">
                            <thead>
                                <tr>
                                    <th>Miner</th>
                                    <th>Sector</th>
                                    <th>Type</th>
                                    <th>Created</th>
                                    <th>Status</th>
                                </tr>
                            </thead>
                            <tbody>
                                ${this.data.GCMarks.map(m => html`
                                    <tr>
                                        <td><a href="/pages/actor/?id=${m.Miner}">${m.Miner}</a></td>
                                        <td><a href="/pages/sector/?sp=${m.Miner}&id=${m.SectorNum}">${m.SectorNum}</a></td>
                                        <td><span class="tag">${m.FileTypeStr}</span></td>
                                        <td>${m.CreatedAtStr}</td>
                                        <td class="${m.Approved ? 'gc-approved' : 'gc-pending'}">
                                            ${m.Approved ? 'Approved' : 'Pending'}
                                        </td>
                                    </tr>
                                `)}
                            </tbody>
                        </table>
                        <a href="/gc/" class="btn btn-secondary btn-sm" style="margin-top: 10px;">Manage GC</a>
                    </div>
                ` : ''}
            </div>
        `;
    }

    getTypeClass(info) {
        if (info.CanSeal && info.CanStore) return 'tag-seal';
        if (info.CanSeal) return 'tag-seal';
        if (info.CanStore) return 'tag-store';
        return 'tag-readonly';
    }
});
