import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import '/actor-summary.mjs'; // <sector-expirations>
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('actor-detail', class Actor extends LitElement {
    connectedCallback() {
        super.connectedCallback();
        this.loadData();
    }

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
    
    .address-container {
      display: flex;
      align-items: center;
    }
    .dash-tile {
      display: flex;
      flex-direction: column;
      padding: 0.75rem;
      background: #3f3f3f;
    }
    .dash-tile b {
      padding-bottom: 0.5rem;
      color: deeppink;
    }
  `;

    async loadData() {
        this.data = await RPCCall('ActorInfo', [this.id]);
        this.requestUpdate();
    }

    render() {
        return html`
            <link
                    href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                    rel="stylesheet"
                    integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8t94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
                    crossorigin="anonymous"
            >
            <link
                    rel="stylesheet"
                    href="/ux/main.css"
                    onload="document.body.style.visibility = 'initial'"
            >
            ${
                    !this.data
                            ? html`<div>Loading...</div>`
                            : [this.data].map(actorInfo => html`
                                <section class="section">
                                    <div class="row">
                                        <h1>Actor Overview</h1>
                                    </div>
                                    <div class="row">
                                        <div class="col-md-auto">
                                            <div class="info-block dash-tile">
                                                <h3>Actor</h3>
                                                <table class="table table-dark">
                                                    <tbody>
                                                    <tr>
                                                        <td>Address:</td>
                                                        <td>${actorInfo.Summary.Address}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>CLayers:</td>
                                                        <td>${actorInfo.Summary.CLayers}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>SectorSize:</td>
                                                        <td>${this.toHumanBytes(actorInfo.SectorSize)}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>QualityAdjustedPower:</td>
                                                        <td>${actorInfo.Summary.QualityAdjustedPower}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>RawBytePower:</td>
                                                        <td>${actorInfo.Summary.RawBytePower}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>Balance:</td>
                                                        <td>${actorInfo.Summary.ActorBalance}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>Available:</td>
                                                        <td>${actorInfo.Summary.ActorAvailable}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>WorkerBalance:</td>
                                                        <td>${actorInfo.Summary.WorkerBalance}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>Vesting:</td>
                                                        <td>${actorInfo.Summary.VestingFunds}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>InitialPledgeRequirement:</td>
                                                        <td>${actorInfo.Summary.InitialPledgeRequirement}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>PreCommitDeposits:</td>
                                                        <td>${actorInfo.Summary.PreCommitDeposits}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>OwnerAddress:</td>
                                                        <td>${actorInfo.OwnerAddress}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>Beneficiary:</td>
                                                        <td>${actorInfo.Beneficiary}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>WorkerAddress:</td>
                                                        <td>${actorInfo.WorkerAddress}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>PeerID:</td>
                                                        <td>${actorInfo.PeerID}</td>
                                                    </tr>

                                                    <!-- Addresses (simple array of strings) -->
                                                    <tr>
                                                        <td>Address:</td>
                                                        <td>
                                                            ${actorInfo.Address.map(addr => html`
                                                                <div>${addr}</div>
                                                            `)}
                                                        </td>
                                                    </tr>

                                                    <tr>
                                                        <td>Deadlines:</td>
                                                        <td>${this.renderDeadlines(actorInfo.Summary.Deadlines)}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>Wins 24h:</td>
                                                        <td>${actorInfo.Summary.Win1}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>Win 7 day:</td>
                                                        <td>${actorInfo.Summary.Win7}</td>
                                                    </tr>
                                                    <tr>
                                                        <td>Win 30 day:</td>
                                                        <td>${actorInfo.Summary.Win30}</td>
                                                    </tr>

                                                    <!-- Render these rows ONLY if the Go value is non-null -->
                                                    ${
                                                            actorInfo.BeneficiaryTerm
                                                                    ? html`
                                  <tr>
                                    <td>BeneficiaryTerm:</td>
                                    <td>${actorInfo.BeneficiaryTerm}</td>
                                  </tr>
                                `
                                                                    : null
                                                    }
                                                    ${
                                                            actorInfo.PendingOwnerAddress
                                                                    ? html`
                                  <tr>
                                    <td>PendingOwnerAddress:</td>
                                    <td>${actorInfo.PendingOwnerAddress}</td>
                                  </tr>
                                `
                                                                    : null
                                                    }
                                                    ${
                                                            actorInfo.PendingBeneficiaryTerm
                                                                    ? html`
                                  <tr>
                                    <td>PendingBeneficiaryTerm:</td>
                                    <td>${actorInfo.PendingBeneficiaryTerm}</td>
                                  </tr>
                                `
                                                                    : null
                                                    }
                                                    </tbody>
                                                </table>
                                            </div>
                                        </div>

                                        <!-- Wallets Section -->
                                        <div class="col-md-auto">
                                            <div class="info-block dash-tile">
                                                <h3>Wallets</h3>
                                            </div>
                                            <table class="table table-dark">
                                                <thead>
                                                <tr>
                                                    <th scope="col">Type</th>
                                                    <th scope="col">Address</th>
                                                    <th scope="col">Balance</th>
                                                </tr>
                                                </thead>
                                                <tbody>
                                                ${
                                                        actorInfo.Wallets.map(wallet => html`
                            <tr>
                              <td>${wallet.Type}</td>
                              <td>${wallet.Address}</td>
                              <td>${wallet.Balance}</td>
                            </tr>
                          `)
                                                }
                                                </tbody>
                                            </table>
                                        </div>
                                    </div>

                                    <!-- Sector Expirations -->
                                    <div class="row">
                                        <div class="col-md-auto">
                                            <div class="info-block dash-tile">
                                                <h3>Charts</h3>
                                                <div style="background: #3f3f3f; height: 200px; width: 90vw;"></div>
                                                <actor-charts address="${actorInfo.Summary.Address}"></actor-charts>
                                            </div>
                                        </div>
                                    </div>
                                </section>
                            `)
            }
        `;
    }

    /**
     * Convert a bytes number to a human-readable string (e.g., KB, MB, etc.)
     */
    toHumanBytes(bytes) {
        if (typeof bytes !== 'number') {
            return 'N/A';
        }
        const sizes = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB', 'ZiB'];
        let sizeIndex = 0;
        for (; bytes >= 1024 && sizeIndex < sizes.length - 1; sizeIndex++) {
            bytes /= 1024;
        }
        return bytes.toFixed(2) + ' ' + sizes[sizeIndex];
    }

    /**
     * Renders the deadlines as colored boxes
     */
    renderDeadlines(deadlines) {
        return html`
            <div class="deadline-box">
                ${deadlines.map(d => html`
                    <div
                            class="deadline-entry
              ${d.Current ? 'deadline-entry-cur' : ''}
              ${d.Proven ? 'deadline-proven' : ''}
              ${d.PartFaulty ? 'deadline-partially-faulty' : ''}
              ${d.Faulty ? 'deadline-faulty' : ''}"
                    ></div>
                `)}
            </div>
        `;
    }
});

/**
 * <sector-charts> component
 * Renders line charts for "All" and "CC" sector expiration data.
 * Renders line chart for "All" sector QAP and Vesting
 */
class ActorCharts extends LitElement {
    static properties = {
        address: { type: String },
    };

    static styles = css`
    :host {
      display: block;
      width: 900px; /* adjust as needed */
    }
    .chart-container {
      width: 100%;
      height: 200px;
      margin-bottom: 2rem;
    }
  `;

    constructor() {
        super();
        this.data = { All: [], CC: [] };
        this.chartExpiration = null;
        this.chartQAP = null;
        this.chartVested = null;
    }

    updated(changedProps) {
        if (changedProps.has('address') && this.address) {
            this.loadData();
        }
    }

    async loadData() {
        if (!this.address) {
            console.error('Address is not set');
            return;
        }

        try {
            this.data = await RPCCall('ActorCharts', [this.address]);
            this.renderCharts();

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

    /**
     * Creates and/or updates all 3 charts
     */
    renderCharts() {
        if (!this.data || (!this.data.All.length && !this.data.CC.length)) {
            console.warn('No data to render');
            return;
        }

        // We'll define "nowEpoch" from the earliest BucketEpoch we have
        // (in All or CC).
        const firstAll = this.data.All[0]?.BucketEpoch ?? Infinity;
        const firstCC = this.data.CC[0]?.BucketEpoch ?? Infinity;
        const nowEpoch = Math.min(firstAll, firstCC);

        // ---------------------------
        // 1) EXPIRATION CHART (All vs. CC)
        // ---------------------------
        {
            // expiration (Count) data sets:
            const allExpData = this.data.All.map(d => ({ x: d.BucketEpoch, y: d.Count }));
            const ccExpData = this.data.CC.map(d => ({ x: d.BucketEpoch, y: d.Count }));

            const expConfig = {
                type: 'line',
                data: {
                    datasets: [
                        {
                            label: 'All Sectors (Count)',
                            borderColor: 'rgb(75, 192, 192)',
                            backgroundColor: 'rgba(75, 192, 192, 0.2)',
                            borderWidth: 1,
                            stepped: true,
                            fill: true,
                            pointRadius: 2,
                            data: allExpData,
                        },
                        {
                            label: 'CC Sectors (Count)',
                            borderColor: 'rgb(99,255,161)',
                            backgroundColor: 'rgba(99,255,161,0.2)',
                            borderWidth: 1,
                            stepped: true,
                            fill: true,
                            pointRadius: 2,
                            data: ccExpData,
                        },
                    ],
                },
                options: this.createChartOptions('Expiration (Count)', 'Count', nowEpoch, allExpData, ccExpData),
            };

            if (!this.chartExpiration) {
                const ctx = this.shadowRoot.querySelector('#expiration-chart').getContext('2d');
                this.chartExpiration = new Chart(ctx, expConfig);
            } else {
                this.chartExpiration.data = expConfig.data;
                this.chartExpiration.options = expConfig.options;
                this.chartExpiration.update();
            }
        }

        // ---------------------------
        // 2) QAP CHART (Only All)
        // ---------------------------
        {
            // QAP is a big-int string; parse to float
            const allQAPData = this.data.All.map(d => ({
                x: d.BucketEpoch,
                y: parseFloat(d.QAP), // note: large values lose precision in float
            }));

            const qapConfig = {
                type: 'line',
                data: {
                    datasets: [
                        {
                            label: 'All Sectors (QAP)',
                            borderColor: 'rgb(255, 205, 86)',
                            backgroundColor: 'rgba(255, 205, 86, 0.2)',
                            borderWidth: 1,
                            stepped: true,
                            fill: true,
                            pointRadius: 2,
                            data: allQAPData,
                        },
                    ],
                },
                options: this.createChartOptions('Quality-Adjusted Power (All)', 'QAP', nowEpoch, allQAPData),
            };

            if (!this.chartQAP) {
                const ctx = this.shadowRoot.querySelector('#qap-chart').getContext('2d');
                this.chartQAP = new Chart(ctx, qapConfig);
            } else {
                this.chartQAP.data = qapConfig.data;
                this.chartQAP.options = qapConfig.options;
                this.chartQAP.update();
            }
        }

        // ---------------------------
        // 3) VESTED LOCKED FUNDS CHART (Only All)
        // ---------------------------
        {
            // Also a big-int string; parse to float
            const allLockedData = this.data.All.map(d => ({
                x: d.BucketEpoch,
                y: parseFloat(d.VestedLockedFunds), // again, big values => float precision
            }));

            const lockedConfig = {
                type: 'line',
                data: {
                    datasets: [
                        {
                            label: 'All Sectors (Locked Funds)',
                            borderColor: 'rgb(153, 102, 255)',
                            backgroundColor: 'rgba(153, 102, 255, 0.2)',
                            borderWidth: 1,
                            stepped: true,
                            fill: true,
                            pointRadius: 2,
                            data: allLockedData,
                        },
                    ],
                },
                options: this.createChartOptions('Vested Locked Funds (All)', 'Locked Funds', nowEpoch, allLockedData),
            };

            if (!this.chartVested) {
                const ctx = this.shadowRoot.querySelector('#lockedfunds-chart').getContext('2d');
                this.chartVested = new Chart(ctx, lockedConfig);
            } else {
                this.chartVested.data = lockedConfig.data;
                this.chartVested.options = lockedConfig.options;
                this.chartVested.update();
            }
        }
    }

    /**
     * Creates a Chart.js options object with shared logic for axis, tooltips, etc.
     * @param {string} chartTitle - The chart title
     * @param {string} yTitle - Label for Y axis
     * @param {number} nowEpoch - The earliest epoch we consider "current"
     * @param {Array} allData - The data array for the "All" set
     * @param {Array} [ccData] - Optional data array for the "CC" set
     */
    createChartOptions(chartTitle, yTitle, nowEpoch, allData, ccData = []) {
        return {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                title: {
                    display: true,
                    text: chartTitle,
                },
                tooltip: {
                    callbacks: {
                        label: (context) => {
                            // Convert epoch to 'days' offset from nowEpoch
                            const epochVal = context.parsed.x;
                            const daysOffset = Math.round(((epochVal - nowEpoch) * 30) / 86400);
                            const months = (daysOffset / 30).toFixed(1);
                            return `${
                                context.dataset.label
                            }: ${context.parsed.y}, Days: ${daysOffset} (months: ${months})`;
                        },
                    },
                },
            },
            scales: {
                x: {
                    type: 'linear',
                    position: 'bottom',
                    title: {
                        display: true,
                        text: 'Days in Future',
                    },
                    ticks: {
                        callback: (value) => {
                            const days = Math.round(((value - nowEpoch) * 30) / 86400);
                            return days + 'd';
                        },
                    },
                    // compute max from both sets if available
                    min: nowEpoch,
                    max: (() => {
                        const maxAll = allData.length ? allData[allData.length - 1].x : 0;
                        const maxCC = ccData.length ? ccData[ccData.length - 1].x : 0;
                        return Math.max(maxAll, maxCC);
                    })(),
                    afterDataLimits: (scale) => {
                        scale.max += (scale.max - scale.min) * 0.05;
                    },
                },
                y: {
                    title: {
                        display: true,
                        text: yTitle,
                    },
                    beginAtZero: true,
                },
            },
        };
    }

    render() {
        return html`
            <div class="chart-container">
                <!-- Expiration Chart (All vs CC) -->
                <canvas id="expiration-chart"></canvas>
            </div>

            <div class="chart-container">
                <!-- QAP Chart (Only All) -->
                <canvas id="qap-chart"></canvas>
            </div>

            <div class="chart-container">
                <!-- Vested Locked Funds (Only All) -->
                <canvas id="lockedfunds-chart"></canvas>
            </div>
        `;
    }
}

customElements.define('actor-charts', ActorCharts);

