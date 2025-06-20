import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { renderSectorPipeline, pipelineStyles } from '/pages/pipeline_porep/pipeline-porep-sectors.mjs';
import { renderSectorSnapPipeline, snapPipelineStyles} from '/snap/upgrade-sectors.mjs';
import '/ux/epoch.mjs';

customElements.define('sector-info',class SectorInfo extends LitElement {
    constructor() {
        super();
        this.loadData();
    }
    async loadData() {
        const params = new URLSearchParams(window.location.search);
        this.data = await RPCCall('SectorInfo', [params.get('sp'), params.get('id')|0]);
        setTimeout(() => this.loadData(), 5000);
        this.requestUpdate();
    }
    async removeSector() {
        await RPCCall('SectorRemove', [this.data.SpID, this.data.SectorNumber]);
        window.location.href = '/pages/pipeline_porep/';
    }
    async resumeSector() {
        await RPCCall('SectorResume', [this.data.SpID, this.data.SectorNumber]);
        window.location.reload();
    }
    async restartSector() {
        await RPCCall('SectorRestart', [this.data.SpID, this.data.SectorNumber]);
        window.location.reload();
    }

    static styles = [pipelineStyles, snapPipelineStyles];

    render() {
        if (!this.data) {
            return html`<div>Loading...</div>`;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <div class="row" style="margin-bottom: 20px;">
                <h2 style="text-align: center; margin-top: 20px;">Sector ${this.data.SectorNumber}</h2>
                <div class="col-md-auto">
                    <details>
                        <summary class="btn btn-warning">Remove ${!this.data.PipelinePoRep?.Failed && !this.data.PipelineSnap?.Failed ? '(THIS SECTOR IS NOT FAILED!)' : ''}</summary>
                        <button class="btn btn-danger" @click="${() => this.removeSector()}">Confirm Remove</button>
                    </details>
                </div>
                <div class="col-md-auto">
                    ${this.data.Restart ? html`<details>
                        <summary class="btn btn-warning">Restart (THIS WILL RESTART SECTOR!)</summary>
                        <button class="btn btn-danger" @click="${() => this.restartSector()}"> Confirm Restart</button></details>` : ''}
                </div>
                <div class="col-md-auto">
                    ${this.data.Resumable ? html`<button class="btn btn-primary" @click="${() => this.resumeSector()}">Resume</button>` : ''}
                </div>
            </div>
            <div>
                <h3>Sector Info</h3>
                <table class="table table-dark table-striped table-sm">
                        <tr><td>Miner ID</td><td>${this.data.Miner}</td></tr>
                        <tr><td>Sector Number</td><td>${this.data.SectorNumber}</td></tr>
                        <tr><td>PreCommit Message</td><td>${this.data.PreCommitMsg}</td></tr>
                        <tr><td>Commit Message</td><td>${this.data.CommitMsg}</td></tr>
                        <tr><td>Activation Epoch</td><td><pretty-epoch epoch=${this.data.ActivationEpoch}></pretty-epoch></td></tr>
                        <tr><td>Expiration Epoch</td><td><pretty-epoch epoch=${this.data.ExpirationEpoch}></pretty-epoch></td></tr>
                        <tr><td>Deal Weight</td><td>${this.data.DealWeight}</td></tr>
                        <tr><td>Deadline</td><td>${this.data.Deadline}</td></tr>
                        <tr><td>Partition</td><td>${this.data.Partition}</td></tr>
                        <tr><td>Unsealed CID</td><td>${this.data.UnsealedCid}</td></tr>
                        <tr><td>Sealed CID</td><td>${this.data.SealedCid}</td></tr>
                        <tr><td>Updated Unsealed CID</td><td>${this.data.UpdatedUnsealedCid}</td></tr>
                        <tr><td>Updated Sealed CID</td><td>${this.data.UpdatedSealedCid}</td></tr>
                        <tr><td>Is Snap</td><td>${this.data.IsSnap}</td></tr>
                        <tr><td>Update Message</td><td>${this.data.UpdateMsg}</td></tr>
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
            <div>
                ${this.data.PipelinePoRep ? html`
                    <h3>PoRep Pipeline</h3>
                    ${renderSectorPipeline(this.data.PipelinePoRep)}
                ` : html`
                    <p>No data available for the PoRep pipeline.</p>
                `}
            </div>
            <div>
                ${this.data.PipelineSnap ? html`
                    <h3>SnapDeals Pipeline</h3>
                    ${renderSectorSnapPipeline(this.data.PipelineSnap)}
                ` : html`
                    <p>No data available for the SnapDeals pipeline.</p>
                `}
            </div>
            <div>
                <h3>Pieces</h3>
                <table class="table table-dark">
                    <tr>
                        <th>Piece Index</th>
                        <th>Piece CID</th>
                        <th>Piece Size</th>
                        <th>Deal ID</th>
                        <th>Data URL</th>
                        <th>Data Raw Size</th>
                        <th>Delete On Finalize</th>
                        <th>Pipeline</th>
                        <th>F05 Publish CID</th>
                        <th>F05 Deal ID</th>
                        <th>Direct Piece Activation Manifest</th>
                        <th>PiecePark ID</th>
                        <th>PP URL</th>
                        <th>PP Created At</th>
                        <th>PP Complete</th>
                        <th>PP Cleanup Task</th>
                    </tr>
                    ${(this.data.Pieces||[]).map(piece => html`
                        <tr>
                            <td>${piece.PieceIndex}</td>
                            <td><a href="/pages/piece/?id=${piece.PieceCid}">${piece.PieceCid}</a></td>
                            <td>${piece.PieceSize}</td>
                            <td><a href="/pages/mk12-deal/?id=${piece.DealID}">${piece.DealID}</a></td>
                            <td>${piece.DataUrl}</td>
                            <td>${piece.DataRawSize}</td>
                            <td>${piece.DeleteOnFinalize === null ? 'Either' : piece.DeleteOnFinalize}</td>
                            <td>${piece.IsSnapPiece ? 'SnapDeals' : 'PoRep'}</td>
                            <td>${piece.F05PublishCid}</td>
                            <td>${piece.F05DealID}</td>
                            <td>${piece.DDOPam}</td>
                            ${piece.IsParkedPiece ? html`
                                <td>${piece.PieceParkID}</td>
                                <td>${piece.PieceParkDataUrl}</td>
                                <td>${piece.PieceParkCreatedAt}</td>
                                <td>${piece.PieceParkComplete}</td>
                                <td>${piece.PieceParkCleanupTaskID}</td>
                            ` : html`
                                <td>${!piece.IsParkedPieceFound ? 'ERR:RefNotFound' : ''}</td>
                                <td></td>
                                <td></td>
                                <td></td>
                                <td></td>
                            `}
                        </tr>
                    `)}
                </table>
            </div>
            <div>
                <h3>Storage</h3>
                <table class="table table-dark">
                    <tr>
                        <th>Path Type</th>
                        <th>File Type</th>
                        <th>Path ID</th>
                        <th>Host</th>
                    </tr>
                    ${this.data.Locations.map(location => html`
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
                </table>
            </div>
            <div>
                <h3>Tasks</h3>
                <table class="table table-dark">
                    <tr>
                        <th>Task Type</th>
                        <th>Task ID</th>
                        <th>Posted</th>
                        <th>Worker</th>
                    </tr>
                    ${(this.data.Tasks||[]).map(task => html`
                        <tr>
                            <td>${task.Name}</td>
                            <td>${task.ID}</td>
                            <td>${task.SincePosted}</td>
                            <td>${task.OwnerID ? html`<a href="/pages/node_info/?id=${task.OwnerID}">${task.Owner}</a>` : ''}</td>
                        </tr>
                    `)}
                </table>
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
