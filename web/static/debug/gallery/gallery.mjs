import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

class StyleGallery extends LitElement {
  static styles = css`
    :host { display: block; }
    section { margin-bottom: 32px; }
    h2 {
      font-size: 16px;
      font-weight: 600;
      margin-bottom: 16px;
      padding-bottom: 8px;
      border-bottom: 1px solid var(--color-border-default, #30363d);
    }
    .swatch-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
      gap: 12px;
    }
    .swatch {
      border: 1px solid var(--color-border-default, #30363d);
      border-radius: 8px;
      overflow: hidden;
    }
    .swatch-color {
      height: 48px;
    }
    .swatch-label {
      padding: 8px 12px;
      font-size: 12px;
      background: var(--color-bg-subtle, #161b22);
    }
    .swatch-name { font-weight: 500; color: var(--color-text-primary, #e6edf3); }
    .swatch-value { color: var(--color-text-secondary, #8b949e); font-family: var(--font-mono); }
    .type-row { margin-bottom: 12px; }
    .type-label { font-size: 11px; color: var(--color-text-secondary); margin-bottom: 4px; text-transform: uppercase; letter-spacing: 0.04em; }
    .demo-row { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; margin-bottom: 12px; }
    .demo-table { width: 100%; }
  `;

  connectedCallback() {
    super.connectedCallback();
    if (!sessionStorage.getItem('curio-mock')) {
      sessionStorage.setItem('curio-mock', '1');
    }
  }

  renderSwatches() {
    const tokens = [
      ['--color-bg-canvas', 'Canvas'],
      ['--color-bg-subtle', 'Subtle'],
      ['--color-bg-elevated', 'Elevated'],
      ['--color-border-default', 'Border'],
      ['--color-text-primary', 'Text primary'],
      ['--color-text-secondary', 'Text secondary'],
      ['--color-accent-fg', 'Accent link'],
      ['--color-accent-emphasis', 'Accent emphasis'],
      ['--color-success-fg', 'Success'],
      ['--color-warning-fg', 'Warning'],
      ['--color-danger-fg', 'Danger'],
      ['--color-info-fg', 'Info'],
    ];
    return html`
      <div class="swatch-grid">
        ${tokens.map(([token, name]) => html`
          <div class="swatch">
            <div class="swatch-color" style="background: var(${token})"></div>
            <div class="swatch-label">
              <div class="swatch-name">${name}</div>
              <div class="swatch-value">${token}</div>
            </div>
          </div>`)}
      </div>`;
  }

  render() {
    return html`
      <section>
        <h2>Color Tokens</h2>
        ${this.renderSwatches()}
      </section>

      <section>
        <h2>Typography</h2>
        <div class="type-row"><div class="type-label">Page title / 20px 600</div><h1>Cluster Overview</h1></div>
        <div class="type-row"><div class="type-label">Section title / 16px 600</div><h2>Chain Connectivity</h2></div>
        <div class="type-row"><div class="type-label">Body / 14px 400</div><p>Storage provider administration console for Filecoin sealing operations.</p></div>
        <div class="type-row"><div class="type-label">Secondary / 14px 400</div><p style="color: var(--color-text-secondary)">Last updated 2 seconds ago · epoch 4123456</p></div>
        <div class="type-row"><div class="type-label">Mono data / 13px</div><code class="mono">bafybeigdyrzt5sfp7udm7hm27ectm5vx3m5m5m5m5m5m5m5m5m5m5m5m5m5</code></div>
        <div class="type-row"><div class="type-label">Link</div><a href="#">View sector details →</a></div>
      </section>

      <section>
        <h2>Status</h2>
        <div class="demo-row">
          <span class="success">Success text</span>
          <span class="warning">Warning text</span>
          <span class="error">Error text</span>
          <span class="info">Info text</span>
        </div>
        <div class="demo-row">
          <span class="badge-status badge-success">Healthy</span>
          <span class="badge-status badge-warning">Degraded</span>
          <span class="badge-status badge-danger">Critical</span>
          <span class="badge-status badge-info">Info</span>
        </div>
      </section>

      <section>
        <h2>Buttons</h2>
        <div class="demo-row">
          <button class="btn btn-primary btn-sm">Primary</button>
          <button class="btn btn-secondary btn-sm">Secondary</button>
          <button class="btn btn-primary btn-sm" disabled>Disabled</button>
        </div>
      </section>

      <section>
        <h2>Form Controls</h2>
        <div class="info-block" style="max-width: 400px">
          <input class="form-control mb-2" placeholder="Search sectors…">
          <select class="form-select mb-2">
            <option>All states</option>
            <option>Sealing</option>
            <option>Proving</option>
          </select>
          <div class="form-check">
            <input class="form-check-input" type="checkbox" id="demo-check" checked>
            <label class="form-check-label" for="demo-check">Show detailed view</label>
          </div>
        </div>
      </section>

      <section>
        <h2>Card Panel</h2>
        <div class="info-block" style="max-width: 600px">
          <h2>Cluster Machines</h2>
          <p style="color: var(--color-text-secondary); margin-bottom: 12px;">3 nodes · 2 schedulable</p>
          <table class="table table-dark demo-table">
            <thead><tr><th>Name</th><th>Host</th><th>Status</th></tr></thead>
            <tbody>
              <tr><td>worker-01</td><td class="mono">10.0.1.10</td><td><span class="badge-status badge-success">Enabled</span></td></tr>
              <tr><td>worker-02</td><td class="mono">10.0.1.11</td><td><span class="badge-status badge-warning">Cordoned</span></td></tr>
              <tr><td>worker-03</td><td class="mono">10.0.1.12</td><td><span class="badge-status badge-danger">Restarting</span></td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h2>Live Components (mock data)</h2>
        <div class="row">
          <div class="col-md-auto" style="max-width: 1000px">
            <div class="info-block">
              <h2>Chain Connectivity</h2>
              <chain-connectivity></chain-connectivity>
            </div>
          </div>
        </div>
        <div class="row">
          <div class="col-md-auto" style="max-width: 1000px">
            <div class="info-block">
              <h2>24h Task Counts</h2>
              <harmony-task-counts></harmony-task-counts>
            </div>
          </div>
        </div>
      </section>
    `;
  }
}

customElements.define('style-gallery', StyleGallery);

// Load dashboard components for live demo section
import '/chain-connectivity.mjs';
import '/harmony-task-counts.mjs';
