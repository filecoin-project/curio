import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class RSealProviderElement extends LitElement {
  static properties = {
    partners: { type: Array },
    newName: { type: String },
    newURL: { type: String },
    newAllowance: { type: Number },
    connectStringPartnerID: { type: Number },
    connectString: { type: String },
  };

  constructor() {
    super();
    this.partners = [];
    this.newName = '';
    this.newURL = '';
    this.newAllowance = 10;
    this.connectStringPartnerID = null;
    this.connectString = '';
    this.loadData();
  }

  createRenderRoot() {
    return this;
  }

  async loadData() {
    try {
      this.partners = await RPCCall('RSealListPartners', []);
    } catch (err) {
      console.error('Failed to load partners:', err);
      this.partners = [];
    }
    this.requestUpdate();
  }

  async addPartner() {
    if (!this.newName || !this.newURL) {
      alert('Name and URL are required');
      return;
    }
    try {
      await RPCCall('RSealAddPartner', [this.newName, this.newURL, this.newAllowance]);
      this.newName = '';
      this.newURL = '';
      this.newAllowance = 10;
      await this.loadData();
    } catch (err) {
      alert(`Failed to add partner: ${err.message || err}`);
    }
  }

  async removePartner(id) {
    if (!confirm(`Remove partner ${id}?`)) return;
    try {
      await RPCCall('RSealRemovePartner', [id]);
      await this.loadData();
    } catch (err) {
      alert(`Failed to remove partner: ${err.message || err}`);
    }
  }

  async updateAllowance(id) {
    const total = prompt('New total allowance:');
    if (total === null) return;
    const remaining = prompt('New remaining allowance:');
    if (remaining === null) return;
    try {
      await RPCCall('RSealUpdatePartnerAllowance', [id, parseInt(total), parseInt(remaining)]);
      await this.loadData();
    } catch (err) {
      alert(`Failed to update allowance: ${err.message || err}`);
    }
  }

  async getConnectString(id) {
    try {
      const cs = await RPCCall('RSealGetConnectString', [id]);
      this.connectStringPartnerID = id;
      this.connectString = cs;
      this.requestUpdate();
    } catch (err) {
      alert(`Failed to get connect string: ${err.message || err}`);
    }
  }

  copyConnectString() {
    navigator.clipboard.writeText(this.connectString).then(() => {
      alert('Connect string copied to clipboard');
    });
  }

  render() {
    return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet"
        integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous" />
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

      <div class="container">
        <h2>Provider - Partner Management</h2>
        <p>Manage partners that are allowed to delegate sealing to this node.</p>

        ${this.partners.length > 0 ? html`
          <table class="table table-dark table-striped table-hover">
            <thead>
              <tr>
                <th>ID</th>
                <th>Name</th>
                <th>URL</th>
                <th>Remaining</th>
                <th>Total</th>
                <th>Created</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              ${this.partners.map(p => html`
                <tr>
                  <td>${p.id}</td>
                  <td>${p.partner_name}</td>
                  <td class="text-break" style="max-width:200px">${p.partner_url}</td>
                  <td>${p.allowance_remaining}</td>
                  <td>${p.allowance_total}</td>
                  <td>${new Date(p.created_at).toLocaleDateString()}</td>
                  <td>
                    <button class="btn btn-sm btn-info me-1" @click=${() => this.getConnectString(p.id)} title="Get connect string">Connect</button>
                    <button class="btn btn-sm btn-warning me-1" @click=${() => this.updateAllowance(p.id)} title="Update allowance">Allowance</button>
                    <button class="btn btn-sm btn-danger" @click=${() => this.removePartner(p.id)} title="Remove partner">Remove</button>
                  </td>
                </tr>
              `)}
            </tbody>
          </table>
        ` : html`<p>No partners configured.</p>`}

        ${this.connectString ? html`
          <div class="alert alert-info">
            <strong>Connect String (Partner ID ${this.connectStringPartnerID}):</strong>
            <div class="input-group mt-2">
              <input type="text" class="form-control" readonly .value=${this.connectString} />
              <button class="btn btn-outline-secondary" @click=${this.copyConnectString}>Copy</button>
            </div>
            <small class="text-muted">Share this with the client operator to configure their provider connection.</small>
          </div>
        ` : ''}

        <h4>Add Partner</h4>
        <div class="row g-2 mb-3">
          <div class="col-md-3">
            <input type="text" class="form-control" placeholder="Partner name" .value=${this.newName} @input=${e => this.newName = e.target.value} />
          </div>
          <div class="col-md-4">
            <input type="text" class="form-control" placeholder="Partner URL (their cuhttp base)" .value=${this.newURL} @input=${e => this.newURL = e.target.value} />
          </div>
          <div class="col-md-2">
            <input type="number" class="form-control" placeholder="Allowance" .value=${this.newAllowance} @input=${e => this.newAllowance = parseInt(e.target.value)} />
          </div>
          <div class="col-md-2">
            <button class="btn btn-primary" @click=${this.addPartner}>Add Partner</button>
          </div>
        </div>
      </div>
    `;
  }
}

customElements.define('rseal-provider', RSealProviderElement);
