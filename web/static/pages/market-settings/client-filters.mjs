import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class ClientFilters extends LitElement {
    static properties = {
        clientFilters: { type: Array },
        editingClientFilter: { type: Object },
        errorMessage: { type: String }, // Added errorMessage property
    };

    constructor() {
        super();
        this.clientFilters = [];
        this.editingClientFilter = null;
        this.errorMessage = ''; // Initialize errorMessage
        this.loadData();
    }

    async loadData() {
        try {
            // Load client filters using the correct RPC method name
            const result = await RPCCall('GetClientFilters', []);
            console.log('GetClientFilters result:', result);
            if (Array.isArray(result)) {
                this.clientFilters = result;
            } else {
                console.error('GetClientFilters did not return an array:', result);
                this.clientFilters = [];
            }
        } catch (error) {
            console.error('Failed to load client filters:', error);
        }
        this.requestUpdate();
    }

    // Client Filters Handlers
    addClientFilter() {
        this.editingClientFilter = {
            name: '',
            active: false,
            wallets: [],
            peers: [],
            pricing_filters: [],
            max_deals_per_hour: 0,
            max_deal_size_per_hour: 0,
            info: '',
        };
        this.errorMessage = ''; // Reset error message
    }

    editClientFilter(filter) {
        // Ensure property names match the backend JSON structure
        this.editingClientFilter = { ...filter };
        this.errorMessage = ''; // Reset error message
    }

    async removeClientFilter(filter) {
        if (!confirm('Are you sure you want to delete this client filter?')) {
            return;
        }
        try {
            await RPCCall('RemoveClientFilter', [filter.name]);
            await this.loadData();
        } catch (error) {
            console.error('Failed to remove client filter:', error);
            alert(`Error removing client filter: ${error.message || error}`);
        }
    }

    async saveClientFilter() {
        try {
            const params = [
                this.editingClientFilter.name,
                this.editingClientFilter.active,
                this.editingClientFilter.wallets,
                this.editingClientFilter.peers,
                this.editingClientFilter.pricing_filters,
                this.editingClientFilter.max_deals_per_hour,
                this.editingClientFilter.max_deal_size_per_hour,
                this.editingClientFilter.info,
            ];

            if (this.clientFilters.find((f) => f.name === this.editingClientFilter.name)) {
                // Update existing filter using SetClientFilters
                await RPCCall('SetClientFilters', params);
            } else {
                // Add new filter using AddClientFilters
                await RPCCall('AddClientFilters', params);
            }
            await this.loadData();
            this.editingClientFilter = null;
            this.errorMessage = ''; // Reset error message
        } catch (error) {
            console.error('Failed to save client filter:', error);
            this.errorMessage = error.message || 'An error occurred while saving the client filter.';
        }
    }

    cancelClientFilterEdit() {
        this.editingClientFilter = null;
        this.errorMessage = ''; // Reset error message
    }

    render() {
        return html`
            <link
                href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                rel="stylesheet"
                crossorigin="anonymous"
            />
            <div class="container">
                <h2>Client Filters
                    <button class="info-btn">
                        <!-- Inline SVG icon for the info button -->
                        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-info-circle" viewBox="0 0 16 16">
                            <path d="M8 15A7 7 0 1 1 8 1a7 7 0 0 1 0 14m0 1A8 8 0 1 0 8 0a8 8 0 0 0 0 16"/>
                            <path d="m8.93 6.588-2.29.287-.082.38.45.083c.294.07.352.176.288.469l-.738 3.468c-.194.897.105 1.319.808 1.319.545 0 1.178-.252 1.465-.598l.088-.416c-.2.176-.492.246-.686.246-.275 0-.375-.193-.304-.533zM9 4.5a1 1 0 1 1-2 0 1 1 0 0 1 2 0"/>
                        </svg>
                        <span class="tooltip-text">
                          When creating a client rule you must select to apply a specific pricing rule to that client.
                        </span>
                    </button>
                </h2>
                <button class="btn btn-primary mb-2" @click="${this.addClientFilter}">Add Client Filter</button>
                ${this.renderClientFiltersTable()}
                ${this.editingClientFilter ? this.renderClientFilterForm() : ''}
            </div>
        `;
    }

    renderClientFiltersTable() {
        return html`
            <table class="table table-dark table-striped">
                <thead>
                    <tr>
                        <th>Name</th>
                        <th>Active</th>
                        <th>Wallets</th>
                        <th>Peers</th>
                        <th>Pricing Filters</th>
                        <th>Max Deals/Hour</th>
                        <th>Max Deal Size/Hour</th>
                        <th>Info</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.clientFilters.map(
            (filter) => html`
                            <tr>
                                <td>${filter.name}</td>
                                <td>${filter.active ? 'Yes' : 'No'}</td>
                                <td>${(filter.wallets || []).join(', ')}</td>
                                <td>${(filter.peers || []).join(', ')}</td>
                                <td>${(filter.pricing_filters || []).join(', ')}</td>
                                <td>${filter.max_deals_per_hour}</td>
                                <td>${this.formatBytes(filter.max_deal_size_per_hour)}</td>
                                <td>${filter.info || ''}</td>
                                <td>
                                    <button
                                        class="btn btn-secondary btn-sm"
                                        @click="${() => this.editClientFilter(filter)}"
                                    >
                                        Edit
                                    </button>
                                    <button
                                            class="btn btn-danger btn-sm"
                                            @click="${() => this.removeClientFilter(filter)}"
                                    >
                                        Remove
                                    </button>
                                </td>
                            </tr>
                        `
        )}
                </tbody>
            </table>
        `;
    }

    renderClientFilterForm() {
        return html`
            <div class="modal">
                <div class="modal-dialog modal-lg">
                    <div class="modal-content">
                        <form @submit="${this.handleClientFilterSubmit}">
                            <div class="modal-header">
                                <h5 class="modal-title">
                                    ${this.editingClientFilter.name ? 'Edit' : 'Add'} Client Filter
                                </h5>
                                <button
                                    type="button"
                                    class="btn-close"
                                    @click="${this.cancelClientFilterEdit}"
                                ></button>
                            </div>
                            <div class="modal-body">
                                ${this.errorMessage
                                        ? html`<div class="alert alert-danger">${this.errorMessage}</div>`
                                        : ''}
                                <!-- Form fields for client filter -->
                                <div class="mb-3">
                                    <label class="form-label">Name</label>
                                    <input
                                        type="text"
                                        class="form-control"
                                        .value="${this.editingClientFilter.name}"
                                        @input="${(e) => (this.editingClientFilter.name = e.target.value)}"
                                        required
                                        ?readonly="${!!this.editingClientFilter.name}"
                                    />
                                </div>
                                <div class="mb-3 form-check">
                                    <input
                                        class="form-check-input"
                                        type="checkbox"
                                        .checked="${this.editingClientFilter.active}"
                                        @change="${(e) =>
            (this.editingClientFilter.active = e.target.checked)}"
                                    />
                                    <label class="form-check-label">Active</label>
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Wallets (comma-separated)</label>
                                    <input
                                        type="text"
                                        class="form-control"
                                        .value="${(this.editingClientFilter.wallets || []).join(', ')}"
                                        @input="${(e) =>
            (this.editingClientFilter.wallets = e.target.value
                .split(',')
                .map((w) => w.trim()))}"
                                    />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Peers (comma-separated)</label>
                                    <input
                                        type="text"
                                        class="form-control"
                                        .value="${(this.editingClientFilter.peers || []).join(', ')}"
                                        @input="${(e) =>
            (this.editingClientFilter.peers = e.target.value
                .split(',')
                .map((p) => p.trim()))}"
                                    />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Pricing Filters (Names, comma-separated)</label>
                                    <input
                                        type="text"
                                        class="form-control"
                                        .value="${(this.editingClientFilter.pricing_filters || []).join(', ')}"
                                        @input="${(e) =>
            (this.editingClientFilter.pricing_filters = e.target.value
                .split(',')
                .map((p) => p.trim()))}"
                                    />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Max Deals per Hour</label>
                                    <input
                                        type="number"
                                        class="form-control"
                                        .value="${this.editingClientFilter.max_deals_per_hour}"
                                        @input="${(e) =>
            (this.editingClientFilter.max_deals_per_hour = parseInt(
                e.target.value
            ))}"
                                        required
                                    />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Max Deal Size per Hour (GiB)</label>
                                    <input
                                        type="number"
                                        class="form-control"
                                        .value="${this.bytesToGiB(this.editingClientFilter.max_deal_size_per_hour)}"
                                        @input="${(e) =>
                                                (this.editingClientFilter.max_deal_size_per_hour = this.gibToBytes(
                                                        parseInt(e.target.value)
                                                ))}"
                                        required
                                    />
                                    <div class="form-text">
                                        ${this.formatBytes(this.editingClientFilter.max_deal_size_per_hour)}
                                    </div>
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Additional Info</label>
                                    <textarea
                                        class="form-control"
                                        @input="${(e) => (this.editingClientFilter.info = e.target.value)}"
                                    >
${this.editingClientFilter.info}</textarea
                                    >
                                </div>
                            </div>
                            <div class="modal-footer">
                                <button
                                    type="button"
                                    class="btn btn-secondary"
                                    @click="${this.cancelClientFilterEdit}"
                                >
                                    Cancel
                                </button>
                                <button type="submit" class="btn btn-primary">
                                    ${this.editingClientFilter.name ? 'Update' : 'Add'}
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            </div>
            <div class="modal-backdrop"></div>
        `;
    }

    handleClientFilterSubmit(e) {
        e.preventDefault();
        this.saveClientFilter();
    }

    // Utility Methods
    formatBytes(bytes) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const sizes = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    bytesToGiB(bytes) {
        return (bytes / (1024 ** 3)).toFixed(); // Convert bytes to GiB and limit to 2 decimal places
    }

    gibToBytes(gib) {
        return gib * (1024 ** 3); // Convert GiB to bytes
    }

    static styles = css`
        /* Styles for the modal and form */
        :host {
            display: block;
        }
        
        .alert {
            margin-bottom: 1rem;
        }
        
        .modal {
            position: fixed;
            top: 0;
            left: 0;
            z-index: 1050;
            width: 100%;
            height: 100%;
            overflow: hidden;
            outline: 0;
            display: flex;
            align-items: center;
            justify-content: center;
            backdrop-filter: blur(5px);
        }

        .modal-dialog {
            max-width: 800px;
            margin: 1.75rem auto;
        }

        .modal-content {
            background-color: var(--color-form-field, #1d1d21);
            border: 1px solid var(--color-form-default, #808080);
            border-radius: 0.3rem;
            box-shadow: 0 0.5rem 1rem rgba(0, 0, 0, 0.5);
            color: var(--color-text-primary, #FFF);
        }

        .modal-backdrop {
            position: fixed;
            top: 0;
            left: 0;
            z-index: 1040;
            width: 100vw;
            height: 100vh;
            background-color: var(--color-text-secondary, #171717);
            opacity: 0.5;
        }
        
        .info-btn {
            position: relative;
            border: none;
            background: transparent;
            cursor: pointer;
            color: #17a2b8;
            font-size: 1em;
            margin-left: 8px;
        }
    
        .tooltip-text {
            display: none;
            position: absolute;
            top: 50%;
            left: 120%; /* Position the tooltip to the right of the button */
            transform: translateY(-50%); /* Center the tooltip vertically */
            min-width: 440px;
            max-width: 600px;
            background-color: #333;
            color: #fff;
            padding: 8px;
            border-radius: 4px;
            font-size: 0.8em;
            text-align: left;
            white-space: normal;
            z-index: 10;
        }
    
        .info-btn:hover .tooltip-text {
            display: block;
        }
    `;
}

customElements.define('client-filters', ClientFilters);
