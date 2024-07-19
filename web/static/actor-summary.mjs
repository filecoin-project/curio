import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class Expirations extends LitElement {
    static properties = {
        address: { type: String },
    };

    static styles = css`
        :host {
            display: block;
            width: 450px;
            height: 200px;
        }
    `;

    constructor() {
        super();
        this.data = [];
    }

    updated(changedProperties) {
        if (changedProperties.has('address') && this.address) {
            this.loadData();
        }
    }

    async loadData() {
        if (!this.address) {
            console.error('Address is not set');
            return;
        }

        try {
            this.data = await RPCCall('ActorSectorExpirations', [this.address]);
            this.renderChart();

            // Poll for updates
            if (this.intervalId) {
                clearInterval(this.intervalId);
            }
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
        if (!this.data || this.data.length === 0) {
            console.warn('No data to render');
            return;
        }

        const nowEpoch = this.data[0].Expiration;

        if (!this.chart) {
            const ctx = this.shadowRoot.querySelector('canvas').getContext('2d');
            this.chart = new Chart(ctx, {
                type: 'line',
                data: {
                    datasets: [{
                        label: 'Live Sectors',
                        borderColor: 'rgb(75, 192, 192)',
                        borderWidth: 1,
                        backgroundColor: 'rgba(75, 192, 192, 0.2)',
                        stepped: true,
                        fill: true,
                        pointRadius: 2,
                    }]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    scales: {
                        x: {
                            type: 'linear',
                            position: 'bottom',
                            title: {
                                display: true,
                                text: 'Days in Future'
                            },
                            ticks: {
                                callback: function(value, index, values) {
                                    const days = Math.round((value - nowEpoch) * 30 / 86400);
                                    return days + 'd';
                                }
                            },
                            min: nowEpoch,  // Set the minimum value to the current epoch
                            max: this.data[this.data.length - 1].Expiration,  // Set the maximum value to the last data point
                            afterDataLimits: (scale) => {
                                scale.max += (scale.max - scale.min) * 0.05;  // Add a small padding to the right
                            }
                        },
                        y: {
                            title: {
                                display: true,
                                text: 'Count'
                            },
                            beginAtZero: true
                        }
                    },
                    plugins: {
                        tooltip: {
                            callbacks: {
                                label: function(context) {
                                    const days = Math.round((context.parsed.x - nowEpoch) * 30 / 86400);
                                    return `Count: ${context.parsed.y}, Days: ${days} (months: ${(days / 30).toFixed(1)})`;
                                }
                            }
                        }
                    }
                }
            });
        }

        this.chart.data.datasets[0].data = this.data.map(d => ({
            x: d.Expiration,
            y: d.Count
        }));
        this.chart.options.scales.x.ticks.callback = function(value, index, values) {
            const days = Math.round((value - nowEpoch) * 30 / 86400);
            return days + 'd';
        };
        this.chart.update();
    }

    render() {
        return html`
            <canvas></canvas>
        `;
    }
}

customElements.define('sector-expirations', Expirations);

class ActorSummary extends LitElement {
    static styles = css`
    .deadline-box {
            display: grid;
            grid-template-columns: repeat(16, auto);
            grid-template-rows: repeat(3, auto);
            grid-gap: 1px;
        }

        .deadline-entry {
            width: 10px;
            height: 10px;
            background-color: grey;
            margin: 1px;
        }

        .deadline-entry-cur {
            border-bottom: 3px solid deepskyblue;
            height: 7px;
        }

        .deadline-proven {
            background-color: green;
        }

        .deadline-partially-faulty {
            background-color: yellow;
        }

        .deadline-faulty {
            background-color: red;
        }
    `;

    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('ActorSummary');
        this.requestUpdate();

        // Poll for updates
        setInterval(async () => {
            this.data = await RPCCall('ActorSummary');
            this.requestUpdate();
        }, 30000);
    }

    renderWins(win1, win7, win30) {
        return html`
            <table>
                <tr><td>1day:&nbsp; ${win1}</td></tr>
                <tr><td>7day:&nbsp; ${win7}</td></tr>
                <tr><td>30day: ${win30}</td></tr>
            </table>
        `;
    }

    renderDeadlines(deadlines) {
        return html`
            <div class="deadline-box">
                ${deadlines.map(d => html`
                    <div class="deadline-entry
                        ${d.Current ? 'deadline-entry-cur' : ''}
                        ${d.Proven ? 'deadline-proven' : ''}
                        ${d.PartFaulty ? 'deadline-partially-faulty' : ''}
                        ${d.Faulty ? 'deadline-faulty' : ''}
                    "></div>
                `)}
            </div>
        `;
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Address</th>
                    <th>Source Layer</th>
                    <th>QaP</th>
                    <th>Deadlines</th>
                    <th>Balance</th>
                    <th>Available</th>
                    <th>Worker</th>
                    <th style="min-width: 100px">Wins</th>
                    <th>Expirations</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>${entry.Address}</td>
                        <td>
                            ${entry.CLayers.map(layer => html`<span>${layer} </span>`)}
                        </td>
                        <td>${entry.QualityAdjustedPower}</td>
                        <td>${this.renderDeadlines(entry.Deadlines)}</td>
                        <td>${entry.ActorBalance}</td>
                        <td>${entry.ActorAvailable}</td>
                        <td>${entry.WorkerBalance}</td>
                        <td>${this.renderWins(entry.Win1, entry.Win7, entry.Win30)}</td>
                        <th><sector-expirations address="${entry.Address}"></sector-expirations></th>
                    </tr>
                `)}
                </tbody>
            </table>
        `;
    }
}

customElements.define('actor-summary', ActorSummary);
