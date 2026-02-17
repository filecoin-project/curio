import {css, html, LitElement} from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('fs-registry-info', class FSRegistryInfo extends LitElement {
    static properties = {
        status: { type: Object },
        loading: { type: Boolean },
        error: { type: String },

        // modal toggles
        showRegisterModal: { type: Boolean },
        showUpdateModal: { type: Boolean },
        showUpdatePDPModal: { type: Boolean },
        showDeregisterModal: { type: Boolean },

        // service form state
        name: { type: String },
        description: { type: String },
        location: { type: String },

        // PDP update state
        pdpServiceURL: { type: String },
        pdpMinPieceSize: { type: Number },
        pdpMaxPieceSize: { type: Number },
        pdpIpniPiece: { type: Boolean },
        pdpIpniIpfs: { type: Boolean },
        pdpPrice: { type: Number },
        pdpMinProvingPeriod: { type: Number },
        pdpLocation: { type: String },

        // deregister confirmation input
        deregisterConfirmation: { type: String },

        capabilities: { type: Object },

    };

    static styles = css`
        .modal-backdrop {
          position: fixed; inset: 0; background: rgba(0,0,0,0.6);
          display: flex; align-items: center; justify-content: center; z-index: 1050;
        }
        .modal-card {
          background: #1f1f1f; color: #fff; border-radius: .5rem; width: 40rem; max-width: 95vw;
          box-shadow: 0 1rem 3rem rgba(0,0,0,.3);
        }
        .modal-header, .modal-footer { padding: 1rem 1.25rem; border-color: rgba(255,255,255,.1); }
        .modal-header { border-bottom: 1px solid; }
        .modal-footer { border-top: 1px solid; }
        .modal-body { padding: 1rem 1.25rem; }
        .close-btn { background: transparent; border: 0; color: #aaa; font-size: 1.5rem; line-height: 1; }
        .kv-table th { width: 28%; }
        .badge { font-size: .85rem; }
        .muted { color: #9aa0a6; }
         `;

    constructor() {
        super();
        this.status = null;
        this.loading = true;
        this.error = '';

        this.showRegisterModal = false;
        this.showUpdateModal = false;
        this.showUpdatePDPModal = false;

        this.name = '';
        this.description = '';
        this.location = '';

        // PDP update state
        this.pdpServiceURL = '';
        this.pdpMinPieceSize = 0;
        this.pdpMaxPieceSize = 0;
        this.pdpIpniPiece = true;
        this.pdpIpniIpfs = true;
        this.pdpPrice = 0;
        this.pdpMinProvingPeriod = 0;
        this.pdpLocation = '';

        this.capabilities = {};

        this.deregisterConfirmation = '';

        this.loadStatus();
    }

    async loadStatus() {
        this.loading = true;
        this.error = '';
        try {
            const s = await RPCCall('FSRegistryStatus', []);
            this.status = s ?? null;
            if (this.status && typeof this.status === 'object') {
                this.status.capabilities = this.status.capabilities || {};
            }
        } catch (e) {
            console.error('FSRegistryStatus error:', e);
            this.error = e?.message || String(e);
            this.status = null;
        } finally {
            this.loading = false;
        }
    }

    openRegister() {
        this.name = '';
        this.description = '';
        this.location = '';
        this.showRegisterModal = true;
    }
    closeRegister() { this.showRegisterModal = false; }

    async submitRegister(e) {
        e.preventDefault();
        const name = this.name.trim();
        const description = this.description.trim();
        const location = this.location.trim();

        if (!name || !description || !location) {
            alert('Please fill all fields (name, description, location).');
            return;
        }

        try {
            await RPCCall('FSRegister', [name, description, location]);
            this.showRegisterModal = false;
            await this.loadStatus();
            alert('Provider registered successfully. Please wait for the 5 epochs to see the updated status.');
        } catch (err) {
            console.error('FSRegister failed:', err);
            alert('Register failed: ' + (err?.message || err));
        }
    }

    openUpdate() {
        this.name = this.status?.name || '';
        this.description = this.status?.description || '';
        this.showUpdateModal = true;
    }
    closeUpdate() { this.showUpdateModal = false; }

    async submitUpdate(e) {
        e.preventDefault();
        const name = this.name.trim();
        const description = this.description.trim();
        if (!name || !description) {
            alert('Please fill name and description.');
            return;
        }
        try {
            await RPCCall('FSUpdateProvider', [name, description]);
            this.showUpdateModal = false;
            await this.loadStatus();
            alert('Provider updated successfully. Please wait for the 5 epochs to see the updated status.');
        } catch (err) {
            console.error('FSUpdateProvider failed:', err);
            alert('Update failed: ' + (err?.message || err));
        }
    }

    openDeregisterModal() {
        this.deregisterConfirmation = ''; // Reset confirmation input
        this.showDeregisterModal = true;
    }

    closeDeregisterModal() {
        this.showDeregisterModal = false;
    }

    async submitDeregister(e) {
        e.preventDefault();

        if (this.deregisterConfirmation.trim().toLowerCase() !== 'deregister') {
            alert('You must type "deregister" to confirm removal.');
            return;
        }

        try {
            await RPCCall('FSDeregister', []); // Call backend deregister method
            this.showDeregisterModal = false;
            await this.loadStatus(); // Reload status to reflect deregistration
            alert('Provider deregistered successfully. Please wait for the 5 epochs to see the updated status.');
        } catch (err) {
            console.error('FSDeregister failed:', err);
            alert('Deregistration failed: ' + (err?.message || err));
        }
    }

    addCapability() {
        const key = prompt('Enter capability key:');
        if (key) {
            if (this.capabilities[key]) {
                alert('This key already exists.');
                return;
            }
            const value = prompt('Enter capability value:');
            this.capabilities = {...this.capabilities, [key]: value};
        }
    }

    editCapability(key) {
        const value = prompt(`Edit value for key "${key}":`, this.capabilities[key]);
        this.capabilities = {...this.capabilities, [key]: value};
    }

    removeCapability(key) {
        if (confirm(`Are you sure you want to remove the capability "${key}"?`)) {
            const { [key]: _, ...updatedCapabilities } = this.capabilities;
            this.capabilities = updatedCapabilities;
        }
    }

    openUpdatePDP() {
        const pdp = this.status?.pdp_service || {};
        this.pdpServiceURL = pdp.service_url || '';
        this.pdpMinPieceSize = pdp.min_size || 0;
        this.pdpMaxPieceSize = pdp.max_size || 0;
        this.pdpIpniPiece = pdp.ipni_piece || false;
        this.pdpIpniIpfs = pdp.ipni_ipfs || false;
        this.pdpPrice = pdp.price || 0;
        this.pdpMinProvingPeriod = pdp.min_proving_period || 0;
        this.pdpLocation = pdp.location || '';
        this.capabilities = this.status?.capabilities || {};
        this.showUpdatePDPModal = true;
    }

    closeUpdatePDP() {
        this.showUpdatePDPModal = false;
    }

    isValidLocation(location) {
        // Regular expression for the format "C=US;ST=California;L=San Francisco"
        const regex = /^C=[A-Z]{2}(;ST=[\w\s]+)?(;L=[\w\s]+)?$/;
        return regex.test(location);
    }

    async submitUpdatePDP(e) {
        e.preventDefault();

        const pdpOffering = {
            service_url: this.pdpServiceURL.trim(),
            min_size: parseInt(this.pdpMinPieceSize, 10),
            max_size: parseInt(this.pdpMaxPieceSize, 10),
            ipni_piece: this.pdpIpniPiece,
            ipni_ipfs: this.pdpIpniIpfs,
            ipni_peer_id: this.status?.pdp_service?.ipni_peer_id || '',
            price: parseInt(this.pdpPrice, 10),
            min_proving_period: parseInt(this.pdpMinProvingPeriod, 10),
            location: this.pdpLocation.trim(),
        };

        // Validate location format
        if (!this.isValidLocation(pdpOffering.location)) {
            alert('Location must be in the format: "C=US;ST=California;L=San Francisco".');
            return;
        }

        // Validate fields
        if (!pdpOffering.service_url || !pdpOffering.location ||
            pdpOffering.min_size <= 0 || pdpOffering.max_size <= 0 || pdpOffering.price < 0 || pdpOffering.min_proving_period <= 0) {
            alert('Please provide all required fields with valid data.');
            return;
        }

        try {
            await RPCCall('FSUpdatePDP', [pdpOffering, this.capabilities]);
            this.showUpdatePDPModal = false;
            await this.loadStatus(); // Refresh status
            alert('PDP Offering updated successfully. Please wait for the 5 epochs to see the updated status.');
        } catch (err) {
            console.error('FSUpdatePDP failed:', err);
            alert('Update failed: ' + (err?.message || err));
        }
    }

    renderKV(label, value) {
        return html`
            <tr>
                <th class="text-light">${label}</th>
                <td class="text-light">${value ?? html`<span class="muted">—</span>`}</td>
            </tr>
        `;
    }

    renderCapabilities(cap) {
        const entries = Object.entries(cap || {});
        if (!entries.length) return html`<span class="muted">None</span>`;
        return html`${entries.map(([k, v]) => html`
            <span class="badge bg-secondary me-2 mb-2">${k}: ${v}</span>
        `)}`;
    }

    renderPDP(pdp, capabilities) {
        if (!pdp) return html`<span class="muted">None</span>`;
        return html`
            <table class="table table-dark table-striped table-bordered mb-0">
                <tbody>
                ${this.renderKV('Service URL', pdp.service_url)}
                ${this.renderKV('Min Piece Size', this.formatBytes(pdp.min_size))}
                ${this.renderKV('Max Piece Size', this.formatBytes(pdp.max_size))}
                ${this.renderKV('IPNI Piece', String(!!pdp.ipni_piece))}
                ${this.renderKV('IPNI IPFS', String(!!pdp.ipni_ipfs))}
                ${this.renderKV('IPNI Peer ID', pdp.ipni_peer_id)}
                ${this.renderKV('Price (per TiB per day)', this.unitsToUSDFCPerTiBPerDay(pdp.price))}
                ${this.renderKV('Price (per TiB per month)', (parseFloat(this.unitsToUSDFCPerTiBPerDay(pdp.price)) * 30).toFixed(4))}
                ${this.renderKV('Min Proving Period (epochs)', pdp.min_proving_period)}
                ${this.renderKV('Location', pdp.location)}
                ${this.renderKV('Capabilities', this.renderCapabilities(capabilities))}
                </tbody>
            </table>
        `;
    }

    formatBytes(bytes) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const sizes = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    get UNIT_PER_USDFC() {
        return 1e18;
    }

    unitsToUSDFCPerTiBPerDay(value) {
        const inUSDFC = value / this.UNIT_PER_USDFC;
        return inUSDFC.toFixed(4);
    }

    USDFCToUnitsPerTibPerDay(value) {
        const inUnits = value * this.UNIT_PER_USDFC;
        return Math.round(inUnits);
    }

    render() {
        return html`
            <link
                    href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css"
                    rel="stylesheet"
            />
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <p></p>

            <div class="container-fluid" style="min-width: 70em">
                <h2 class="mb-3">Filecoin Service Registry</h2>

                ${this.loading ? html`
                    <div class="d-flex align-items-center">
                        <div class="spinner-border me-2" role="status" aria-hidden="true"></div>
                        <span>Loading status…</span>
                    </div>
                ` : this.error ? html`
                    <div class="alert alert-danger">Failed to load status: ${this.error}</div>
                ` : html`${this.status === null ? html`
                    <div class="alert alert-warning">This provider is not registered.</div>
                    <button class="btn btn-primary" @click=${this.openRegister}>Register Provider</button>
                ` : html`
                    <div class="card bg-dark text-light mb-3">
                        <div class="card-header">Current Status</div>
                        <div class="card-body p-0">
                            <table class="table table-dark table-striped table-bordered mb-0 kv-table">
                                <tbody>
                                ${this.renderKV('Address', this.status.address)}
                                ${this.renderKV('ID', this.status.id)}
                                ${this.renderKV('Active', String(!!this.status.status))}
                                ${this.renderKV('Name', this.status.name)}
                                ${this.renderKV('Description', this.status.description)}
                                ${this.renderKV('Payee', this.status.payee)}
                                <tr>
                                    <th class="text-light">PDP Service</th>
                                    <td class="text-light">
                                        ${this.renderPDP(this.status.pdp_service, this.status.capabilities)}
                                    </td>
                                </tr>
                                </tbody>
                            </table>
                        </div>
                    </div>

                    <button class="btn btn-primary me-2" @click=${this.openUpdate}>Update Details</button>
                    <button class="btn btn-primary me-2" @click=${this.openUpdatePDP}>Update PDP Offering</button>
                    ${this.status !== null ? html`<button class="btn btn-danger me-2" @click=${this.openDeregisterModal}>Deregister Provider</button>` : ''}
                    <p></p>
                `}`}

                ${this.showRegisterModal ? html`
                    <div class="modal-backdrop" @click=${(e)=>{ if(e.target===e.currentTarget) this.closeRegister(); }}>
                        <div class="modal-card">
                            <div class="modal-header d-flex justify-content-between align-items-center">
                                <h5 class="m-0">Register Provider</h5>
                                <button class="close-btn" @click=${this.closeRegister} aria-label="Close">&times;</button>
                            </div>
                            <form @submit=${this.submitRegister}>
                                <div class="modal-body">
                                    <div class="mb-3">
                                        <label class="form-label">Name</label>
                                        <input class="form-control" .value=${this.name}
                                               @input=${e=>this.name=e.target.value} required />
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">Description</label>
                                        <textarea class="form-control" rows="3" .value=${this.description}
                                                  @input=${e=>this.description=e.target.value} required></textarea>
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">Location</label>
                                        <input class="form-control" .value=${this.location}
                                               @input=${e=>this.location=e.target.value} required />
                                        <div class="form-text text-warning">Location must be in format "C=US;ST=California;L=San Francisco"</div>
                                    </div>
                                </div>
                                <div class="modal-footer">
                                    <button type="button" class="btn btn-secondary" @click=${this.closeRegister}>Cancel</button>
                                    <button type="submit" class="btn btn-success">Register</button>
                                </div>
                            </form>
                        </div>
                    </div>
                ` : ''}

                ${this.showUpdateModal ? html`
                    <div class="modal-backdrop" @click=${(e)=>{ if(e.target===e.currentTarget) this.closeUpdate(); }}>
                        <div class="modal-card">
                            <div class="modal-header d-flex justify-content-between align-items-center">
                                <h5 class="m-0">Update Provider</h5>
                                <button class="close-btn" @click=${this.closeUpdate} aria-label="Close">&times;</button>
                            </div>
                            <form @submit=${this.submitUpdate}>
                                <div class="modal-body">
                                    <div class="mb-3">
                                        <label class="form-label">Name</label>
                                        <input class="form-control" .value=${this.name}
                                               @input=${e=>this.name=e.target.value} required />
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">Description</label>
                                        <textarea class="form-control" rows="3" .value=${this.description}
                                                  @input=${e=>this.description=e.target.value} required></textarea>
                                    </div>
                                </div>
                                <div class="modal-footer">
                                    <button type="button" class="btn btn-secondary me-2" @click=${this.closeUpdate}>Cancel</button>
                                    <button type="submit" class="btn btn-success me-2">Update</button>
                                </div>
                            </form>
                        </div>
                    </div>
                ` : ''}

                ${this.showUpdatePDPModal ? html`
                    <div class="modal-backdrop" @click=${(e) => { if (e.target === e.currentTarget) this.closeUpdatePDP(); }}>
                        <div class="modal-card">
                            <div class="modal-header d-flex justify-content-between align-items-center">
                                <h5 class="m-0">Update PDP Offering</h5>
                                <button class="close-btn" @click=${this.closeUpdatePDP} aria-label="Close">&times;</button>
                            </div>
                            <form @submit=${this.submitUpdatePDP}>
                                <div class="modal-body">
                                    <div class="mb-3">
                                        <label class="form-label">Service URL</label>
                                        <input
                                                class="form-control"
                                                .value=${this.pdpServiceURL}
                                                @input=${e => this.pdpServiceURL = e.target.value}
                                                required
                                        />
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">Minimum Piece Size (Bytes)</label>
                                        <input
                                                class="form-control"
                                                type="number"
                                                .value=${this.pdpMinPieceSize}
                                                @input=${e => this.pdpMinPieceSize = e.target.value}
                                                required
                                        />
                                        <div class="form-text">
                                            ${this.formatBytes(this.pdpMinPieceSize)}
                                        </div>
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">Maximum Piece Size (Bytes)</label>
                                        <input
                                                class="form-control"
                                                type="number"
                                                .value=${this.pdpMaxPieceSize}
                                                @input=${e => this.pdpMaxPieceSize = e.target.value}
                                                required
                                        />
                                        <div class="form-text">
                                            ${this.formatBytes(this.pdpMaxPieceSize)}
                                        </div>
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">IPNI Piece</label>
                                        <input
                                                type="checkbox"
                                                .checked=${this.pdpIpniPiece}
                                                @change=${e => this.pdpIpniPiece = e.target.checked}
                                        />
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">IPNI IPFS</label>
                                        <input
                                                type="checkbox"
                                                .checked=${this.pdpIpniIpfs}
                                                @change=${e => this.pdpIpniIpfs = e.target.checked}
                                        />
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">Storage Price (per TiB per day in USDFC)</label>
                                        <input
                                                class="form-control"
                                                type="number"
                                                step="0.0001"
                                                .value=${this.unitsToUSDFCPerTiBPerDay(this.pdpPrice)}
                                                @input=${e => this.pdpPrice = this.USDFCToUnitsPerTibPerDay(parseFloat(e.target.value))}
                                                required
                                        />
                                        <div class="form-text">Approximately ${(parseFloat(this.unitsToUSDFCPerTiBPerDay(this.pdpPrice)) * 30).toFixed(2)} USDFC per TiB per month</div>
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">Minimum Proving Period (Epochs)</label>
                                        <input
                                                class="form-control"
                                                type="number"
                                                .value=${this.pdpMinProvingPeriod}
                                                @input=${e => this.pdpMinProvingPeriod = e.target.value}
                                                required
                                        />
                                    </div>
                                    <div class="mb-3">
                                        <label class="form-label">Location</label>
                                        <input
                                                class="form-control"
                                                .value=${this.pdpLocation}
                                                @input=${e => this.pdpLocation = e.target.value}
                                                required
                                        />
                                        <div class="form-text text-warning">Location must be in format "C=US;ST=California;L=San Francisco"</div>
                                    </div>
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Capabilities</label>
                                    <button type="button" class="btn btn-sm btn-primary mb-2" @click=${this.addCapability}>Add Capability</button>
                                    <ul class="list-group">
                                        ${Object.entries(this.capabilities).map(([key, value]) => html`
                                            <li class="list-group-item d-flex justify-content-between align-items-center">
                                                <span><strong>${key}</strong>: ${value}</span>
                                                <span>
                                                    <button type="button" class="btn btn-sm btn-warning" @click=${() => this.editCapability(key)}>Edit</button>
                                                    <button type="button" class="btn btn-sm btn-danger" @click=${() => this.removeCapability(key)}>Remove</button>
                                                </span>
                                            </li>
                                        `)}
                                    </ul>
                                </div>
                                <div class="modal-footer">
                                    <button type="button" class="btn btn-secondary me-2" @click=${this.closeUpdatePDP}>Cancel</button>
                                    <button type="submit" class="btn btn-success me-2">Update PDP</button>
                                </div>
                            </form>
                        </div>
                    </div>
                ` : ''}

                ${this.showDeregisterModal ? html`
                <div class="modal-backdrop" @click=${(e) => { if (e.target === e.currentTarget) this.closeDeregisterModal(); }}>
                    <div class="modal-card">
                        <div class="modal-header d-flex justify-content-between align-items-center">
                            <h5 class="m-0">Deregister Provider</h5>
                            <button class="close-btn" @click=${this.closeDeregisterModal} aria-label="Close">&times;</button>
                        </div>
                        <form @submit=${this.submitDeregister}>
                            <div class="modal-body">
                                <p>
                                    Are you sure you want to deregister this provider? This action cannot be undone.
                                    Please type <strong>"deregister"</strong> to confirm.
                                </p>
                                <div class="mb-3">
                                    <input class="form-control"
                                           placeholder="Type 'deregister' to confirm"
                                           .value=${this.deregisterConfirmation}
                                           @input=${e => this.deregisterConfirmation = e.target.value}
                                           required />
                                </div>
                            </div>
                            <div class="modal-footer">
                                <button type="button" class="btn btn-secondary me-2" @click=${this.closeDeregisterModal}>Cancel</button>
                                <button type="submit" class="btn btn-danger me-2">Confirm Deregister</button>
                            </div>
                        </form>
                    </div>
                </div>
            ` : ''}
            </div>
        `;
    }
});