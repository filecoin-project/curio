import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('deadline-detail', class DeadlineDetail extends LitElement {
    static styles = css`
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

            if (!sp || isNaN(deadline)) {
                this.error = 'Missing or invalid SP or deadline parameter';
                this.loading = false;
                this.requestUpdate();
                return;
            }

            this.data = await RPCCall('DeadlineDetail', [sp, deadline]);
            this.loading = false;
            this.requestUpdate();
        } catch (err) {
            console.error('Failed to load deadline detail:', err);
            this.error = err.message || 'Failed to load data';
            this.loading = false;
            this.requestUpdate();
        }
    }

    render() {
        if (this.loading) {
            return html`<div style="text-align: center; padding: 2rem;">Loading deadline detail...</div>`;
        }

        if (this.error) {
            return html`<div class="error">${this.error}</div>`;
        }

        if (!this.data) {
            return html`<div class="error">No data available</div>`;
        }

        const totalAll = this.data.partitions.reduce((sum, p) => sum + p.all_sectors, 0);
        const totalFaulty = this.data.partitions.reduce((sum, p) => sum + p.faulty_sectors, 0);
        const totalRecovering = this.data.partitions.reduce((sum, p) => sum + p.recovering_sectors, 0);
        const totalActive = this.data.partitions.reduce((sum, p) => sum + p.active_sectors, 0);

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            
            <h1>Deadline ${this.data.deadline} - ${this.data.sp_address}</h1>
            
            <div class="info-block">
                <div class="stat-overview">
                    <div class="stat-item">
                        <div class="stat-value">${this.data.partitions.length}</div>
                        <div class="stat-label">Partitions</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value">${totalAll.toLocaleString()}</div>
                        <div class="stat-label">Total Sectors</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value">${totalActive.toLocaleString()}</div>
                        <div class="stat-label">Active Sectors</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value" style="color: ${totalFaulty > 0 ? 'var(--color-danger-main, #B63333)' : 'inherit'};">
                            ${totalFaulty.toLocaleString()}
                        </div>
                        <div class="stat-label">Faulty Sectors</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value" style="color: ${totalRecovering > 0 ? 'var(--color-warning-main, #FFD600)' : 'inherit'};">
                            ${totalRecovering.toLocaleString()}
                        </div>
                        <div class="stat-label">Recovering Sectors</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value">${this.data.post_submissions}</div>
                        <div class="stat-label">Post Submissions</div>
                    </div>
                    <div class="stat-item">
                        <div class="stat-value">${this.data.disputable_proof_count}</div>
                        <div class="stat-label">Disputable Proofs</div>
                    </div>
                </div>
            </div>

            <div class="info-block">
                <h2>Partitions</h2>
                <table class="table table-dark">
                    <thead>
                        <tr>
                            <th>Partition</th>
                            <th>All Sectors</th>
                            <th>Active</th>
                            <th>Live</th>
                            <th>Faulty</th>
                            <th>Recovering</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${this.data.partitions.map(part => html`
                            <tr>
                                <td><strong>${part.partition}</strong></td>
                                <td>${part.all_sectors.toLocaleString()}</td>
                                <td>${part.active_sectors.toLocaleString()}</td>
                                <td>${part.live_sectors.toLocaleString()}</td>
                                <td style="${part.faulty_sectors > 0 ? 'color: var(--color-danger-main, #B63333); font-weight: 500;' : ''}">
                                    ${part.faulty_sectors.toLocaleString()}
                                </td>
                                <td style="${part.recovering_sectors > 0 ? 'color: var(--color-warning-main, #FFD600); font-weight: 500;' : ''}">
                                    ${part.recovering_sectors.toLocaleString()}
                                </td>
                                <td>
                                    <a href="/pages/partition/?sp=${this.data.sp_address}&deadline=${this.data.deadline}&partition=${part.partition}">
                                        View Details →
                                    </a>
                                </td>
                            </tr>
                        `)}
                    </tbody>
                </table>
            </div>

            <div style="margin-top: 2rem;">
                <a href="/sector/">← Back to Sector Dashboard</a>
            </div>
        `;
    }
});

