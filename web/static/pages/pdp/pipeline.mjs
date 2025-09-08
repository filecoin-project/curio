import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { formatDate } from '/lib/dateutil.mjs';

class MK20PDPPipelines extends LitElement {
    static properties = {
        deals: { type: Array },
        limit: { type: Number },
        offset: { type: Number },
        totalCount: { type: Number },
        failedTasks: { type: Object },
        restartingTaskType: { type: String },
        removingTaskType: { type: String }
    };

    constructor() {
        super();
        this.deals = [];
        this.limit = 25;
        this.offset = 0;
        this.totalCount = 0;
        this.failedTasks = {};
        this.restartingTaskType = '';
        this.removingTaskType = '';
        this.loadData();
    }

    connectedCallback() {
        super.connectedCallback();
        // Set up an interval to update data every 5 seconds
        this.intervalId = setInterval(() => this.loadData(), 5000);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        // Clear the interval when the element is disconnected
        clearInterval(this.intervalId);
    }

    async loadData() {
        try {
            const params = [this.limit, this.offset];
            const deals = await RPCCall('MK20PDPPipelines', params);
            this.deals = deals || [];

            // Load failed tasks data
            const failed = await RPCCall('MK20PipelineFailedTasks', []);
            this.failedTasks = failed || {};

            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load deal pipelines or failed tasks:', error);
        }
    }

    nextPage() {
        this.offset += this.limit;
        this.loadData();
    }

    prevPage() {
        if (this.offset >= this.limit) {
            this.offset -= this.limit;
        } else {
            this.offset = 0;
        }
        this.loadData();
    }

    renderFailedTasks() {
        const { DownloadingFailed, CommPFailed, AggFailed, IndexFailed } = this.failedTasks;
        const entries = [];

        const renderLine = (label, count, type) => {
            const isRestarting = this.restartingTaskType === type;
            const isRemoving = this.removingTaskType === type;
            const isWorking = isRestarting || isRemoving;
            return html`
                <div>
                    <b>${label}</b> Task: ${count}
                    <details style="display: inline-block; margin-left: 8px;">
                        <summary class="btn btn-sm btn-info">
                            ${isWorking ? 'Working...' : 'Actions'}
                        </summary>
                        <button
                                class="btn btn-sm btn-warning"
                                @click="${() => this.restartFailedTasks(type)}"
                                ?disabled=${isWorking}
                        >
                            Restart All
                        </button>
                        <button
                                class="btn btn-sm btn-danger"
                                @click="${() => this.removeFailedPipelines(type)}"
                                ?disabled=${isWorking}
                        >
                            Remove All
                        </button>
                    </details>
                </div>
            `;
        };

        if (DownloadingFailed > 0) {
            entries.push(renderLine('Downloading', DownloadingFailed, 'downloading'));
        }
        if (CommPFailed > 0) {
            entries.push(renderLine('CommP', CommPFailed, 'commp'));
        }
        if (AggFailed > 0) {
            entries.push(renderLine('Aggregate', AggFailed, 'aggregate'));
        }
        if (IndexFailed > 0) {
            entries.push(renderLine('Index', IndexFailed, 'index'));
        }

        if (entries.length === 0) {
            return null;
        }

        return html`
            <div class="failed-tasks">
                <h2>Failed Tasks</h2>
                ${entries}
            </div>
        `;
    }

    async restartFailedTasks(type) {
        this.restartingTaskType = type;
        this.removingTaskType = '';
        this.requestUpdate();

        try {
            await RPCCall('MK20BulkRestartFailedMarketTasks', [type]);
            await this.loadData();
        } catch (err) {
            console.error('Failed to restart tasks:', err);
            alert(`Failed to restart ${type} tasks: ${err.message || err}`);
        } finally {
            this.restartingTaskType = '';
            this.requestUpdate();
        }
    }

    async removeFailedPipelines(type) {
        this.removingTaskType = type;
        this.restartingTaskType = '';
        this.requestUpdate();

        try {
            await RPCCall('MK20BulkRemoveFailedMarketPipelines', [type]);
            await this.loadData();
        } catch (err) {
            console.error('Failed to remove pipelines:', err);
            alert(`Failed to remove ${type} pipelines: ${err.message || err}`);
        } finally {
            this.removingTaskType = '';
            this.requestUpdate();
        }
    }

    render() {
        return html`
            <link
                    href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                    rel="stylesheet"
                    crossorigin="anonymous"
            />
            <link rel="stylesheet" href="/ux/main.css" />

            <div>
                ${this.renderFailedTasks()}
                <h2>
                    Deal Pipelines
                    <button class="info-btn">
                        <!-- Inline SVG icon for the info button -->
                        <svg
                                xmlns="http://www.w3.org/2000/svg"
                                width="16"
                                height="16"
                                fill="currentColor"
                                class="bi bi-info-circle"
                                viewBox="0 0 16 16"
                        >
                            <path
                                    d="M8 15A7 7 0 1 1 8 1a7 7 0 0 1 0 14m0 1A8 8 0 1 0 8 0a8 8 0 0 0 0 16"
                            />
                            <path
                                    d="m8.93 6.588-2.29.287-.082.38.45.083c.294.07.352.176.288.469l-.738
                3.468c-.194.897.105 1.319.808 1.319.545 0
                1.178-.252 1.465-.598l.088-.416c-.2.176-.492.246-.686.246-.275
                0-.375-.193-.304-.533zM9 4.5a1 1 0 1
                1-2 0 1 1 0 0 1 2 0"
                            />
                        </svg>
                        <span class="tooltip-text">
              List of all active pieces (not deals) in the pipeline. Use the pagination
              controls to navigate through the list.
            </span>
                    </button>
                </h2>
                <table class="table table-dark table-striped table-sm">
                    <thead>
                    <tr>
                        <th>Created At</th>
                        <th>UUID</th>
                        <th>SP ID</th>
                        <th>Piece CID</th>
                        <th>Status</th>
                    </tr>
                    </thead>
                    <tbody>
                    ${this.deals.map(
            (deal) => html`
                            <tr>
                                <td>${formatDate(deal.created_at)}</td>
                                <td>
                                    <a href="/pages/mk20-deal/?id=${deal.id}">${deal.id}</a>
                                </td>
                                <td>${deal.miner}</td>
                                <td>
                                    <a href="/pages/piece/?id=${deal.piece_cid_v2}">${this.formatPieceCid(deal.piece_cid_v2)}</a>
                                </td>
                                <td>${this.getDealStatus(deal)}</td>
                            </tr>
                        `
        )}
                    </tbody>
                </table>
                <div class="pagination-controls">
                    <button
                            class="btn btn-secondary"
                            @click="${this.prevPage}"
                            ?disabled="${this.offset === 0}"
                    >
                        Previous
                    </button>
                    <span>Page ${(this.offset / this.limit) + 1}</span>
                    <button
                            class="btn btn-secondary"
                            @click="${this.nextPage}"
                            ?disabled="${this.deals.length < this.limit}"
                    >
                        Next
                    </button>
                </div>
            </div>
        `;
    }

    formatPieceCid(pieceCid) {
        if (!pieceCid) return '';
        if (pieceCid.length <= 24) {
            return pieceCid;
        }
        const start = pieceCid.substring(0, 16);
        const end = pieceCid.substring(pieceCid.length - 8);
        return `${start}...${end}`;
    }

    formatBytes(bytes) {
        const units = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB'];
        let i = 0;
        let size = bytes;
        while (size >= 1024 && i < units.length - 1) {
            size /= 1024;
            i++;
        }
        if (i === 0) {
            return `${size} ${units[i]}`;
        } else {
            return `${size.toFixed(2)} ${units[i]}`;
        }
    }

    getDealStatus(deal) {
        if (deal.complete) {
            return '(#########) Complete';
        } else if (!deal.complete && deal.announce && deal.indexed) {
            return '(########.) Announcing';
        } else if (deal.sealed && !deal.indexed) {
            return '(#######..) Indexing';
        } else if (deal.sector?.Valid && !deal.sealed) {
            return '(######...) Sealing';
        } else if (deal.aggregated && !deal.sector?.Valid) {
            return '(#####....) Assigning Sector';
        } else if (deal.after_commp && !deal.aggregated) {
            return '(####.....) Aggregating Deal';
        } else if (deal.downloaded && !deal.after_commp) {
            return '(###......) CommP';
        } else if (deal.started && !deal.downloaded) {
            return '(##.......) Downloading';
        } else {
            return '(#........) Accepted';
        }
    }

    static styles = css`
        .pagination-controls {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-top: 1rem;
        }

        .info-btn {
            position: relative;
            border: none;
            background: transparent;
            cursor: pointer;
            color: #17a2b8;
            font-size: 1em;
            margin-left: 8px;
        }

        .tooltip-text {
            display: none;
            position: absolute;
            top: 50%;
            left: 120%;
            transform: translateY(-50%);
            min-width: 440px;
            max-width: 600px;
            background-color: #333;
            color: #fff;
            padding: 8px;
            border-radius: 4px;
            font-size: 0.8em;
            text-align: left;
            white-space: normal;
            z-index: 10;
        }

        .info-btn:hover .tooltip-text {
            display: block;
        }

        .copy-btn {
            border: none;
            background: transparent;
            cursor: pointer;
            color: #17a2b8;
            padding: 0 0 0 5px;
        }

        .copy-btn svg {
            vertical-align: middle;
        }

        .copy-btn:hover {
            color: #0d6efd;
        }

        .failed-tasks {
            margin-bottom: 1rem;
        }
        .failed-tasks h2 {
            margin: 0 0 0.5rem 0;
        }

        details > summary {
            display: inline-block;
            cursor: pointer;
            outline: none;
        }

        .btn {
            margin: 0 4px;
        }
    `;
}

customElements.define('mk20-pdp-pipelines', MK20PDPPipelines);
