import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class PipelineWaterfall extends LitElement {
    static properties = {
        sourceRPC: { type: String },
        title: { type: String },
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
        this.title = '';
        this.data = null;
        this.chart = null;
        this.intervalId = null;

        // Hardcode colors from provided vars
        this.colorTotal = '#1DC8CC';    // var(--color-primary-main)
        this.colorPending = '#B63333';  // var(--color-danger-main)
        this.colorRunning = '#7EA83E';  // var(--color-success-main)
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
            this.intervalId = setInterval(() => this.loadData(), 10000);
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

        let currentTotal = this.data.Total;
        const labels = ['Total'];
        const bars = [[0, this.data.Total]];
        const backgroundColors = [this.colorTotal];

        // Create bars for each stage (pending and running)
        for (const stage of this.data.Stages) {
            // Pending bar
            {
                const oldTotal = currentTotal;
                const newTotal = currentTotal - stage.Pending;
                labels.push(`${stage.Name} Pending`);
                bars.push([Math.min(oldTotal, newTotal), Math.max(oldTotal, newTotal)]);
                backgroundColors.push(this.colorPending);
                currentTotal = newTotal;
            }

            // Running bar
            {
                const oldTotal = currentTotal;
                const newTotal = currentTotal - stage.Running;
                labels.push(`${stage.Name} Running`);
                bars.push([Math.min(oldTotal, newTotal), Math.max(oldTotal, newTotal)]);
                backgroundColors.push(this.colorRunning);
                currentTotal = newTotal;
            }
        }

        const chartData = {
            labels: labels,
            datasets: [
                {
                    label: 'Changes',
                    data: bars,
                    backgroundColor: backgroundColors,
                    borderColor: 'rgba(0,0,0,0.1)',
                    borderWidth: 1,
                    type: 'bar'
                },
            ]
        };

        const config = {
            type: 'bar',
            data: chartData,
            options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: false, // Disable animations to prevent jumping
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
                        min: 0,
                        // suggestedMax helps keep the scale stable based on total
                        suggestedMax: this.data.Total > 0 ? this.data.Total : undefined,
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
                                if (context.dataset.type === 'bar') {
                                    const values = context.raw; // [min, max]
                                    const diff = values[1] - values[0];
                                    return `${context.label}: ${diff > 0 ? '+' : ''}${diff} (from ${values[0]} to ${values[1]})`;
                                } else {
                                    return `${context.label}: ${context.formattedValue}`;
                                }
                            }
                        }
                    },
                    title: {
                        display: !!this.title,
                        text: this.title,
                    },
                    legend: {
                        display: true,
                        position: 'bottom',
                    },
                    datalabels: {
                        anchor: 'end',
                        align: 'end',
                        offset: 4,
                        clamp: true,
                        color: '#fff', // Use a contrasting color, e.g. white
                        formatter: (value, ctx) => {
                            const diff = value[1] - value[0];
                            return diff === 0 ? '0' : (diff > 0 ? `+${diff}` : `${diff}`);
                        }
                    }
                }
            },
            plugins: [ChartDataLabels] // Enable datalabels plugin
        };

        if (!this.chart) {
            const ctx = this.shadowRoot.querySelector('canvas').getContext('2d');
            this.chart = new Chart(ctx, config);
        } else {
            // Update existing chart data and options
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
                            <div style="background-color: var(--color-fg)">
                                <pipeline-waterfall sourceRPC="PipelineStatsMarket" title="Market Pipeline"></pipeline-waterfall>
                                <pipeline-waterfall sourceRPC="PipelineStatsSnap" title="Sector Update Pipeline"></pipeline-waterfall>
                                <pipeline-waterfall sourceRPC="PipelineStatsSDR" title="SDR Pipeline"></pipeline-waterfall>
                            </div>
                        </div>
                    </div>
                <div class="col-md-auto">
                </div>
            </div>
        `;
    }
} );
