import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class ActorSummary extends LitElement {
    static styles = css`
    .deadline-box {
            display: grid;
            grid-template-columns: repeat(16, auto);
            grid-template-rows: repeat(3, auto);
            grid-gap: 1px;
        }

        .deadline-entry {
            width: 10px;
            height: 10px;
            background-color: grey;
            margin: 1px;
        }

        .deadline-entry-cur {
            border-bottom: 3px solid deepskyblue;
            height: 7px;
        }

        .deadline-proven {
            background-color: green;
        }

        .deadline-partially-faulty {
            background-color: yellow;
        }

        .deadline-faulty {
            background-color: red;
        }
    `;

    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('ActorSummary');
        this.requestUpdate();

        // Poll for updates
        setInterval(async () => {
            this.data = await RPCCall('ActorSummary');
            this.requestUpdate();
        }, 30000);
    }

    renderWins(win1, win7, win30) {
        return html`
            <table>
                <tr><td>1day:&nbsp; ${win1}</td></tr>
                <tr><td>7day:&nbsp; ${win7}</td></tr>
                <tr><td>30day: ${win30}</td></tr>
            </table>
        `;
    }

    renderDeadlines(deadlines) {
        return html`
            <div class="deadline-box">
                ${deadlines.map(d => html`
                    <div class="deadline-entry
                        ${d.Current ? 'deadline-entry-cur' : ''}
                        ${d.Proven ? 'deadline-proven' : ''}
                        ${d.PartFaulty ? 'deadline-partially-faulty' : ''}
                        ${d.Faulty ? 'deadline-faulty' : ''}
                    "></div>
                `)}
            </div>
        `;
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Address</th>
                    <th>Source Layer</th>
                    <th>QaP</th>
                    <th>Deadlines</th>
                    <th>Balance</th>
                    <th>Available</th>
                    <th>Worker</th>
                    <th style="min-width: 100px">Wins</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>${entry.Address}</td>
                        <td>
                            ${entry.CLayers.map(layer => html`<span>${layer} </span>`)}
                        </td>
                        <td>${entry.QualityAdjustedPower}</td>
                        <td>${this.renderDeadlines(entry.Deadlines)}</td>
                        <td>${entry.ActorBalance}</td>
                        <td>${entry.ActorAvailable}</td>
                        <td>${entry.WorkerBalance}</td>
                        <td>${this.renderWins(entry.Win1, entry.Win7, entry.Win30)}</td>
                    </tr>
                `)}
                </tbody>
            </table>
        `;
    }
}

customElements.define('actor-summary', ActorSummary);
