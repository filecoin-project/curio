import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class PipelineWaterfall extends LitElement {
    static properties = {
        sourceRPC: { type: String },
    };

    static styles = css`
        :host {
            display: block;
            width: 600px;
            height: 400px;
        }
        .chart-container {
            position: relative;
            width: 100%;
            height: 100%;
        }
    `;

    constructor() {
        super();
        this.sourceRPC = '';
        this.data = null;
        this.chart = null;
        this.intervalId = null;
    }

    updated(changedProperties) {
        if (changedProperties.has('sourceRPC') && this.sourceRPC) {
            this.loadData();
        }
    }

    async loadData() {
        if (!this.sourceRPC) {
            console.error('sourceRPC is not set');
            return;
        }

        try {
            this.data = await RPCCall(this.sourceRPC, []);
            this.renderChart();

            if (this.intervalId) {
                clearInterval(this.intervalId);
            }
            // Poll for updates
            this.intervalId = setInterval(() => this.loadData(), 30000);
        } catch (error) {
            console.error('Error loading data:', error);
        }
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this.intervalId) {
            clearInterval(this.intervalId);
        }
    }

    renderChart() {
        if (!this.data || !this.data.Stages) {
            console.warn('No data to render');
            return;
        }

        // We'll build a waterfall:
        // Start with total: a bar from 0 to total.
        // For each stage:
        //   - Pending: a bar that goes down from currentTotal to currentTotal - pending
        //   - Running: a bar that goes up from currentTotal to currentTotal + running
        //
        // Keep track of currentTotal as we move along.

        let currentTotal = 0;
        const labels = [];
        const bars = [];
        const backgroundColors = [];

        // Initial total bar
        labels.push('Total');
        bars.push([0, this.data.Total]);
        backgroundColors.push('rgba(54, 162, 235, 0.8)');
        currentTotal = this.data.Total;

        // For each stage, we add two bars: pending (down) and running (up)
        for (const stage of this.data.Stages) {
            const stageName = stage.Name;

            // Pending: goes down
            if (stage.Pending !== 0) {
                labels.push(`${stageName} Pending`);
                const newTotal = currentTotal - stage.Pending;
                // We'll store as [min, max] and min < max, so if pending is a decrease:
                // The top should be currentTotal and bottom newTotal,
                // but we must always give array as [lower, upper].
                bars.push([newTotal, currentTotal]);
                backgroundColors.push('rgba(255, 99, 132, 0.8)'); // red-ish for decrease
                currentTotal = newTotal;
            }

            // Running: goes up
            if (stage.Running !== 0) {
                labels.push(`${stageName} Running`);
                const newTotal = currentTotal + stage.Running;
                bars.push([currentTotal, newTotal]);
                backgroundColors.push('rgba(75, 192, 192, 0.8)'); // green-ish for increase
                currentTotal = newTotal;
            }
        }

        const chartData = {
            labels: labels,
            datasets: [{
                label: 'Pipeline Flow',
                data: bars,
                backgroundColor: backgroundColors,
                borderColor: 'rgba(0,0,0,0.1)',
                borderWidth: 1
            }]
        };

        const config = {
            type: 'bar',
            data: chartData,
            options: {
                responsive: true,
                maintainAspectRatio: false,
                indexAxis: 'x', // default for vertical bars
                scales: {
                    x: {
                        title: {
                            display: true,
                            text: 'Stages'
                        },
                        ticks: {
                            maxRotation: 45,
                            minRotation: 45
                        }
                    },
                    y: {
                        beginAtZero: true,
                        title: {
                            display: true,
                            text: 'Count'
                        }
                    }
                },
                plugins: {
                    tooltip: {
                        callbacks: {
                            label: function(context) {
                                const label = context.label || '';
                                const values = context.raw; // This will be [min, max]
                                const diff = values[1] - values[0];
                                return `${label}: ${diff > 0 ? '+' : ''}${diff} (from ${values[0]} to ${values[1]})`;
                            }
                        }
                    },
                    title: {
                        display: true,
                        text: 'Pipeline Waterfall Visualization'
                    },
                    legend: {
                        display: false
                    }
                }
            }
        };

        if (!this.chart) {
            const ctx = this.shadowRoot.querySelector('canvas').getContext('2d');
            this.chart = new Chart(ctx, config);
        } else {
            this.chart.data = chartData;
            this.chart.options = config.options;
            this.chart.update();
        }
    }

    render() {
        return html`
            <div class="chart-container">
                <canvas></canvas>
            </div>
        `;
    }
}

customElements.define('pipeline-waterfall', PipelineWaterfall);


customElements.define('pipeline-stats', class PipelineStats extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }
    async loadData() {
        this.data = await RPCCall('PorepPipelineSummary') || [];
        setTimeout(() => this.loadData(), 5000);
        this.requestUpdate();
    }
    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <div class="row">
            <div class="col-md-auto" style="max-width: 1000px">
                <div class="info-block">
                    <h2>Pipelines</h2>
                    <pipeline-waterfall sourceRPC="PipelineStatsMarket"></pipeline-waterfall>
                </div>
            </div>
            <div class="col-md-auto">
            </div>
        </div>
        `;
    }
} );
