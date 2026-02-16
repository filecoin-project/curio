import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class RSealClientElement extends LitElement {
  static properties = {
    providers: { type: Array },
    actors: { type: Array },
    newSpAddr: { type: String },
    newConnectString: { type: String },
    ourURL: { type: String },
  };

  constructor() {
    super();
    this.providers = [];
    this.actors = [];
    this.newSpAddr = '';
    this.newConnectString = '';
    this.ourURL = '';
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
    try {
      this.actors = await RPCCall('ActorList', []) || [];
      if (this.actors.length > 0 && !this.newSpAddr) {
        this.newSpAddr = this.actors[0];
      }
    } catch (err) {
      console.error('Failed to load actor list:', err);
      this.actors = [];
    }
    try {
      this.ourURL = await RPCCall('RSealGetPartnerURL', []) || '';
    } catch (err) {
      console.error('Failed to load partner URL:', err);
    }
    this.requestUpdate();
  }

  async addProvider() {
    if (!this.newSpAddr || !this.newConnectString) {
      alert('SP Address and connect string are required');
      return;
    }
    // Strip f0 prefix to get numeric SP ID
    const spIDStr = this.newSpAddr.replace(/^[ftk]0*/, '');
    const spID = parseInt(spIDStr);
    if (isNaN(spID) || spID <= 0) {
      alert(`Invalid SP address: ${this.newSpAddr}`);
      return;
    }
    try {
      await RPCCall('RSealAddProvider', [spID, this.newConnectString]);
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
      <style>
        .rseal-input, .rseal-select {
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
        .rseal-select {
          appearance: auto;
          -webkit-appearance: auto;
        }
        .rseal-select option {
          background-color: #2A2A2E;
          color: var(--color-text-primary);
        }
        .rseal-input:hover, .rseal-input:focus,
        .rseal-select:hover, .rseal-select:focus {
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
          grid-template-columns: 0.6fr 1fr max-content;
          grid-column-gap: 0.75rem;
          align-items: end;
          margin-bottom: 1.5rem;
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
        <h3>Provider Connections</h3>
        <p>Configure remote seal providers that will handle SDR and tree computation for this node's sectors.</p>

        ${this.ourURL ? html`
          <div class="rseal-info">Our Partner URL: <code>${this.ourURL}</code> (share this with providers when they add you as a partner)</div>
        ` : html`
          <div class="rseal-info" style="border-left-color: var(--color-warning-main)">
            HTTP.DomainName not configured. Remote seal callbacks will not work until it is set.
          </div>
        `}

        <h4>Add Provider</h4>
        <div class="rseal-add-row">
          <div class="rseal-field">
            <label>Miner Address</label>
            ${this.actors.length > 0 ? html`
              <select class="rseal-select" .value=${this.newSpAddr} @change=${e => this.newSpAddr = e.target.value}>
                ${this.actors.map(a => html`<option value=${a} ?selected=${a === this.newSpAddr}>${a}</option>`)}
              </select>
            ` : html`
              <input type="text" class="rseal-input" placeholder="f01234" .value=${this.newSpAddr} @input=${e => this.newSpAddr = e.target.value} />
            `}
          </div>
          <div class="rseal-field">
            <label>Connect String</label>
            <input type="text" class="rseal-input" placeholder="Paste connect string from provider" .value=${this.newConnectString} @input=${e => this.newConnectString = e.target.value} />
          </div>
          <div class="rseal-field">
            <label>&nbsp;</label>
            <button class="rseal-btn" @click=${this.addProvider}>Add Provider</button>
          </div>
        </div>

        ${this.providers.length > 0 ? html`
          <h4>Providers</h4>
          <table class="table table-dark table-striped table-hover">
            <thead>
              <tr>
                <th>ID</th>
                <th>Miner</th>
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
        ` : html`<p style="color:#aaa">No providers configured yet.</p>`}
      </div>
    `;
  }
}

customElements.define('rseal-client', RSealClientElement);
