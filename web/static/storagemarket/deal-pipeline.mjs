import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
customElements.define('deal-pipeline',class DealPipeline extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }
    async loadData() {
        this.data = await RPCCall('PendingStorageDeals');
        setTimeout(() => this.loadData(), 3000);
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
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <style>${DealPipeline.styles}</style>
            <table class="table table-dark porep-state porep-pipeline-table">
                <tbody>
                    ${this.renderDeals()}
                </tbody>
            </table>
        `;
    }

    renderDeals() {
        return this.data.map((deal) => html`
            <tr>
                <td>${deal.Miner}</td>
                <td rowspan="2">${deal.CreateTime}</td>
                <td rowspan="2">${this.renderDeal(deal)}</td>
                <td rowspan="2">
                    <a href="/storagemarket/deal/?id=${deal.UUID}">DETAILS</a>
                </td>
            </tr>
            <tr>
                <td>${deal.UUID}</td>
            </tr>
        `);
    }

    renderDeal(deal) {
        return html`
            <table class="porep-state">
                <tbody>
                    <tr>
                        ${this.renderDealState('PiecePark', 2, deal.ParkPieceTaskID, deal.AfterParkPiece)}
                        ${this.renderDealStateNoTask('Started', 2, !deal.Started, deal.Started)}
                        ${this.renderDealState('CommP', 2, deal.CommpTaskID, deal.AfterCommp)}
                        ${this.renderSectorState('PSD', 1, deal.PsdTaskID, deal.AfterPsd)}
                        <td rowspan="2" class="${deal.Sector ? 'pipeline-success' : 'pipeline-active'}">
                            <div>Add To Sector</div>
                            <div>${deal.Sector}</div>
                        </td>
                        ${this.renderDealStateNoTask('Sealing', 1, !deal.Sealed, deal.Sealed)}
                    </tr>
                    <tr>
                        <!-- PP, Started, CommP -->
                        ${this.renderSectorState('PSD Wait', 1, deal.FindDealTaskID, deal.AfterFindDeal)}
                        ${this.renderSectorState('Indexing', 1, deal.IndexingTaskID, deal.Indexed)}
                    </tr>
                </tbody>
            </table>
        `;
    }
    renderDealStateNoTask(name, rowspan, active, after) {
        return html`
            <td rowspan="${rowspan}" class="${active?'pipeline-active':''} ${after?'pipeline-success':''}">
                <div>${name}</div>
                <div>${after?'done':'--'}</div>
            </td>
        `;
    }
    renderDealState(name, rowspan, task, after) {
        return html` 
            <td rowspan="${rowspan}" class="${task?'pipeline-active':''} ${after?'pipeline-success':''}">
                <div>${name}</div>
                <div>${after?'done':task?'T:'+task:'--'}</div>
            </td>
        `;
    }

} );
