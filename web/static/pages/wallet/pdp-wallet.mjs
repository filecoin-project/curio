import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('pdp-wallet', class PDPWalletElement extends LitElement {
    static properties = {
        keys: { type: Array },
        keyStatus: { type: Object },
        keyStatusLoading: { type: Boolean },
        showImportForm: { type: Boolean },
        showCreatedKeyModal: { type: Boolean },
        createdKey: { type: Object },
        creating: { type: Boolean },
        importing: { type: Boolean },
    }

    constructor() {
        super();
        this.keys = [];
        this.keyStatus = undefined;
        this.keyStatusLoading = true;
        this.showImportForm = false;
        this.showCreatedKeyModal = false;
        this.createdKey = null;
        this.creating = false;
        this.importing = false;
        this.loadKeys();
        this.loadKeyStatus();
    }

    async loadKeys() {
        try {
            this.keys = await RPCCall('ListPDPKeys', []);
        } catch (error) {
            console.error('Failed to load PDP keys:', error);
        }
    }

    async loadKeyStatus() {
        this.keyStatusLoading = true;
        try {
            this.keyStatus = await RPCCall('PDPKeyStatus', []);
        } catch (error) {
            console.error('Failed to load PDP key status:', error);
            this.keyStatus = null;
        } finally {
            this.keyStatusLoading = false;
        }
    }

    toggleImportForm() {
        this.showImportForm = !this.showImportForm;
    }

    closeCreatedKeyModal() {
        this.showCreatedKeyModal = false;
        this.createdKey = null;
    }

    walletConfigured() {
        if (this.keyStatusLoading) return false;
        return this.keyStatus?.configured || this.keys.length > 0;
    }

    async createKey() {
        if (this.walletConfigured()) {
            alert('A PDP wallet is already configured. Remove it first to create a new one.');
            return;
        }

        this.creating = true;
        try {
            const created = await RPCCall('CreatePDPKey', []);
            this.createdKey = created;
            this.showCreatedKeyModal = true;
            await this.loadKeys();
            await this.loadKeyStatus();
        } catch (error) {
            console.error('Failed to create key:', error);
            alert('Failed to create key: ' + (error.message || error));
        } finally {
            this.creating = false;
        }
    }

    async importKey(event) {
        event.preventDefault();

        if (this.walletConfigured()) {
            alert('A PDP wallet is already configured. Remove it first to import a different key.');
            return;
        }

        const privateKeyInput = this.shadowRoot.getElementById('private-key');
        const privateKey = privateKeyInput.value.trim();

        if (!privateKey) {
            alert('Please provide a private key.');
            return;
        }

        this.importing = true;
        try {
            const address = await RPCCall('ImportPDPKey', [privateKey]);
            privateKeyInput.value = '';
            this.showImportForm = false;
            await this.loadKeys();
            await this.loadKeyStatus();

            if (this.keyStatus?.funded) {
                alert(`Wallet imported for address ${address}. Balance: ${this.keyStatus.balance}.`);
            } else if (!this.keyStatus?.balanceKnown) {
                alert(`Wallet imported for address ${address}. Balance could not be fetched from the chain yet.`);
            } else {
                alert(`Wallet imported for address ${address}, but it has no funds yet. Send FIL/tFIL to this address before registering with FWSS.`);
            }
        } catch (error) {
            console.error('Failed to import key:', error);
            alert('Failed to import key: ' + (error.message || error));
        } finally {
            this.importing = false;
        }
    }

    async removeKey(ownerAddress) {
        const confirmed = confirm(`Are you sure you want to remove the key for address "${ownerAddress}"?`);
        if (!confirmed) {
            return;
        }

        try {
            await RPCCall('RemovePDPKey', [ownerAddress]);
            await this.loadKeys();
            await this.loadKeyStatus();
        } catch (error) {
            console.error('Failed to remove key:', error);
            alert('Failed to remove key: ' + (error.message || error));
        }
    }

    renderFundingAlert() {
        if (this.keyStatusLoading) {
            return html`
                <div class="alert alert-secondary">
                    Loading wallet status…
                </div>
            `;
        }
        if (!this.keyStatus?.configured) {
            return '';
        }
        if (this.keyStatus.funded) {
            return html`
                <div class="alert alert-success">
                    Wallet funded — balance ${this.keyStatus.balance}${this.keyStatus.usdfcBalance ? `, ${this.keyStatus.usdfcBalance}` : ''}.
                </div>
            `;
        }
        if (!this.keyStatus.balanceKnown) {
            return html`
                <div class="alert alert-warning">
                    Wallet is configured, but FIL/USDFC balances could not be fetched from the chain right now.
                    Address: <code>${this.keyStatus.address}</code>
                </div>
            `;
        }
        return html`
            <div class="alert alert-warning">
                Wallet has no funds. Send FIL/tFIL to
                <code>${this.keyStatus.address}</code>
                before registering or proving with FWSS.
            </div>
        `;
    }

    render() {
        const walletMissing = !this.keyStatusLoading && !this.walletConfigured();

        return html`
            <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <h2>PDP Wallet</h2>
            <p class="text-muted">
                Ethereum keys used for PDP registration and proving with FWSS.
                Create and Import run on this node only.
            </p>

            ${walletMissing ? html`
                <div class="alert alert-warning">
                    PDP wallet not configured — create or import a key on this node before registering or proving.
                </div>
            ` : this.renderFundingAlert()}

            ${this.keys.length > 0 ? html`
                <table class="table table-dark table-striped w-100">
                    <thead>
                        <tr>
                            <th style="width: 90%;">Owner Address</th>
                            <th style="width: 10%;">Action</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${this.keys.map(key => html`
                            <tr>
                                <td>${key}</td>
                                <td>
                                    <button class="btn btn-danger btn-sm" @click="${() => this.removeKey(key)}">
                                        Remove
                                    </button>
                                </td>
                            </tr>
                        `)}
                    </tbody>
                </table>
            ` : html`
                <p class="text-muted">No PDP wallet configured.</p>
            `}

            ${walletMissing ? html`
                <button class="btn btn-primary me-2 mb-2" ?disabled=${this.creating || this.importing}
                        @click="${this.createKey}">
                    ${this.creating ? 'Creating…' : 'Create'}
                </button>
                <button class="btn btn-secondary me-2 mb-2" ?disabled=${this.creating || this.importing}
                        @click="${this.toggleImportForm}">
                    ${this.showImportForm ? 'Cancel Import' : 'Import'}
                </button>
            ` : ''}

            ${this.showImportForm && walletMissing ? html`
                <form @submit="${this.importKey}" style="margin-top: 20px;">
                    <div class="mb-3">
                        <label for="private-key" class="form-label">Private Key (Hex or lotus export)</label>
                        <textarea class="form-control" id="private-key" rows="3" required></textarea>
                        <div class="form-text">Paste a hex secp256k1 key, or the output of <code>lotus wallet export &lt;f4-address&gt;</code>.</div>
                    </div>
                    <button type="submit" class="btn btn-success" ?disabled=${this.importing}>
                        ${this.importing ? 'Importing…' : 'Import'}
                    </button>
                </form>
            ` : ''}

            ${this.showCreatedKeyModal && this.createdKey ? html`
                <div class="modal d-block" tabindex="-1" style="background: rgba(0,0,0,0.6);">
                    <div class="modal-dialog modal-lg">
                        <div class="modal-content bg-dark text-light">
                            <div class="modal-header">
                                <h5 class="modal-title">Save Your PDP Wallet Key</h5>
                                <button type="button" class="btn-close btn-close-white"
                                        @click="${this.closeCreatedKeyModal}"></button>
                            </div>
                            <div class="modal-body">
                                <div class="alert alert-danger">
                                    Copy and store this private key now. It will not be shown again.
                                </div>
                                <p><strong>Ethereum address (0x):</strong> ${this.createdKey.address}</p>
                                ${this.createdKey.filAddress ? html`
                                    <p><strong>Filecoin address:</strong> ${this.createdKey.filAddress}</p>
                                ` : ''}
                                <p><strong>Private key (hex):</strong></p>
                                <textarea class="form-control font-monospace" rows="3" readonly>${this.createdKey.privateKeyHex}</textarea>
                                ${this.keyStatus?.funded ? html`
                                    <div class="alert alert-success mt-3 mb-0">
                                        Wallet balance: ${this.keyStatus.balance}.
                                    </div>
                                ` : !this.keyStatus?.balanceKnown ? html`
                                    <div class="alert alert-warning mt-3 mb-0">
                                        Balance could not be fetched from the chain yet. Address:
                                        <code>${this.createdKey.address}</code>
                                    </div>
                                ` : html`
                                    <div class="alert alert-warning mt-3 mb-0">
                                        This wallet has no funds yet. Send FIL/tFIL to
                                        <code>${this.createdKey.address}</code>
                                        before registering with FWSS.
                                    </div>
                                `}
                            </div>
                            <div class="modal-footer">
                                <button type="button" class="btn btn-primary" @click="${this.closeCreatedKeyModal}">
                                    I have saved the key
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            ` : ''}
        `;
    }
});
