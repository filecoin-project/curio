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

    // All proofshare_client_wallets rows
    this.wallets = [];
    
    // All proofshare_client_messages rows
    this.messages = [];

    // Initial load of settings, wallets, and client messages
    this.loadAllSettings();

    // Set up an auto-refresh to call loadAllSettings every 30 seconds.
    // This avoids scheduling additional timers when loadAllSettings is invoked by other functions.
    this.refreshIntervalId = setInterval(() => this.loadAllSettings(), 30000);
  }

  createRenderRoot() {
    // Use light DOM so Bootstrap + main.css apply
    return this;
  }

  /**
   * Fetch all rows from PSClientGet (which returns a list of settings)
   * and also pull the client messages.
   */
  async loadAllSettings() {
    try {
      const list = await RPCCall('PSClientGet', []);
      // If server returns null or not an array, default to []
      this.settingsList = Array.isArray(list) ? list : [];

      this.wallets = await RPCCall('PSClientWallets', []);
      
      // Pull client messages from the server
      this.messages = await RPCCall('PSClientListMessages', []);
    } catch (err) {
      console.error('Error loading proofshare client data:', err);
      this.settingsList = [];
      this.wallets = [];
      this.messages = [];
    }
    // Re-render
    this.requestUpdate();
  }


  /**
   * Toggle whether to show requests for a particular spId.
   * If turning on, fetch them first if we haven’t yet.
   */
  async toggleRequests(spId, address) {
    const wasShown = !!this.showRequests[spId];
    // Flip boolean
    this.showRequests[spId] = !wasShown;

    // If we *just* turned it on, and we don’t have requests loaded, fetch them.
    if (!wasShown && !this.spRequests[spId]) {
      try {
        const reqs = await RPCCall('PSClientRequests', [spId]);
        this.spRequests[spId] = Array.isArray(reqs) ? reqs : [];
      } catch (err) {
        console.error(`Error loading requests for ${address}:`, err);
        this.spRequests[spId] = [];
      }
    }

    // Re-render after toggling or fetching
    this.requestUpdate();
  }

  /**
   * Update a single field (key) in the row object (settings).
   * We store changes in this.settingsList directly so user can press Save later.
   */
  onChange(row, field, value) {
    row[field] = value;
    this.requestUpdate();
  }

  /**
   * Saves the given row by calling PSClientSet.
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
      }]);
      console.log(`Saved row for sp_id=${row.sp_id}`);
    } catch (err) {
      console.error(`Error saving settings for sp_id=${row.sp_id}:`, err);
    }
  }

  /**
   * Add a new sp_id row. 
   * Prompts the user for sp_id, then sets some defaults & calls PSClientSet.
   */
  async addSP() {
    const address = prompt('Enter new SP address:');
    if (!address) return; // user cancelled

    // Use some defaults for new row
    const newRow = {
      address,
      enabled: false,
      wallet: null,
      minimum_pending_seconds: 0,
      do_porep: false,
      do_snap: false,
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
    if (!address) return; // user cancelled
    await RPCCall('PSClientAddWallet', [address]);
    await this.loadAllSettings();
  }

  async promptForAmount(action, address) {
    const rawAmount = prompt(`Enter balance in FIL to ${action} (e.g., "1.23"):`);
    if (!rawAmount) return null; // user cancelled input

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


  /**
   * Remove a row (sp_id != 0).
   * Calls a new server method: PSClientRemove(spId).
   * Calls a new server method: PSClientRemove(spId).
   */
  async removeRow(spId, address) {
    if (!confirm(`Are you sure you want to remove ${address}?`)) {
      return;
    }
    try {
      await RPCCall('PSClientRemove', [spId]);
      console.log(`Removed ${address}`);
      await this.loadAllSettings();
    } catch (err) {
      console.error(`Error removing ${address}:`, err);
      alert(`Failed to remove ${address}. See console for details.`);
    }
  }

  /**
   * Render the sub-table of requests for a given sp_id, if loaded.
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
                  <button class="btn btn-success btn-sm" @click=${() => this.saveRow(row)}>
                    Save
                  </button>
                  <button class="btn btn-info btn-sm" @click=${() => this.toggleRequests(row.sp_id, row.address)}>
                    ${this.showRequests[row.sp_id] ? 'Hide' : 'View'} Requests
                  </button>
                  <!-- Remove is not shown for sp_id=0 -->
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

              <!-- If showRequests[sp_id], render requests table in a sub-row -->
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
              <th>Unsettled FIL</th>
              <th>Available FIL</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            ${this.wallets.map(wallet => html`
              <tr>
                <td>${wallet.address}</td>
                <td>${wallet.chain_balance}</td>
                <td>${wallet.router_avail_balance}</td>
                <td>${wallet.router_unsettled_balance}</td>
                <td>${wallet.available_balance}</td>
                <td>
                  <button class="btn btn-info btn-sm" @click=${() => this.clientRouterAddBalance(wallet.address)}>
                    Deposit
                  </button>
                  <button class="btn btn-info btn-sm" @click=${() => this.clientRouterRequestWithdrawal(wallet.address)}>
                    Withdraw
                  </button>
                </td>
              </tr>
            `)}
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

// Register the custom element
customElements.define('proof-share-client', ProofShareClient);
