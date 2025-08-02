import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('cluster-machines', class ClusterMachines extends LitElement {
    static properties = {
        data: { type: Array },
        detailed: { type: Boolean }
    };

    constructor() {
        super();
        this.data = [];
        this.detailed = false; // Default to not detailed
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('ClusterMachines');
        setTimeout(() => this.loadData(), 5000);
        this.requestUpdate();
    }

    _toggleDetailed(e) {
        this.detailed = e.target.checked;
    }

    async _cordon(id) {
        await RPCCall('Cordon', [id]);
        this.loadData();
    }

    async _uncordon(id) {
        await RPCCall('Uncordon', [id]);
        this.loadData();
    }

    render() {
        return html`
            <link
                href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                rel="stylesheet"
                integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
                crossorigin="anonymous"
            />
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <div class="row">
                <div class="col-md-auto" style="max-width: 1000px">
                    <div class="info-block">
                        <h2>Cluster Machines</h2>
                        <div class="form-check mb-3">
                            <input
                            class="form-check-input"
                                type="checkbox"
                                id="detailedCheckbox"
                                .checked=${this.detailed}
                                @change=${this._toggleDetailed}
                            />
                            <label class="form-check-label" for="detailedCheckbox">
                                Detailed
                            </label>
                        </div>
                        <table class="table table-dark">
                            <thead>
                                <tr>
                                    <th>Name</th>
                                    <th>Host</th>
                                    <th>ID</th>
                                    ${
                                      this.detailed
                                        ? html`<th>CPUs</th>
                                               <th>RAM</th>
                                               <th>GPUs</th>`
                                        : ''
                                    }
                                    <th>Last Contact</th>
                                    <th>Uptime</th>
                                    <th>Scheduling</th>
                                    ${
                                      this.detailed
                                        ? html`<th>Tasks Supported</th>
                                               <th>Layers Enabled</th>`
                                        : ''
                                    }
                                </tr>
                            </thead>
                            <tbody>
                                ${this.data.map(item => html`
                                <tr>
                                    <td>
                                        <a href="/pages/node_info/?id=${item.ID}">
                                            ${item.Name}
                                        </a>
                                    </td>
                                    <td>
                                        <a href="/pages/node_info/?id=${item.ID}">
                                            ${item.Address}
                                        </a>
                                    </td>
                                    <td>${item.ID}</td>
                                    ${
                                      this.detailed
                                        ? html`
                                          <td>${item.Cpu}</td>
                                          <td>${item.RamHumanized}</td>
                                          <td>${item.Gpu}</td>
                                          `
                                        : ''
                                    }
                                    <td>${item.SinceContact}</td>
                                    <td>${item.Uptime}</td>

                                    <td>
                                        <a href="javascript:void(0)" @click=${() => this._cordon(item.ID)} style="${item.Unschedulable ? 'opacity: 0.3; pointer-events: none;' : ''}" >
                                            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-pause" viewBox="0 0 16 16">
                                                <path d="M6 3.5a.5.5 0 0 1 .5.5v8a.5.5 0 0 1-1 0V4a.5.5 0 0 1 .5-.5m4 0a.5.5 0 0 1 .5.5v8a.5.5 0 0 1-1 0V4a.5.5 0 0 1 .5-.5"/>
                                            </svg>
                                        </a>
                                        <a href="javascript:void(0)" @click=${() => this._uncordon(item.ID)} style="${!item.Unschedulable ? 'opacity: 0.3; pointer-events: none;' : ''}">
                                            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-play" viewBox="0 0 16 16">
                                                <path d="M10.804 8 5 4.633v6.734zm.792-.696a.802.802 0 0 1 0 1.392l-6.363 3.692C4.713 12.69 4 12.345 4 11.692V4.308c0-.653.713-.998 1.233-.696z"/>
                                            </svg>
                                        </a>
                                        ${!item.Unschedulable
                                            ? html`<span class="success" style="white-space: nowrap;">enabled</span>`
                                            : html`
                                                <span class="warning" style="white-space: nowrap;">
                                                    ${
                                                      item.RunningTasks > 0
                                                        ? html`cordoned (${item.RunningTasks} tasks still running)`
                                                        : html`cordoned`
                                                    }
                                                </span>
                                            `
                                        }
                                    </td>
                                    ${
                                      this.detailed
                                        ? html`
                                          <td>
                                              ${item.Tasks.split(',').map(task => html`
                                              <a href="/pages/task/?name=${task}">${task}</a> `)}
                                          </td>
                                          <td>
                                              ${item.Layers.split(',').map(layer => html`
                                              <a href="/config/edit.html?layer=${layer}">${layer}</a> `)}
                                          </td>
                                          `
                                        : ''
                                    }
                                </tr>
                                `)}
                            </tbody>
                        </table>
                    </div>
                </div>
                <div class="col-md-auto"></div>
            </div>
        `;
    }
});
