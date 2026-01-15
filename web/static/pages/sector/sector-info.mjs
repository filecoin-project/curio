import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { renderSectorPipeline, pipelineStyles } from '/pages/pipeline_porep/pipeline-porep-sectors.mjs';
import { renderSectorSnapPipeline, snapPipelineStyles} from '/snap/upgrade-sectors.mjs';
import '/ux/epoch.mjs';
import '/ux/message.mjs';
import '/ux/task.mjs';

customElements.define('sector-info',class SectorInfo extends LitElement {
    constructor() {
        super();
        this.expandedPieces = new Set();
        this.gcMarks = [];
        this.loadData();
        this.loadGCMarks();
    }
    
    togglePiece(index) {
        if (this.expandedPieces.has(index)) {
            this.expandedPieces.delete(index);
        } else {
            this.expandedPieces.add(index);
        }
        this.requestUpdate();
    }
    
    formatCid(cid) {
        if (!cid || cid === '') return 'N/A';
        if (cid.length <= 16) return cid;
        return `${cid.substring(0, 8)}...${cid.substring(cid.length - 8)}`;
    }
    
    formatSize(bytes) {
        if (!bytes) return 'N/A';
        const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
        let size = parseInt(bytes);
        if (isNaN(size)) return 'N/A';
        let unitIndex = 0;
        while (size >= 1024 && unitIndex < units.length - 1) {
            size /= 1024;
            unitIndex++;
        }
        return `${size.toFixed(2)} ${units[unitIndex]}`;
    }
    async loadData() {
        const params = new URLSearchParams(window.location.search);
        this.data = await RPCCall('SectorInfo', [params.get('sp'), params.get('id')|0]);
        this.requestUpdate();
        setTimeout(() => this.loadData(), 5000);
    }
    
    async loadGCMarks() {
        const params = new URLSearchParams(window.location.search);
        const sectorNum = params.get('id')|0;
        const sp = params.get('sp');
        try {
            const result = await RPCCall('StorageGCMarks', [sp, sectorNum, 1000, 0]);
            this.gcMarks = result.Marks || [];
            this.requestUpdate();
        } catch (err) {
            console.error('Failed to load GC marks:', err);
        }
        setTimeout(() => this.loadGCMarks(), 5000);
    }
    async removeSector() {
        await RPCCall('SectorRemove', [this.data.SpID, this.data.SectorNumber]);
        window.location.href = '/pages/pipeline_porep/';
    }
    async resumeSector() {
        await RPCCall('SectorResume', [this.data.SpID, this.data.SectorNumber]);
        await this.loadData();
    }
    async restartSector() {
        await RPCCall('SectorRestart', [this.data.SpID, this.data.SectorNumber]);
        await this.loadData();
    }
    async approveGCMark(actor, sectorNum, fileType, storageId) {
        try {
            await RPCCall('StorageGCApprove', [actor, sectorNum, fileType, storageId]);
            await this.loadGCMarks();
        } catch (err) {
            console.error('Failed to approve GC mark:', err);
            alert('Failed to approve GC mark: ' + err.message);
        }
    }
    async unapproveGCMark(actor, sectorNum, fileType, storageId) {
        try {
            await RPCCall('StorageGCUnapprove', [actor, sectorNum, fileType, storageId]);
            await this.loadGCMarks();
        } catch (err) {
            console.error('Failed to unapprove GC mark:', err);
            alert('Failed to unapprove GC mark: ' + err.message);
        }
    }

    static styles = [pipelineStyles, snapPipelineStyles, css`
        .piece-row {
            cursor: pointer;
        }
        .piece-row:hover {
            background-color: rgba(255, 255, 255, 0.05);
        }
        .piece-details {
            background-color: rgba(0, 0, 0, 0.2);
        }
        .piece-details td {
            padding: 10px 15px;
        }
        .detail-grid {
            display: grid;
            grid-template-columns: auto 1fr;
            gap: 8px 20px;
            font-size: 0.9em;
        }
        .detail-label {
            font-weight: 500;
            color: #aaa;
        }
        .detail-value {
            word-break: break-all;
        }
        .toggle-icon {
            margin-right: 8px;
            display: inline-block;
            transition: transform 0.2s;
        }
        .toggle-icon.expanded {
            transform: rotate(90deg);
        }
        .state-badge {
            display: inline-block;
            padding: 4px 12px;
            border-radius: 4px;
            margin-right: 8px;
            font-size: 0.85em;
            font-weight: 500;
        }
        .state-badge.active {
            background-color: rgba(75, 181, 67, 0.2);
            color: #4BB543;
            border: 1px solid #4BB543;
        }
        .state-badge.inactive {
            background-color: rgba(128, 128, 128, 0.2);
            color: #888;
            border: 1px solid #666;
        }
        .state-badge.faulty {
            background-color: rgba(182, 51, 51, 0.2);
            color: #B63333;
            border: 1px solid #B63333;
        }
        .state-badge.unproven {
            background-color: rgba(255, 214, 0, 0.2);
            color: #FFD600;
            border: 1px solid #FFD600;
        }
        .state-overview {
            display: flex;
            gap: 1rem;
            flex-wrap: wrap;
            align-items: center;
        }
    `];

    render() {
        if (!this.data) {
            return html`<div>Loading...</div>`;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <div style="margin-bottom: 20px;">
                <h2 style="text-align: center; margin-top: 20px;">Sector ${this.data.SectorNumber}</h2>
            </div>
            <div>
                <h3>Sector Info</h3>
                <table class="table table-dark table-striped table-sm">
                        <tr><td>Miner ID</td><td>${this.data.Miner}</td></tr>
                        <tr><td>Sector Number</td><td>${this.data.SectorNumber}</td></tr>
                        <tr>
                            <td>PreCommit Message</td>
                            <td>${this.data.PreCommitMsg ? html`<fil-message cid="${this.data.PreCommitMsg}"></fil-message>` : 'N/A'}</td>
                        </tr>
                        <tr>
                            <td>Commit Message</td>
                            <td>${this.data.CommitMsg ? html`<fil-message cid="${this.data.CommitMsg}"></fil-message>` : 'N/A'}</td>
                        </tr>
                        <tr><td>Activation Epoch</td><td><pretty-epoch epoch=${this.data.ActivationEpoch}></pretty-epoch></td></tr>
                        <tr><td>Expiration Epoch</td><td><pretty-epoch epoch=${this.data.ExpirationEpoch}></pretty-epoch></td></tr>
                        <tr><td>Deal Weight</td><td>${this.data.DealWeight}</td></tr>
                        <tr>
                            <td>Deadline</td>
                            <td>
                                ${this.data.Deadline != null ? html`<a href="/pages/deadline/?sp=${this.data.Miner}&deadline=${this.data.Deadline}">${this.data.Deadline}</a>` : 'N/A'}
                            </td>
                        </tr>
                        <tr>
                            <td>Partition</td>
                            <td>
                                ${this.data.Partition != null && this.data.Deadline != null ? html`<a href="/pages/partition/?sp=${this.data.Miner}&deadline=${this.data.Deadline}&partition=${this.data.Partition}">${this.data.Partition}</a>` : this.data.Partition != null ? this.data.Partition : 'N/A'}
                            </td>
                        </tr>
                        <tr><td>Unsealed CID</td><td>${this.data.UnsealedCid}</td></tr>
                        <tr><td>Sealed CID</td><td>${this.data.SealedCid}</td></tr>
                        <tr><td>Updated Unsealed CID</td><td>${this.data.UpdatedUnsealedCid}</td></tr>
                        <tr><td>Updated Sealed CID</td><td>${this.data.UpdatedSealedCid}</td></tr>
                        <tr><td>Is Snap</td><td>${this.data.IsSnap}</td></tr>
                        <tr>
                            <td>Update Message</td>
                            <td>${this.data.UpdateMsg ? html`<fil-message cid="${this.data.UpdateMsg}"></fil-message>` : 'N/A'}</td>
                        </tr>
                        <tr>
                            <td>Unsealed State</td>
                            <td style="color: ${
                                    (this.data.UnsealedState === false && this.data.HasUnsealed) ||
                                    (this.data.UnsealedState === true && !this.data.HasUnsealed)
                                            ? 'orange'
                                            : 'inherit'
                            }">
                                ${this.data.UnsealedState == null
                                        ? 'Either'
                                        : this.data.UnsealedState
                                                ? 'Keep Unsealed'
                                                : 'Remove Unsealed'}
                            </td>
                        </tr>
                </table>
            </div>
            ${this.data.PipelinePoRep ? html`
                <div>
                    <h3>On-Chain Sector State</h3>
                    <div class="state-overview">
                        <span class="state-badge ${this.data.PipelinePoRep.ChainAlloc ? 'active' : 'inactive'}">
                            ${this.data.PipelinePoRep.ChainAlloc ? '✓' : '✗'} Allocated
                        </span>
                        <span class="state-badge ${this.data.PipelinePoRep.ChainSector ? 'active' : 'inactive'}">
                            ${this.data.PipelinePoRep.ChainSector ? '✓' : '✗'} Live (All Sectors)
                        </span>
                        <span class="state-badge ${this.data.PipelinePoRep.ChainActive ? 'active' : 'inactive'}">
                            ${this.data.PipelinePoRep.ChainActive ? '✓' : '✗'} Active
                        </span>
                        <span class="state-badge ${this.data.PipelinePoRep.ChainFaulty ? 'faulty' : 'active'}">
                            ${this.data.PipelinePoRep.ChainFaulty ? '⚠' : '✓'} ${this.data.PipelinePoRep.ChainFaulty ? 'Faulty' : 'Not Faulty'}
                        </span>
                        <span class="state-badge ${this.data.PipelinePoRep.ChainUnproven ? 'unproven' : 'active'}">
                            ${this.data.PipelinePoRep.ChainUnproven ? '⚠' : '✓'} ${this.data.PipelinePoRep.ChainUnproven ? 'Unproven' : 'Proven'}
                        </span>
                    </div>
                    <p style="margin-top: 10px; font-size: 0.9em; color: #aaa;">
                        These states reflect the sector's membership in on-chain bitfields from the miner actor.
                    </p>
                </div>
            ` : ''}
            <div>
                ${this.data.PipelinePoRep ? html`
                    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px;">
                        <h3 style="margin: 0;">PoRep Pipeline</h3>
                        <div style="display: flex; gap: 10px;">
                            ${this.data.Resumable ? html`
                                <button class="btn btn-primary btn-sm" @click="${() => this.resumeSector()}">Resume Pipeline</button>
                            ` : ''}
                            ${this.data.Restart ? html`
                                <details style="display: inline-block;">
                                    <summary class="btn btn-warning btn-sm">Restart Pipeline</summary>
                                    <button class="btn btn-danger btn-sm" @click="${() => this.restartSector()}">Confirm Restart (Deletes Cache & Sealed Files)</button>
                                </details>
                            ` : ''}
                            <details style="display: inline-block;">
                                <summary class="btn btn-warning btn-sm">Remove from Pipeline ${!this.data.PipelinePoRep?.Failed && !this.data.PipelineSnap?.Failed ? '(NOT FAILED!)' : ''}</summary>
                                <button class="btn btn-danger btn-sm" @click="${() => this.removeSector()}">Confirm Remove (Marks Files for GC)</button>
                            </details>
                        </div>
                    </div>
                    ${renderSectorPipeline(this.data.PipelinePoRep)}
                ` : html`
                    <p>No data available for the PoRep pipeline.</p>
                `}
            </div>
            <div>
                ${this.data.PipelineSnap ? html`
                    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px;">
                        <h3 style="margin: 0;">SnapDeals Pipeline</h3>
                        <div style="display: flex; gap: 10px;">
                            ${this.data.Resumable ? html`
                                <button class="btn btn-primary btn-sm" @click="${() => this.resumeSector()}">Resume Pipeline</button>
                            ` : ''}
                            ${this.data.Restart ? html`
                                <details style="display: inline-block;">
                                    <summary class="btn btn-warning btn-sm">Restart Pipeline</summary>
                                    <button class="btn btn-danger btn-sm" @click="${() => this.restartSector()}">Confirm Restart (Deletes Cache & Sealed Files)</button>
                                </details>
                            ` : ''}
                            <details style="display: inline-block;">
                                <summary class="btn btn-warning btn-sm">Remove from Pipeline ${!this.data.PipelinePoRep?.Failed && !this.data.PipelineSnap?.Failed ? '(NOT FAILED!)' : ''}</summary>
                                <button class="btn btn-danger btn-sm" @click="${() => this.removeSector()}">Confirm Remove (Marks Files for GC)</button>
                            </details>
                        </div>
                    </div>
                    ${renderSectorSnapPipeline(this.data.PipelineSnap)}
                ` : html`
                    <p>No data available for the SnapDeals pipeline.</p>
                `}
            </div>
            <div>
                <h3>Pieces</h3>
                ${(this.data.Pieces||[]).length === 0 ? html`<p>No pieces in this sector</p>` : html`
                    <table class="table table-dark">
                        <thead>
                            <tr>
                                <th style="width: 40px;"></th>
                                <th>Index</th>
                                <th>CID</th>
                                <th>Size</th>
                                <th>Deal ID</th>
                                <th>Pipeline</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${(this.data.Pieces||[]).map((piece, index) => {
                                const isExpanded = this.expandedPieces.has(index);
                                return html`
                                    <tr class="piece-row" @click="${() => this.togglePiece(index)}">
                                        <td>
                                            <span class="toggle-icon ${isExpanded ? 'expanded' : ''}">▶</span>
                                        </td>
                                        <td><strong>${piece.PieceIndex}</strong></td>
                                        <td>
                                            ${piece.PieceCidV2 && piece.PieceCidV2.trim() !== ""
                                                ? html`<a href="/pages/piece/?id=${piece.PieceCidV2}" @click="${(e) => e.stopPropagation()}">${this.formatCid(piece.PieceCidV2)}</a>`
                                                : piece.PieceCid ? this.formatCid(piece.PieceCid) : 'N/A'}
                                        </td>
                                        <td>${this.formatSize(piece.PieceSize)}</td>
                                        <td>
                                            ${piece.DealID ? html`<a href="/pages/mk12-deal/?id=${piece.DealID}" @click="${(e) => e.stopPropagation()}">${piece.DealID}</a>` : 'N/A'}
                                        </td>
                                        <td>
                                            <span style="color: ${piece.IsSnapPiece ? 'var(--color-warning-main, #FFD600)' : 'var(--color-primary-light, #8BEFE0)'}">
                                                ${piece.IsSnapPiece ? 'SnapDeals' : 'PoRep'}
                                            </span>
                                        </td>
                                    </tr>
                                    ${isExpanded ? html`
                                        <tr class="piece-details">
                                            <td colspan="6">
                                                <div class="detail-grid">
                                                    ${piece.PieceCid ? html`
                                                        <div class="detail-label">Piece CID:</div>
                                                        <div class="detail-value">${piece.PieceCid}</div>
                                                    ` : ''}
                                                    ${piece.PieceCidV2 && piece.PieceCidV2.trim() !== "" ? html`
                                                        <div class="detail-label">Piece CID V2:</div>
                                                        <div class="detail-value">${piece.PieceCidV2}</div>
                                                    ` : ''}
                                                    ${piece.DataUrl ? html`
                                                        <div class="detail-label">Data URL:</div>
                                                        <div class="detail-value">${piece.DataUrl}</div>
                                                    ` : ''}
                                                    ${piece.DataRawSize ? html`
                                                        <div class="detail-label">Data Raw Size:</div>
                                                        <div class="detail-value">${this.formatSize(piece.DataRawSize)}</div>
                                                    ` : ''}
                                                    <div class="detail-label">Delete On Finalize:</div>
                                                    <div class="detail-value">${piece.DeleteOnFinalize === null ? 'Either' : piece.DeleteOnFinalize ? 'Yes' : 'No'}</div>
                                                    ${piece.F05PublishCid ? html`
                                                        <div class="detail-label">F05 Publish CID:</div>
                                                        <div class="detail-value">${piece.F05PublishCid}</div>
                                                    ` : ''}
                                                    ${piece.F05DealID ? html`
                                                        <div class="detail-label">F05 Deal ID:</div>
                                                        <div class="detail-value">${piece.F05DealID}</div>
                                                    ` : ''}
                                                    ${piece.DDOPam ? html`
                                                        <div class="detail-label">DDO PAM:</div>
                                                        <div class="detail-value">${piece.DDOPam}</div>
                                                    ` : ''}
                                                    ${piece.IsParkedPiece ? html`
                                                        <div class="detail-label">PiecePark ID:</div>
                                                        <div class="detail-value">${piece.PieceParkID}</div>
                                                        ${piece.PieceParkDataUrl ? html`
                                                            <div class="detail-label">PiecePark URL:</div>
                                                            <div class="detail-value">${piece.PieceParkDataUrl}</div>
                                                        ` : ''}
                                                        ${piece.PieceParkCreatedAt ? html`
                                                            <div class="detail-label">PiecePark Created:</div>
                                                            <div class="detail-value">${piece.PieceParkCreatedAt}</div>
                                                        ` : ''}
                                                        <div class="detail-label">PiecePark Complete:</div>
                                                        <div class="detail-value">${piece.PieceParkComplete ? 'Yes' : 'No'}</div>
                                                        ${piece.PieceParkCleanupTaskID ? html`
                                                            <div class="detail-label">PiecePark Cleanup Task:</div>
                                                            <div class="detail-value">${piece.PieceParkCleanupTaskID}</div>
                                                        ` : ''}
                                                    ` : !piece.IsParkedPieceFound ? html`
                                                        <div class="detail-label" style="color: var(--color-danger-main, #B63333);">Error:</div>
                                                        <div class="detail-value" style="color: var(--color-danger-main, #B63333);">Reference Not Found</div>
                                                    ` : ''}
                                                </div>
                                            </td>
                                        </tr>
                                    ` : ''}
                                `;
                            })}
                        </tbody>
                    </table>
                `}
            </div>
            <div>
                <h3>Storage</h3>
                ${(this.data.Locations||[]).length === 0 ? html`
                    <p style="color: #aaa;">No storage locations found for this sector.</p>
                ` : html`
                    <table class="table table-dark">
                        <thead>
                            <tr>
                                <th>Path Type</th>
                                <th>File Type</th>
                                <th>Path ID</th>
                                <th>Host</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${(this.data.Locations||[]).map(location => html`
                                <tr>
                                    ${location.PathType ? html`<td rowspan="${location.PathTypeRowSpan}">${location.PathType}</td>` : ''}
                                    ${location.FileType ? html`<td rowspan="${location.FileTypeRowSpan}">${location.FileType}</td>` : ''}
                                    <td>${location.Locations[0].StorageID}</td>
                                    <td>${location.Locations[0].Urls.map(url => html`<p>${url}</p>`)}</td>
                                </tr>
                                ${location.Locations.slice(1).map(loc => html`
                                    <tr>
                                        <td>${loc.StorageID}</td>
                                        <td>${loc.Urls.map(url => html`<p>${url}</p>`)}</td>
                                    </tr>
                                `)}
                            `)}
                        </tbody>
                    </table>
                `}
            </div>
            <div>
                <h3>Garbage Collection Marks</h3>
                ${this.gcMarks.length === 0 ? html`
                    <p style="color: #aaa;">No files marked for garbage collection.</p>
                ` : html`
                    <table class="table table-dark table-sm">
                        <thead>
                            <tr>
                                <th>File Type</th>
                                <th>Storage ID</th>
                                <th>Path Type</th>
                                <th>URLs</th>
                                <th>Created At</th>
                                <th>Status</th>
                                <th>Action</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${this.gcMarks.map(mark => html`
                                <tr style="${mark.Approved ? 'background-color: rgba(182, 51, 51, 0.1);' : ''}">
                                    <td><strong>${mark.TypeName}</strong></td>
                                    <td>${mark.StorageID}</td>
                                    <td>${mark.PathType}</td>
                                    <td style="font-size: 0.85em;">${mark.Urls}</td>
                                    <td>${new Date(mark.CreatedAt).toLocaleString()}</td>
                                    <td>
                                        ${mark.Approved ? html`
                                            <span style="color: var(--color-danger-main, #B63333); font-weight: 500;">
                                                ✓ Approved (Will be deleted)
                                            </span>
                                        ` : html`
                                            <span style="color: var(--color-warning-main, #FFD600);">
                                                Pending Approval
                                            </span>
                                        `}
                                    </td>
                                    <td>
                                        ${!mark.Approved ? html`
                                            <button class="btn btn-danger btn-sm" 
                                                    @click="${() => this.approveGCMark(mark.Actor, mark.SectorNum, mark.FileType, mark.StorageID)}">
                                                Approve Delete
                                            </button>
                                        ` : html`
                                            <button class="btn btn-warning btn-sm" 
                                                    @click="${() => this.unapproveGCMark(mark.Actor, mark.SectorNum, mark.FileType, mark.StorageID)}">
                                                Unapprove
                                            </button>
                                        `}
                                    </td>
                                </tr>
                            `)}
                        </tbody>
                    </table>
                    <p style="margin-top: 10px; font-size: 0.9em; color: #aaa;">
                        <strong>Note:</strong> Approved marks will be automatically deleted by the StorageGCSweep task. 
                        Files are only removed after explicit approval.
                    </p>
                `}
            </div>
            <div>
                <h3>Active Tasks</h3>
                ${(this.data.Tasks||[]).length === 0 ? html`
                    <p style="color: #aaa;">No active tasks for this sector.</p>
                ` : html`
                    <table class="table table-dark">
                        <thead>
                            <tr>
                                <th>Task Type</th>
                                <th>Task Status</th>
                                <th>Posted</th>
                                <th>Worker</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${(this.data.Tasks||[]).map(task => html`
                                <tr>
                                    <td><strong>${task.Name}</strong></td>
                                    <td><task-status .taskId=${task.ID}></task-status></td>
                                    <td>${task.SincePosted}</td>
                                    <td>${task.OwnerID ? html`<a href="/pages/node_info/?id=${task.OwnerID}">${task.Owner}</a>` : 'Not assigned'}</td>
                                </tr>
                            `)}
                        </tbody>
                    </table>
                `}
            </div>
            <div>
                <h3>Current task history</h3>
                <table class="table table-dark">
                    <tr>
                        <th>Task ID</th>
                        <th>Task Type</th>
                        <th>Completed By</th>
                        <th>Result</th>
                        <th>Started</th>
                        <th>Took</th>
                        <th>Error</th>
                    </tr>
                    ${(this.data.TaskHistory||[]).map(history => html`
                        ${history.Name ? html`
                            <tr>
                                <td><a href="/pages/task/id/?id=${history.PipelineTaskID}">${history.PipelineTaskID}</a></td>
                                <td>${history.Name}</td>
                                <td>${history.CompletedBy}</td>
                                <td>${history.Result ? 'Success' : 'Failed'}</td>
                                <td>${history.WorkStart}</td>
                                <td>${history.Took}</td>
                                <td>${history.Err}</td>
                            </tr>
                        ` : ''}
                    `)}
                </table>
            </div>
        `;
    }
} );
