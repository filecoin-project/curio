import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('sector-dashboard', class SectorDashboard extends LitElement {
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
            min-width: 200px;
        }
        .stat-value {
            font-size: 2.5rem;
            font-weight: 400;
            color: var(--color-primary-light, #8BEFE0);
        }
        .stat-label {
            color: var(--color-text-dense, #e0e0e0);
            font-size: 0.9rem;
            margin-top: 0.25rem;
        }
        .row {
            display: flex;
            gap: 2rem;
            margin-bottom: 2rem;
            flex-wrap: wrap;
        }
        .col-md-auto {
            flex: 1;
            min-width: 400px;
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
        .badge-pipeline {
            padding: 0.25rem 0.5rem;
            border-radius: 3px;
            font-size: 0.8rem;
            display: inline-block;
        }
        .badge-porep {
            background-color: #303060;
            color: white;
        }
        .badge-snap {
            background-color: #a06010;
            color: white;
        }
        .badge-failed {
            background-color: #603030;
            color: white;
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
    `;

    constructor() {
        super();
        this.spStats = null;
        this.pipelineStats = null;
        this.deadlineStats = null;
        this.fileTypeStats = null;
        this.gcStats = null;
        this.loading = true;
        this.refreshing = false;
        this.error = null;
        this.loadData();
        // Refresh every 10 seconds
        setInterval(() => this.loadData(), 10000);
    }

    async loadData() {
        try {
            // Set refreshing flag instead of loading if we already have data
            if (this.spStats !== null) {
                this.refreshing = true;
            } else {
                this.loading = true;
            }
            this.error = null;
            this.requestUpdate();

            const [spStats, pipelineStats, deadlineStats, fileTypeStats, gcStats] = await Promise.all([
                RPCCall('SectorSPStats', []),
                RPCCall('SectorPipelineStats', []),
                RPCCall('SectorDeadlineStats', []),
                RPCCall('SectorFileTypeStats', []),
                RPCCall('StorageGCStats', [])
            ]);

            this.spStats = spStats;
            this.pipelineStats = pipelineStats;
            this.deadlineStats = deadlineStats;
            this.fileTypeStats = fileTypeStats;
            this.gcStats = gcStats;
            this.loading = false;
            this.refreshing = false;
            this.requestUpdate();
        } catch (err) {
            console.error('Failed to load sector stats:', err);
            this.error = err.message || 'Failed to load data';
            this.loading = false;
            this.refreshing = false;
            this.requestUpdate();
        }
    }

    renderOverviewCards() {
        if (!this.spStats || this.spStats.length === 0) {
            return html`<p>No sector data available</p>`;
        }

        const totals = this.spStats.reduce((acc, sp) => {
            acc.total += sp.total_count;
            acc.cc += sp.cc_count;
            acc.nonCC += sp.non_cc_count;
            return acc;
        }, { total: 0, cc: 0, nonCC: 0 });

        return html`
            <div class="stat-overview">
                <div class="stat-item">
                    <div class="stat-value">${totals.total.toLocaleString()}</div>
                    <div class="stat-label">Total Sectors</div>
                </div>
                <div class="stat-item">
                    <div class="stat-value">${totals.cc.toLocaleString()}</div>
                    <div class="stat-label">CC Sectors</div>
                </div>
                <div class="stat-item">
                    <div class="stat-value">${totals.nonCC.toLocaleString()}</div>
                    <div class="stat-label">Deal Sectors</div>
                </div>
            </div>
        `;
    }

    renderSPTable() {
        if (!this.spStats || this.spStats.length === 0) {
            return html`<p>No SP data available</p>`;
        }

        return html`
            <table>
                <thead>
                    <tr>
                        <th>SP Address</th>
                        <th>Total Sectors</th>
                        <th>CC Sectors</th>
                        <th>Deal Sectors</th>
                        <th>Deal %</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.spStats.map(sp => {
                        const dealPercent = sp.total_count > 0 
                            ? ((sp.non_cc_count / sp.total_count) * 100).toFixed(1)
                            : '0.0';
                        return html`
                            <tr>
                                <td><strong>${sp.sp_address}</strong></td>
                                <td>${sp.total_count.toLocaleString()}</td>
                                <td>${sp.cc_count.toLocaleString()}</td>
                                <td>${sp.non_cc_count.toLocaleString()}</td>
                                <td>${dealPercent}%</td>
                            </tr>
                        `;
                    })}
                </tbody>
            </table>
        `;
    }

    renderPipelineTable() {
        if (!this.pipelineStats || this.pipelineStats.length === 0) {
            return html`<p>No pipeline data available</p>`;
        }

        // Group by pipeline type
        const porepStats = this.pipelineStats.filter(s => s.pipeline_type === 'PoRep');
        const snapStats = this.pipelineStats.filter(s => s.pipeline_type === 'Snap');

        return html`
            <table>
                <thead>
                    <tr>
                        <th>Pipeline</th>
                        <th>Stage</th>
                        <th>Count</th>
                    </tr>
                </thead>
                <tbody>
                    ${porepStats.map(stat => html`
                        <tr>
                            <td><span class="badge-pipeline ${stat.stage === 'Failed' ? 'badge-failed' : 'badge-porep'}">PoRep</span></td>
                            <td>${stat.stage}</td>
                            <td><strong>${stat.count.toLocaleString()}</strong></td>
                        </tr>
                    `)}
                    ${snapStats.map(stat => html`
                        <tr>
                            <td><span class="badge-pipeline ${stat.stage === 'Failed' ? 'badge-failed' : 'badge-snap'}">Snap</span></td>
                            <td>${stat.stage}</td>
                            <td><strong>${stat.count.toLocaleString()}</strong></td>
                        </tr>
                    `)}
                </tbody>
            </table>
        `;
    }

    renderDeadlineTable() {
        if (!this.deadlineStats || this.deadlineStats.length === 0) {
            return html`<p>No deadline data available</p>`;
        }

        // Group by SP
        const groupedBysp = {};
        this.deadlineStats.forEach(stat => {
            if (!groupedBysp[stat.sp_address]) {
                groupedBysp[stat.sp_address] = [];
            }
            groupedBysp[stat.sp_address].push(stat);
        });

        return html`
            ${Object.entries(groupedBysp).map(([spAddress, stats]) => {
                const total = stats.reduce((acc, s) => acc + s.count, 0);
                const totalFaulty = stats.reduce((acc, s) => acc + (s.faulty_sectors || 0), 0);
                const totalRecovering = stats.reduce((acc, s) => acc + (s.recovering_sectors || 0), 0);
                const totalActive = stats.reduce((acc, s) => acc + (s.active_sectors || 0), 0);
                
                return html`
                    <h3 style="margin-top: 1.5rem; margin-bottom: 0.5rem;">
                        ${spAddress} 
                        <span style="font-weight: 200; font-size: 0.9rem;">
                            (${total.toLocaleString()} sectors
                            ${totalFaulty > 0 ? html`, <span style="color: var(--color-danger-main, #B63333);">${totalFaulty} faulty</span>` : ''}
                            ${totalRecovering > 0 ? html`, <span style="color: var(--color-warning-main, #FFD600);">${totalRecovering} recovering</span>` : ''})
                        </span>
                    </h3>
                    <table style="margin-bottom: 1.5rem;">
                        <thead>
                            <tr>
                                <th>Deadline</th>
                                <th>Sectors</th>
                                <th>%</th>
                                <th>Active</th>
                                <th>Faulty</th>
                                <th>Recovering</th>
                                <th>Post Sub.</th>
                                <th>Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${stats.sort((a, b) => a.deadline - b.deadline).map(stat => {
                                const percent = ((stat.count / total) * 100).toFixed(1);
                                const hasChainData = stat.all_sectors > 0;
                                return html`
                                    <tr>
                                        <td>DL ${stat.deadline}</td>
                                        <td>${stat.count.toLocaleString()}</td>
                                        <td>${percent}%</td>
                                        <td>${hasChainData ? stat.active_sectors.toLocaleString() : '-'}</td>
                                        <td style="${stat.faulty_sectors > 0 ? 'color: var(--color-danger-main, #B63333);' : ''}">
                                            ${hasChainData ? stat.faulty_sectors.toLocaleString() : '-'}
                                        </td>
                                        <td style="${stat.recovering_sectors > 0 ? 'color: var(--color-warning-main, #FFD600);' : ''}">
                                            ${hasChainData ? stat.recovering_sectors.toLocaleString() : '-'}
                                        </td>
                                        <td>${stat.post_submissions || '-'}</td>
                                        <td>
                                            <a href="/pages/deadline/?sp=${stat.sp_address}&deadline=${stat.deadline}">Details →</a>
                                        </td>
                                    </tr>
                                `;
                            })}
                        </tbody>
                    </table>
                `;
            })}
        `;
    }

    renderFileTypeTable() {
        if (!this.fileTypeStats || this.fileTypeStats.length === 0) {
            return html`<p>No file type data available</p>`;
        }

        const total = this.fileTypeStats.reduce((acc, stat) => acc + stat.count, 0);

        return html`
            <table>
                <thead>
                    <tr>
                        <th>File Type</th>
                        <th>Sector Count</th>
                        <th>Percentage</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.fileTypeStats.map(stat => {
                        const percent = total > 0 ? ((stat.count / total) * 100).toFixed(1) : '0.0';
                        return html`
                            <tr>
                                <td>${stat.file_type}</td>
                                <td>${stat.count.toLocaleString()}</td>
                                <td>${percent}%</td>
                            </tr>
                        `;
                    })}
                </tbody>
            </table>
        `;
    }

    renderGCStats() {
        if (!this.gcStats || this.gcStats.length === 0) {
            return html`<p>No sectors marked for garbage collection</p>`;
        }

        const total = this.gcStats.reduce((acc, stat) => acc + stat.Count, 0);

        return html`
            <p style="margin-bottom: 0.5rem;">${total.toLocaleString()} sectors marked for removal</p>
            <table>
                <thead>
                    <tr>
                        <th>Miner</th>
                        <th>Marked for Removal</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.gcStats.map(stat => html`
                        <tr>
                            <td>${stat.Miner}</td>
                            <td>${stat.Count.toLocaleString()}</td>
                        </tr>
                    `)}
                </tbody>
            </table>
            <p style="margin-top: 1rem; font-size: 0.9rem;">
                <a href="/gc/">Manage GC marks →</a>
            </p>
        `;
    }

    render() {
        if (this.loading) {
            return html`<div class="loading">Loading sector statistics...</div>`;
        }

        if (this.error) {
            return html`<div class="error">Error: ${this.error}</div>`;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <div class="info-block" style="position: relative;">
                ${this.refreshing ? html`<span style="position: absolute; top: 0; right: 0; color: var(--color-text-dense, #e0e0e0); font-size: 0.8rem;">Refreshing...</span>` : ''}
                ${this.renderOverviewCards()}
            </div>
            
            <div class="info-block">
                <h2>Storage Providers</h2>
                ${this.renderSPTable()}
            </div>

            <div class="row">
                <div class="col-md-auto">
                    <div class="info-block">
                        <h2>Pipeline Status</h2>
                        ${this.renderPipelineTable()}
                    </div>
                </div>
                <div class="col-md-auto">
                    <div class="info-block">
                        <h2>GC Marks</h2>
                        ${this.renderGCStats()}
                    </div>
                </div>
            </div>

            <div class="row">
                <div class="col-md-auto">
                    <div class="info-block">
                        <h2>Deadline Distribution</h2>
                        ${this.renderDeadlineTable()}
                    </div>
                </div>
                <div class="col-md-auto">
                    <div class="info-block">
                        <h2>Sector File Types</h2>
                        ${this.renderFileTypeTable()}
                    </div>
                </div>
            </div>
        `;
    }
});

