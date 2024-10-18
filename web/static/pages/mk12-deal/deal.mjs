import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class DealDetails extends LitElement {
    constructor() {
        super();
        this.loadData();
    }

    async loadData() {
        try {
            const params = new URLSearchParams(window.location.search);
            this.data = await RPCCall('StorageDealInfo', [params.get('id')]);
            setTimeout(() => this.loadData(), 10000);
            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load deal details:', error);
        }
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet"
                  integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
                  crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Property</th>
                    <th>Value</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.flatMap(entry => [
                    {property: 'ID', value: entry.UUID},
                    {property: 'Provider', value: entry.Miner},
                    {property: 'Sector Number', value: entry.Sector},
                    {property: 'Created At', value: entry.CreatedAt},
                    {property: 'Signed Proposal Cid', value: entry.SignedProposalCid},
                    {property: 'Offline', value: entry.Offline},
                    {property: 'Verified', value: entry.Verified},
                    {property: 'Is Legacy', value: entry.IsLegacy},    
                    {property: 'Start Epoch', value: entry.StartEpoch},
                    {property: 'End Epoch', value: entry.EndEpoch},
                    {property: 'Client Peer ID', value: entry.ClientPeerId},
                    {property: 'Chain Deal ID', value: entry.ChainDealId},
                    {property: 'Publish CID', value: entry.PublishCid},
                    {property: 'Piece CID', value: entry.PieceCid},
                    {property: 'Piece Size', value: entry.PieceSize},
                    {property: 'Fast Retrieval', value: entry.FastRetrieval},
                    {property: 'Announce To IPNI', value: entry.AnnounceToIpni},
                    {property: 'Url', value: entry.Url},
                    {property: 'Url Headers', value: JSON.stringify(entry.UrlHeaders, null, 2)},
                    {property: 'Error', value: entry.Error},
                ]).map(item => html`
                    <tr>
                        <td>${item.property}</td>
                        <td>${item.value}</td>
                    </tr>
                `)}
                </tbody>
            </table>
        `;
    }
}
customElements.define('deal-details', DealDetails);
