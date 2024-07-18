import { html, css, LitElement } from 'https://cdn.skypack.dev/lit';

class PipelinePorepSectors extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('PipelinePorepSectors');
        super.requestUpdate();
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
        return this.sectors.map((sector) => html`
            <tr>
                <td>${sector.Address}</td>
                <td rowspan="2">${sector.CreateTime}</td>
                <td rowspan="2">${this.renderSectorState(sector)}</td>
                <td rowspan="2">
                    <a href="/hapi/sector/${sector.Address}/${sector.SectorNumber}">DETAILS</a>
                </td>
            </tr>
            <tr>
                <td>${sector.SectorNumber}</td>
            </tr>
        `);
    }

    renderSectorState(sector) {
        return html`
            <td class="${this.getClasses(sector.TaskSDR, sector.AfterSDR)}">
                <div>SDR</div>
                <div>${this.renderTask(sector.TaskSDR, sector.AfterSDR)}</div>
            </td>
            <!-- Rest of the table cells... -->
        `;
    }

    getClasses(task, after) {
        let classes = '';
        if (task !== null) {
            classes += 'pipeline-active ';
        }
        if (after) {
            classes += 'pipeline-success ';
        }
        return classes.trim();
    }

    renderTask(task, after) {
        if (after) {
            return 'done';
        } else if (task !== null) {
            return `T:${task}`;
        } else {
            return '--';
        }
    }

    static properties = {
        sectors: { type: Array },
    };
}

customElements.define('pipeline-porep-sectors', PipelinePorepSectors);
