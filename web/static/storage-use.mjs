import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('storage-use', class StorageUse extends LitElement {
    static styles = css`
    /* Style for sub-rows: indented and with slightly smaller text/background tint */
    .sub-row td {
      padding-left: 40px;
      background-color: #2a2a2a;
      font-size: 0.9em;
    }
  `;

    constructor() {
        super();
        this.data = [];
    }

    async loadData() {
        // Load summary storage use stats.
        const summary = await RPCCall('StorageUseStats');
        // Load breakdown stats by file type.
        const storeBreakdown = await RPCCall('StorageStoreTypeStats');

        // Determine if we have a detailed breakdown:
        // if there is more than one entry, or the single entry is not "Other"
        const hasDetailedBreakdown = storeBreakdown.length > 1 ||
            (storeBreakdown.length === 1 && storeBreakdown[0].type !== 'Other');

        // If we have a detailed breakdown, merge it into the summary data by
        // attaching the store breakdown as subEntries to the "Store" row.
        if (hasDetailedBreakdown) {
            summary.forEach(row => {
                if (row.Type === "Store") {
                    row.subEntries = storeBreakdown;
                }
            });
        }

        this.data = summary;
        this.requestUpdate();
    }

    connectedCallback() {
        super.connectedCallback();
        this.loadData();
    }

    render() {
        return html`
      <!-- Including Bootstrap and our main stylesheet -->
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
            rel="stylesheet" crossorigin="anonymous">
      <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
      <table class="table table-dark">
        <thead>
          <tr>
            <th>Type</th>
            <th>Storage</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          ${this.data.map(row => this.renderRow(row))}
        </tbody>
      </table>
    `;
    }

    renderRow(row) {
        // Calculate percentage used if capacity > 0.
        const pct = row.Capacity > 0 ? (100 - 100 * row.Available / row.Capacity).toFixed(2) : "0";
        // Use the pre-formatted strings if available; otherwise, fall back to raw numbers.
        const usedStr = row.UseStr || (row.Capacity - row.Available);
        const capStr = row.CapStr || row.Capacity;
        return html`
            <tr>
                <td>${row.Type}</td>
                <td>${usedStr} / ${capStr} (${pct}%)</td>
                <td>
                    <div style="display:inline-block; width:250px; height:16px; border:3px solid #3f3f3f;">
                        <div style="width:${pct}%; height:10px; background-color:green;"></div>
                    </div>
                </td>
            </tr>
            ${row.subEntries ? row.subEntries.map(sub => this.renderSubRow(sub)) : ''}
        `;
    }

    renderSubRow(sub) {
        // For sub-entries from the breakdown RPC, assume keys: type, capacity, available, and avail_str.
        // pct here remains the used percentage for bar rendering.
        const pct = sub.capacity > 0 ? (100 - 100 * sub.available / sub.capacity).toFixed(2) : "0";
        // Calculate available percentage as 100 - used percentage.
        const availablePct = sub.capacity > 0 ? (100 - parseFloat(pct)).toFixed(2) : "0";
        return html`
      <tr class="sub-row">
        <td>${sub.type}</td>
        <td>Available: ${sub.avail_str} (${availablePct}%)</td>
        <td>
          <div style="display:inline-block; width:220px; height:14px; border:2px solid #3f3f3f;">
            <div style="width:${pct}%; height:10px; background-color:green;"></div>
          </div>
        </td>
      </tr>
    `;
    }
});
