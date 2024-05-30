import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class HarmonyMachineTable extends LitElement {
    constructor() {
        super();
        this.machines = [];
        // url ?name=taskName
        this.taskName = new URLSearchParams(window.location.search).get('name');
        this.loadMachines();
    }

    async loadMachines() {
        try {
            this.machines = await RPCCall('HarmonyTaskMachines', [this.taskName]);
            this.requestUpdate();
        } catch (error) {
            console.error('Error fetching machine data:', error);
        }
    }

    static get styles() {
        return css`
    `;
    }

    render() {
        return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
      <table class="table table-dark">
        <thead>
          <tr>
            <th>Name</th>
            <th>Machine Address</th>
            <th>Actors</th>
          </tr>
        </thead>
        <tbody>
          ${this.machines.map(machine => html`
            <tr>
              <td><a href="/hapi/node/${machine.MachineID}">${machine.Name}</a></td>
              <td><a href="/hapi/node/${machine.MachineID}">${machine.MachineAddr}</a></td>
              <td>${machine.Actors}</td>
            </tr>
          `)}
        </tbody>
      </table>
    `;
    }
}

customElements.define('harmony-task-machines', HarmonyMachineTable);
