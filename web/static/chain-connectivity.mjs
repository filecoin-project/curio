import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { pollRPC } from '/lib/poll.mjs';

window.customElements.define('chain-connectivity', class MyElement extends LitElement {
    static properties = {
        data: { type: Array },
    };

    constructor() {
        super();
        this.data = [];
        pollRPC(async () => {
            this.data = await RPCCall('SyncerState');
        }, 30 * 1000);
    }

    static get styles() {
        return [css`
        :host {
            box-sizing: border-box; /* Keep padding and border inside the element width */
        }
    `];
    }
    render() {
        return html`
  <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
    <link rel="stylesheet" href="/ux/main.css">
  <link href="https://fonts.cdnfonts.com/css/metropolis-2" rel="stylesheet" crossorigin="anonymous">
  <table class="table table-dark">
    <thead>
        <tr>
            <th>RPC Address</th>
            <th>Reachability</th>
            <th>Sync Status</th>
            <th>Version</th>
        </tr>
    </thead>
    <tbody>
        ${this.data.map(item => html`
        <tr>
            <td>${item.Address}</td>
            <td>${item.Reachable ? html`<span class="success">ok</span>` : html`<span class="error">FAIL</span>`}</td>
            <td>${item.SyncState === "ok" ? html`<span class="success">ok</span>` : html`<span class="warning">No${item.SyncState ? ', ' + item.SyncState : ''}</span>`}</td>
            <td>${item.Version}</td>
        </tr>
        `)}
    </tbody>
  </table>`
    }
});
