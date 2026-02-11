import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class RSealClientElement extends LitElement {
  static properties = {
    providers: { type: Array },
    newSpID: { type: String },
    newConnectString: { type: String },
  };

  constructor() {
    super();
    this.providers = [];
    this.newSpID = '';
    this.newConnectString = '';
    this.loadData();
  }

  createRenderRoot() {
    return this;
  }

  async loadData() {
    try {
      this.providers = await RPCCall('RSealListProviders', []);
    } catch (err) {
      console.error('Failed to load providers:', err);
      this.providers = [];
    }
    this.requestUpdate();
  }

  async addProvider() {
    if (!this.newSpID || !this.newConnectString) {
      alert('SP ID and connect string are required');
      return;
    }
    try {
      await RPCCall('RSealAddProvider', [parseInt(this.newSpID), this.newConnectString]);
      this.newSpID = '';
      this.newConnectString = '';
      await this.loadData();
    } catch (err) {
      alert(`Failed to add provider: ${err.message || err}`);
    }
  }

  async removeProvider(id) {
    if (!confirm(`Remove provider ${id}?`)) return;
    try {
      await RPCCall('RSealRemoveProvider', [id]);
      await this.loadData();
    } catch (err) {
      alert(`Failed to remove provider: ${err.message || err}`);
    }
  }

  async toggleProvider(id, currentEnabled) {
    try {
      await RPCCall('RSealToggleProvider', [id, !currentEnabled]);
      await this.loadData();
    } catch (err) {
      alert(`Failed to toggle provider: ${err.message || err}`);
    }
  }

  render() {
    return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet"
        integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous" />
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

      <div class="container">
        <h2>Client - Provider Connections</h2>
        <p>Configure remote seal providers that will handle SDR and tree computation for this node's sectors.</p>

        ${this.providers.length > 0 ? html`
          <table class="table table-dark table-striped table-hover">
            <thead>
              <tr>
                <th>ID</th>
                <th>SP ID</th>
                <th>Provider URL</th>
                <th>Name</th>
                <th>Enabled</th>
                <th>Created</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              ${this.providers.map(p => html`
                <tr>
                  <td>${p.id}</td>
                  <td>f0${p.sp_id}</td>
                  <td class="text-break" style="max-width:250px">${p.provider_url}</td>
                  <td>${p.provider_name || '-'}</td>
                  <td>
                    <span class="badge ${p.enabled ? 'bg-success' : 'bg-secondary'}">${p.enabled ? 'Yes' : 'No'}</span>
                  </td>
                  <td>${new Date(p.created_at).toLocaleDateString()}</td>
                  <td>
                    <button class="btn btn-sm ${p.enabled ? 'btn-warning' : 'btn-success'} me-1"
                      @click=${() => this.toggleProvider(p.id, p.enabled)}>
                      ${p.enabled ? 'Disable' : 'Enable'}
                    </button>
                    <button class="btn btn-sm btn-danger" @click=${() => this.removeProvider(p.id)}>Remove</button>
                  </td>
                </tr>
              `)}
            </tbody>
          </table>
        ` : html`<p>No providers configured.</p>`}

        <h4>Add Provider</h4>
        <div class="row g-2 mb-3">
          <div class="col-md-2">
            <input type="text" class="form-control" placeholder="SP ID (e.g. 1000)" .value=${this.newSpID} @input=${e => this.newSpID = e.target.value} />
          </div>
          <div class="col-md-6">
            <input type="text" class="form-control" placeholder="Connect string from provider" .value=${this.newConnectString} @input=${e => this.newConnectString = e.target.value} />
          </div>
          <div class="col-md-2">
            <button class="btn btn-primary" @click=${this.addProvider}>Add Provider</button>
          </div>
        </div>
      </div>
    `;
  }
}

customElements.define('rseal-client', RSealClientElement);
