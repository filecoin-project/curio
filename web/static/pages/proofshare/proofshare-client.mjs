import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { formatDate } from '/lib/dateutil.mjs';

class ProofShareClient extends LitElement {
  static properties = {
    settingsList: { type: Array },
    spRequests: { type: Object },
    showRequests: { type: Object },
    wallets: { type: Array },
    messages: { type: Array },
  };

  constructor() {
    super();
    // All proofshare_client_settings rows
    this.settingsList = [];

    // For each sp_id => an array of requests (from proofshare_client_requests)
    this.spRequests = {};

    // For each sp_id => boolean whether we’re showing requests
    this.showRequests = {};

    // All proofshare_client_wallets rows.
    // Note: each wallet object is now expected to include a new field, "withdraw_timestamp",
    // which is a numeric string (unix seconds) showing when the withdrawal can be completed.
    this.wallets = [];
    
    // All proofshare_client_messages rows
    this.messages = [];

    // Initial load of settings, wallets, and client messages
    this.loadAllSettings();

    // Refresh settings every 10 seconds
    this.refreshIntervalId = setInterval(() => this.loadMessages(), 10000);

    // Refresh UI every second to update any countdowns
    this.countdownIntervalId = setInterval(() => this.requestUpdate(), 1000);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearInterval(this.refreshIntervalId);
    clearInterval(this.countdownIntervalId);
  }

  createRenderRoot() {
    // Use light DOM so Bootstrap + main.css apply
    return this;
  }

  /**
   * Fetch all rows from PSClientGet (settings), wallets, and client messages.
   */
  async loadAllSettings() {
    this.settingsList = await RPCCall('PSClientGet', []);
    this.wallets = await RPCCall('PSClientWallets', []);
    this.loadMessages();
  }

  async loadMessages() {
    this.messages = await RPCCall('PSClientListMessages', []);
    this.requestUpdate();
  }

  /**
   * Toggle whether to show requests for a particular sp_id.
   */
  async toggleRequests(spId, address) {
    const wasShown = !!this.showRequests[spId];
    // Flip boolean
    this.showRequests[spId] = !wasShown;

    // If turning on and no requests loaded yet, fetch them.
    if (!wasShown && !this.spRequests[spId]) {
      try {
        const reqs = await RPCCall('PSClientRequests', [spId]);
        this.spRequests[spId] = Array.isArray(reqs) ? reqs : [];
      } catch (err) {
        console.error(`Error loading requests for ${address}:`, err);
        this.spRequests[spId] = [];
      }
    }
    this.requestUpdate();
  }

  /**
   * Update a field in a settings row.
   */
  onChange(row, field, value) {
    row[field] = value;
    this.requestUpdate();
  }

  /**
   * Saves a settings row.
   */
  async saveRow(row) {
    try {
      await RPCCall('PSClientSet', [{
        sp_id: row.sp_id,
        address: row.address,
        enabled: row.enabled,
        wallet: row.wallet,
        minimum_pending_seconds: row.minimum_pending_seconds,
        do_porep: row.do_porep,
        do_snap: row.do_snap,
        price: row.price,
      }]);
      console.log(`Saved row for sp_id=${row.sp_id}`);
    } catch (err) {
      console.error(`Error saving settings for sp_id=${row.sp_id}:`, err);
    }
  }

  /**
   * Add a new SP row.
   */
  async addSP() {
    const address = prompt('Enter new SP address:');
    if (!address) return; // cancelled

    const newRow = {
      address,
      enabled: false,
      wallet: null,
      minimum_pending_seconds: 0,
      do_porep: false,
      do_snap: false,
      price: 0,
    };

    try {
      await RPCCall('PSClientSet', [newRow]);
      console.log(`Added new ${address}`);
      await this.loadAllSettings();
    } catch (err) {
      console.error('Error adding new sp row:', err);
      alert(`Failed to add ${address}. See console for details.`);
    }
  }

  async clientAddWallet() {
    const address = prompt('Enter new client wallet address:');
    if (!address) return;
    await RPCCall('PSClientAddWallet', [address]);
    await this.loadAllSettings();
  }

  async promptForAmount(action, address) {
    const rawAmount = prompt(`Enter balance in FIL to ${action} (e.g., "1.23"):`);
    if (!rawAmount) return null;
    const amount = rawAmount.trim();
    const amountRegex = /^[0-9]+(\.[0-9]+)?$/;
    if (!amountRegex.test(amount)) {
      alert(`Invalid amount: "${amount}". Please enter a valid numeric value (e.g., "1.23").`);
      return null;
    }

    if (!confirm(`Are you sure you want to ${action} ${amount} FIL for ${address}?`)) return null;
    return amount;
  }

  async clientRouterAddBalance(address) {
    const amount = await this.promptForAmount('add', address);
    if (amount === null) return;
    let cid = await RPCCall('PSClientRouterAddBalance', [address, amount]);
    alert(`Deposit message sent: ${cid['/']}`);
    await this.loadAllSettings();
  }

  async clientRouterRequestWithdrawal(address) {
    const amount = await this.promptForAmount('withdraw', address);
    if (amount === null) return;
    await RPCCall('PSClientRouterRequestWithdrawal', [address, amount]);
    await this.loadAllSettings();
  }

  async clientRouterCancelWithdrawal(address) {
    if (!confirm(`Are you sure you want to cancel withdrawal for ${address}?`)) return;
    await RPCCall('PSClientRouterCancelWithdrawal', [address]);
    await this.loadAllSettings();
  }

  async clientRouterCompleteWithdrawal(address) {
    if (!confirm(`Are you sure you want to complete withdrawal for ${address}?`)) return;
    await RPCCall('PSClientRouterCompleteWithdrawal', [address]);
    await this.loadAllSettings();
  }

  /**
   * Render a sub-table of requests for a given SP.
   */
  renderRequestsFor(spId, address) {
    const list = this.spRequests[spId] || [];
    return html`
      <h4>Requests for ${address}</h4>
      <table class="table table-dark table-sm table-striped">
        <thead>
          <tr>
            <th>Task ID</th>
            <th>Sector</th>
            <th>Service ID</th>
            <th>Done</th>
            <th>Created At</th>
            <th>Done At</th>
          </tr>
        </thead>
        <tbody>
          ${list.map(req => html`
            <tr>
              <td>${req.task_id}</td>
              <td>${req.sp_id}:${req.sector_num}</td>
              <td>${req.service_id}</td>
              <td>${req.done ? 'Yes' : 'No'}</td>
              <td>${req.created_at}</td>
              <td>${req.done_at?.Time || ''}</td>
            </tr>
          `)}
        </tbody>
      </table>
    `;
  }

  render() {
    return html`
      <link
        rel="stylesheet"
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
        crossorigin="anonymous"
      />
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

      <div class="container">
        <h2>🛒 Client Settings</h2>
        <p>Buy proof compute from a market. Select which SP pipelines should use the market.</p>
        <div class="pricing-info">
          <p>
            <strong>FIL/P</strong> is the price of proof compute in FIL per approximately 130M constraints.
          </p>
          <ul>
            <li><strong>PoRep Proof:</strong> 10x 130M constraints.</li>
            <li><strong>NI-PoRep:</strong> 126x 130M constraints.</li>
            <li><strong>SnapDeals Proof:</strong> 16x 130M constraints.</li>
            <li><strong>WindowPoSt:</strong> 1x 130M constraints.</li>
          </ul>
        </div>

        <div class="mb-2">
          <button class="btn btn-primary" @click=${this.addSP}>
            Add SP
          </button>
        </div>

        <table class="table table-dark">
          <thead>
            <tr>
              <th>SP ID</th>
              <th>Enabled</th>
              <th>Wallet</th>
              <th><abbr title="Delay before outsourcing work, allowing time for local compute to be used first">buy_delay_secs</abbr></th>
              <th>do_porep</th>
              <th>do_snap</th>
              <th>FIL/P</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            ${this.settingsList.map((row) => html`
              <tr>
                <td>${row.sp_id === 0 ? 'all/other' : row.address}</td>
                <td>
                  <input
                    type="checkbox"
                    .checked=${row.enabled}
                    @change=${(e) => this.onChange(row, 'enabled', e.target.checked)}
                  />
                </td>
                <td>
                  <input
                    type="text"
                    style="width:150px"
                    .value=${row.wallet || ''}
                    @input=${(e) => this.onChange(row, 'wallet', e.target.value || null)}
                  />
                </td>
                <td>
                  <input
                    type="number"
                    style="width:120px"
                    .value=${row.minimum_pending_seconds}
                    @input=${(e) => this.onChange(row, 'minimum_pending_seconds', parseInt(e.target.value) || 0)}
                  />
                </td>
                <td>
                  <input
                    type="checkbox"
                    .checked=${row.do_porep}
                    @change=${(e) => this.onChange(row, 'do_porep', e.target.checked)}
                  />
                </td>
                <td>
                  <input
                    type="checkbox"
                    .checked=${row.do_snap}
                    @change=${(e) => this.onChange(row, 'do_snap', e.target.checked)}
                  />
                </td>
                <td>
                  <input
                    type="number"
                    step="0.0001"
                    style="width:100px"
                    .value=${row.price}
                    @input=${(e) => this.onChange(row, 'price', e.target.value === '' ? "0" : e.target.value)}
                  />
                </td>
                <td>
                  <button class="btn btn-success btn-sm" @click=${() => this.saveRow(row)}>
                    Save
                  </button>
                  <button class="btn btn-info btn-sm" @click=${() => this.toggleRequests(row.sp_id, row.address)}>
                    ${this.showRequests[row.sp_id] ? 'Hide' : 'View'} Requests
                  </button>
                  ${row.sp_id !== 0 ? html`
                    <button
                      class="btn btn-danger btn-sm"
                      @click=${() => this.removeRow(row.sp_id, row.address)}
                    >
                      Remove
                    </button>
                  ` : null}
                </td>
              </tr>

              ${this.showRequests[row.sp_id] ? html`
                <tr>
                  <td colspan="7">
                    ${this.renderRequestsFor(row.sp_id, row.address)}
                  </td>
                </tr>
              ` : ''}
            `)}
          </tbody>
        </table>

        <h2>💰 Client Wallets</h2>
        <p>Wallets that are used for client payments.</p>
        <div class="mb-2">
          <button class="btn btn-primary" @click=${this.clientAddWallet}>
            Add Wallet
          </button>
        </div>
        <table class="table table-dark">
          <thead>
            <tr>
              <th>Wallet</th>
              <th>Chain FIL</th>
              <th>Router FIL</th>
              <th>Unlocked FIL</th>
              <th>Available FIL</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            ${this.wallets.map(wallet => {
              // Parse the withdraw timestamp (a unix second value returned as a string)
              const withdrawTs = parseInt(wallet.withdraw_timestamp || "0", 10);
              const now = Math.floor(Date.now() / 1000);
              let withdrawalActions;
              if (withdrawTs > 0) {
                const countdown = withdrawTs - now;
                withdrawalActions = html`
                  <button class="btn btn-warning btn-sm" @click=${() => this.clientRouterCancelWithdrawal(wallet.address)}>
                    Cancel Withdrawal
                  </button>
                  <button class="btn btn-secondary btn-sm" ?disabled=${countdown > 0} @click=${() => this.clientRouterCompleteWithdrawal(wallet.address)}>
                    Complete Withdrawal ${countdown > 0 ? '(' + countdown + 's)' : ''}
                  </button>
                `;
              } else {
                withdrawalActions = html`
                  <button class="btn btn-info btn-sm" @click=${() => this.clientRouterRequestWithdrawal(wallet.address)}>
                    Withdraw
                  </button>
                `;
              }
              return html`
                <tr>
                  <td>${wallet.address}</td>
                  <td>${wallet.chain_balance}</td>
                  <td>${wallet.router_avail_balance}</td>
                  <td>${wallet.router_unlocked_balance}</td>
                  <td>${wallet.available_balance}</td>
                  <td>
                    <button class="btn btn-info btn-sm" @click=${() => this.clientRouterAddBalance(wallet.address)}>
                      Deposit
                    </button>
                    ${withdrawalActions}
                  </td>
                </tr>
              `;
            })}
          </tbody>
        </table>

        <h2>Client Messages</h2>
        <p>Client messages sent to the router.</p>
        <table class="table table-dark">
          <thead>
            <tr>
              <th>Started At</th>
              <th>Action</th>
              <th>Wallet</th>
              <th>Signed CID</th>
              <th>Status</th>
              <th>Completed At</th>
            </tr>
          </thead>
          <tbody>
            ${this.messages.map(msg => html`
              <tr>
                <td>${msg.started_at ? formatDate(msg.started_at) : ''}</td>
                <td>${msg.action}</td>
                <td>${msg.address}</td>
                <td>${msg.signed_cid ? html`<abbr title="${msg.signed_cid}">${msg.signed_cid.slice(0, 3) + '..' + msg.signed_cid.slice(-5)}</abbr>` : ''}</td>
                <td>${msg.success == null ? '⏳ Pending' : (msg.success ? '✅ Success' : '❌ Failed')}</td>
                <td>${msg.completed_at ? formatDate(msg.completed_at) : ''}</td>
              </tr>
            `)}
          </tbody>
        </table>
      </div>
    `;
  }
}

customElements.define('proof-share-client', ProofShareClient);
