import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

const SUPPORT_URL = 'https://filecoin.io/slack';

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
        retargeting: { type: Boolean },
        newWorkerKey: { type: String },
        browserPayee: { type: String },
        submittingWorkerWallet: { type: Boolean },
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
        this.retargeting = false;
        this.newWorkerKey = '';
        this.browserPayee = '';
        this.submittingWorkerWallet = false;
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

    async connectPayeeBrowserWallet() {
        try {
            const account = await this.requireBrowserPayeeAccount();
            this.browserPayee = account;
            alert(`Connected payee browser wallet: ${account}`);
        } catch (error) {
            console.error('Failed to connect browser wallet:', error);
            alert(error.message || error);
        }
    }

    async requireBrowserPayeeAccount() {
        const role = this.roleStatus;
        if (!role?.configured) {
            throw new Error('Provider is not registered.');
        }
        if (!window.ethereum) {
            throw new Error('No browser wallet found. Use MetaMask or Glif (window.ethereum).');
        }
        const accounts = await window.ethereum.request({ method: 'eth_requestAccounts' });
        const from = accounts?.[0];
        if (!from) {
            throw new Error('No account selected in the browser wallet.');
        }
        if (from.toLowerCase() !== (role.payee || '').toLowerCase()) {
            throw new Error(`Browser wallet ${from} is not the owner wallet (payee) ${role.payee}. That key is not stored in Curio. Switch accounts in MetaMask/Glif.`);
        }
        await this.ensureBrowserChain(role.chainId);
        this.browserPayee = from;
        return from;
    }

    async ensureBrowserChain(chainIdDecimal) {
        const wantedHex = '0x' + BigInt(chainIdDecimal).toString(16);
        const current = await window.ethereum.request({ method: 'eth_chainId' });
        if ((current || '').toLowerCase() === wantedHex.toLowerCase()) {
            return;
        }
        try {
            await window.ethereum.request({
                method: 'wallet_switchEthereumChain',
                params: [{ chainId: wantedHex }],
            });
        } catch (error) {
            throw new Error(`Switch the browser wallet to chainId ${chainIdDecimal} (${wantedHex}). ${error.message || error}`);
        }
    }

    async waitForTxReceipt(txHash) {
        const deadline = Date.now() + 4 * 60 * 1000;
        while (Date.now() < deadline) {
            const receipt = await window.ethereum.request({
                method: 'eth_getTransactionReceipt',
                params: [txHash],
            });
            if (receipt) {
                const status = (receipt.status || '').toLowerCase();
                if (status === '0x0' || status === '0x') {
                    throw new Error(`Transaction ${txHash} reverted. The database was not updated.`);
                }
                return receipt;
            }
            await new Promise((resolve) => setTimeout(resolve, 4000));
        }
        throw new Error(`Timed out waiting for ${txHash}. The database was not updated.`);
    }

    async storeWorkerKey(keyMaterial) {
        let lastError;
        for (let attempt = 0; attempt < 8; attempt++) {
            try {
                return await RPCCall('ReplacePDPWorkerKey', [keyMaterial]);
            } catch (error) {
                lastError = error;
                const message = String(error?.message || error);
                if (!message.includes('on-chain worker')) {
                    throw error;
                }
                await new Promise((resolve) => setTimeout(resolve, 4000));
            }
        }
        throw lastError;
    }

    async changeWorkerViaBrowserWallet() {
        const keyMaterial = this.newWorkerKey.trim();
        if (!keyMaterial) {
            alert('Paste the new worker private key. Curio stores it after the owner wallet signature confirms.');
            return;
        }
        if (!confirm(
            'The owner wallet (payee) signs changeServiceProvider in MetaMask or Glif.\n' +
            'That key is not stored in Curio.\n\n' +
            'After the transaction confirms, Curio replaces the worker key in the database.'
        )) {
            return;
        }
        this.submittingWorkerWallet = true;
        try {
            const newWorker = await RPCCall('PDPKeyAddress', [keyMaterial]);
            let tx;
            try {
                tx = await RPCCall('BuildChangeWorkerTx', [newWorker]);
            } catch (error) {
                const message = String(error?.message || error);
                if (message.includes('unchanged')) {
                    const stored = await this.storeWorkerKey(keyMaterial);
                    this.newWorkerKey = '';
                    alert(`On-chain worker is already ${stored}. Database updated.`);
                    await this.loadKeys();
                    await this.loadKeyStatus();
                    await this.loadRoleStatus();
                    return;
                }
                throw error;
            }
            const from = await this.requireBrowserPayeeAccount();
            if (from.toLowerCase() !== (tx.fromMustBe || '').toLowerCase()) {
                throw new Error(`Connected account must be the owner wallet (payee) ${tx.fromMustBe}.`);
            }
            const txHash = await window.ethereum.request({
                method: 'eth_sendTransaction',
                params: [{ from, to: tx.to, data: tx.data, value: '0x0' }],
            });
            await this.waitForTxReceipt(txHash);
            const stored = await this.storeWorkerKey(keyMaterial);
            this.newWorkerKey = '';
            alert(`Worker is now ${stored}. Database updated. Transaction: ${txHash}`);
            await this.loadKeys();
            await this.loadKeyStatus();
            await this.loadRoleStatus();
        } catch (error) {
            console.error('Failed browser changeServiceProvider:', error);
            alert('Failed to change worker: ' + (error.message || error));
        } finally {
            this.submittingWorkerWallet = false;
        }
    }

    railsNeedMove() {
        return (this.roleStatus?.railsToMove || 0) > 0 && !!this.roleStatus?.localWorkerMatches;
    }

    async queueRetargets() {
        if (!this.railsNeedMove()) {
            return;
        }
        this.retargeting = true;
        try {
            const estimate = await RPCCall('EstimatePayeeRetargetGas', []);
            this.payeeEstimate = estimate;
            const warning = [
                'Move payment rails to the current payee.',
                'Gas is paid by the worker key stored in Curio.',
                `Data sets to move: ${estimate.liveDataSets}`,
                `Estimated gas: ${estimate.totalGas} (~${estimate.estimatedCostFil} FIL at the current base fee)`,
                estimate.note,
            ].join('\n');
            if (!confirm(warning)) {
                return;
            }
            const hashes = await RPCCall('RetargetAllDatasetPayees', []);
            alert(`Submitted ${hashes.length} retarget transaction(s).`);
            await this.loadRoleStatus();
        } catch (error) {
            console.error('Failed to queue retargets:', error);
            alert('Failed to move payment rails: ' + (error.message || error));
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
                            <th style="width: 90%;">Worker address</th>
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
            <h3>Owner wallet and worker</h3>
            <p class="text-muted">
                The owner wallet is the payee. It is not stored on this node. Its signature changes the worker,
                and Curio then stores that worker key. Changing the owner wallet itself is a support request.
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
                    <dt class="col-sm-3">Owner wallet (payee)</dt>
                    <dd class="col-sm-9"><code>${this.roleStatus.payee}</code>
                        ${this.roleStatus.sameAddress ? html` <span class="badge text-bg-secondary">same as worker</span>` : ''}
                    </dd>
                    <dt class="col-sm-3">Registry</dt>
                    <dd class="col-sm-9"><code>${this.roleStatus.registryAddress}</code></dd>
                    <dt class="col-sm-3">Live data sets</dt>
                    <dd class="col-sm-9">${this.roleStatus.liveDataSets}</dd>
                </dl>

                <h4 class="h5 mt-3">Change worker</h4>
                <p class="text-muted">
                    Connect the owner wallet <code>${this.roleStatus.payee}</code> in MetaMask or Glif.
                    Paste the new worker private key. Curio writes it to the database after that signature confirms.
                </p>
                <button class="btn btn-outline-light mb-3"
                        @click=${this.connectPayeeBrowserWallet}>
                    ${this.browserPayee ? `Connected owner wallet: ${this.browserPayee}` : 'Connect owner wallet (MetaMask / Glif)'}
                </button>
                <div class="mb-3">
                    <label class="form-label" for="new-worker-key">New worker private key</label>
                    <textarea class="form-control font-monospace" id="new-worker-key" rows="3"
                              .value=${this.newWorkerKey}
                              @input=${(e) => { this.newWorkerKey = e.target.value; }}></textarea>
                    <div class="form-text">Hex secp256k1 key, or the output of <code>lotus wallet export</code>.</div>
                    <button class="btn btn-primary mt-2" ?disabled=${this.submittingWorkerWallet}
                            @click=${this.changeWorkerViaBrowserWallet}>
                        ${this.submittingWorkerWallet ? 'Waiting for owner signature…' : 'Change worker'}
                    </button>
                </div>

                <h4 class="h5 mt-3">Change owner wallet</h4>
                <div class="alert alert-info">
                    This node cannot change the owner wallet. Contact support and include provider
                    <strong>${this.roleStatus.providerId}</strong>. Ask in <strong>#fil-sp-help</strong> on Filecoin Slack.
                    <div class="mt-2">
                        <a class="btn btn-outline-light btn-sm" href="${SUPPORT_URL}" target="_blank" rel="noopener noreferrer">
                            Contact support
                        </a>
                    </div>
                </div>

                <h4 class="h5 mt-3">Payment rails</h4>
                ${this.roleStatus.railsToMove > 0 ? html`
                    <div class="alert alert-warning">
                        ${this.roleStatus.railsNote}
                        ${this.roleStatus.localWorkerMatches ? html`
                            The button moves those rails to <code>${this.roleStatus.payee}</code>.
                        ` : html`
                            The button stays off until the worker key stored here matches the on-chain worker,
                            so gas is paid by that wallet.
                        `}
                    </div>
                ` : html`
                    <div class="alert ${this.roleStatus.railsNote?.startsWith('Could not check') ? 'alert-warning' : 'alert-success'}">
                        ${this.roleStatus.railsNote || 'Payment rails already pay the current payee.'}
                    </div>
                `}
                <button class="btn btn-warning mb-2" ?disabled=${this.retargeting || !this.railsNeedMove()}
                        @click=${this.queueRetargets}>
                    ${this.retargeting ? 'Moving rails…' : 'Move payment rails to current payee'}
                </button>
                ${this.payeeEstimate && this.roleStatus.railsToMove > 0 ? html`
                    <div class="alert alert-warning mt-2">
                        <strong>Gas:</strong> ${this.payeeEstimate.liveDataSets} data set(s),
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
