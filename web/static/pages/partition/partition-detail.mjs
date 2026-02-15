import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/task.mjs';

customElements.define('partition-detail', class PartitionDetail extends LitElement {
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
        .faulty {
            background-color: rgba(182, 51, 51, 0.2);
        }
        .recovering {
            background-color: rgba(255, 214, 0, 0.1);
        }
        .sector-list {
            max-height: 600px;
            overflow-y: auto;
        }
        .path-url {
            font-size: 0.85rem;
            color: #999;
        }
        .test-result-summary {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
            gap: 1rem;
            margin: 1rem 0;
            padding: 1rem;
            background: rgba(0,0,0,0.2);
            border-radius: 4px;
        }
        .test-stat {
            text-align: center;
        }
        .test-stat-value {
            font-size: 1.5rem;
            font-weight: 500;
        }
        .test-stat-label {
            font-size: 0.8rem;
            color: #aaa;
        }
        .test-results-table {
            max-height: 400px;
            overflow-y: auto;
        }
        .test-passed {
            color: #4BB543;
        }
        .test-failed {
            color: #B63333;
        }
        .test-slow {
            color: #FFD600;
        }
        .test-stat-clickable {
            cursor: pointer;
            padding: 0.5rem;
            border-radius: 4px;
            transition: background-color 0.2s;
        }
        .test-stat-clickable:hover {
            background-color: rgba(255,255,255,0.1);
        }
        .test-stat-active {
            background-color: rgba(255,255,255,0.15);
            outline: 1px solid rgba(255,255,255,0.3);
        }
    `;

    constructor() {
        super();
        this.data = null;
        this.loading = true;
        this.error = null;
        this.vanillaTestResult = null;
        this.vanillaTestRunning = false;
        // WdPost task test state
        this.wdpostTaskId = null;
        this.wdpostTaskResult = null;
        this.wdpostTaskRunning = false;
        this.wdpostTaskPolling = null;
        // Vanilla test filter: null (all), 'passed', 'failed', 'slow'
        this.vanillaTestFilter = null;
        this.loadData();
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this.wdpostTaskPolling) {
            clearInterval(this.wdpostTaskPolling);
            this.wdpostTaskPolling = null;
        }
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

            this.sp = sp;
            this.deadlineIdx = deadline;
            this.partitionIdx = partition;

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

    async runVanillaTest() {
        if (this.vanillaTestRunning) return;
        this.vanillaTestRunning = true;
        this.vanillaTestResult = null;
        this.requestUpdate();
        
        try {
            this.vanillaTestResult = await RPCCall('PartitionVanillaTest', [this.sp, this.deadlineIdx, this.partitionIdx]);
        } catch (err) {
            this.vanillaTestResult = { error: err.message };
        }
        this.vanillaTestRunning = false;
        this.requestUpdate();
    }

    async startWdPostTask() {
        if (this.wdpostTaskRunning) return;
        this.wdpostTaskRunning = true;
        this.wdpostTaskId = null;
        this.wdpostTaskResult = null;
        this.requestUpdate();

        try {
            const result = await RPCCall('WdPostTaskStart', [this.sp, this.deadlineIdx, this.partitionIdx]);
            if (result.error) {
                this.wdpostTaskResult = { error: result.error };
                this.wdpostTaskRunning = false;
                this.requestUpdate();
                return;
            }
            this.wdpostTaskId = result.task_id;
            this.wdpostTaskRunning = false;
            this.requestUpdate();

            // Start polling for task completion
            this.pollWdPostTask();
        } catch (err) {
            this.wdpostTaskResult = { error: err.message };
            this.wdpostTaskRunning = false;
            this.requestUpdate();
        }
    }

    async pollWdPostTask() {
        if (this.wdpostTaskPolling) {
            clearInterval(this.wdpostTaskPolling);
        }
        
        const checkTask = async () => {
            try {
                const result = await RPCCall('WdPostTaskCheck', [this.wdpostTaskId]);
                if (result.result || result.error) {
                    // Task completed
                    this.wdpostTaskResult = result;
                    if (this.wdpostTaskPolling) {
                        clearInterval(this.wdpostTaskPolling);
                        this.wdpostTaskPolling = null;
                    }
                }
                this.requestUpdate();
            } catch (err) {
                console.error('Failed to check WdPost task:', err);
            }
        };

        // Check immediately, then every 3 seconds
        await checkTask();
        this.wdpostTaskPolling = setInterval(checkTask, 3000);
    }

    renderWdPostTaskResult() {
        if (this.wdpostTaskResult && this.wdpostTaskResult.error) {
            return html`
                <div style="color: #B63333; padding: 1rem; background: rgba(0,0,0,0.2); border-radius: 4px; margin-top: 1rem;">
                    <strong>Error:</strong> ${this.wdpostTaskResult.error}
                </div>
            `;
        }

        if (this.wdpostTaskResult && this.wdpostTaskResult.result) {
            return html`
                <div style="color: #4BB543; padding: 1rem; background: rgba(0,0,0,0.2); border-radius: 4px; margin-top: 1rem;">
                    <strong>Completed:</strong> ${this.wdpostTaskResult.result}
                </div>
            `;
        }

        return '';
    }

    toggleVanillaFilter(filter) {
        // Toggle off if clicking the same filter, otherwise set new filter
        this.vanillaTestFilter = this.vanillaTestFilter === filter ? null : filter;
        this.requestUpdate();
    }

    renderVanillaTestResults() {
        if (!this.vanillaTestResult) return '';
        
        if (this.vanillaTestResult.error) {
            return html`
                <div style="color: #B63333; padding: 1rem; background: rgba(0,0,0,0.2); border-radius: 4px;">
                    <strong>Error:</strong> ${this.vanillaTestResult.error}
                </div>
            `;
        }

        const r = this.vanillaTestResult;
        
        // Filter results based on selected filter
        let filteredResults = r.results || [];
        if (this.vanillaTestFilter === 'passed') {
            filteredResults = filteredResults.filter(x => x.generate_ok && x.verify_ok);
        } else if (this.vanillaTestFilter === 'failed') {
            filteredResults = filteredResults.filter(x => !x.generate_ok || !x.verify_ok);
        } else if (this.vanillaTestFilter === 'slow') {
            filteredResults = filteredResults.filter(x => x.slow);
        }
        
        return html`
            <div class="test-result-summary">
                <div class="test-stat">
                    <div class="test-stat-value">${r.tested_count}</div>
                    <div class="test-stat-label">Tested</div>
                </div>
                <div class="test-stat test-stat-clickable ${this.vanillaTestFilter === 'passed' ? 'test-stat-active' : ''}"
                     @click="${() => this.toggleVanillaFilter('passed')}"
                     title="Click to filter">
                    <div class="test-stat-value test-passed">${r.passed_count}</div>
                    <div class="test-stat-label">Passed</div>
                </div>
                <div class="test-stat test-stat-clickable ${this.vanillaTestFilter === 'failed' ? 'test-stat-active' : ''}"
                     @click="${() => this.toggleVanillaFilter('failed')}"
                     title="Click to filter">
                    <div class="test-stat-value test-failed">${r.failed_count}</div>
                    <div class="test-stat-label">Failed</div>
                </div>
                <div class="test-stat test-stat-clickable ${this.vanillaTestFilter === 'slow' ? 'test-stat-active' : ''}"
                     @click="${() => this.toggleVanillaFilter('slow')}"
                     title="Click to filter">
                    <div class="test-stat-value test-slow">${r.slow_count}</div>
                    <div class="test-stat-label">Slow (&gt;2s)</div>
                </div>
                <div class="test-stat">
                    <div class="test-stat-value">${r.total_time}</div>
                    <div class="test-stat-label">Total Time</div>
                </div>
            </div>
            
            ${filteredResults.length > 0 ? html`
                ${this.vanillaTestFilter ? html`
                    <div style="margin-bottom: 0.5rem; color: #aaa; font-size: 0.9em;">
                        Showing ${filteredResults.length} ${this.vanillaTestFilter} sector${filteredResults.length !== 1 ? 's' : ''}
                        <a href="#" @click="${(e) => { e.preventDefault(); this.toggleVanillaFilter(this.vanillaTestFilter); }}" style="margin-left: 0.5rem;">(clear filter)</a>
                    </div>
                ` : ''}
                <div class="test-results-table">
                    <table class="table table-dark table-sm">
                        <thead>
                            <tr>
                                <th>Sector</th>
                                <th>Generate</th>
                                <th>Verify</th>
                                <th>Gen Time</th>
                                <th>Verify Time</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${filteredResults.map(result => html`
                                <tr class="${result.slow ? 'test-slow' : ''}">
                                    <td>
                                        <a href="/pages/sector/?sp=${this.sp}&id=${result.sector_number}">
                                            ${result.sector_number}
                                        </a>
                                    </td>
                                    <td class="${result.generate_ok ? 'test-passed' : 'test-failed'}">
                                        ${result.generate_ok ? 'OK' : 'FAIL'}
                                        ${result.generate_error ? html`<br><small>${result.generate_error}</small>` : ''}
                                    </td>
                                    <td class="${result.verify_ok ? 'test-passed' : result.generate_ok ? 'test-failed' : ''}">
                                        ${result.generate_ok ? (result.verify_ok ? 'OK' : 'FAIL') : '-'}
                                        ${result.verify_error ? html`<br><small>${result.verify_error}</small>` : ''}
                                    </td>
                                    <td>${result.generate_time}</td>
                                    <td>${result.verify_time || '-'}</td>
                                </tr>
                            `)}
                        </tbody>
                    </table>
                </div>
            ` : html`
                ${this.vanillaTestFilter ? html`
                    <div style="color: #aaa; padding: 1rem;">
                        No ${this.vanillaTestFilter} sectors
                        <a href="#" @click="${(e) => { e.preventDefault(); this.toggleVanillaFilter(this.vanillaTestFilter); }}" style="margin-left: 0.5rem;">(clear filter)</a>
                    </div>
                ` : ''}
            `}
        `;
    }

    renderStoragePaths() {
        if (!this.data.faulty_storage_paths || this.data.faulty_storage_paths.length === 0) {
            return html`<p>No faulty sectors or no storage path data available</p>`;
        }

        return html`
            <table class="table table-dark">
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
                <table class="table table-dark">
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
            return html`<div style="text-align: center; padding: 2rem;">Loading partition detail...</div>`;
        }

        if (this.error) {
            return html`<div class="error">${this.error}</div>`;
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

            <div class="info-block" style="margin-top: 2rem;">
                <h2>WindowPoSt Vanilla Test</h2>
                <p style="font-size: 0.9em; color: #aaa; margin-bottom: 1rem;">
                    Test vanilla proof generation and verification for all ${this.data.live_sectors_count} live sectors in this partition.
                    This simulates the work done during WindowPoSt proving.
                </p>
                <button 
                    class="btn btn-primary" 
                    @click="${() => this.runVanillaTest()}"
                    ?disabled="${this.vanillaTestRunning}"
                >
                    ${this.vanillaTestRunning ? 'Running Test...' : 'Run Vanilla Proof Test'}
                </button>
                ${this.vanillaTestRunning ? html`
                    <span style="margin-left: 1rem; color: #aaa;">
                        Testing ${this.data.live_sectors_count} sectors in parallel...
                    </span>
                ` : ''}
                ${this.renderVanillaTestResults()}
            </div>

            <div class="info-block" style="margin-top: 2rem;">
                <h2>WindowPoSt Task Test</h2>
                <p style="font-size: 0.9em; color: #aaa; margin-bottom: 1rem;">
                    Create an actual WdPost task in the Curio task scheduler for this partition.
                    This tests the full WindowPoSt pipeline including task pickup, proof generation, and SNARK aggregation.
                    The task will be marked as a test and will not submit proofs on-chain.
                </p>
                <button 
                    class="btn btn-warning" 
                    @click="${() => this.startWdPostTask()}"
                    ?disabled="${this.wdpostTaskRunning || this.wdpostTaskId !== null}"
                >
                    ${this.wdpostTaskRunning ? 'Creating Task...' : 'Run WdPost Task Test'}
                </button>
                ${this.wdpostTaskId !== null ? html`
                    <span style="margin-left: 1rem;">
                        Task: <task-status .taskId="${this.wdpostTaskId}"></task-status>
                    </span>
                ` : ''}
                ${this.renderWdPostTaskResult()}
            </div>

            <div style="margin-top: 2rem;">
                <a href="/pages/deadline/?sp=${this.data.sp_address}&deadline=${this.data.deadline}">← Back to Deadline ${this.data.deadline}</a>
                |
                <a href="/sector/">← Back to Sector Dashboard</a>
            </div>
        `;
    }
});

