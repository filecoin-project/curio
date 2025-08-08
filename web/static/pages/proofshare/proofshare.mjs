// SPDX-License-Identifier: CCL-1.0

import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/task.mjs';
import { formatDate } from '/lib/dateutil.mjs';
import '/lib/cu-wallet.mjs';
import '/ux/yesno.mjs';
import '/ux/message.mjs';
import '/ux/tos-modal.mjs';

class ProofShareElement extends LitElement {
  static properties = {
    enabled: { type: Boolean },
    wallet: { type: String },
    price: { type: String },
    queue: { type: Array },
    paymentSummaries: { type: Array },
    settlementHistory: { type: Array },
    activeAsks: { type: Array },
    // Current backend state (read-only)
    currentEnabled: { type: Boolean },
    currentWallet: { type: String },
    currentPrice: { type: String },
  };

  constructor() {
    super();
    // UI form state
    this.enabled = false;
    this.wallet = '';
    this.price = '';
    
    // Current backend state (read-only)
    this.currentEnabled = false;
    this.currentWallet = '';
    this.currentPrice = '';
    
    this.queue = [];
    this.paymentSummaries = [];
    this.settlementHistory = [];
    this.activeAsks = [];
    this.originalEnabled = false; // Track original state for TOS handling
    this.formLoaded = false;
    this.loadData();
    
    // Auto-refresh backend state every 3 seconds
    this.backendStateInterval = setInterval(() => {
      this.loadBackendState();
    }, 3000);
    
    // Auto-refresh other data every 5 seconds
    this.refreshInterval = setInterval(() => {
      this.loadOtherData();
    }, 5000);

    // Add TOS event listeners
    this.addEventListener('tos-accepted', this.handleTosAccepted.bind(this));
    this.addEventListener('tos-rejected', this.handleTosRejected.bind(this));
  }

  // Disable shadow DOM, so Bootstrap + your main.css apply naturally.
  createRenderRoot() {
    return this;
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    if (this.refreshInterval) {
      clearInterval(this.refreshInterval);
    }
    if (this.backendStateInterval) {
      clearInterval(this.backendStateInterval);
    }
  }

  // Load all data on initial load
  async loadData() {
    await this.loadBackendState();
    await this.loadOtherData();
  }

  // Load backend state for current state display
  async loadBackendState() {
    try {
      const meta = await RPCCall('PSGetMeta', []);
      // Update current backend state (for display)
      this.currentEnabled = meta.enabled;
      this.currentWallet = meta.wallet || '';
      this.currentPrice = meta.price || '';
      
      if (!this.formLoaded) {
        this.enabled = meta.enabled;
        this.wallet = meta.wallet || '';
        this.price = meta.price || '';
        this.originalEnabled = this.enabled;
        this.formLoaded = true;
      }
      
      this.requestUpdate();
    } catch (err) {
      console.error('Failed to load proofshare meta:', err);
    }
  }

  // Load other data (queue, payments, etc.)
  async loadOtherData() {
    // 2) Get the queue
    try {
      const queue = await RPCCall('PSListQueue', []);
      this.queue = queue;
    } catch (err) {
      console.error('Failed to load proofshare queue:', err);
      this.queue = [];
    }

    // 3) Get provider payment summaries
    try {
      const summaries = await RPCCall('PSProviderLastPaymentsSummary', []);
      this.paymentSummaries = summaries;
    } catch (err) {
      console.error('Failed to load payment summaries:', err);
      this.paymentSummaries = [];
    }

    // 4) Get recent settlements
    try {
      const settlements = await RPCCall('PSListSettlements', []);
      this.settlementHistory = settlements;
    } catch (err) {
      console.error('Failed to load settlements:', err);
      this.settlementHistory = [];
    }

    // 5) Get active asks
    try {
      const asks = await RPCCall('PSListAsks', []);
      this.activeAsks = asks || [];
    } catch (err) {
      console.error('Failed to load active asks:', err);
      this.activeAsks = [];
    }
    // Re-render
    this.requestUpdate();
  }

  /**
   * Handle TOS acceptance from the modal component
   */
  handleTosAccepted(event) {
    if (event.detail.type === 'provider') {
      // Enable the provider and update checkbox
      this.enabled = true;
      const checkbox = this.querySelector('input[type="checkbox"]');
      if (checkbox) {
        checkbox.checked = true;
      }
      this.requestUpdate();
    }
  }

  /**
   * Handle TOS rejection from the modal component
   */
  handleTosRejected(event) {
    if (event.detail.type === 'provider') {
      // Revert enabled state
      this.enabled = this.originalEnabled;
      this.requestUpdate();
      window.location.href = '/';
    }
  }

  /**
   * Handle enabled checkbox change with TOS check
   */
  async onEnabledChange(event) {
    const newValue = event.target.checked;
    
    // If trying to enable and not previously enabled, check TOS
    if (newValue && !this.originalEnabled) {
      const tosAccepted = localStorage.getItem('curio-proofshare-provider-tos-accepted') === 'true';
      
      if (!tosAccepted) {
        // Reset checkbox to unchecked state
        event.target.checked = false;
        
        const tosModal = this.querySelector('tos-modal');
        if (tosModal) {
          await tosModal.showModal('provider');
        }
        return; // Don't update enabled state yet
      }
    }
    
    // Update the enabled state
    this.enabled = newValue;
    this.requestUpdate();
  }

  // Update meta info on the server
  async setMeta() {
    await this.actuallySetMeta();
  }

  async actuallySetMeta() {
    try {
      // Call PSSetMeta(enabled, wallet, price)
      await RPCCall('PSSetMeta', [this.enabled, this.wallet, this.price]);
      console.log('Updated proofshare meta successfully');
      
      // Immediately refresh backend state to update current state display
      await this.loadBackendState();
      
      // Also refresh other data
      this.loadOtherData();
    } catch (err) {
      console.error('Failed to update proofshare meta:', err);
      alert(`Error updating settings: ${err.message || err}`);
    }
  }

  async handleSettleProvider(providerID) {
    if (!confirm(`Are you sure you want to settle payments for provider ID ${providerID}?`)) {
      return;
    }
    try {
      // providerID in the summary is summary.wallet_id, which is the numeric ID.
      const resultCid = await RPCCall('PSProviderSettle', [providerID]);
      alert(`Settlement message sent successfully! CID: ${resultCid}`);
      this.loadData(); // Refresh data
    } catch (err) {
      console.error('Failed to settle provider payments:', err);
      alert(`Error settling payments: ${err.message || err}`);
    }
  }

  async handleWithdrawAsk(askID) {
    if (!confirm(`Are you sure you want to withdraw ask ${askID}?`)) {
      return;
    }
    try {
      await RPCCall('PSAskWithdraw', [askID]);
      alert(`Ask ${askID} withdrawn successfully!`);
      this.loadData(); // Refresh data
    } catch (err) {
      console.error('Failed to withdraw ask:', err);
      alert(`Error withdrawing ask: ${err.message || err}`);
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

                  <div class="mb-4">
            <span>📊 Current State</span>
            <div class="d-flex gap-4">
              <div>Status: <yes-no .value=${this.currentEnabled}></yes-no></div>
              <div>Wallet: ${this.currentWallet ? html`<code>${this.currentWallet}</code>` : html`<em>Not configured</em>`}</div>
              <div>Price: ${this.currentPrice ? html`<code>${this.currentPrice} FIL/P</code>` : html`<em>Not set</em>`}</div>
            </div>
          </div>

        <span>⚙️ Update Settings</span>
        <div class="mb-2">
          <label class="form-check-label">
            <input
              type="checkbox"
              ?checked=${this.enabled}
              @change=${this.onEnabledChange}
            />
            <span>Enabled</span>
          </label>
        </div>
        <div class="mb-3">
          <div class="row mb-2 align-items-center">
            <label for="proofshareWallet" class="col-sm-4 col-md-3 col-lg-2 col-form-label text-sm-end">Wallet:</label>
            <div class="col-sm-8 col-md-9 col-lg-10">
              <input
                id="proofshareWallet"
                type="text"
                placeholder="f0/1/2/3..."
                .value=${this.wallet}
                @input=${(e) => (this.wallet = e.target.value)}
                style="max-width: 400px; width: 100%;"
              />
            </div>
          </div>
          <div class="row align-items-center"> <!-- mb-2 removed from last row in group to use parent's mb-3 -->
            <label for="proofsharePrice" class="col-sm-4 col-md-3 col-lg-2 col-form-label text-sm-end">Price (FIL/P):</label>
            <div class="col-sm-8 col-md-9 col-lg-10">
              <input
                id="proofsharePrice"
                type="number"
                step="0.0001"
                placeholder="0.0001"
                .value=${this.price}
                @input=${(e) => (this.price = e.target.value)}
                style="max-width: 100px; width: 100%;"
              />
            </div>
          </div>
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
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              ${this.paymentSummaries.map((summary) => html`
                <tr>
                  <td class="text-break"><cu-wallet wallet_id=${summary.address}></cu-wallet></td>
                  <td>${summary.last_payment_nonce}</td>
                  <td>${summary.last_settled_amount_fil || 'N/A'}</td>
                  <td>${summary.unsettled_amount_fil || '0 FIL'}</td>
                  <td>${summary.last_settled_at ? formatDate(summary.last_settled_at) : 'N/A'}</td>
                  <td>${summary.time_since_last_settlement || 'N/A'}</td>
                  <td>${summary.contract_last_nonce !== null && summary.contract_last_nonce !== undefined ? summary.contract_last_nonce : 'N/A'}</td>
                  <td>${summary.contract_settled_fil || 'N/A'}</td>
                  <td>
                    <button 
                      class="btn btn-sm btn-info"
                      @click=${() => this.handleSettleProvider(summary.wallet_id)}
                      title="Settle payments for this provider"
                    >
                      Settle
                    </button>
                  </td>
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
                  <td class="text-break"><cu-wallet wallet_id=${settlement.address}></cu-wallet></td>
                  <td>${settlement.payment_nonce}</td>
                  <td>${settlement.amount_for_this_settlement_fil}</td>
                  <td>${formatDate(settlement.settled_at)}</td>
                  <td class="text-break"><fil-message cid=${settlement.settle_message_cid}></fil-message></td>
                </tr>
              `)}
            </tbody>
          </table>
        ` : html`<p>No settlement history available.</p>`}

        <hr />

        <h2>🏷️ Active Asks</h2>
        ${this.activeAsks && this.activeAsks.length > 0 ? html`
          <table class="table table-dark table-striped table-hover">
            <thead>
              <tr>
                <th>Ask ID</th>
                <th>Min Price (FIL)</th>
                <th>Created At</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              ${this.activeAsks.map((ask) => html`
                <tr>
                  <td>${ask.id}</td>
                  <td style="white-space: nowrap;">${ask.min_price_fil}</td>
                  <td>${formatDate(ask.created_at)}</td>
                  <td>
                    <button class="btn btn-sm btn-warning" @click=${() => this.handleWithdrawAsk(ask.id)}>Withdraw</button>
                  </td>
                </tr>
              `)}
            </tbody>
          </table>
        ` : html`<p>No active asks available.</p>`}

        <hr />

        <h2>⏭️ Queue</h2>
        <table class="table table-dark">
          <thead>
            <tr>
              <th>Service ID</th>
              <th>Obtained At</th>
              <th>Compute Done</th>
              <th>Submit Done</th>
              <th>Reward</th>
            </tr>
          </thead>
          <tbody>
            ${this.queue.map((item) => html`
              <tr>
                <td><abbr title="${item.service_id}">${item.service_id.substring(0, 3)}..${item.service_id.slice(-10)}</abbr></td>
                <td>${formatDate(item.obtained_at)}</td>
                <td><done-not-done .value=${item.compute_done}></done-not-done>${item.compute_task_id ? html`<task-status .taskId=${item.compute_task_id}></task-status>` : ''}</td>
                <td><done-not-done .value=${item.submit_done}></done-not-done>${item.submit_task_id ? html`<task-status .taskId=${item.submit_task_id}></task-status>` : ''}</td>
                <td style="white-space: nowrap;">${item.was_pow ? html`<span style="color: #888;">Challenge</span>` : item.payment_amount}</td>
              </tr>
            `)}
          </tbody>
        </table>

        <!-- TOS Modal Component -->
        <tos-modal></tos-modal>
      </div>
    `;
  }
}

// Register the custom element
customElements.define('proof-share', ProofShareElement);
