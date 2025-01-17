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

    render() {
        return html`
            <link
                href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                rel="stylesheet"
                integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
                crossorigin="anonymous"
            />
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <!-- Toggle for Detailed View -->
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

            <div class="row">
                <div class="col-md-auto" style="max-width: 1000px">
                    <div class="info-block">
                        <h2>Cluster Machines</h2>
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
                                    <th>Schedulable</th>
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
                                        ${!item.Unschedulable
                                            ? html`<span class="success">ok</span>`
                                            : html`
                                                <span class="warning">
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
