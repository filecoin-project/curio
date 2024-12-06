import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@2/core/lit-core.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class PieceSummary extends LitElement {
    static styles = css`
        .container {
            padding-left: 80px; 
            padding-top: 70px;   
        }
        .chart-container {
            width: 400px;
            margin: 20px auto;
        }
        h2 {
            text-align: center;
            margin-bottom: 10px;
        }
        .bar-chart {
            display: flex;
            align-items: flex-end;
            position: relative;
            height: 200px;
            border-left: 2px solid white;
            border-bottom: 2px solid white;
            padding: 0 5px;
        }
        .bar {
            flex: 1;
            margin: 0 10px;
            color: white;
            position: relative;
            display: flex;
            align-items: flex-end;
            justify-content: center;
            font-size: 14px;
            cursor: pointer;
            transition: background-color 0.2s;
        }
        .tooltip {
            position: absolute;
            bottom: 100%;
            left: 50%;
            transform: translateX(-50%);
            background-color: black;
            color: white;
            padding: 5px;
            font-size: 12px;
            border-radius: 3px;
            opacity: 0;
            transition: opacity 0.2s;
            pointer-events: none;
        }
        .bar:hover .tooltip {
            opacity: 1;
        }
        .x-axis {
            display: flex;
            justify-content: space-between;
            padding: 5px 10px;
            font-weight: bold;
        }
        .y-axis {
            position: absolute;
            left: -40px;
            top: 0;
            display: flex;
            flex-direction: column;
            justify-content: space-between;
            height: 100%;
            font-size: 12px;
            color: white;
        }
        .last-updated {
            text-align: center;
            font-size: 14px;
            color: gray;
            margin-top: 10px;
        }
    `;

    constructor() {
        super();
        this.data = { Total: 0, Indexed: 0, Announced: 0 };
        this.lastUpdated = '';
    }

    async loadData() {
        try {
            const response = await RPCCall('PieceSummary', []);
            console.log('PieceSummary data:', response);

            // Map response data to this.data and lastUpdated for display
            this.data = {
                Total: response.total,
                Indexed: response.indexed,
                Announced: response.announced
            };
            this.lastUpdated = new Date(response.last_updated).toLocaleString();

            // Trigger a re-render with the updated data
            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load piece summary:', error);
            this.data = { Total: 0, Indexed: 0, Announced: 0 };
            this.lastUpdated = '';
        }
    }

    firstUpdated() {
        // Load the data when the component is first rendered
        this.loadData();
    }

    render() {
        // Calculate the height of each bar relative to the maximum value
        const maxValue = Math.max(this.data.Total, this.data.Indexed, this.data.Announced);
        const totalHeight = (this.data.Total / maxValue) * 100 || 0;
        const indexedHeight = (this.data.Indexed / maxValue) * 100 || 0;
        const announcedHeight = (this.data.Announced / maxValue) * 100 || 0;

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet"
                  integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
                  crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <div class="container">
                <h2>Piece Summary</h2>
                <div class="chart-container">
                    <div class="bar-chart">
                        <div class="y-axis">
                            ${[maxValue, maxValue / 2, 0].map(value => html`<div>${value.toFixed(0)}</div>`)}
                        </div>
                        <div class="bar" style="height: ${totalHeight}%; background-color: #4CAF50;">
                            <div class="tooltip">${this.data.Total}</div>
                        </div>
                        <div class="bar" style="height: ${indexedHeight}%; background-color: #2196F3;">
                            <div class="tooltip">${this.data.Indexed}</div>
                        </div>
                        <div class="bar" style="height: ${announcedHeight}%; background-color: #FF9800;">
                            <div class="tooltip">${this.data.Announced}</div>
                        </div>
                    </div>
                    <div class="x-axis">
                        <div>Total</div>
                        <div>Indexed</div>
                        <div>Announced</div>
                    </div>
                    <div class="last-updated">Last Updated: ${this.lastUpdated}</div>
                </div>
            </div>
        `;
    }
}

customElements.define('piece-summary', PieceSummary);
