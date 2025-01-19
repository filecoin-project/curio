import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/task.mjs';

class ProofShareElement extends LitElement {
  static properties = {
    enabled: { type: Boolean },
    wallet: { type: String },
    queue: { type: Array },
  };

  constructor() {
    super();
    this.enabled = false;
    this.wallet = '';
    this.queue = [];
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

      // 2) Get the queue
      const queue = await RPCCall('PSListQueue', []);
      this.queue = queue;
    } catch (err) {
      console.error('Failed to load proofshare data:', err);
    }
    // Re-render
    this.requestUpdate();

    // Auto-refresh every N seconds (adjust as desired)
    setTimeout(() => this.loadData(), 5000);
  }

  // Update meta info on the server
  async setMeta() {
    try {
      // Call PSSetMeta(enabled, wallet)
      await RPCCall('PSSetMeta', [this.enabled, this.wallet]);
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
        <button class="btn btn-primary" @click=${this.setMeta}>Update Settings</button>

        <hr />

        <h2>Queue</h2>
        <table class="table table-dark table-striped table-sm">
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
                <td>${item.obtained_at}</td>
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
