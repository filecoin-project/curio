import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class PendingDeals extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('DealsPending');
        this.requestUpdate();

        // Poll for updates
        setInterval(async () => {
            this.data = await RPCCall('DealsPending');
            this.requestUpdate();
        }, 3000);
    }

    async sealNow(entry) {
        await RPCCall('DealsSealNow', [entry.Actor, entry.SectorNumber]);
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet"
                  integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
                  crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <h2>Wait Deal Sector Pieces
                <button class="info-btn">
                    <!-- Inline SVG icon for the info button -->
                    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-info-circle" viewBox="0 0 16 16">
                        <path d="M8 15A7 7 0 1 1 8 1a7 7 0 0 1 0 14m0 1A8 8 0 1 0 8 0a8 8 0 0 0 0 16"/>
                        <path d="m8.93 6.588-2.29.287-.082.38.45.083c.294.07.352.176.288.469l-.738 3.468c-.194.897.105 1.319.808 1.319.545 0 1.178-.252 1.465-.598l.088-.416c-.2.176-.492.246-.686.246-.275 0-.375-.193-.304-.533zM9 4.5a1 1 0 1 1-2 0 1 1 0 0 1 2 0"/>
                    </svg>
                    <span class="tooltip-text">
              List of pieces which are assigned to a sector but sector has not yet entered the sealing/upgrading pipeline.
            </span>
                </button>
            </h2>
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Address</th>
                    <th>Sector Number</th>
                    <th>Piece CID</th>
                    <th>Piece Size</th>
                    <th>Created At</th>
                    <th>SnapDeals</th>
                    <th>Control</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>${entry.Miner}</td>
                        <td><a href="/pages/sector/?sp=${entry.Miner}&id=${entry.SectorNumber}">${entry.SectorNumber}</a></td>
                        <td><a href="/pages/piece/?id=${entry.PieceCID}">${entry.PieceCID}</a></td>
                        <td>${entry.PieceSizeStr}</td>
                        <td>${entry.CreatedAtStr}</td>
                        <td>
                            ${entry.SnapDeals ? "Yes" : "No"}
                        </td>
                        <td>
                            <button @click="${() => this.sealNow(entry)}" class="btn btn-primary btn-sm">Seal Now
                            </button>
                        </td>
                    </tr>
                `)}
                </tbody>
            </table>
        `;
    }

    static styles = css`
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
customElements.define('pending-deals', PendingDeals);
