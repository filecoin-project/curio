import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('cluster-tasks', class ClusterTasks extends LitElement {
    static get properties() {
        return {
            data: { type: Array },
            showBackgroundTasks: { type: Boolean },
        };
    }

    constructor() {
        super();
        this.data = [];
        this.showBackgroundTasks = false;
        this.loadData();
    }

    async loadData() {
        this.data = (await RPCCall('ClusterTaskSummary')) || [];
        setTimeout(() => this.loadData(), 1000);
        this.requestUpdate();
    }

    toggleShowBackgroundTasks(e) {
        this.showBackgroundTasks = e.target.checked;
    }

    render() {
        return html`
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3"
        crossorigin="anonymous"
      />
      <link
        rel="stylesheet"
        href="/ux/main.css"
        onload="document.body.style.visibility = 'initial'"
      />

      <!-- Toggle for showing background tasks -->
      <label>
        <input
          type="checkbox"
          @change=${this.toggleShowBackgroundTasks}
          ?checked=${this.showBackgroundTasks}
        />
        Show background tasks
      </label>

<!--      todo: workaround for the jumping width; fix should introduce constraints for all of the columns -->
      <table class="table table-dark" style="min-width: 520px">
        <thead>
          <tr>
            <th>SpID</th>
            <th style="min-width: 128px">Task</th>
            <th>ID</th>
            <th>Posted</th>
            <th>Owner</th>
          </tr>
        </thead>
        <tbody>
          ${this.data
            .filter(
                (entry) =>
                    this.showBackgroundTasks || !entry.Name.startsWith('bg:')
            )
            .map(
                (entry) => html`
                <tr>
                  <td>${entry.SpID ? entry.Miner : 'n/a'}</td>
                  <td>${entry.Name}</td>
                  <td>${entry.ID}</td>
                  <td>${entry.SincePostedStr}</td>
                  <td>
                    ${entry.OwnerID
                    ? html`<a href="/pages/node_info/?id=${entry.OwnerID}"
                          >${entry.Owner}</a
                        >`
                    : ''}
                  </td>
                </tr>
              `
            )}
        </tbody>
      </table>
    `;
    }
});
