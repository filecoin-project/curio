import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import '/pages/pipeline_porep/pipeline_porep_sectors.mjs';
class SectorInfo extends HTMLElement {
    constructor() {
        super();
        this.data = [];
    }

    async loadData() {
        const params = new URLSearchParams(window.location.search);
        this.data = await RPCCall('SectorInfo', [params.get('sp'), params.get('sector')]);
        setTimeout(() => this.loadData(), 5000);
        super.requestUpdate();
    }

    connectedCallback() {
        this.loadData();
        this.render();
    }

    async removeSector() {
        await RPCCall('SectorRemove', [this.data.SpID, this.data.SectorNumber]);
        window.location.href = '/pages/pipeline_porep/pipeline_porep_sectors';
    }
    async resumeSector() {
        await RPCCall('SectorResume', [this.data.SpID, this.data.SectorNumber]);
        window.location.reload();
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <h2>Sector ${this.data.SectorNumber}</h2>
            <div>
                <details>
                    <summary class="btn btn-warning">Remove ${!this.data.PipelinePoRep.Failed ? '(THIS SECTOR IS NOT FAILED!)' : ''}</summary>
                    <button class="btn btn-danger" onclick="${this.removeSector}">Confirm Remove</button>
                </details>
                ${this.data.Resumable ? html`<button class="btn btn-primary" onclick="${this.resumeSector}">Resume</button>` : ''}
            </div>
            <div>
                <h3>PoRep Pipeline</h3>
                <sector-porep-state .data=${this.data.PipelinePoRep}></sector-porep-state>
            </div>
            <div>
                <h3>Pieces</h3>
                <table class="table table-dark">
                    <tr>
                        <th>Piece Index</th>
                        <th>Piece CID</th>
                        <th>Piece Size</th>
                        <th>Data URL</th>
                        <th>Data Raw Size</th>
                        <th>Delete On Finalize</th>
                        <th>F05 Publish CID</th>
                        <th>F05 Deal ID</th>
                        <th>Direct Piece Activation Manifest</th>
                        <th>PiecePark ID</th>
                        <th>PP URL</th>
                        <th>PP Created At</th>
                        <th>PP Complete</th>
                        <th>PP Cleanup Task</th>
                    </tr>
                    ${this.data.Pieces.map(piece => html`
                        <tr>
                            <td>${piece.PieceIndex}</td>
                            <td>${piece.PieceCid}</td>
                            <td>${piece.PieceSize}</td>
                            <td>${piece.DataUrl}</td>
                            <td>${piece.DataRawSize}</td>
                            <td>${piece.DeleteOnFinalize}</td>
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
                        ${location.PathType ? html`<td rowspan="${location.PathTypeRowSpan}">${location.PathType}</td>` : ''}
                        ${location.FileType ? html`<td rowspan="${location.FileTypeRowSpan}">${location.FileType}</td>` : ''}
                        <td>${location.Locations[0].StorageID}</td>
                        <td>${location.Locations[0].Urls.map(url => html`<p>${url}</p>`)}</td>
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
                <table class="porep-state">
                    <tr>
                        <th>Task Type</th>
                        <th>Task ID</th>
                        <th>Posted</th>
                        <th>Worker</th>
                    </tr>
                    ${this.data.Tasks.map(task => html`
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
                    ${this.data.TaskHistory.map(history => html`
                        ${history.Name ? html`
                            <tr>
                                <td>${history.PipelineTaskID}</td>
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
}

customElements.define('sector-info', SectorInfo);
