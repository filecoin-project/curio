import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/task.mjs';
import { formatDate } from '/lib/dateutil.mjs';

class ProofShareElement extends LitElement {
  static properties = {
    enabled: { type: Boolean },
    wallet: { type: String },
    price: { type: String },
    queue: { type: Array },
    paymentSummaries: { type: Array },
    settlementHistory: { type: Array },
  };

  constructor() {
    super();
    this.enabled = false;
    this.wallet = '';
    this.price = '';
    this.queue = [];
    this.paymentSummaries = [];
    this.settlementHistory = [];
    this.loadData();
  }

  // Disable shadow DOM, so Bootstrap + your main.css apply naturally.
  createRenderRoot() {
    return this;
  }

  // Periodically load data from the server
  async loadData() {
    try {
      // 1) Get the meta info (enabled/wallet/request_task_id)
      const meta = await RPCCall('PSGetMeta', []);
      this.enabled = meta.enabled;
      this.wallet = meta.wallet || '';
      this.price = meta.price || '';
      // 2) Get the queue
      const queue = await RPCCall('PSListQueue', []);
      this.queue = queue;
      // 3) Get provider payment summaries
      const summaries = await RPCCall('PSProviderLastPaymentsSummary', []);
      this.paymentSummaries = summaries;
      // 4) Get recent settlements
      const settlements = await RPCCall('PSListSettlements', []);
      this.settlementHistory = settlements;
    } catch (err) {
      console.error('Failed to load proofshare data:', err);
      this.paymentSummaries = [];
      this.settlementHistory = [];
    }
    // Re-render
    this.requestUpdate();
  }

  // Update meta info on the server
  async setMeta() {
    try {
      // Call PSSetMeta(enabled, wallet, price)
      await RPCCall('PSSetMeta', [this.enabled, this.wallet, this.price]);
      console.log('Updated proofshare meta successfully');
    } catch (err) {
      console.error('Failed to update proofshare meta:', err);
    }
  }

  render() {
    return html`
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
        crossorigin="anonymous"
      />
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

      <div class="container">
        <h2>🏗️ Provider Settings</h2>
        <p>Sell idle compute to a proving market.</p>

        <div class="mb-2">
          <label class="form-check-label">
            <input
              type="checkbox"
              ?checked=${this.enabled}
              @change=${(e) => (this.enabled = e.target.checked)}
            />
            <span>Enabled</span>
          </label>
        </div>
        <div class="mb-2">
          <label>Wallet:</label>
          <input
            type="text"
            placeholder="f0/1/2/3..."
            .value=${this.wallet}
            @input=${(e) => (this.wallet = e.target.value)}
            style="max-width: 400px;"
          />
        </div>
        <div class="mb-2">
          <label>Price:</label>
          <input
            type="number"
            step="0.0001"
            placeholder="0.0001"
            .value=${this.price}
            @input=${(e) => (this.price = e.target.value)}
            style="max-width: 100px;"
          />
        </div>

        <button class="btn btn-primary" @click=${this.setMeta}>Update Settings</button>

        <hr />

        <h2>💰 Provider Payments Summary</h2>
        ${this.paymentSummaries && this.paymentSummaries.length > 0 ? html`
          <table class="table table-dark table-striped table-hover">
            <thead>
              <tr>
                <th>Provider Address</th>
                <th>Last Nonce</th>
                <th>Last Settled FIL</th>
                <th>Unsettled FIL</th>
                <th>Last Settled At</th>
                <th>Time Since Settlement</th>
                <th>Contract Last Nonce</th>
                <th>Contract Settled FIL</th>
              </tr>
            </thead>
            <tbody>
              ${this.paymentSummaries.map((summary) => html`
                <tr>
                  <td class="text-break">${summary.address}</td>
                  <td>${summary.last_payment_nonce}</td>
                  <td>${summary.last_settled_amount_fil || 'N/A'}</td>
                  <td>${summary.unsettled_amount_fil || '0 FIL'}</td>
                  <td>${summary.last_settled_at ? formatDate(summary.last_settled_at) : 'N/A'}</td>
                  <td>${summary.time_since_last_settlement || 'N/A'}</td>
                  <td>${summary.contract_last_nonce !== null && summary.contract_last_nonce !== undefined ? summary.contract_last_nonce : 'N/A'}</td>
                  <td>${summary.contract_settled_fil || 'N/A'}</td>
                </tr>
              `)}
            </tbody>
          </table>
        ` : html`<p>No payment summary data available.</p>`}

        <hr />

        <h2>📜 Recent Settlements</h2>
        ${this.settlementHistory && this.settlementHistory.length > 0 ? html`
          <table class="table table-dark table-striped table-hover">
            <thead>
              <tr>
                <th>Provider Address</th>
                <th>Nonce</th>
                <th>Amount Settled in Tx (FIL)</th>
                <th>Settled At</th>
                <th>Settle Message CID</th>
              </tr>
            </thead>
            <tbody>
              ${this.settlementHistory.map((settlement) => html`
                <tr>
                  <td class="text-break">${settlement.address}</td>
                  <td>${settlement.payment_nonce}</td>
                  <td>${settlement.amount_for_this_settlement_fil}</td>
                  <td>${formatDate(settlement.settled_at)}</td>
                  <td class="text-break">${settlement.settle_message_cid}</td>
                </tr>
              `)}
            </tbody>
          </table>
        ` : html`<p>No settlement history available.</p>`}

        <hr />

        <h2>Queue</h2>
        <table class="table table-dark">
          <thead>
            <tr>
              <th>Service ID</th>
              <th>Obtained At</th>
              <th>Compute Task</th>
              <th>Compute Done</th>
              <th>Submit Task</th>
              <th>Submit Done</th>
            </tr>
          </thead>
          <tbody>
            ${this.queue.map((item) => html`
              <tr>
                <td>${item.service_id}</td>
                <td>${formatDate(item.obtained_at)}</td>
                <td>${item.compute_task_id ? html`<task-status .taskId=${item.compute_task_id}></task-status>` : ''}</td>
                <td>${item.compute_done ? 'Yes' : 'No'}</td>
                <td>${item.submit_task_id ? html`<task-status .taskId=${item.submit_task_id}></task-status>` : ''}</td>
                <td>${item.submit_done ? 'Yes' : 'No'}</td>
              </tr>
            `)}
          </tbody>
        </table>
      </div>
    `;
  }
}

// Register the custom element
customElements.define('proof-share', ProofShareElement);
