import { html, LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

/**
 * A custom Web Component for managing products, data sources, and market contracts.
 */
class MarketManager extends LitElement {
    static properties = {
        products: { type: Array },
        dataSources: { type: Array },
        contracts: { type: Array },
        newContractAddress: { type: String },
    };

    constructor() {
        super();
        this.products = [];
        this.dataSources = [];
        this.contracts = [];
        this.newContractAddress = '';
        this.loadAllData();
    }

    async loadAllData() {
        try {
            const productsResult = await RPCCall('ListProducts', []);
            this.products = Array.isArray(productsResult)
                ? productsResult
                : Object.entries(productsResult || {}).map(([name, enabled]) => ({ name, enabled }));

            const dataSourcesResult = await RPCCall('ListDataSources', []);
            this.dataSources = Array.isArray(dataSourcesResult)
                ? dataSourcesResult
                : Object.entries(dataSourcesResult || {}).map(([name, enabled]) => ({ name, enabled }));

            const contractsResult = await RPCCall('ListMarketContracts', []);
            this.contracts = Array.isArray(contractsResult)
                ? contractsResult
                : Object.entries(contractsResult || {}).map(([address, allowed]) => ({
                    address,
                    allowed: Boolean(allowed),
                }));
            this.contracts.sort((a, b) => a.address.localeCompare(b.address));

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
            await this.loadAllData();
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
            await this.loadAllData();
        } catch (err) {
            console.error('Failed to toggle data source state:', err);
        }
    }

    async addContract() {
        const address = (this.newContractAddress || '').trim();
        if (!address) {
            alert('Contract address is required.');
            return;
        }

        try {
            await RPCCall('AddMarketContract', [address, true]);
            this.newContractAddress = '';
            await this.loadAllData();
        } catch (err) {
            console.error('Failed to add contract:', err);
            alert(`Failed to add contract: ${err.message}`);
        }
    }

    async toggleContractState(contract) {
        const nextAllowed = !contract.allowed;
        const confirmation = confirm(
            `Are you sure you want to ${nextAllowed ? 'allow' : 'block'} contract "${contract.address}"?`
        );

        if (!confirmation) return;

        try {
            await RPCCall('UpdateMarketContract', [contract.address, nextAllowed]);
            await this.loadAllData();
        } catch (err) {
            console.error('Failed to update contract state:', err);
            alert(`Failed to update contract: ${err.message}`);
        }
    }

    async removeContract(contract) {
        const confirmation = confirm(`Are you sure you want to remove contract "${contract.address}"?`);
        if (!confirmation) return;

        try {
            await RPCCall('RemoveMarketContract', [contract.address]);
            await this.loadAllData();
        } catch (err) {
            console.error('Failed to remove contract:', err);
            alert(`Failed to remove contract: ${err.message}`);
        }
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
                            <h2>DDO Contracts</h2>

                            <div class="d-flex gap-2 mb-2">
                                <input
                                    class="form-control"
                                    type="text"
                                    .value=${this.newContractAddress}
                                    @input=${e => this.newContractAddress = e.target.value}
                                    placeholder="0x..."
                                />
                                <button class="btn btn-primary" @click=${this.addContract}>Add</button>
                            </div>

                            <table class="table table-dark table-striped table-sm">
                                <thead>
                                    <tr>
                                        <th>Address</th>
                                        <th>Allowed</th>
                                        <th>Action</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    ${this.contracts?.map(
                                        (contract) => html`
                                            <tr>
                                                <td>${contract.address}</td>
                                                <td>${contract.allowed ? 'Yes' : 'No'}</td>
                                                <td>
                                                    <div class="d-flex gap-2">
                                                        <button
                                                            class="btn btn-${contract.allowed ? 'warning' : 'success'}"
                                                            @click=${() => this.toggleContractState(contract)}
                                                        >
                                                            ${contract.allowed ? 'Block' : 'Allow'}
                                                        </button>
                                                        <button
                                                            class="btn btn-danger"
                                                            @click=${() => this.removeContract(contract)}
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
                        </td>
                    </tr>
                </table>
            </div>
        `;
    }
}

customElements.define('market-manager', MarketManager);
