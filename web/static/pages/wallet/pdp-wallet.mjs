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
        roleStatus: { type: Object },
        roleStatusLoading: { type: Boolean },
        payeeEstimate: { type: Object },
        estimating: { type: Boolean },
        retargeting: { type: Boolean },
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
        this.roleStatus = null;
        this.roleStatusLoading = true;
        this.payeeEstimate = null;
        this.estimating = false;
        this.retargeting = false;
        this.loadKeys();
        this.loadKeyStatus();
        this.loadRoleStatus();
    }

    async loadRoleStatus() {
        this.roleStatusLoading = true;
        try {
            this.roleStatus = await RPCCall('PDPRoleStatus', []);
        } catch (error) {
            console.error('Failed to load PDP role status:', error);
            this.roleStatus = null;
        } finally {
            this.roleStatusLoading = false;
        }
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
            await this.loadRoleStatus();
        } catch (error) {
            console.error('Failed to remove key:', error);
            alert('Failed to remove key: ' + (error.message || error));
        }
    }

    async estimateRetargetGas() {
        this.estimating = true;
        try {
            this.payeeEstimate = await RPCCall('EstimatePayeeRetargetGas', []);
        } catch (error) {
            console.error('Failed to estimate retarget gas:', error);
            alert('Failed to estimate gas: ' + (error.message || error));
        } finally {
            this.estimating = false;
        }
    }

    async queueRetargets() {
        if (!this.payeeEstimate) {
            alert('Estimate gas first so you can review the cost warning.');
            return;
        }
        const role = this.roleStatus;
        if (role?.localWorker && !role.localWorkerMatches) {
            alert(
                'Local Curio key does not match the on-chain worker.\n' +
                'Import the current (new) worker key first so retarget gas is paid by that wallet, not the old one.'
            );
            return;
        }
        const warning = [
            'Second step only: submit retargetDataSetPayee from the CURRENT on-chain worker (Curio pdp key).',
            'Do this after changePayee (and after worker rotation if the old worker should not spend gas).',
            `Live data sets: ${this.payeeEstimate.liveDataSets}`,
            `Estimated gas: ${this.payeeEstimate.totalGas} (~${this.payeeEstimate.estimatedCostFil} FIL at current base fee)`,
            this.payeeEstimate.note,
        ].join('\n');
        if (!confirm(warning)) {
            return;
        }
        this.retargeting = true;
        try {
            const hashes = await RPCCall('QueueDataSetPayeeRetargets', []);
            alert(`Submitted ${hashes.length} retarget transaction(s).`);
            await this.loadRoleStatus();
        } catch (error) {
            console.error('Failed to queue retargets:', error);
            alert('Failed to queue retargets: ' + (error.message || error));
        } finally {
            this.retargeting = false;
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

            <hr class="my-4">
            <h3>Payee and Worker</h3>
            <p class="text-muted">
                Payee and worker rotation is done by the ServiceProviderRegistry owner (FilOzone) after human vetting.
                Curio holds only the worker key for proving and rail retargets — never ask a possibly compromised wallet to authorize role changes.
            </p>
            ${this.roleStatusLoading ? html`
                <div class="alert alert-secondary">Loading on-chain roles…</div>
            ` : !this.roleStatus?.configured ? html`
                <p class="text-muted">Register the provider before updating payee or worker.</p>
            ` : html`
                ${this.roleStatus.localWorker && !this.roleStatus.localWorkerMatches ? html`
                    <div class="alert alert-danger">
                        <strong>Wrong worker wallet.</strong> ${this.roleStatus.workerMismatchNote}
                        Local: <code>${this.roleStatus.localWorker}</code>
                        On-chain: <code>${this.roleStatus.worker}</code>
                    </div>
                ` : ''}
                <dl class="row">
                    <dt class="col-sm-3">Provider ID</dt>
                    <dd class="col-sm-9">${this.roleStatus.providerId}</dd>
                    <dt class="col-sm-3">On-chain worker</dt>
                    <dd class="col-sm-9"><code>${this.roleStatus.worker}</code></dd>
                    <dt class="col-sm-3">Local Curio key</dt>
                    <dd class="col-sm-9"><code>${this.roleStatus.localWorker || 'none'}</code></dd>
                    <dt class="col-sm-3">Payee</dt>
                    <dd class="col-sm-9"><code>${this.roleStatus.payee}</code>
                        ${this.roleStatus.sameAddress ? html` <span class="badge text-bg-secondary">same as worker</span>` : ''}
                    </dd>
                    <dt class="col-sm-3">Registry</dt>
                    <dd class="col-sm-9"><code>${this.roleStatus.registryAddress}</code></dd>
                    <dt class="col-sm-3">Live data sets</dt>
                    <dd class="col-sm-9">${this.roleStatus.liveDataSets}</dd>
                </dl>

                <h4 class="h5 mt-3">Change payee or worker</h4>
                <div class="alert alert-info">
                    Request FilOzone to call
                    <code>changePayee(providerId, newPayee)</code>
                    and/or
                    <code>changeServiceProvider(providerId, newWorker)</code>
                    on registry <code>${this.roleStatus.registryAddress}</code>
                    (providerId <code>${this.roleStatus.providerId}</code>).
                    Create or import the new worker key on this node first when rotating the worker.
                </div>

                <h4 class="h5 mt-3">Retarget payment rails (second step)</h4>
                <p class="text-muted">
                    After FilOzone updates the payee on-chain, call this from the
                    <em>current</em> on-chain worker so gas is paid by that wallet — import the new
                    worker key first if the old worker must not spend gas.
                </p>
                <button class="btn btn-outline-light me-2 mb-2" ?disabled=${this.estimating || this.retargeting}
                        @click=${this.estimateRetargetGas}>
                    ${this.estimating ? 'Estimating…' : 'Estimate retarget gas'}
                </button>
                <button class="btn btn-warning mb-2" ?disabled=${this.retargeting || !this.payeeEstimate}
                        @click=${this.queueRetargets}>
                    ${this.retargeting ? 'Retargeting…' : 'Retarget rails for all data sets'}
                </button>
                ${this.payeeEstimate ? html`
                    <div class="alert alert-warning mt-2">
                        <strong>Gas warning:</strong> ${this.payeeEstimate.liveDataSets} data set(s),
                        ~${this.payeeEstimate.perRailGas} gas each.
                        Total ~${this.payeeEstimate.totalGas} gas
                        (~${this.payeeEstimate.estimatedCostFil} FIL at base fee ${this.payeeEstimate.baseFeeWei} wei).
                        ${this.payeeEstimate.note}
                    </div>
                ` : ''}
            `}
        `;
    }
});
