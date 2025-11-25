import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('sector-expiration', class SectorExpiration extends LitElement {
    static styles = css`
        .manage-link {
            color: var(--color-primary-light, #8BEFE0);
            cursor: pointer;
            text-decoration: underline;
            font-size: 0.9rem;
            margin-bottom: 0.5rem;
            display: inline-block;
        }
        .manage-link:hover {
            color: var(--color-primary-main, #4CAF50);
        }
        .modal {
            position: fixed;
            top: 0;
            left: 0;
            z-index: 1050;
            width: 100%;
            height: 100%;
            overflow: hidden;
            outline: 0;
            display: flex;
            align-items: center;
            justify-content: center;
            backdrop-filter: blur(5px);
        }
        .modal-backdrop {
            position: fixed;
            top: 0;
            left: 0;
            z-index: 1040;
            width: 100vw;
            height: 100vh;
            background-color: rgba(0, 0, 0, 0.5);
        }
        .modal-dialog {
            max-width: 500px;
            margin: 1.75rem auto;
            z-index: 1050;
        }
        .modal-content {
            background-color: var(--color-form-field, #1d1d21);
            border: 1px solid var(--color-form-default, #808080);
            border-radius: 0.3rem;
            box-shadow: 0 0.5rem 1rem rgba(0, 0, 0, 0.5);
            color: var(--color-text-primary, #FFF);
        }
        .modal-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 1rem;
            border-bottom: 1px solid var(--color-form-default, #808080);
        }
        .modal-body {
            padding: 1rem;
        }
        .modal-footer {
            display: flex;
            align-items: center;
            justify-content: flex-end;
            padding: 1rem;
            border-top: 1px solid var(--color-form-default, #808080);
        }
        .bucket-add-form {
            display: flex;
            gap: 0.5rem;
            align-items: center;
            margin-bottom: 1rem;
        }
        .bucket-add-form input {
            width: 150px;
            padding: 0.375rem 0.75rem;
            border: 1px solid #555;
            background-color: #2a2a2a;
            color: #e0e0e0;
            border-radius: 4px;
        }
        .bucket-add-form button {
            padding: 0.375rem 0.75rem;
            background-color: var(--color-primary-main, #4CAF50);
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
        }
        .bucket-add-form button:hover {
            background-color: var(--color-primary-dark, #45a049);
        }
        .btn-danger {
            background-color: var(--color-danger-main, #B63333);
            color: white;
            border: none;
            padding: 0.25rem 0.5rem;
            border-radius: 3px;
            cursor: pointer;
            font-size: 0.85rem;
        }
        .btn-danger:hover {
            background-color: #a02828;
        }
        .btn-secondary {
            background-color: #6c757d;
            color: white;
            border: none;
            padding: 0.375rem 0.75rem;
            border-radius: 4px;
            cursor: pointer;
        }
        .btn-secondary:hover {
            background-color: #5a6268;
        }
        .exp-table {
            margin-top: 1rem;
        }
        .exp-table td:nth-child(even),
        .exp-table th:nth-child(even) {
            background-color: rgba(33, 37, 41, 0.7);
        }
        .sp-group {
            margin-top: 1.5rem;
        }
        .sp-group h3 {
            margin-bottom: 0.5rem;
            font-size: 1.1rem;
        }
        .sp-selector {
            margin-bottom: 1rem;
            display: flex;
            gap: 0.5rem;
            flex-wrap: wrap;
        }
        .sp-selector label {
            display: flex;
            align-items: center;
            gap: 0.25rem;
            padding: 0.25rem 0.5rem;
            background-color: #2a2a2a;
            border-radius: 4px;
            cursor: pointer;
        }
        .sp-selector input[type="checkbox"] {
            cursor: pointer;
        }
        .loading {
            text-align: center;
            padding: 2rem;
            color: var(--color-text-dense, #e0e0e0);
        }
        .error {
            color: var(--color-danger-main, #B63333);
            padding: 1rem;
        }
        .cell-with-bar {
            padding: 0.5rem 1rem !important;
        }
        .cell-content {
            display: flex;
            align-items: center;
            gap: 1rem;
        }
        .cell-number {
            flex: 1;
        }
        .cell-bar-section {
            flex: 1;
            display: flex;
            flex-direction: column;
            align-items: flex-end;
            gap: 0.2rem;
            min-width: 60px;
        }
        .cell-percent {
            font-size: 0.75em;
            color: var(--color-text-dense, #999);
        }
        .cell-bar-container {
            width: 100%;
            height: 2.5px;
            background-color: rgba(128, 128, 128, 0.2);
            position: relative;
        }
        .cell-bar {
            position: absolute;
            top: 0;
            left: 0;
            height: 2.5px;
            background: linear-gradient(90deg, var(--color-secondary-main, #8BEFE0) 0%, var(--color-secondary-light, #4CAF50) 100%);
            transition: width 0.3s ease;
        }
    `;

    constructor() {
        super();
        this.buckets = null;
        this.counts = null;
        this.loading = true;
        this.error = null;
        this.newBucketDays = '';
        this.showModal = false;
        this.selectedSPs = new Set();
        this.loadData();
        // Refresh every 30 seconds
        setInterval(() => this.loadData(), 30000);
    }

    async loadData() {
        try {
            this.error = null;
            const [buckets, counts] = await Promise.all([
                RPCCall('SectorExpBuckets', []),
                RPCCall('SectorExpBucketCounts', [])
            ]);
            this.buckets = buckets;
            this.counts = counts;
            this.loading = false;
            this.requestUpdate();
        } catch (err) {
            console.error('Failed to load sector expiration data:', err);
            this.error = err.message || 'Failed to load data';
            this.loading = false;
            this.requestUpdate();
        }
    }

    openModal() {
        this.showModal = true;
        this.requestUpdate();
    }

    closeModal() {
        this.showModal = false;
        this.requestUpdate();
    }

    toggleSP(spId) {
        if (this.selectedSPs.has(spId)) {
            this.selectedSPs.delete(spId);
        } else {
            this.selectedSPs.add(spId);
        }
        this.requestUpdate();
    }

    getBucketRanges() {
        if (!this.buckets || this.buckets.length === 0) return [];
        
        // Sort buckets
        const sorted = [...this.buckets].sort((a, b) => a.less_than_days - b.less_than_days);
        const ranges = [];
        
        // Create ranges: 0 to first, then between consecutive buckets, then last to infinity
        for (let i = 0; i < sorted.length; i++) {
            if (i === 0) {
                ranges.push({
                    low: 0,
                    high: sorted[i].less_than_days,
                    highBucket: sorted[i].less_than_days,
                    isLast: false
                });
            } else {
                ranges.push({
                    low: sorted[i - 1].less_than_days,
                    high: sorted[i].less_than_days,
                    highBucket: sorted[i].less_than_days,
                    isLast: false
                });
            }
        }
        
        // Add last open-ended range
        if (sorted.length > 0) {
            ranges.push({
                low: sorted[sorted.length - 1].less_than_days,
                high: null,
                highBucket: null,
                isLast: true
            });
        }
        
        return ranges;
    }

    async addBucket() {
        const days = parseInt(this.newBucketDays, 10);
        if (isNaN(days) || days <= 0) {
            alert('Please enter a valid positive number of days');
            return;
        }
        try {
            await RPCCall('SectorExpBucketAdd', [days]);
            this.newBucketDays = '';
            this.requestUpdate();
            await this.loadData();
        } catch (err) {
            console.error('Failed to add bucket:', err);
            alert(`Failed to add bucket: ${err.message}`);
        }
    }

    async deleteBucket(days) {
        if (!confirm(`Delete bucket for ${days} days?`)) {
            return;
        }
        try {
            await RPCCall('SectorExpBucketDelete', [days]);
            await this.loadData();
        } catch (err) {
            console.error('Failed to delete bucket:', err);
            alert(`Failed to delete bucket: ${err.message}`);
        }
    }

    handleInputChange(e) {
        this.newBucketDays = e.target.value;
        this.requestUpdate();
    }

    handleKeyPress(e) {
        if (e.key === 'Enter') {
            this.addBucket();
        }
    }

    renderBucketModal() {
        if (!this.showModal || !this.buckets) return html``;

        return html`
            <div class="modal-backdrop" @click="${this.closeModal}"></div>
            <div class="modal">
                <div class="modal-dialog">
                    <div class="modal-content">
                        <div class="modal-header">
                            <h5>Manage Expiration Buckets</h5>
                            <button type="button" class="btn-secondary" @click="${this.closeModal}">×</button>
                        </div>
                        <div class="modal-body">
                            <div class="bucket-add-form">
                                <input 
                                    type="number" 
                                    placeholder="Days"
                                    .value="${this.newBucketDays}"
                                    @input="${this.handleInputChange}"
                                    @keypress="${this.handleKeyPress}"
                                />
                                <button @click="${this.addBucket}">Add Bucket</button>
                            </div>
                            <table class="table table-dark">
                                <thead>
                                    <tr>
                                        <th>Days</th>
                                        <th>Actions</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    ${this.buckets.map(bucket => html`
                                        <tr>
                                            <td>${bucket.less_than_days} days</td>
                                            <td>
                                                <button 
                                                    class="btn-danger" 
                                                    @click="${() => this.deleteBucket(bucket.less_than_days)}"
                                                >
                                                    Delete
                                                </button>
                                            </td>
                                        </tr>
                                    `)}
                                </tbody>
                            </table>
                        </div>
                        <div class="modal-footer">
                            <button type="button" class="btn-secondary" @click="${this.closeModal}">Close</button>
                        </div>
                    </div>
                </div>
            </div>
        `;
    }

    renderExpirationCounts() {
        if (!this.buckets || !this.counts) return html``;

        // Group cumulative counts by SP
        const spGroups = {};
        this.counts.forEach(count => {
            if (!spGroups[count.sp_id]) {
                spGroups[count.sp_id] = {
                    address: count.sp_address,
                    cumulativeCounts: {}
                };
            }
            const key = count.less_than_days === -1 ? 'open' : count.less_than_days;
            spGroups[count.sp_id].cumulativeCounts[key] = count;
        });

        const bucketRanges = this.getBucketRanges();
        const allSPs = Object.entries(spGroups);

        // Convert cumulative counts to range counts for each SP
        allSPs.forEach(([spId, spData]) => {
            spData.rangeCounts = {};
            
            bucketRanges.forEach((range, idx) => {
                const bucket = range.highBucket || 'open';
                
                if (range.isLast) {
                    // Open-ended bucket: use the -1 marker data directly
                    const openCount = spData.cumulativeCounts['open'];
                    spData.rangeCounts['open'] = openCount || {
                        total_count: 0,
                        cc_count: 0,
                        deal_count: 0
                    };
                } else {
                    // Calculate range by subtracting previous cumulative from current
                    const currentCumulative = spData.cumulativeCounts[range.high] || { total_count: 0, cc_count: 0, deal_count: 0 };
                    const previousCumulative = idx > 0 && spData.cumulativeCounts[bucketRanges[idx - 1].high] 
                        ? spData.cumulativeCounts[bucketRanges[idx - 1].high]
                        : { total_count: 0, cc_count: 0, deal_count: 0 };
                    
                    spData.rangeCounts[bucket] = {
                        total_count: currentCumulative.total_count - previousCumulative.total_count,
                        cc_count: currentCumulative.cc_count - previousCumulative.cc_count,
                        deal_count: currentCumulative.deal_count - previousCumulative.deal_count
                    };
                }
            });
        });

        // Calculate aggregated "All" data from range counts
        const aggregatedData = {};
        bucketRanges.forEach(range => {
            const bucket = range.highBucket || 'open';
            aggregatedData[bucket] = {
                total_count: 0,
                cc_count: 0,
                deal_count: 0
            };
        });

        allSPs.forEach(([spId, spData]) => {
            bucketRanges.forEach(range => {
                const bucket = range.highBucket || 'open';
                const count = spData.rangeCounts[bucket];
                if (count) {
                    aggregatedData[bucket].total_count += count.total_count;
                    aggregatedData[bucket].cc_count += count.cc_count;
                    aggregatedData[bucket].deal_count += count.deal_count;
                }
            });
        });

        return html`
            <div class="info-block">
                <h2>Sector Expirations by Bucket</h2>
                <a class="manage-link" @click="${this.openModal}">manage expiration buckets</a>
                
                <!-- Always show aggregate "All" view -->
                <div class="sp-group">
                    <h3>All SPs</h3>
                    ${this.renderSPTable('All SPs', aggregatedData, bucketRanges)}
                </div>

                <!-- Individual SP selector and tables -->
                ${allSPs.length > 0 ? html`
                    <h3 style="margin-top: 2rem; margin-bottom: 0.5rem;">Per-SP Details</h3>
                    <div class="sp-selector">
                        ${allSPs.map(([spId, spData]) => html`
                            <label>
                                <input 
                                    type="checkbox" 
                                    ?checked="${this.selectedSPs.has(spId)}"
                                    @change="${() => this.toggleSP(spId)}"
                                />
                                ${spData.address}
                            </label>
                        `)}
                    </div>
                ` : ''}

                ${allSPs.filter(([spId]) => this.selectedSPs.has(spId)).map(([spId, spData]) => html`
                    <div class="sp-group">
                        <h3>${spData.address}</h3>
                        ${this.renderSPTable(spData.address, spData.rangeCounts, bucketRanges)}
                    </div>
                `)}
            </div>
        `;
    }

    renderSPTable(title, rangeCounts, bucketRanges) {
        // Calculate column totals
        let totalSum = 0;
        let ccSum = 0;
        let dealSum = 0;
        
        bucketRanges.forEach(range => {
            const bucket = range.highBucket || 'open';
            const count = rangeCounts[bucket] || { total_count: 0, cc_count: 0, deal_count: 0 };
            totalSum += count.total_count;
            ccSum += count.cc_count;
            dealSum += count.deal_count;
        });

        return html`
            <table class="table table-dark exp-table">
                <thead>
                    <tr>
                        <th>Bucket (Days)</th>
                        <th>Total Sectors</th>
                        <th>CC Sectors</th>
                        <th>Deal Sectors</th>
                        <th>Deal %</th>
                    </tr>
                </thead>
                <tbody>
                    ${bucketRanges.map(range => {
                        const bucket = range.highBucket || 'open';
                        const count = rangeCounts[bucket] || { total_count: 0, cc_count: 0, deal_count: 0 };
                        const totalCount = count.total_count;
                        const ccCount = count.cc_count;
                        const dealCount = count.deal_count;
                        
                        // Calculate percentages of column totals
                        const totalPercent = totalSum > 0 ? ((totalCount / totalSum) * 100).toFixed(1) : '0.0';
                        const ccPercent = ccSum > 0 ? ((ccCount / ccSum) * 100).toFixed(1) : '0.0';
                        const dealPercent = dealSum > 0 ? ((dealCount / dealSum) * 100).toFixed(1) : '0.0';
                        
                        // Deal % of row total
                        const rowDealPercent = totalCount > 0 
                            ? ((dealCount / totalCount) * 100).toFixed(1)
                            : '0.0';
                        
                        const rangeLabel = range.isLast 
                            ? `${range.low} < EXP`
                            : `${range.low} < EXP < ${range.high}`;
                        
                        return html`
                            <tr>
                                <td>${rangeLabel}</td>
                                <td class="cell-with-bar">
                                    <div class="cell-content">
                                        <span class="cell-number"><strong>${totalCount.toLocaleString()}</strong></span>
                                        <div class="cell-bar-section">
                                            <span class="cell-percent">${totalPercent}%</span>
                                            <div class="cell-bar-container">
                                                <div class="cell-bar" style="width: ${totalPercent}%"></div>
                                            </div>
                                        </div>
                                    </div>
                                </td>
                                <td class="cell-with-bar">
                                    <div class="cell-content">
                                        <span class="cell-number">${ccCount.toLocaleString()}</span>
                                        <div class="cell-bar-section">
                                            <span class="cell-percent">${ccPercent}%</span>
                                            <div class="cell-bar-container">
                                                <div class="cell-bar" style="width: ${ccPercent}%"></div>
                                            </div>
                                        </div>
                                    </div>
                                </td>
                                <td class="cell-with-bar">
                                    <div class="cell-content">
                                        <span class="cell-number">${dealCount.toLocaleString()}</span>
                                        <div class="cell-bar-section">
                                            <span class="cell-percent">${dealPercent}%</span>
                                            <div class="cell-bar-container">
                                                <div class="cell-bar" style="width: ${dealPercent}%"></div>
                                            </div>
                                        </div>
                                    </div>
                                </td>
                                <td>${rowDealPercent}%</td>
                            </tr>
                        `;
                    })}
                </tbody>
            </table>
        `;
    }

    render() {
        if (this.loading) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
                <link rel="stylesheet" href="/ux/main.css">
                <div class="loading">Loading sector expiration data...</div>
            `;
        }

        if (this.error) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
                <link rel="stylesheet" href="/ux/main.css">
                <div class="error">${this.error}</div>
            `;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css">
            ${this.renderExpirationCounts()}
            ${this.renderBucketModal()}
        `;
    }
});

