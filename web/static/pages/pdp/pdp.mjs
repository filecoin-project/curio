import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('pdp-info', class PDPElement extends LitElement {
    static properties = {
        services: { type: Array },
        keys: { type: Array },
        showAddServiceForm: { type: Boolean },
        showAddKeyForm: { type: Boolean },
    }

    constructor() {
        super();
        this.services = [];
        this.keys = [];
        this.showAddServiceForm = false;
        this.showAddKeyForm = false;
        this.loadServices();
        this.loadKeys();
    }

    async loadServices() {
        try {
            this.services = await RPCCall('PDPServices', []);
        } catch (error) {
            console.error('Failed to load PDP services:', error);
        }
    }

    async loadKeys() {
        try {
            this.keys = await RPCCall('ListPDPKeys', []);
        } catch (error) {
            console.error('Failed to load PDP keys:', error);
        }
    }

    toggleAddKeyForm() {
        this.showAddKeyForm = !this.showAddKeyForm;
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
            // Call the RPC method to import the private key
            const address = await RPCCall('ImportPDPKey', [privateKey]);

            // Reset the form
            privateKeyInput.value = '';

            // Reload the keys
            await this.loadKeys();

            // Hide the form
            this.showAddKeyForm = false;

            alert(`Successfully imported key for address: ${address}`);
        } catch (error) {
            console.error('Failed to import key:', error);
            alert('Failed to import key: ' + (error.message || error));
        }
    }

    async removeKey(ownerAddress) {
        const confirmed = confirm(`Are you sure you want to remove the key for address "${ownerAddress}"?`);
        if (!confirmed) {
            return;
        }

        try {
            // Call the RPC method to remove the key
            await RPCCall('RemovePDPKey', [ownerAddress]);

            // Reload the keys
            await this.loadKeys();
        } catch (error) {
            console.error('Failed to remove key:', error);
            alert('Failed to remove key: ' + (error.message || error));
        }
    }

    render() {
        return html`
            <!-- Include Bootstrap CSS -->
            <link
                href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css"
                rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <div class="container-fluid" style="min-width: 70em">
                <h2>Owner Addresses</h2>
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
                    <p>No owner addresses available.</p>
                `}

                <button class="btn btn-primary me-2" @click="${this.toggleAddKeyForm}">
                    ${this.showAddKeyForm ? 'Cancel' : 'Import Key'}
                </button>

                ${this.showAddKeyForm ? html`
                    <form @submit="${this.addKey}" style="margin-top: 20px;">
                        <div class="mb-3">
                            <label for="private-key" class="form-label">Private Key (Hex)</label>
                            <textarea class="form-control" id="private-key" rows="3" required></textarea>
                        </div>
                        <button type="submit" class="btn btn-success">Import Key</button>
                    </form>
                ` : ''}
            </div>
        `;
    }
});
