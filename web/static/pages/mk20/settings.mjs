import { html, css, LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

/**
 * A custom Web Component for managing products, data sources, and market contracts.
 * Extends the LitElement class to leverage the Lit library for efficient rendering.
 */
class MarketManager extends LitElement {
    static properties = {
        products: { type: Array },
        dataSources: { type: Array },
        contracts: { type: Array },
        selectedContract: { type: Object },
    };

    constructor() {
        super();
        this.products = [];
        this.dataSources = [];
        this.contracts = [];
        this.selectedContract = null; // For modal
        this.loadAllData();
    }

    async loadAllData() {
        try {
            const productsResult = await RPCCall('ListProducts', []);
            this.products = Array.isArray(productsResult)
                ? productsResult
                : Object.entries(productsResult).map(([name, enabled]) => ({ name, enabled }));

            const dataSourcesResult = await RPCCall('ListDataSources', []);
            this.dataSources = Array.isArray(dataSourcesResult)
                ? dataSourcesResult
                : Object.entries(dataSourcesResult).map(([name, enabled]) => ({ name, enabled }));

            const contractsResult = await RPCCall('ListMarketContracts', []);
            this.contracts = Array.isArray(contractsResult)
                ? contractsResult
                : Object.entries(contractsResult).map(([address, abi]) => ({ address, abi }));

            this.requestUpdate();
        } catch (err) {
            console.error('Failed to load data:', err);
            this.products = [];
            this.dataSources = [];
            this.contracts = [];
        }
    }

    async toggleProductState(product) {
        const confirmation = confirm(
            `Are you sure you want to ${product.enabled ? 'disable' : 'enable'} the product "${product.name}"?`
        );

        if (!confirmation) return;

        try {
            if (product.enabled) {
                await RPCCall('DisableProduct', [product.name]);
            } else {
                await RPCCall('EnableProduct', [product.name]);
            }
            this.loadAllData(); // Refresh after toggling
        } catch (err) {
            console.error('Failed to toggle product state:', err);
        }
    }

    async toggleDataSourceState(dataSource) {
        const confirmation = confirm(
            `Are you sure you want to ${dataSource.enabled ? 'disable' : 'enable'} the data source "${dataSource.name}"?`
        );

        if (!confirmation) return;

        try {
            if (dataSource.enabled) {
                await RPCCall('DisableDataSource', [dataSource.name]);
            } else {
                await RPCCall('EnableDataSource', [dataSource.name]);
            }
            this.loadAllData(); // Refresh after toggling
        } catch (err) {
            console.error('Failed to toggle data source state:', err);
        }
    }

    openContractModal(contract) {
        this.selectedContract = { ...contract };
        this.updateComplete.then(() => {
            const modal = this.shadowRoot.querySelector('#contract-modal');
            if (modal && typeof modal.showModal === 'function') {
                modal.showModal();
            }
        });
    }

    async removeContract(contract) {
        if (!confirm(`Are you sure you want to remove contract ${contract.address}?`)) return;

        try {
            await RPCCall('RemoveMarketContract', [contract.address]);
            await this.loadAllData();
        } catch (err) {
            console.error('Failed to remove contract:', err);
            alert(`Failed to remove contract: ${err.message}`);
        }
    }


    async saveContractChanges() {
        try {
            const { address, abi } = this.selectedContract;

            if (!address || !abi) {
                alert("Contract address and ABI are required.");
                return;
            }

            const method = this.contracts.find(c => c.address === address)
                ? 'UpdateMarketContract'
                : 'AddMarketContract';

            await RPCCall(method, [address, abi]);

            this.loadAllData();
            this.closeModal();
        } catch (err) {
            console.error('Failed to save contract changes:', err);
            alert(`Failed to save contract: ${err.message}`);
        }
    }


    closeModal() {
        this.selectedContract = null;
        const modal = this.shadowRoot.querySelector('#contract-modal');
        if (modal) modal.close();
    }

    render() {
        return html`
            <link
                href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                rel="stylesheet"
                crossorigin="anonymous"
            />
            <link rel="stylesheet" href="/ux/main.css" />

            <div class="container">
                <table style="border-collapse: separate">
                    <tr style="vertical-align: top">
                        <td class="pe-2">
                            <h2>Products</h2>
                            <table class="table table-dark table-striped table-sm">
                                <thead>
                                <tr>
                                    <th>Name</th>
                                    <th>Enabled</th>
                                    <th>Action</th>
                                </tr>
                                </thead>
                                <tbody>
                                ${this.products?.map(
                                        (product) => html`
                                            <tr>
                                                <td>${product.name}</td>
                                                <td>${product.enabled ? 'Yes' : 'No'}</td>
                                                <td>
                                                    <button
                                                            class="btn btn-${product.enabled ? 'danger' : 'success'}"
                                                            @click=${() => this.toggleProductState(product)}
                                                    >
                                                        ${product.enabled ? 'Disable' : 'Enable'}
                                                    </button>
                                                </td>
                                            </tr>
                                        `
                                )}
                                </tbody>
                            </table>
                        </td>
                        <td class="pe-2">
                            <h2>Data Sources</h2>
                            <table class="table table-dark table-striped table-sm">
                                <thead>
                                <tr>
                                    <th>Name</th>
                                    <th>Enabled</th>
                                    <th>Action</th>
                                </tr>
                                </thead>
                                <tbody>
                                ${this.dataSources?.map(
                                        (source) => html`
                                            <tr>
                                                <td>${source.name}</td>
                                                <td>${source.enabled ? 'Yes' : 'No'}</td>
                                                <td>
                                                    <button
                                                            class="btn btn-${source.enabled ? 'danger' : 'success'}"
                                                            @click=${() => this.toggleDataSourceState(source)}
                                                    >
                                                        ${source.enabled ? 'Disable' : 'Enable'}
                                                    </button>
                                                </td>
                                            </tr>
                                        `
                                )}
                                </tbody>
                            </table>
                        </td>
                        <td class="pe-2">
                            <h2>Contracts
                                <button class="btn btn-primary mb-3" @click=${() => this.openContractModal({ address: '', abi: '' })}>
                                    Add Contract
                                </button>
                            </h2>
                                <table class="table table-dark table-striped table-sm" style="display: block;max-height: 200px; overflow-y: auto">
                                    <thead>
                                    <tr>
                                        <th>Address</th>
                                        <th>Actions</th>
                                    </tr>
                                    </thead>
                                    <tbody>
                                    ${this.contracts?.map(
                                            (contract) => html`
                                        <tr>
                                            <td>${contract.address}</td>
                                            <td>
                                                <div class="d-flex gap-2">
                                                    <button class="btn btn-secondary btn-sm" @click=${() => this.openContractModal(contract)}>
                                                        Edit
                                                    </button>
                                                    <button class="btn btn-danger btn-sm" @click=${() => this.removeContract(contract)}>
                                                        Remove
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                          `
                                    )}
                                </tbody>
                            </table>
                        </td>
                    </tr>
                </table>
                ${this.renderContractModal()}
            </div>
        `;
    }

    renderContractModal() {
        if (!this.selectedContract) return null;

        return html`
            <dialog id="contract-modal">
                <h3>
                    ${this.contracts.some(c => c.address === this.selectedContract.address)
                            ? html`Edit Contract: ${this.selectedContract.address}`
                            : html`Add New Contract`}
                </h3>

                <div class="mb-3">
                    <label class="form-label">Contract Address</label>
                    <input
                            class="form-control"
                            type="text"
                            .value=${this.selectedContract.address}
                            @input=${e => this.selectedContract.address = e.target.value}
                            placeholder="0x..."
                    />
                </div>

                <div class="mb-3">
                    <label class="form-label">ABI</label>
                    <textarea
                            class="form-control"
                            rows="10"
                            .value=${this.selectedContract.abi}
                            @input=${e => this.selectedContract.abi = e.target.value}
                            placeholder='[{"type":"function",...}]'
                    ></textarea>
                </div>

                <div class="mt-3">
                    <button class="btn btn-success" @click=${this.saveContractChanges}>Save</button>
                    <button class="btn btn-secondary" @click=${this.closeModal}>Cancel</button>
                </div>
            </dialog>
        `;
    }
}

customElements.define('market-manager', MarketManager);