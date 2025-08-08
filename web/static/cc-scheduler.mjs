import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class CCScheduler extends LitElement {
  static properties = {
    rows: { type: Array },
    loading: { type: Boolean },
    error: { type: String },
    form: { type: Object },
    showAdd: { type: Boolean },
  };

  constructor() {
    super();
    this.rows = [];
    this.loading = false;
    this.error = '';
    this.form = { sp: '', toSeal: 0, weight: 1000, enabled: true };
    this.showAdd = false;
    this.load();
  }

  async load() {
    this.loading = true;
    this.requestUpdate();
    try {
      this.rows = await RPCCall('SectorCCScheduler') || [];
    } catch (e) {
      this.error = String(e?.message || e);
    } finally {
      this.loading = false;
      this.requestUpdate();
    }
  }

  async upsert() {
    this.error = '';
    try {
      const sp = String(this.form.sp || '').trim();
      await RPCCall('SectorCCSchedulerUpsert', [sp, Number(this.form.toSeal)||0, Number(this.form.weight)||1000, !!this.form.enabled]);
      this.form = { sp: '', toSeal: 0, weight: 1000, enabled: true };
      this.showAdd = false;
      this.load();
    } catch (e) {
      this.error = String(e?.message || e);
    }
  }

  async remove(sp) {
    try {
      await RPCCall('SectorCCSchedulerDelete', [String(sp)]);
      this.load();
    } catch (e) {
      this.error = String(e?.message || e);
    }
  }

  renderAddRow() {
    if (!this.showAdd) return '';
    return html`
      <tr>
        <td>
          <form @submit=${e=>{e.preventDefault(); this.upsert();}}>
            <input type="text" placeholder="f0xxx" .value=${this.form.sp}
                   @input=${e=>this.form.sp = e.target.value} />
          </form>
        </td>
        <td>-</td>
        <td>
          <input type="number" .value=${String(this.form.toSeal)}
                 @input=${e=>this.form.toSeal = e.target.value} />
        </td>
        <td>
          <input type="number" .value=${String(this.form.weight)}
                 @input=${e=>this.form.weight = e.target.value} />
        </td>
        <td>
          <input type="checkbox" id="cc-enabled" ?checked=${this.form.enabled}
                 @change=${e=>this.form.enabled = e.target.checked}>
        </td>
        <td>
          <button class="btn btn-sm btn-primary" @click=${()=>this.upsert()} title="Add / Update">Add</button>
          <button class="btn btn-sm btn-secondary" @click=${()=>{this.showAdd=false;}} title="Cancel">Cancel</button>
        </td>
      </tr>
      ${this.error ? html`
        <tr>
          <td colspan="6">
            <div class="alert alert-danger" style="margin-top:6px;">${this.error}</div>
          </td>
        </tr>
      ` : ''}
    `;
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
      <div class="info-block container">
        <h2>CC Scheduler</h2>
        <div class="table-wrapper">
          <table class="table table-dark table-sm">
            <thead>
              <tr>
                <th>SP</th>
                <th>Requested</th>
                <th>To Seal</th>
                <th>Weight</th>
                <th>Enabled</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              ${this.rows.map(r => html`
                <tr>
                  <td><b>${r.SPAddress}</b></td>
                  <td>${r.RequestedSize}</td>
                  <td>${r.ToSeal}</td>
                  <td>${r.Weight}</td>
                  <td>${r.Enabled ? 'Yes' : 'No'}</td>
                  <td>
                    <button class="btn btn-sm btn-danger" @click=${()=>this.remove(r.SPAddress)}>Delete</button>
                  </td>
                </tr>
              `)}
              ${this.renderAddRow()}
              <tr>
                <td colspan="6">
                  <a href="#" @click=${(e)=>{e.preventDefault(); this.showAdd = true;}}>add</a>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    `;
  }
}

customElements.define('cc-scheduler', CCScheduler); 