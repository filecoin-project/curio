import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class UpgradeSectors extends LitElement {
    static properties = {
        data: { type: Array }
    };

    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('UpgradeSectors');
        super.requestUpdate();
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Address</th>
                    <th>Sector Number</th>
                    
                    <th>Encode</th>
                    <th>Prove</th>
                    <th>Submit</th>
                    <th>Move Storage</th>
                    
                    <th>Actions</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>f0${entry.SpID}</td>
                        <td>${entry.SectorNum}</td>

                        <td>${entry.AfterEncode ? 'Done' : entry.TaskIDEncode === null ? 'Not Started' : entry.TaskIDEncode}</td>
                        <td>${entry.AfterProve ? 'Done' : entry.TaskIDProve === null ? 'Not Started' : entry.TaskIDProve}</td>
                        <td>${entry.AfterSubmit ? 'Done' : entry.TaskIDSubmit === null ? 'Not Started' : entry.TaskIDSubmit}</td>
                        <td>${entry.AfterMoveStorage ? 'Done' : entry.TaskIDMoveStorage === null ? 'Not Started' : entry.TaskIDMoveStorage}</td>
                        
                        <td>
                            ${ '' /*todo: this button is a massive footgun, it should get some more safety*/ }
                            <button class="btn btn-primary" @click=${() => RPCCall('UpgradeResetTaskIDs', [entry.SpID, entry.SectorNum])}>unsafe:ResetTasks</button>
                        </td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
}
customElements.define('upgrade-sectors', UpgradeSectors);