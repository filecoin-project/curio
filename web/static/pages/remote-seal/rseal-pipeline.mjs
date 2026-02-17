import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/task.mjs';

class RSealPipelineElement extends LitElement {
  static properties = {
    providerPipeline: { type: Array },
    clientPipeline: { type: Array },
  };

  constructor() {
    super();
    this.providerPipeline = [];
    this.clientPipeline = [];
    this.loadData();
    this.refreshInterval = setInterval(() => this.loadData(), 5000);
  }

  createRenderRoot() {
    return this;
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    if (this.refreshInterval) clearInterval(this.refreshInterval);
  }

  async loadData() {
    try {
      this.providerPipeline = await RPCCall('RSealProviderPipeline', []) || [];
    } catch (err) {
      console.error('Failed to load provider pipeline:', err);
      this.providerPipeline = [];
    }
    try {
      this.clientPipeline = await RPCCall('RSealClientPipeline', []) || [];
    } catch (err) {
      console.error('Failed to load client pipeline:', err);
      this.clientPipeline = [];
    }
    this.requestUpdate();
  }

  renderStage(done) {
    return done
      ? html`<span class="badge bg-success">Done</span>`
      : html`<span class="badge bg-secondary">-</span>`;
  }

  renderTaskStage(taskId, done) {
    if (taskId) {
      return html`<task-status .taskId=${taskId}></task-status>`;
    }
    return this.renderStage(done);
  }

  render() {
    return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet"
        integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous" />
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

      <div>
        <h3>Pipeline Status</h3>

        <h4>Provider Pipeline</h4>
        ${this.providerPipeline.length > 0 ? html`
          <div class="table-responsive">
            <table class="table table-dark table-striped table-hover table-sm">
              <thead>
                <tr>
                  <th>SP</th>
                  <th>Sector</th>
                  <th>Partner</th>
                  <th>SDR</th>
                  <th>TreeD</th>
                  <th>TreeC</th>
                  <th>TreeR</th>
                  <th>Notify</th>
                  <th>C1</th>
                  <th>Finalize</th>
                  <th>Cleanup</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                ${this.providerPipeline.map(r => html`
                  <tr class="${r.failed ? 'table-danger' : ''}">
                    <td>f0${r.sp_id}</td>
                    <td>${r.sector_number}</td>
                    <td>${r.partner_name}</td>
                    <td>${this.renderStage(r.after_sdr)}</td>
                    <td>${this.renderStage(r.after_tree_d)}</td>
                    <td>${this.renderStage(r.after_tree_c)}</td>
                    <td>${this.renderStage(r.after_tree_r)}</td>
                    <td>${this.renderStage(r.after_notify_client)}</td>
                    <td>${this.renderStage(r.after_c1_supplied)}</td>
                    <td>${this.renderStage(r.after_finalize)}</td>
                    <td>${this.renderStage(r.after_cleanup)}</td>
                    <td>${r.failed ? html`<span class="badge bg-danger" title="${r.failed_reason_msg}">Failed</span>` : html`<span class="badge bg-info">Active</span>`}</td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        ` : html`<p>No active provider pipeline rows.</p>`}

        <h4>Client Pipeline</h4>
        ${this.clientPipeline.length > 0 ? html`
          <div class="table-responsive">
            <table class="table table-dark table-striped table-hover table-sm">
              <thead>
                <tr>
                  <th>SP</th>
                  <th>Sector</th>
                  <th>Provider</th>
                  <th>SDR</th>
                  <th>TreeD</th>
                  <th>TreeC</th>
                  <th>TreeR</th>
                  <th>Fetch</th>
                  <th>Cleanup</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                ${this.clientPipeline.map(r => html`
                  <tr class="${r.failed ? 'table-danger' : ''}">
                    <td>f0${r.sp_id}</td>
                    <td>${r.sector_number}</td>
                    <td>${r.provider_name}</td>
                    <td>${this.renderTaskStage(r.task_id_sdr, r.after_sdr)}</td>
                    <td>${this.renderTaskStage(r.task_id_tree_d, r.after_tree_d)}</td>
                    <td>${this.renderTaskStage(r.task_id_tree_c, r.after_tree_c)}</td>
                    <td>${this.renderTaskStage(r.task_id_tree_r, r.after_tree_r)}</td>
                    <td>${this.renderTaskStage(r.task_id_fetch, r.after_fetch)}</td>
                    <td>${this.renderTaskStage(r.task_id_cleanup, r.after_cleanup)}</td>
                    <td>${r.failed ? html`<span class="badge bg-danger" title="${r.failed_reason_msg}">Failed</span>` : html`<span class="badge bg-info">Active</span>`}</td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        ` : html`<p>No active client pipeline rows.</p>`}
      </div>
    `;
  }
}

customElements.define('rseal-pipeline', RSealPipelineElement);
