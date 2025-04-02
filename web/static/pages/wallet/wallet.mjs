import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class WalletNames extends LitElement {
  static properties = {
    wallets: { type: Array },
    searchQuery: { type: String },
    filteredWallets: { type: Array },
    newWallet: { type: String },
    newName: { type: String },
    editingWallet: { type: String },
    editingName: { type: String }
  };

  constructor() {
    super();
    this.wallets = [];
    this.searchQuery = '';
    this.filteredWallets = [];
    this.newWallet = '';
    this.newName = '';
    this.editingWallet = null;
    this.editingName = '';
    this.loadWallets();
  }

  connectedCallback() {
    super.connectedCallback();
    // Get the search term from the URL parameter (if available)
    const urlParams = new URLSearchParams(window.location.search);
    const wallet = urlParams.get('id');
    if (wallet) {
      this.searchQuery = wallet.toLowerCase();
      this.handleSearch();
    }
  }

  async loadWallets() {
    try {
      const result = await RPCCall('WalletNames', []);
      if (result && typeof result === 'object') {
        this.wallets = Object.entries(result).map(([wallet, name]) => ({ wallet, name }));
        this.handleSearch();
      } else {
        this.wallets = [];
        this.filteredWallets = [];
      }
    } catch (err) {
      console.error('Failed to fetch wallet names:', err);
      alert('Failed to fetch wallet names: ' + err.message);
      this.wallets = [];
      this.filteredWallets = [];
    }
  }

  handleSearch(e) {
    if (e) this.searchQuery = e.target.value.toLowerCase();
    this.filteredWallets = this.wallets.filter(({ wallet, name }) =>
        wallet.toLowerCase().includes(this.searchQuery) ||
        name.toLowerCase().includes(this.searchQuery)
    );
  }

  async handleAddWallet(e) {
    e.preventDefault();
    try {
      await RPCCall('WalletAdd', [this.newWallet, this.newName]);
      this.newWallet = '';
      this.newName = '';
      await this.loadWallets();
    } catch (err) {
      console.error('Failed to add wallet:', err);
      alert('Failed to add wallet: ' + err.message);
    }
  }

  async handleDeleteWallet(wallet) {
    try {
      await RPCCall('WalletRemove', [wallet]);
      await this.loadWallets();
    } catch (err) {
      console.error('Failed to remove wallet:', err);
      alert('Failed to remove wallet: ' + err.message);
    }
  }

  startEdit(wallet, currentName) {
    this.editingWallet = wallet;
    this.editingName = currentName;
  }

  cancelEdit() {
    this.editingWallet = null;
    this.editingName = '';
  }

  async saveEdit() {
    try {
      await RPCCall('WalletNameChange', [this.editingWallet, this.editingName]);
      this.cancelEdit();
      await this.loadWallets();
    } catch (err) {
      console.error('Failed to update wallet name:', err);
      alert('Failed to update wallet name: ' + err.message);
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
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'" />

      <div class="container py-3">
        <h2>Wallet Names</h2>

        <form class="row g-2 align-items-center mb-4" @submit="${this.handleAddWallet}">
          <div class="col-auto">
            <input
              type="text"
              placeholder="Wallet address"
              .value="${this.newWallet.trim()}"
              @input="${(e) => (this.newWallet = e.target.value)}"
              required
            />
          </div>
          <div class="col-auto">
            <input
              type="text"
              placeholder="Friendly name"
              .value="${this.newName.trim()}"
              @input="${(e) => (this.newName = e.target.value)}"
              required
            />
          </div>
          <div class="col-auto">
            <button class="btn btn-success" type="submit">Add Wallet</button>
          </div>
        </form>

        <div class="search-container mb-3">
          <input
              type="text"
              class="w-100"
              placeholder="Search wallet or name..."
              @input="${this.handleSearch}"
          />
        </div>

        ${this.filteredWallets.length === 0
        ? html`<p class="text-muted">No wallet names found.</p>`
        : html`
              <table class="table table-dark table-striped">
                <thead>
                  <tr>
                    <th>Wallet</th>
                    <th>Name</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  ${this.filteredWallets.map(
            ({ wallet, name }) => html`
                      <tr>
                        <td>${wallet}</td>
                        <td>
                          ${this.editingWallet === wallet
                ? html`<input
                                class="form-control form-control-sm"
                                type="text"
                                .value="${this.editingName.trim()}"
                                @input="${(e) => (this.editingName = e.target.value)}"
                              />`
                : name}
                        </td>
                        <td>
                          ${this.editingWallet === wallet
                ? html`<div class="d-flex gap-2">
                              <button class="btn btn-sm btn-success me-1" @click="${this.saveEdit}">Save</button>
                                <button class="btn btn-sm btn-secondary" @click="${this.cancelEdit}">Cancel</button>
                              </div>`
                : html`<div class="d-flex gap-2">

                              <button class="btn btn-sm btn-secondary" @click="${() => this.startEdit(wallet, name)}">Edit</button>
                              <button class="btn btn-danger btn-sm" @click="${() => this.handleDeleteWallet(wallet)}">Delete</button>
                              <div`}
                        </td>
                      </tr>
                    `
        )}
                </tbody>
              </table>
            `}
      </div>
    `;
  }

  static styles = css`
    :host {
      display: block;
    }
    .me-1 {
      margin-right: 0.5rem;
    }
  `;
}

customElements.define('wallet-names', WalletNames);
