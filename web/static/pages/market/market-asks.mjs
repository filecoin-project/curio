// market-asks.mjs

import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class MarketAsks extends LitElement {
    static properties = {
        actorList: { type: Array },
        spAsks: { type: Map },
        updatingSpID: { type: Number },
        newAsk: { type: Object },
    };

    constructor() {
        super();
        this.actorList = [];
        this.spAsks = new Map();
        this.updatingSpID = null;
        this.newAsk = {};
        this.loadData();
    }

    async loadData() {
        // Fetch actor list
        const addresses = await RPCCall('ActorList', []);
        // Map addresses to spIDs
        const spIDs = addresses.map((addr) => parseInt(addr.substring(2)));
        // Fetch storage asks for each spID
        this.spAsks.clear();
        await Promise.all(
            spIDs.map(async (spID) => {
                try {
                    const ask = await RPCCall('GetStorageAsk', [spID]);
                    this.spAsks.set(spID, ask);
                } catch (error) {
                    // No ask for this spID
                }
            })
        );
        this.actorList = spIDs;
        this.requestUpdate();
    }

    updateAsk(spID) {
        // Open the update form
        this.updatingSpID = spID;
        const existingAsk = this.spAsks.get(spID) || {};
        // Initialize newAsk with existing values or defaults
        this.newAsk = {
            SpID: spID,
            Price: existingAsk.Price || '',
            VerifiedPrice: existingAsk.VerifiedPrice || '',
            MinSize: existingAsk.MinSize || '',
            MaxSize: existingAsk.MaxSize || '',
            PriceFIL: '',
            VerifiedPriceFIL: '',
            CreatedAt: '',
            Expiry: '',
        };

        // If existing ask, convert AttoFIL/GiB/Epoch to FIL/TiB/Month
        if (existingAsk.Price) {
            this.newAsk.PriceFIL = this.attoFilToFilPerTiBPerMonth(existingAsk.Price);
        }
        if (existingAsk.VerifiedPrice) {
            this.newAsk.VerifiedPriceFIL = this.attoFilToFilPerTiBPerMonth(existingAsk.VerifiedPrice);
        }

        this.requestUpdate();
    }

    async saveAsk() {
        // Convert FIL/TiB/Month to AttoFIL/GiB/Epoch
        const priceAtto = this.filToAttoFilPerGiBPerEpoch(parseFloat(this.newAsk.PriceFIL));
        const verifiedPriceAtto = this.filToAttoFilPerGiBPerEpoch(parseFloat(this.newAsk.VerifiedPriceFIL));

        // Set CreatedAt and Expiry
        const now = Math.floor(Date.now() / 1000); // Unix timestamp in seconds
        this.newAsk.CreatedAt = now;
        // Set expiry to 30 days from now
        this.newAsk.Expiry = now + 30 * 24 * 60 * 60; // 30 days in seconds

        // Prepare the ask object to send to the server
        const askToSend = {
            SpID: this.newAsk.SpID,
            Price: priceAtto,
            VerifiedPrice: verifiedPriceAtto,
            MinSize: parseInt(this.newAsk.MinSize),
            MaxSize: parseInt(this.newAsk.MaxSize),
            CreatedAt: this.newAsk.CreatedAt,
            Expiry: this.newAsk.Expiry,
        };

        try {
            await RPCCall('SetStorageAsk', [askToSend]);
            // Reload data
            await this.loadData();
            // Close the form
            this.updatingSpID = null;
        } catch (error) {
            console.error('Failed to set storage ask:', error);
        }
    }

    // Conversion constants
    get EPOCHS_IN_MONTH() {
        return 86400;
    }
    get GIB_IN_TIB() {
        return 1024;
    }
    get ATTOFIL_PER_FIL() {
        return 1e18;
    }

    // Convert attoFIL/GiB/Epoch to FIL/TiB/Month
    attoFilToFilPerTiBPerMonth(attoFilPerGiBPerEpoch) {
        const filPerTiBPerMonth = (attoFilPerGiBPerEpoch * this.GIB_IN_TIB * this.EPOCHS_IN_MONTH) / this.ATTOFIL_PER_FIL;
        return filPerTiBPerMonth.toFixed(8); // Limit to 8 decimal places
    }

    // Convert FIL/TiB/Month to attoFIL/GiB/Epoch
    filToAttoFilPerGiBPerEpoch(filPerTiBPerMonth) {
        const attoFilPerGiBPerEpoch = (filPerTiBPerMonth * this.ATTOFIL_PER_FIL) / this.GIB_IN_TIB / this.EPOCHS_IN_MONTH;
        return Math.round(attoFilPerGiBPerEpoch); // Round to nearest integer
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

            <div>
                <h2>Storage Asks</h2>
                <table class="table table-dark table-striped">
                    <thead>
                        <tr>
                            <th>SP ID</th>
                            <th>Price (FIL/TiB/Month)</th>
                            <th>Price (attoFIL/GiB/Epoch)</th>
                            <th>Verified Price (FIL/TiB/Month)</th>
                            <th>Verified Price (attoFIL/GiB/Epoch)</th>
                            <th>Min Size (bytes)</th>
                            <th>Max Size (bytes)</th>
                            <th>Sequence</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${this.actorList.map((spID) => {
            const ask = this.spAsks.get(spID);
            return html`
                                <tr>
                                    <td>f0${spID}</td>
                                    <td>${ask ? this.attoFilToFilPerTiBPerMonth(ask.Price) : '-'}</td>
                                    <td>${ask ? ask.Price : '-'}</td>
                                    <td>${ask ? this.attoFilToFilPerTiBPerMonth(ask.VerifiedPrice) : '-'}</td>
                                    <td>${ask ? ask.VerifiedPrice : '-'}</td>
                                    <td>${ask ? ask.MinSize : '-'}</td>
                                    <td>${ask ? ask.MaxSize : '-'}</td>
                                    <td>${ask ? ask.Sequence : '-'}</td>
                                    <td>
                                        <button class="btn btn-primary" @click="${() => this.updateAsk(spID)}">
                                            ${ask ? 'Update' : 'Set'} Ask
                                        </button>
                                    </td>
                                </tr>
                            `;
        })}
                    </tbody>
                </table>
                ${this.updatingSpID !== null ? this.renderUpdateForm() : ''}
            </div>
        `;
    }

    renderUpdateForm() {
        return html`
            <div class="modal">
                <div class="modal-dialog">
                    <div class="modal-content">
                        <form @submit="${this.handleSubmit}">
                            <div class="modal-header">
                                <h5 class="modal-title">Set Storage Ask for SP f0${this.updatingSpID}</h5>
                                <button type="button" class="btn-close" @click="${() => (this.updatingSpID = null)}"></button>
                            </div>
                            <div class="modal-body">
                                <div class="mb-3">
                                    <label class="form-label">Price (FIL per TiB/Month)</label>
                                    <input
                                        type="number"
                                        step="any"
                                        class="form-control"
                                        .value="${this.newAsk.PriceFIL}"
                                        @input="${(e) => {
            this.newAsk.PriceFIL = e.target.value;
            this.newAsk.Price = this.filToAttoFilPerGiBPerEpoch(parseFloat(e.target.value));
        }}"
                                        required
                                    />
                                    <div class="form-text">
                                        Corresponds to ${this.newAsk.Price || '-'} attoFIL/GiB/Epoch
                                    </div>
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Verified Price (FIL per TiB/Month)</label>
                                    <input
                                        type="number"
                                        step="any"
                                        class="form-control"
                                        .value="${this.newAsk.VerifiedPriceFIL}"
                                        @input="${(e) => {
            this.newAsk.VerifiedPriceFIL = e.target.value;
            this.newAsk.VerifiedPrice = this.filToAttoFilPerGiBPerEpoch(parseFloat(e.target.value));
        }}"
                                        required
                                    />
                                    <div class="form-text">
                                        Corresponds to ${this.newAsk.VerifiedPrice || '-'} attoFIL/GiB/Epoch
                                    </div>
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Min Piece Size (bytes)</label>
                                    <input
                                        type="number"
                                        class="form-control"
                                        .value="${this.newAsk.MinSize}"
                                        @input="${(e) => (this.newAsk.MinSize = e.target.value)}"
                                        required
                                    />
                                </div>
                                <div class="mb-3">
                                    <label class="form-label">Max Piece Size (bytes)</label>
                                    <input
                                        type="number"
                                        class="form-control"
                                        .value="${this.newAsk.MaxSize}"
                                        @input="${(e) => (this.newAsk.MaxSize = e.target.value)}"
                                        required
                                    />
                                </div>
                            </div>
                            <div class="modal-footer">
                                <button
                                    type="button"
                                    class="btn btn-secondary"
                                    @click="${() => (this.updatingSpID = null)}"
                                >
                                    Cancel
                                </button>
                                <button type="submit" class="btn btn-primary">Save Ask</button>
                            </div>
                        </form>
                    </div>
                </div>
            </div>
            <div class="modal-backdrop"></div>
        `;
    }

    handleSubmit(e) {
        e.preventDefault();
        this.saveAsk();
    }

    static styles = css`
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

        .modal-header,
        .modal-footer {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 1rem;
            border-bottom: 1px solid var(--color-form-default, #808080);
        }

        .modal-header {
            border-bottom: none;
        }

        .modal-body {
            position: relative;
            padding: 1rem;
        }

        .form-label {
            margin-bottom: 0.5rem;
            color: var(--color-text-primary, #FFF);
        }

        .form-control {
            display: block;
            width: 100%;
            padding: 0.375rem 0.75rem;
            font-size: 1rem;
            line-height: 1.5;
            color: var(--color-text-primary, #FFF);
            background-color: var(--color-form-group-1, #484848);
            background-clip: padding-box;
            border: 1px solid var(--color-form-default, #808080);
            border-radius: 0.25rem;
            transition: border-color 0.15s ease-in-out, box-shadow 0.15s ease-in-out;
        }

        .form-control:focus {
            color: var(--color-text-primary, #FFF);
            background-color: var(--color-form-group-1, #484848);
            border-color: var(--color-primary-light, #8BEFE0);
            outline: 0;
            box-shadow: 0 0 0 0.2rem rgba(29, 200, 204, 0.25);
        }

        .form-text {
            color: var(--color-text-primary, #FFF);
            font-size: 0.875em;
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

        /* Responsive adjustments */
        @media (max-width: 576px) {
            .modal-dialog {
                max-width: 100%;
                margin: 0;
                height: 100%;
                display: flex;
                flex-direction: column;
                justify-content: center;
            }

            .modal-content {
                height: auto;
                border-radius: 0;
            }
        }
    `;
}

customElements.define('market-asks', MarketAsks);