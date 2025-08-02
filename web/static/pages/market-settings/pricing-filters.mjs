import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class PricingFilters extends LitElement {
    static properties = {
        pricingFilters: { type: Array },
        editingPricingFilter: { type: Object },
        errorMessage: { type: String }, // Added errorMessage property
    };

    constructor() {
        super();
        this.pricingFilters = [];
        this.editingPricingFilter = null;
        this.errorMessage = ''; // Initialize errorMessage
        this.loadData();
    }

    async loadData() {
        try {
            // Load pricing filters using the correct RPC method name
            const result = await RPCCall('GetPriceFilters', []);
            console.log('GetPriceFilters result:', result);
            if (Array.isArray(result)) {
                this.pricingFilters = result;
            } else {
                console.error('GetPriceFilters did not return an array:', result);
                this.pricingFilters = [];
            }
        } catch (error) {
            console.error('Failed to load pricing filters:', error);
            this.pricingFilters = []; // Ensure it's an array
        }
        this.requestUpdate();
    }

    // Pricing Filters Handlers
    addPricingFilter() {
        this.editingPricingFilter = {
            name: '',
            min_dur: 180,
            max_dur: 1278,
            min_size: 256,
            max_size: 34359738368,
            price: 11302806713,
            verified: false,
        };
        this.errorMessage = ''; // Reset error message
    }

    editPricingFilter(filter) {
        // Ensure property names match the backend JSON structure
        this.editingPricingFilter = { ...filter };
        this.errorMessage = ''; // Reset error message
    }

    async removePricingFilter(filter) {
        if (!confirm('Are you sure you want to delete this pricing filter?')) {
            return;
        }
        try {
            await RPCCall('RemovePricingFilter', [filter.name]);
            await this.loadData();
        } catch (error) {
            console.error('Failed to remove pricing filter:', error);
            alert(`Error removing pricing filter: ${error.message || error}`);
        }
    }

    async savePricingFilter() {
        try {
            const params = [
                this.editingPricingFilter.name,
                this.editingPricingFilter.min_dur,
                this.editingPricingFilter.max_dur,
                this.editingPricingFilter.min_size,
                this.editingPricingFilter.max_size,
                this.editingPricingFilter.price,
                this.editingPricingFilter.verified,
            ]
            if (this.pricingFilters.find((f) => f.name === this.editingPricingFilter.name)) {
                // Update existing filter using SetPriceFilters
                await RPCCall('SetPriceFilters', params);
            } else {
                // Add new filter using AddPriceFilters
                await RPCCall('AddPriceFilters', params);
            }
            await this.loadData();
            this.editingPricingFilter = null;
            this.errorMessage = ''; // Reset error message
        } catch (error) {
            console.error('Failed to save pricing filter:', error);
            this.errorMessage = error.message || 'An error occurred while saving the pricing filter.';
        }
    }

    cancelPricingFilterEdit() {
        this.editingPricingFilter = null;
        this.errorMessage = ''; // Reset error message
    }

    render() {
        return html`
            <link
                href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                rel="stylesheet"
                crossorigin="anonymous"
            />
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'" />
            <div class="container">
                <h2>Pricing Filters
                    <button class="info-btn">
                        <!-- Inline SVG icon for the info button -->
                        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-info-circle" viewBox="0 0 16 16">
                            <path d="M8 15A7 7 0 1 1 8 1a7 7 0 0 1 0 14m0 1A8 8 0 1 0 8 0a8 8 0 0 0 0 16"/>
                            <path d="m8.93 6.588-2.29.287-.082.38.45.083c.294.07.352.176.288.469l-.738 3.468c-.194.897.105 1.319.808 1.319.545 0 1.178-.252 1.465-.598l.088-.416c-.2.176-.492.246-.686.246-.275 0-.375-.193-.304-.533zM9 4.5a1 1 0 1 1-2 0 1 1 0 0 1 2 0"/>
                        </svg>
                        <span class="tooltip-text">
                          New deal proposals are accepted if they match the first applicable rule; otherwise, default ask is applied, or the proposal is rejected.
                        </span>
                    </button>
                </h2>
                <button class="btn btn-primary mb-2" @click="${this.addPricingFilter}">Add Pricing Filter</button>
                ${this.renderPricingFiltersTable()}
                ${this.editingPricingFilter ? this.renderPricingFilterForm() : ''}
            </div>
        `;
    }

    renderPricingFiltersTable() {
        return html`
            <table class="table table-dark table-striped">
                <thead>
                    <tr>
                        <th>Name</th>
                        <th>Min Duration (Days)</th>
                        <th>Max Duration (Days)</th>
                        <th>Min Size</th>
                        <th>Max Size</th>
                        <th>Price (FIL/TiB/Month)</th>
                        <th>Verified</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.pricingFilters.map(
            (filter) => html`
                            <tr>
                                <td>${filter.name}</td>
                                <td>${filter.min_dur}</td>
                                <td>${filter.max_dur}</td>
                                <td>${this.formatBytes(filter.min_size)}</td>
                                <td>${this.formatBytes(filter.max_size)}</td>
                                <td>${this.attoFilToFilPerTiBPerMonth(filter.price)}</td>
                                <td>${filter.verified ? 'Yes' : 'No'}</td>
                                <td>
                                    <div class="d-flex gap-2">
                                    <button
                                        class="btn btn-secondary btn-sm"
                                        @click="${() => this.editPricingFilter(filter)}"
                                    >
                                        Edit
                                    </button>
                                    <button
                                            class="btn btn-danger btn-sm"
                                            @click="${() => this.removePricingFilter(filter)}"
                                    >
                                        Remove
                                    </button>
                                    </div>
                                </td>
                            </tr>
                        `
        )}
                </tbody>
            </table>
        `;
    }

    renderPricingFilterForm() {
        return html`
            <div class="modal">
                <div class="modal-dialog">
                    <div class="modal-content">
                        <form @submit="${this.handlePricingFilterSubmit}">
                            <div class="modal-header">
                                <h5 class="modal-title">
                                    ${this.editingPricingFilter.name ? 'Edit' : 'Add'} Pricing Filter
                                </h5>
                                <button
                                    type="button"
                                    class="btn-close"
                                    @click="${this.cancelPricingFilterEdit}"
                                ></button>
                            </div>
                            <div class="modal-body">
                                ${this.errorMessage
                                        ? html`<div class="alert alert-danger">${this.errorMessage}</div>`
                                        : ''}
                                <!-- Form fields for pricing filter -->
                                <div class="mb-3">
                                    <label class="form-label">Name</label>
                                    <input
                                            type="text"
                                            class="form-control"
                                            .value="${this.editingPricingFilter.name}"
                                            @input="${(e) => (this.editingPricingFilter.name = e.target.value)}"
                                            required
                                            ?readonly="${!!this.editingPricingFilter.name}"
                                    />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Min Duration (Days)</label>
                                    <input
                                        type="number"
                                        class="form-control"
                                        .value="${this.editingPricingFilter.min_dur}"
                                        @input="${(e) =>
            (this.editingPricingFilter.min_dur = parseInt(e.target.value))}"
                                        required
                                    />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Max Duration (Days)</label>
                                    <input
                                        type="number"
                                        class="form-control"
                                        .value="${this.editingPricingFilter.max_dur}"
                                        @input="${(e) =>
            (this.editingPricingFilter.max_dur = parseInt(e.target.value))}"
                                        required
                                    />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Min Size (Bytes)</label>
                                    <input
                                        type="number"
                                        class="form-control"
                                        .value="${this.editingPricingFilter.min_size}"
                                        @input="${(e) =>
            (this.editingPricingFilter.min_size = parseInt(e.target.value))}"
                                        required
                                    />
                                    <div class="form-text">
                                        ${this.formatBytes(this.editingPricingFilter.min_size)}
                                    </div>
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Max Size (Bytes)</label>
                                    <input
                                        type="number"
                                        class="form-control"
                                        .value="${this.editingPricingFilter.max_size}"
                                        @input="${(e) =>
            (this.editingPricingFilter.max_size = parseInt(e.target.value))}"
                                        required
                                    />
                                    <div class="form-text">
                                        ${this.formatBytes(this.editingPricingFilter.max_size)}
                                    </div>
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Price (FIL/TiB/Month)</label>
                                    <input
                                        type="number"
                                        step="any"
                                        class="form-control"
                                        .value="${this.attoFilToFilPerTiBPerMonth(this.editingPricingFilter.price)}"
                                        @input="${(e) =>
            (this.editingPricingFilter.price = this.filToAttoFilPerGiBPerEpoch(
                parseFloat(e.target.value)
            ))}"
                                        required
                                    />
                                    <div class="form-text">
                                        AttoFIL/GiB/Epoch: ${this.editingPricingFilter.price}
                                    </div>
                                </div>
                                <div class="mb-3 form-check">
                                    <input
                                        class="form-check-input"
                                        type="checkbox"
                                        .checked="${this.editingPricingFilter.verified}"
                                        @change="${(e) =>
            (this.editingPricingFilter.verified = e.target.checked)}"
                                    />
                                    <label class="form-check-label">Verified Deal</label>
                                </div>
                            </div>
                            <div class="modal-footer">
                                <button
                                    type="button"
                                    class="btn btn-secondary"
                                    @click="${this.cancelPricingFilterEdit}"
                                >
                                    Cancel
                                </button>
                                <button type="submit" class="btn btn-primary">
                                    ${this.editingPricingFilter.name ? 'Update' : 'Add'}
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            </div>
            <div class="modal-backdrop"></div>
        `;
    }

    handlePricingFilterSubmit(e) {
        e.preventDefault();
        this.savePricingFilter();
    }

    // Utility Methods
    formatBytes(bytes) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const sizes = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    get EPOCHS_IN_MONTH() {
        return 86400;
    }
    get GIB_IN_TIB() {
        return 1024;
    }
    get ATTOFIL_PER_FIL() {
        return 1e18;
    }

    attoFilToFilPerTiBPerMonth(attoFilPerGiBPerEpoch) {
        const filPerTiBPerMonth =
            (attoFilPerGiBPerEpoch * this.GIB_IN_TIB * this.EPOCHS_IN_MONTH) / this.ATTOFIL_PER_FIL;
        return filPerTiBPerMonth.toFixed(8); // Limit to 8 decimal places
    }

    filToAttoFilPerGiBPerEpoch(filPerTiBPerMonth) {
        const attoFilPerGiBPerEpoch =
            (filPerTiBPerMonth * this.ATTOFIL_PER_FIL) / this.GIB_IN_TIB / this.EPOCHS_IN_MONTH;
        return Math.round(attoFilPerGiBPerEpoch); // Round to nearest integer
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
            max-width: 600px;
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

customElements.define('pricing-filters', PricingFilters);
