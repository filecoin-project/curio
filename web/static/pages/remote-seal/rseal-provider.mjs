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
    ourURL: { type: String },
  };

  constructor() {
    super();
    this.partners = [];
    this.newName = '';
    this.newURL = '';
    this.newAllowance = 10;
    this.connectStringPartnerID = null;
    this.connectString = '';
    this.ourURL = '';
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
    try {
      this.ourURL = await RPCCall('RSealGetPartnerURL', []) || '';
    } catch (err) {
      console.error('Failed to load partner URL:', err);
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
      <style>
        .rseal-input {
          all: unset;
          box-sizing: border-box;
          width: 100%;
          padding: 0.4rem 1rem;
          border: 1px solid #A1A1A1;
          background-color: rgba(255, 255, 255, 0.08);
          font-size: 1rem;
          font-family: 'JetBrains Mono', monospace;
          color: var(--color-text-primary);
        }
        .rseal-input:hover, .rseal-input:focus {
          box-shadow: 0 0 0 1px #FFF inset;
        }
        .rseal-input::placeholder {
          color: var(--color-form-default);
        }
        .rseal-btn {
          padding: 0.4rem 1rem;
          border: none;
          border-radius: 0;
          background-color: var(--color-form-default);
          color: var(--color-text-primary);
          font-family: 'JetBrains Mono', monospace;
          font-size: 1rem;
          cursor: pointer;
          white-space: nowrap;
        }
        .rseal-btn:hover, .rseal-btn:focus {
          background-color: var(--color-form-default-pressed);
          color: var(--color-text-secondary);
        }
        .rseal-field {
          display: flex;
          flex-direction: column;
          gap: 0.25rem;
        }
        .rseal-field label {
          font-size: 0.75rem;
          color: var(--color-text-secondary);
          text-transform: uppercase;
          letter-spacing: 0.5px;
        }
        .rseal-add-row {
          display: grid;
          grid-template-columns: 1fr 1.5fr 0.8fr max-content;
          grid-column-gap: 0.75rem;
          align-items: end;
          margin-bottom: 1.5rem;
        }
        .rseal-connect-row {
          display: grid;
          grid-template-columns: 1fr max-content;
          grid-column-gap: 0.75rem;
          margin-bottom: 0.5rem;
        }
        .rseal-info {
          font-size: 0.85rem;
          color: var(--color-text-secondary);
          margin-bottom: 1rem;
          padding: 0.5rem 1rem;
          background: rgba(255, 255, 255, 0.04);
          border-left: 3px solid var(--color-primary-main);
        }
        .rseal-info code {
          color: var(--color-primary-light);
          font-family: 'JetBrains Mono', monospace;
        }
      </style>

      <div>
        <h3>Partner Management</h3>
        <p>Manage partners that are allowed to delegate sealing to this node.</p>

        ${this.ourURL ? html`
          <div class="rseal-info">Our Partner URL: <code>${this.ourURL}</code></div>
        ` : html`
          <div class="rseal-info" style="border-left-color: var(--color-warning-main)">
            HTTP.DomainName not configured. Connect strings will not work until it is set.
          </div>
        `}

        <h4>Add Partner</h4>
        <div class="rseal-add-row">
          <div class="rseal-field">
            <label>Partner Name</label>
            <input type="text" class="rseal-input" placeholder="e.g. acme-storage" .value=${this.newName} @input=${e => this.newName = e.target.value} />
          </div>
          <div class="rseal-field">
            <label>Partner URL</label>
            <input type="text" class="rseal-input" placeholder="https://their-curio.example.com" .value=${this.newURL} @input=${e => this.newURL = e.target.value} />
          </div>
          <div class="rseal-field">
            <label>Allowance</label>
            <input type="number" class="rseal-input" placeholder="10" .value=${this.newAllowance} @input=${e => this.newAllowance = parseInt(e.target.value)} />
          </div>
          <div class="rseal-field">
            <label>&nbsp;</label>
            <button class="rseal-btn" @click=${this.addPartner}>Add Partner</button>
          </div>
        </div>

        ${this.connectString ? html`
          <div class="rseal-info">
            <strong>Connect String (Partner ID ${this.connectStringPartnerID}):</strong>
            <div class="rseal-connect-row" style="margin-top: 0.5rem">
              <input type="text" class="rseal-input" readonly .value=${this.connectString} />
              <button class="rseal-btn" @click=${this.copyConnectString}>Copy</button>
            </div>
            <small style="color: #aaa">Share this with the client operator to configure their provider connection.</small>
          </div>
        ` : ''}

        ${this.partners.length > 0 ? html`
          <h4>Partners</h4>
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
        ` : html`<p style="color:#aaa">No partners configured yet.</p>`}
      </div>
    `;
  }
}

customElements.define('rseal-provider', RSealProviderElement);
