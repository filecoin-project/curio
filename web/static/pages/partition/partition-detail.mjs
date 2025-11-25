import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('partition-detail', class PartitionDetail extends LitElement {
    static styles = css`
        :host {
            display: block;
        }
        .info-block {
            margin-bottom: 2rem;
        }
        .info-block h2 {
            font-weight: 400;
            margin-bottom: 1rem;
        }
        .stat-overview {
            display: flex;
            gap: 2rem;
            margin-bottom: 2rem;
            flex-wrap: wrap;
        }
        .stat-item {
            min-width: 150px;
        }
        .stat-value {
            font-size: 2rem;
            font-weight: 400;
            color: var(--color-primary-light, #8BEFE0);
        }
        .stat-label {
            color: var(--color-text-dense, #e0e0e0);
            font-size: 0.9rem;
            margin-top: 0.25rem;
        }
        .side-by-side {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 2rem;
        }
        @media (max-width: 1200px) {
            .side-by-side {
                grid-template-columns: 1fr;
            }
        }
        table {
            width: 100%;
            border-collapse: collapse;
            font-size: 0.9rem;
        }
        th {
            text-align: left;
            padding: 0.5rem;
            border-bottom: 1px solid #444;
            font-weight: 400;
            color: var(--color-text-dense, #e0e0e0);
        }
        td {
            padding: 0.5rem;
            border-bottom: 1px solid #333;
        }
        tr:hover {
            background-color: rgba(255, 255, 255, 0.03);
        }
        .faulty {
            background-color: rgba(182, 51, 51, 0.2);
        }
        .recovering {
            background-color: rgba(255, 214, 0, 0.1);
        }
        .loading {
            text-align: center;
            padding: 2rem;
            color: var(--color-text-dense, #e0e0e0);
        }
        .error {
            background-color: var(--color-danger-main, #B63333);
            color: white;
            padding: 1rem;
            border-radius: 4px;
            margin: 1rem 0;
        }
        a {
            color: var(--color-primary-light, #8BEFE0);
            text-decoration: none;
        }
        a:hover {
            text-decoration: underline;
        }
        .sector-list {
            max-height: 600px;
            overflow-y: auto;
        }
        .path-url {
            font-size: 0.85rem;
            color: #999;
        }
    `;

    constructor() {
        super();
        this.data = null;
        this.loading = true;
        this.error = null;
        this.loadData();
    }

    async loadData() {
        try {
            const params = new URLSearchParams(window.location.search);
            const sp = params.get('sp');
            const deadline = parseInt(params.get('deadline'), 10);
            const partition = parseInt(params.get('partition'), 10);

            if (!sp || isNaN(deadline) || isNaN(partition)) {
                this.error = 'Missing or invalid SP, deadline, or partition parameter';
                this.loading = false;
                this.requestUpdate();
                return;
            }

            this.data = await RPCCall('PartitionDetail', [sp, deadline, partition]);
            this.loading = false;
            this.requestUpdate();
        } catch (err) {
            console.error('Failed to load partition detail:', err);
            this.error = err.message || 'Failed to load data';
            this.loading = false;
            this.requestUpdate();
        }
    }

    renderStoragePaths() {
        if (!this.data.faulty_storage_paths || this.data.faulty_storage_paths.length === 0) {
            return html`<p>No faulty sectors or no storage path data available</p>`;
        }

        return html`
            <table>
                <thead>
                    <tr>
                        <th>Storage ID</th>
                        <th>Type</th>
                        <th>Faulty Sectors</th>
                        <th>URLs</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.data.faulty_storage_paths.map(path => html`
                        <tr>
                            <td><strong>${path.storage_id}</strong></td>
                            <td>${path.path_type}</td>
                            <td>${path.count}</td>
                            <td class="path-url">${path.urls.join(', ')}</td>
                        </tr>
                    `)}
                </tbody>
            </table>
        `;
    }

    renderSectors() {
        if (!this.data.sectors || this.data.sectors.length === 0) {
            return html`<p>No sectors in this partition</p>`;
        }

        return html`
            <div class="sector-list">
                <table>
                    <thead>
                        <tr>
                            <th>Sector #</th>
                            <th>Status</th>
                            <th>Active</th>
                            <th>Live</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${this.data.sectors.map(sector => {
                            let statusClass = '';
                            let statusText = 'OK';
                            if (sector.is_faulty) {
                                statusClass = 'faulty';
                                statusText = 'FAULTY';
                            } else if (sector.is_recovering) {
                                statusClass = 'recovering';
                                statusText = 'RECOVERING';
                            }
                            
                            return html`
                                <tr class="${statusClass}">
                                    <td>
                                        <a href="/pages/sector/?sp=${this.data.sp_address}&id=${sector.sector_number}">
                                            ${sector.sector_number}
                                        </a>
                                    </td>
                                    <td style="${sector.is_faulty ? 'color: var(--color-danger-main, #B63333); font-weight: 500;' : sector.is_recovering ? 'color: var(--color-warning-main, #FFD600); font-weight: 500;' : ''}">
                                        ${statusText}
                                    </td>
                                    <td>${sector.is_active ? '✓' : '✗'}</td>
                                    <td>${sector.is_live ? '✓' : '✗'}</td>
                                </tr>
                            `;
                        })}
                    </tbody>
                </table>
            </div>
        `;
    }

    render() {
        if (this.loading) {
            return html`<div class="loading">Loading partition detail...</div>`;
        }

        if (this.error) {
            return html`<div class="error">Error: ${this.error}</div>`;
        }

        if (!this.data) {
            return html`<div class="error">No data available</div>`;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            
            <h1>Partition ${this.data.partition} - Deadline ${this.data.deadline} - ${this.data.sp_address}</h1>
            
            <div class="info-block">
                <div class="stat-overview">
                    <div class="stat-item">
                        <div class="stat-value">${this.data.all_sectors_count.toLocaleString()}</div>
                        <div class="stat-label">Total Sectors</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value">${this.data.active_sectors_count.toLocaleString()}</div>
                        <div class="stat-label">Active Sectors</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value">${this.data.live_sectors_count.toLocaleString()}</div>
                        <div class="stat-label">Live Sectors</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value" style="color: ${this.data.faulty_sectors_count > 0 ? 'var(--color-danger-main, #B63333)' : 'inherit'};">
                            ${this.data.faulty_sectors_count.toLocaleString()}
                        </div>
                        <div class="stat-label">Faulty Sectors</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value" style="color: ${this.data.recovering_sectors_count > 0 ? 'var(--color-warning-main, #FFD600)' : 'inherit'};">
                            ${this.data.recovering_sectors_count.toLocaleString()}
                        </div>
                        <div class="stat-label">Recovering Sectors</div>
                    </div>
                </div>
            </div>

            <div class="side-by-side">
                <div class="info-block">
                    <h2>Sectors in Partition</h2>
                    ${this.renderSectors()}
                </div>
                
                ${this.data.faulty_sectors_count > 0 ? html`
                    <div class="info-block">
                        <h2>Storage Paths with Faulty Sectors</h2>
                        ${this.renderStoragePaths()}
                    </div>
                ` : ''}
            </div>

            <div style="margin-top: 2rem;">
                <a href="/pages/deadline/?sp=${this.data.sp_address}&deadline=${this.data.deadline}">← Back to Deadline ${this.data.deadline}</a>
                |
                <a href="/sector/">← Back to Sector Dashboard</a>
            </div>
        `;
    }
});

