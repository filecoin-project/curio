import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class WinStats extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('WinStats') || [];
        setTimeout(() => this.loadData(), 2 * 60 * 1000); // 2 minutes
        this.requestUpdate();
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Address</th>
                    <th>Epoch</th>
                    <th>Block</th>
                    <th>Task Success</th>
                    <th>Submitted At</th>
                    <th>Compute Time</th>
                    <th>Included</th>
                </tr>
                </thead>
                <tbody>
                    ${this.data.map(entry => html`
                    <tr>
                        <td>f0${entry.Actor}</td>
                        <td>${entry.Epoch}</td>
                        <td><abbr title="${entry.Block}">...${entry.Block.slice(-10)}</abbr></td>
                        <td>${entry.TaskSuccess}</td>
                        <td>${entry.SubmittedAtStr}</td>
                        <td>${entry.ComputeTime}</td>
                        <td>${entry.IncludedStr}</td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
}

customElements.define('win-stats', WinStats);
