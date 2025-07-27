import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/task.mjs';
import '/ux/message.mjs';

class BalanceManager extends LitElement {
  static properties = {
    rules: { type: Array },
    editingId: { type: Number },
    editLow: { type: String },
    editHigh: { type: String },
    adding: { type: Boolean },
    newSubject: { type: String },
    newSecond: { type: String },
    newAction: { type: String },
    newLow: { type: String },
    newHigh: { type: String },
    _refreshHandle: { state: false }
  };

  constructor() {
    super();
    this.rules = [];
    this.editingId = null;
    this.editLow = '';
    this.editHigh = '';
    this.adding = false;
    this.newSubject = '';
    this.newSecond = '';
    this.newAction = 'transfer';
    this.newLow = '';
    this.newHigh = '';
    this.loadRules();
    this._refreshHandle = null;
  }

  connectedCallback() {
    super.connectedCallback();
    this._refreshHandle = setInterval(() => this.loadRules(), 5000);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    if (this._refreshHandle) {
      clearInterval(this._refreshHandle);
      this._refreshHandle = null;
    }
  }

  async loadRules() {
    try {
      const data = await RPCCall('BalanceMgrRules', []);
      this.rules = data || [];
    } catch (err) {
      console.error('Failed to fetch balance-manager rules:', err);
      alert('Failed to fetch balance-manager rules: ' + err.message);
    }
  }

  startEdit(rule) {
    this.editingId = rule.id;
    // Strip possible " FIL" suffix for easier editing
    this.editLow = (rule.low_watermark || '').replace(/\s*FIL$/i, '').trim();
    this.editHigh = (rule.high_watermark || '').replace(/\s*FIL$/i, '').trim();
  }

  cancelEdit() {
    this.editingId = null;
    this.editLow = '';
    this.editHigh = '';
  }

  startAdd() {
    this.adding = true;
    this.newSubject = '';
    this.newSecond = '';
    this.newAction = 'transfer';
    this.newLow = '';
    this.newHigh = '';
  }

  cancelAdd() {
    this.adding = false;
  }

  async saveAdd() {
    try {
      await RPCCall('BalanceMgrRuleAdd', [
        this.newSubject,
        this.newSecond,
        this.newAction,
        this.newLow,
        this.newHigh,
      ]);
      this.cancelAdd();
      await this.loadRules();
    } catch (err) {
      console.error('Failed to add rule:', err);
      alert('Failed to add rule: ' + err.message);
    }
  }

  async saveEdit() {
    try {
      await RPCCall('BalanceMgrRuleUpdate', [this.editingId, this.editLow, this.editHigh]);
      this.cancelEdit();
      await this.loadRules();
    } catch (err) {
      console.error('Failed to update rule:', err);
      alert('Failed to update rule: ' + err.message);
    }
  }

  async deleteRule(id) {
    if (!confirm('Delete this rule?')) return;
    try {
      await RPCCall('BalanceMgrRuleRemove', [id]);
      await this.loadRules();
    } catch (err) {
      console.error('Failed to delete rule:', err);
      alert('Failed to delete rule: ' + err.message);
    }
  }

  render() {
    return html`
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        crossorigin="anonymous"
      />
      <link rel="stylesheet" href="/ux/main.css" />

      <div class="container py-3">
        <h2>Balance-Manager Rules</h2>
        <button class="btn btn-primary btn-sm mb-2" @click=${this.startAdd}>Add Rule</button>
        ${this.adding
          ? html`
              <div class="card card-body bg-dark text-light mb-3">
                <div class="row g-2 align-items-end">
                  <div class="col-md-2">
                    <label class="form-label mb-0">Subject</label>
                    <input class="form-control form-control-sm" .value=${this.newSubject} @input=${(e) => (this.newSubject = e.target.value)} />
                  </div>
                  <div class="col-md-2">
                    <label class="form-label mb-0">Second</label>
                    <input class="form-control form-control-sm" .value=${this.newSecond} @input=${(e) => (this.newSecond = e.target.value)} />
                  </div>
                  <div class="col-md-3">
                    <label class="form-label mb-0">Action</label>
                    <select class="form-select form-select-sm" .value=${this.newAction} @change=${(e) => (this.newAction = e.target.value)}>
                      <option value="requester">Keep Subject Above Low</option>
                      <option value="active-provider">Keep Subject Below High</option>
                    </select>
                  </div>
                  <div class="col-md-1">
                    <label class="form-label mb-0">Low WM (FIL)</label>
                    <input class="form-control form-control-sm" .value=${this.newLow} @input=${(e) => (this.newLow = e.target.value)} />
                  </div>
                  <div class="col-md-1">
                    <label class="form-label mb-0">High WM (FIL)</label>
                    <input class="form-control form-control-sm" .value=${this.newHigh} @input=${(e) => (this.newHigh = e.target.value)} />
                  </div>
                  <div class="col-md-2 d-flex gap-2">
                    <button class="btn btn-success btn-sm" @click=${this.saveAdd}>Save</button>
                    <button class="btn btn-secondary btn-sm" @click=${this.cancelAdd}>Cancel</button>
                  </div>
                </div>
              </div>`
          : ''}
        ${this.rules.length === 0
          ? html`<p class="text-muted">No balance-manager rules found.</p>`
          : html`
              <table class="table table-dark table-striped table-sm">
                <thead>
                  <tr>
                    <th style="white-space: nowrap;">ID</th>
                    <th style="white-space: nowrap;">Subject</th>
                    <th style="white-space: nowrap;">Second</th>
                    <th style="white-space: nowrap;">Action</th>
                    <th style="white-space: nowrap;">Low&nbsp;WM</th>
                    <th style="white-space: nowrap;">High&nbsp;WM</th>
                    <th style="white-space: nowrap;">Last&nbsp;Msg</th>
                    <th style="white-space: nowrap;">Task</th>
                    <th style="white-space: nowrap;">Action</th>
                  </tr>
                </thead>
                <tbody>
                  ${this.rules.map((rule) => {
                    return html`
                      <tr>
                        <td style="white-space: nowrap;">${rule.id}</td>
                        <td style="white-space: nowrap;"><cu-wallet wallet_id="${rule.subject_address}"></cu-wallet></td>
                        <td style="white-space: nowrap;"><cu-wallet wallet_id="${rule.second_address}"></cu-wallet></td>
                        <td style="white-space: nowrap;">${rule.action_type === 'requester' ? 'Keep Subject Above Low' : rule.action_type === 'active-provider' ? 'Keep Subject Below High' : rule.action_type}</td>
                        <td style="white-space: nowrap;">
                          ${this.editingId === rule.id
                            ? html`<input
                                class="form-control form-control-sm"
                                .value=${this.editLow}
                                @input=${(e) => (this.editLow = e.target.value)}
                              />`
                            : rule.low_watermark}
                        </td>
                        <td style="white-space: nowrap;">
                          ${this.editingId === rule.id
                            ? html`<input
                                class="form-control form-control-sm"
                                .value=${this.editHigh}
                                @input=${(e) => (this.editHigh = e.target.value)}
                              />`
                            : rule.high_watermark}
                        </td>
                        <td style="white-space: nowrap;">
                          ${rule.last_msg_cid
                            ? html`<fil-message cid="${rule.last_msg_cid}"></fil-message>`
                            : '-'}
                        </td>
                        <td style="white-space: nowrap;">
                            ${rule.task_id ? html`<task-status taskId=${rule.task_id}></task-status>` : '-'}
                        </td>
                        <td style="white-space: nowrap;">
                          ${this.editingId === rule.id
                            ? html`<div class="d-flex gap-2">
                                  <button class="btn btn-sm btn-success" @click=${this.saveEdit}>Save</button>
                                  <button class="btn btn-sm btn-secondary" @click=${this.cancelEdit}>Cancel</button>
                                </div>`
                            : html`<div class="d-flex gap-2">
                                  <button class="btn btn-sm btn-secondary" @click=${() => this.startEdit(rule)}>Edit</button>
                                  <button class="btn btn-danger btn-sm" @click=${() => this.deleteRule(rule.id)}>
                                    Delete
                                  </button>
                                </div>`}
                        </td>
                      </tr>
                    `;
                  })}
                </tbody>
              </table>
            `}
      </div>
    `;
  }

  static styles = css`
    :host {
      display: block;
    }
    .gap-2 {
      gap: 0.5rem;
    }
  `;
}

customElements.define('balance-manager', BalanceManager);
