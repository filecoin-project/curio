import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
customElements.define('pipeline-porep-sectors',class PipelinePorepSectors extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }
    async loadData() {
        this.data = await RPCCall('PipelinePorepSectors');
        this.requestUpdate();
    };

    static styles = css`
        .porep-pipeline-table,
        .porep-state {
            color: #d0d0d0;
        }

        .porep-pipeline-table td,
        .porep-pipeline-table th {
            border-left: none;
            border-collapse: collapse;
        }

        .porep-pipeline-table tr:nth-child(odd) {
            border-top: 6px solid #999999;

        }

        .porep-pipeline-table tr:first-child,
        .porep-pipeline-table tr:first-child {
            border-top: none;
        }
    `;

    render() {
        return html`
            <link rel="stylesheet" href="/ux/main.css">
            <style>${PipelinePorepSectors.styles}</style>
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
                <td>${sector.Address}</td>
                <td rowspan="2">${sector.CreateTime}</td>
                <td rowspan="2">${this.renderSector(sector)}</td>
                <td rowspan="2">
                    <a href="/pages/sector/?sp=${sector.Address}&id=${sector.SectorNumber}">DETAILS</a>
                </td>
            </tr>
            <tr>
                <td>${sector.SectorNumber}</td>
            </tr>
        `);
    }
} );
customElements.define('sector-porep-state', class SectorPorepState extends LitElement {
    static styles = css`
        .porep-state {
            border-collapse: collapse;
        }

        .porep-state td,
        .porep-state th {
            border-left: 1px solid #f0f0f0;
            border-right: 1px solid #f0f0f0;

            padding: 1px 5px;

            text-align: center;
            font-size: 0.7em;
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
        }`
    properties = {
        sector: Object,
    }

    render() {
        const sector = this.sector;
        return html`
            <table class="porep-state">
                <tbody>
                    <tr>
                        ${this.renderSectorState('SDR', 1, sector.TaskSDR, sector.AfterSDR)}
                        ${this.renderSectorState('TreeC', 1, sector.TaskTreeC, sector.AfterTreeC)}
                        ${this.renderSectorState('PComm Msg', 2, sector.TaskPrecommitMsg, sector.AfterPrecommitMsg)}
                        ${this.renderSectorStateNoTask('PComm Wait', 2, sector.AfterPrecommitMsg, sector.AfterPrecommitMsgSuccess)}
                        ${this.renderSectorStateNoTask('Wait Seed', 2, sector.AfterPrecommitMsgSuccess, sector.AfterWaitSeed)}
                        ${this.renderSectorState('PoRep', 2, sector.TaskPoRep, sector.AfterPoRep)}
                        ${this.renderSectorState('Clear Cache', 1, sector.TaskFinalize, sector.AfterFinalize)}
                        ${this.renderSectorState('Move Storage', 1, sector.TaskMoveStorage, sector.AfterMoveStorage)}
                        <td class="${sector.ChainSector ? 'pipeline-success' : (sector.ChainAlloc ? 'pipeline-active' : 'pipeline-failed')}">
                            <div>On Chain</div>
                            <div>${sector.ChainSector ? 'yes' : (sector.ChainAlloc ? 'allocated' : 'no')}</div>
                        </td>
                        <td rowspan="2" class="${sector.Failed ? 'pipeline-failed' : (sector.ChainActive ? 'pipeline-success' : 'pipeline-active')}">
                            <div>State</div>
                            <div>${sector.Failed ? 'Failed' : (sector.ChainActive ? 'Sealed' : 'Sealing')}</div>
                        </td>
                    </tr>
                    <tr>
                        ${this.renderSectorState('TreeD', 1, sector.TaskTreeD, sector.AfterTreeD)}
                        ${this.renderSectorState('TreeR', 1, sector.TaskTreeR, sector.AfterTreeR)}
                        <!-- PC-S, PC-W, WS, PoRep -->
                        ${this.renderSectorState('Commit Msg', 1, sector.TaskCommitMsg, sector.AfterCommitMsg)}
                        ${this.renderSectorStateNoTask('Commit Wait', 1, sector.AfterCommitMsg, sector.AfterCommitMsgSuccess)}
                        <td class="${sector.ChainActive ? 'pipeline-success' : 'pipeline-failed'}">
                            <div>Active</div>
                            <div>${sector.ChainActive ? 'yes' : (sector.ChainUnproven ? 'unproven' : (sector.ChainFaulty ? 'faulty' : 'no'))}</div>
                        </td>
                    </tr>
                </tbody>
            </table>
        `;
    }
    renderSectorStateNoTask(name, rowspan, active, after) {
        return html`
            <td rowspan="${rowspan}" class="${active?'pipeline-active':''} ${after?'pipeline-success':''}">
                <div>${name}</div>
                <div>${after?'done':'--'}</div>
            </td>
        `;
    }
    renderSectorState(name, rowspan, task, after) {
        return html` 
            <td rowspan="${rowspan}" class="${task?'pipeline-active':''} ${after?'pipeline-success':''}">
                <div>${name}</div>
                <div>${after?'done':task?'T:'+task:'--'}</div>
            </td>
        `;
    }
});