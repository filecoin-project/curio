import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class UpgradeSectors extends LitElement {
    static properties = {
        data: { type: Array },
    };

    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('UpgradeSectors');
        setTimeout(() => this.loadData(), 3000);
        this.requestUpdate();
    }

    static styles = css`
        .porep-pipeline-table,
        .porep-state {
            color: #d0d0d0;
        }

        .porep-pipeline-table td,
        .porep-pipeline-table th {
            border-left: none;
            border-collapse: collapse;
            vertical-align: middle;
        }

        .porep-pipeline-table tr:nth-child(odd) {
            border-top: 6px solid #999999;
        }

        .porep-pipeline-table tr:first-child,
        .porep-pipeline-table tr:first-child {
            border-top: none;
        }
        .porep-state {
            border-collapse: collapse;
        }

        .porep-state td,
        .porep-state th {
            border-left: 1px solid #f0f0f0;
            border-right: 1px solid #f0f0f0;

            padding: 1px 5px;

            text-align: center;
            font-size: 0.8em;
        }

        .porep-state tr {
            border-top: 1px solid #f0f0f0;
        }

        .porep-state tr:first-child {
            border-top: none;
        }

        .pipeline-active {
            background-color: #303060;
        }

        .pipeline-success {
            background-color: #306030;
        }

        .pipeline-failed {
            background-color: #603030;
        }

        .pipeline-waiting {
            background-color: #808080;
        }
    `;

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark porep-state porep-pipeline-table">
                <tbody>
                    ${this.renderSectors()}
                </tbody>
            </table>
        `;
    }

    renderSectors() {
        return this.data.map((sector) => html`
            <tr>
                <td>${sector.Miner}</td>
                <td rowspan="2">${this.renderSector(sector)}</td>
                <td rowspan="2">
                    <a href="/pages/sector/?sp=${sector.Miner}&id=${sector.SectorNum}">DETAILS</a>
                </td>
            </tr>
            <tr>
                <td>${sector.SectorNum}</td>
            </tr>
        `);
    }

    renderSector(sector) {
        return html`
            <table class="porep-state">
                <tbody>
                    <tr>
                        ${this.renderSectorState('Encode', 1, sector, sector.TaskIDEncode, sector.AfterEncode, sector.StartedEncode)}
                        ${this.renderSectorState('Prove', 1, sector, sector.TaskIDProve, sector.AfterProve, sector.StartedProve)}
                        ${this.renderSectorState('Submit', 1, sector, sector.TaskIDSubmit, sector.AfterSubmit, sector.StartedSubmit)}
                        ${this.renderSectorState('Move Storage', 1, sector, sector.TaskIDMoveStorage, sector.AfterMoveStorage, sector.StartedMoveStorage)}
                        ${this.renderSectorStateNoTask('Prove Msg Landed', 1, sector.AfterSubmit, sector.AfterProveSuccess)}
                        <td rowspan="1" class="${sector.Failed ? 'pipeline-failed' : 'pipeline-success'}">
                            <div>State</div>
                            <div>${sector.Failed ? 'Failed' : 'Healthy'}</div>
                        </td>
                    </tr>
                </tbody>
            </table>
        `;
    }

    renderSectorState(name, rowspan, sector, taskID, after, started) {
        if (taskID) {
            const missing = sector.MissingTasks && sector.MissingTasks.includes(taskID);

            return html`
                <td rowspan="${rowspan}" class="${missing ? 'pipeline-failed' : (started ? 'pipeline-active' : 'pipeline-waiting')}">
                    <div>${name}</div>
                    <div>T:<a href="/pages/task/id/?id=${taskID}">${taskID}</a></div>
                    ${missing ? html`<div><b>FAILED</b></div>` : ''}
                </td>
            `;
        } else {
            return html`
                <td rowspan="${rowspan}" class="${after ? 'pipeline-success' : 'pipeline-waiting'}">
                    <div>${name}</div>
                    <div>${after ? 'Done' : '--'}</div>
                </td>
            `;
        }
    }

    renderSectorStateNoTask(name, rowspan, active, after) {
        return html`
            <td rowspan="${rowspan}" class="${active ? 'pipeline-active' : ''} ${after ? 'pipeline-success' : ''}">
                <div>${name}</div>
                <div>${after ? 'Done' : '--'}</div>
            </td>
        `;
    }
}

customElements.define('upgrade-sectors', UpgradeSectors);
