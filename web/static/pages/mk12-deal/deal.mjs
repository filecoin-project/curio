import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { formatDate } from '/lib/dateutil.mjs';
import '/ux/epoch.mjs';

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
        if (this.data) {
            const entry = this.data;
            console.log(entry)
            const items = [
                {property: 'ID', value: entry.id},
                {property: 'Provider', value: entry.miner},
                {property: 'Sector Number', value: entry.sector},
                {property: 'Created At', value: formatDate(entry.created_at)},
                {property: 'Signed Proposal Cid', value: entry.signed_proposal_cid},
                {property: 'Offline', value: entry.offline},
                {property: 'Verified', value: entry.verified},
                {property: 'Is Legacy', value: entry.is_legacy},
                {property: 'Start Epoch', value: html`<pretty-epoch .epoch=${entry.start_epoch}></pretty-epoch>`},
                {property: 'End Epoch', value: html`<pretty-epoch .epoch=${entry.end_epoch}></pretty-epoch>`},
                {property: 'Client Peer ID', value: entry.client_peer_id},
                {property: 'Chain Deal ID', value: entry.chain_deal_id},
                {property: 'Publish CID', value: entry.publish_cid},
                {property: 'Piece CID', value: html`<a href="/pages/piece/?id=${entry.piece_cid}">${entry.piece_cid}</a>`},
                {property: 'Piece Size', value: entry.piece_size},
                {property: 'Fast Retrieval', value: entry.fast_retrieval},
                {property: 'Announce To IPNI', value: entry.announce_to_ipni},
                {property: 'Url', value: entry.url},
                {property: 'Url Headers', value: html`
                        <details>
                            <summary>[SHOW]</summary>
                            <pre>${JSON.stringify(entry.url_headers, null, 2)}</pre>
                        </details>
                    `},
                {property: 'Error', value: entry.error},
            ];
            return html`
          <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet"
              integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
              crossorigin="anonymous">
          <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
          <h2>Deal Details</h2>
          <table class="table table-dark">
              <thead>
              <tr>
                  <th>Property</th>
                  <th>Value</th>
              </tr>
              </thead>
              <tbody>
              ${items.map(item => html`
                  <tr>
                    <td>${item.property}</td>
                    <td>${item.value}</td>
                  </tr>
              `)}
              </tbody>
          </table>
      `;
        }
        return html`<p>Data is not available</p>`
    }
}
customElements.define('deal-details', DealDetails);
