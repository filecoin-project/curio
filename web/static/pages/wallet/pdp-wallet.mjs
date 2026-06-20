import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { isSkiffUI } from '/lib/ui-variant.mjs';

customElements.define('pdp-wallet', class PDPWalletElement extends LitElement {
    static properties = {
        keys: { type: Array },
        keyStatus: { type: Object },
        isSkiff: { type: Boolean },
        showAddKeyForm: { type: Boolean },
        showCreatedKeyModal: { type: Boolean },
        createdKey: { type: Object },
        creating: { type: Boolean },
    }

    constructor() {
        super();
        this.keys = [];
        this.keyStatus = { configured: false };
        this.isSkiff = false;
        this.showAddKeyForm = false;
        this.showCreatedKeyModal = false;
        this.createdKey = null;
        this.creating = false;
        this.loadKeys();
        this.loadKeyStatus();
        isSkiffUI().then((v) => { this.isSkiff = v; });
    }

    async loadKeys() {
        try {
            this.keys = await RPCCall('ListPDPKeys', []);
        } catch (error) {
            console.error('Failed to load PDP keys:', error);
        }
    }

    async loadKeyStatus() {
        try {
            this.keyStatus = await RPCCall('PDPKeyStatus', []);
        } catch (error) {
            console.error('Failed to load PDP key status:', error);
            this.keyStatus = { configured: false };
        }
    }

    toggleAddKeyForm() {
        this.showAddKeyForm = !this.showAddKeyForm;
    }

    closeCreatedKeyModal() {
        this.showCreatedKeyModal = false;
        this.createdKey = null;
    }

    async createKey(method) {
        if (this.keys.length > 0 || this.keyStatus?.configured) {
            alert('A PDP wallet is already configured. Remove it first to create a new one.');
            return;
        }

        this.creating = true;
        try {
            const created = await RPCCall(method, []);
            this.createdKey = created;
            this.showCreatedKeyModal = true;
            await this.loadKeys();
            await this.loadKeyStatus();
        } catch (error) {
            console.error(`Failed to create key (${method}):`, error);
            alert('Failed to create key: ' + (error.message || error));
        } finally {
            this.creating = false;
        }
    }

    async addKey(event) {
        event.preventDefault();

        const privateKeyInput = this.shadowRoot.getElementById('private-key');
        const privateKey = privateKeyInput.value.trim();

        if (!privateKey) {
            alert('Please provide a private key.');
            return;
        }

        try {
            const address = await RPCCall('ImportPDPKey', [privateKey]);
            privateKeyInput.value = '';
            await this.loadKeys();
            await this.loadKeyStatus();
            this.showAddKeyForm = false;
            alert(`Successfully assigned key for address: ${address}`);
        } catch (error) {
            console.error('Failed to assign key:', error);
            alert('Failed to assign key: ' + (error.message || error));
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

    render() {
        const walletMissing = !this.keyStatus?.configured && this.keys.length === 0;

        return html`
            <link
                href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css"
                rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <h2>PDP Wallet</h2>
            <p class="text-muted">Ethereum keys used for PDP registration and proving with FWSS.</p>

            ${walletMissing ? html`
                <div class="alert alert-warning">
                    PDP wallet not configured — create or assign a key before registering or proving.
                </div>
            ` : ''}

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
                <button class="btn btn-primary me-2 mb-2" ?disabled=${this.creating}
                        @click="${() => this.createKey('CreatePDPKey')}">
                    ${this.creating ? 'Creating…' : 'Create Key'}
                </button>
                ${this.isSkiff ? html`
                    <button class="btn btn-primary me-2 mb-2" ?disabled=${this.creating}
                            @click="${() => this.createKey('CreatePDPKeyLantern')}">
                        ${this.creating ? 'Creating…' : 'Create Delegated Key (Lantern)'}
                    </button>
                ` : ''}
            ` : ''}

            <button class="btn btn-secondary me-2 mb-2" @click="${this.toggleAddKeyForm}"
                    ?disabled=${!walletMissing && !this.showAddKeyForm}>
                ${this.showAddKeyForm ? 'Cancel' : 'Assign Existing Key'}
            </button>

            ${this.showAddKeyForm ? html`
                <form @submit="${this.addKey}" style="margin-top: 20px;">
                    <div class="mb-3">
                        <label for="private-key" class="form-label">Private Key (Hex)</label>
                        <textarea class="form-control" id="private-key" rows="3" required></textarea>
                        <div class="form-text">Paste the secp256k1 private key for an existing 0x address.</div>
                    </div>
                    <button type="submit" class="btn btn-success">Assign Key</button>
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
                                <p class="mt-3 mb-0">
                                    Fund the 0x address with FIL/tFIL before registering with FWSS.
                                </p>
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
