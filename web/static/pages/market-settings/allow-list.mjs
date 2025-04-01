import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/lib/cu-wallet.mjs';

class AllowList extends LitElement {
    static properties = {
        allowList: { type: Array },
        editingAllowListEntry: { type: Object },
        errorMessage: { type: String }, // Added errorMessage property
    };

    constructor() {
        super();
        this.allowList = [];
        this.editingAllowListEntry = null;
        this.errorMessage = ''; // Reset error message
        this.loadData();
    }

    async loadData() {
        try {
            // Load allow list using the correct RPC method name
            const result = await RPCCall('GetAllowDenyList', []);
            console.log('GetAllowDenyList result:', result);
            if (Array.isArray(result)) {
                this.allowList = result;
            } else {
                console.error('GetAllowDenyList did not return an array:', result);
                this.allowList = [];
            }
        } catch (error) {
            console.error('Failed to load allow list:', error);
        }
        this.requestUpdate();
    }

    // Allow List Handlers
    addAllowListEntry() {
        this.editingAllowListEntry = {
            wallet: '',
            status: true,
        };
        this.errorMessage = ''; // Reset error message
    }

    editAllowListEntry(entry) {
        this.editingAllowListEntry = { ...entry };
        this.errorMessage = ''; // Reset error message
    }

    async removeAllowListEntry(entry) {
        if (!confirm('Are you sure you want to delete this allow/deny entry?')) {
            return;
        }
        try {
            await RPCCall('RemoveAllowFilter', [entry.wallet]);
            await this.loadData();
        } catch (error) {
            console.error('Failed to remove allow list entry:', error);
            alert(`Error removing allow list entry: ${error.message || error}`);
        }
    }

    async saveAllowListEntry() {
        try {
            const params = [this.editingAllowListEntry.wallet, this.editingAllowListEntry.status];

            if (this.allowList.find((e) => e.wallet === this.editingAllowListEntry.wallet)) {
                // Update existing entry using SetAllowDenyList
                await RPCCall('SetAllowDenyList', params);
            } else {
                // Add new entry using AddAllowDenyList
                await RPCCall('AddAllowDenyList', params);
            }
            await this.loadData();
            this.editingAllowListEntry = null;
        } catch (error) {
            console.error('Failed to save allow list entry:', error);
            this.errorMessage = error.message || 'An error occurred while saving the client filter.';
        }
    }

    cancelAllowListEdit() {
        this.editingAllowListEntry = null;
        this.errorMessage = ''; // Reset error message
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
            <div class="container">
                <h2>Allow/Deny List
                    <button class="info-btn">
                        <!-- Inline SVG icon for the info button -->
                        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-info-circle" viewBox="0 0 16 16">
                            <path d="M8 15A7 7 0 1 1 8 1a7 7 0 0 1 0 14m0 1A8 8 0 1 0 8 0a8 8 0 0 0 0 16"/>
                            <path d="m8.93 6.588-2.29.287-.082.38.45.083c.294.07.352.176.288.469l-.738 3.468c-.194.897.105 1.319.808 1.319.545 0 1.178-.252 1.465-.598l.088-.416c-.2.176-.492.246-.686.246-.275 0-.375-.193-.304-.533zM9 4.5a1 1 0 1 1-2 0 1 1 0 0 1 2 0"/>
                        </svg>
                        <span class="tooltip-text">
                          Specify wallets to explicitly allow or deny deals from, overriding the default client acceptance behavior.
                        </span>
                    </button>
                </h2>
                <button class="btn btn-primary mb-2" @click="${this.addAllowListEntry}">Add Allow/Deny Entry</button>
                ${this.renderAllowListTable()}
                ${this.editingAllowListEntry ? this.renderAllowListForm() : ''}
            </div class="container">
        `;
    }

    renderAllowListTable() {
        return html`
            <table class="table table-dark table-striped">
                <thead>
                    <tr>
                        <th>Wallet</th>
                        <th>Status</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.allowList.map(
            (entry) => html`
                            <tr>
                                <td><cu-wallet wallet_id=${entry.wallet}></cu-wallet></td>
                                <td>${entry.status ? 'Allow' : 'Deny'}</td>
                                <td>
                                    <button
                                        class="btn btn-secondary btn-sm"
                                        @click="${() => this.editAllowListEntry(entry)}"
                                    >
                                        Edit
                                    </button>
                                    <button
                                            class="btn btn-danger btn-sm"
                                            @click="${() => this.removeAllowListEntry(entry)}"
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

    renderAllowListForm() {
        return html`
            <div class="modal">
                <div class="modal-dialog">
                    <div class="modal-content">
                        <form @submit="${this.handleAllowListSubmit}">
                            <div class="modal-header">
                                <h5 class="modal-title">
                                    ${this.editingAllowListEntry.wallet ? 'Edit' : 'Add'} Allow/Deny Entry
                                </h5>
                                <button
                                    type="button"
                                    class="btn-close"
                                    @click="${this.cancelAllowListEdit}"
                                ></button>
                            </div>
                            <div class="modal-body">
                                ${this.errorMessage
                                        ? html`<div class="alert alert-danger">${this.errorMessage}</div>`
                                        : ''}
                                <!-- Form fields for allow list entry -->
                                <div class="mb-3">
                                    <label class="form-label">Wallet</label>
                                    <input
                                        type="text"
                                        class="form-control"
                                        .value="${this.editingAllowListEntry.wallet}"
                                        @input="${(e) => (this.editingAllowListEntry.wallet = e.target.value)}"
                                        required
                                        ?readonly="${!!this.editingAllowListEntry.wallet}"
                                    />
                                </div>
                                <div class="mb-3 form-check">
                                    <input
                                        class="form-check-input"
                                        type="checkbox"
                                        .checked="${this.editingAllowListEntry.status}"
                                        @change="${(e) =>
            (this.editingAllowListEntry.status = e.target.checked)}"
                                    />
                                    <label class="form-check-label">Allow</label>
                                </div>
                            </div>
                            <div class="modal-footer">
                                <button
                                    type="button"
                                    class="btn btn-secondary"
                                    @click="${this.cancelAllowListEdit}"
                                >
                                    Cancel
                                </button>
                                <button type="submit" class="btn btn-primary">
                                    ${this.editingAllowListEntry.wallet ? 'Update' : 'Add'}
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            </div>
            <div class="modal-backdrop"></div>
        `;
    }

    handleAllowListSubmit(e) {
        e.preventDefault();
        this.saveAllowListEntry();
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

customElements.define('allow-list', AllowList);
