import {LitElement, html, css} from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class ClusterTasks extends LitElement {
  static get properties() {
    return {
      data: { type: Array },
      showBackgroundTasks: { type: Boolean },
      coalesceEntries: { type: Boolean },
    };
  }

  static get styles() {
    return css`
      th,
      td {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      th:nth-child(1),
      td:nth-child(1) {
        width: 8ch;
      }
      th:nth-child(2),
      td:nth-child(2) {
        width: 16ch;
      }
      th:nth-child(3),
      td:nth-child(3) {
        width: 10ch;
      }
      th:nth-child(4),
      td:nth-child(4) {
        width: 10ch;
      }
      th:nth-child(5),
      td:nth-child(5) {
        min-width: 20ch;
      }

      /* Row used to coalesce runs of similar tasks */
      .similar-row > td {
        background: var(--color-form-group-2);
        line-height: 1.2em;
        text-align: center;
        font-style: italic;
      }
    `;
  }

  constructor() {
    super();
    this.data = [];
    this.showBackgroundTasks = false;
    this.coalesceEntries = true; // Default-enabled coalesce checkbox
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

  toggleCoalesceEntries(e) {
    this.coalesceEntries = e.target.checked;
  }

  /**
   * Group consecutive entries that share the same SpID, task name, and OwnerID.
   * Returns an array of groups, where each group is an array of entries.
   */
  groupData(data) {
    const groups = [];
    let currentGroup = [];
    let currentKey = null;

    for (const entry of data) {
      // The grouping key is the triplet: [SpID, Name, OwnerID]
      const key = JSON.stringify([entry.SpID, entry.Name, entry.OwnerID]);
      if (key !== currentKey) {
        if (currentGroup.length > 0) {
          groups.push(currentGroup);
        }
        currentGroup = [entry];
        currentKey = key;
      } else {
        currentGroup.push(entry);
      }
    }
    // Push the last group
    if (currentGroup.length > 0) {
      groups.push(currentGroup);
    }

    return groups;
  }

  /**
   * Renders table rows for a group of entries.
   * If coalesce mode is off or a group has <= 3 entries, render all rows.
   * Otherwise, render the first, a "similar tasks" row, then the last.
   */
  renderTableRows(entries) {
    if (!this.coalesceEntries || entries.length <= 3) {
      return entries.map((entry) => this.renderRow(entry));
    }

    const firstEntry = entries[0];
    const lastEntry = entries[entries.length - 1];
    const middleCount = entries.length - 2;

    return html`
      ${this.renderRow(firstEntry)}
      <tr class="similar-row">
        <td colspan="5">${middleCount} similar tasks</td>
      </tr>
      ${this.renderRow(lastEntry)}
    `;
  }

  renderRow(entry) {
    return html`
      <tr>
        <td>${entry.SpID ? entry.Miner : 'n/a'}</td>
        <td>${entry.Name}</td>
        <td><a href="/pages/task/id/?id=${entry.ID}">${entry.ID}</a></td>
        <td>${entry.SincePostedStr}</td>
        <td>
          ${entry.OwnerID
              ? html`<a href="/pages/node_info/?id=${entry.OwnerID}">${entry.Owner}</a>`
              : ''}
        </td>
      </tr>
    `;
  }

  render() {
    // First, filter out background tasks if needed
    const filtered = this.data.filter(
        (entry) => this.showBackgroundTasks || !entry.Name.startsWith('bg:')
    );

    let sortedOrOriginal = filtered;

    // In coalesced mode, we sort by [Name -> SpID -> OwnerID]
    // Otherwise, leave data in its default order (e.g., posted time).
    if (this.coalesceEntries) {
      sortedOrOriginal = [...filtered].sort((a, b) => {
        const nameCmp = a.Name.localeCompare(b.Name);
        if (nameCmp !== 0) return nameCmp;
        // If SpID is numeric, do numeric sort, else compare as strings
        const spA = typeof a.SpID === 'number' ? a.SpID : Number.parseInt(a.SpID, 10) || a.SpID;
        const spB = typeof b.SpID === 'number' ? b.SpID : Number.parseInt(b.SpID, 10) || b.SpID;
        const spCmp = spA > spB ? 1 : spA < spB ? -1 : 0;
        if (spCmp !== 0) return spCmp;
        // Compare OwnerIDs (if numeric, do numeric compare; fallback to string)
        const ownerA = typeof a.OwnerID === 'number' ? a.OwnerID : Number.parseInt(a.OwnerID, 10) || a.OwnerID || '';
        const ownerB = typeof b.OwnerID === 'number' ? b.OwnerID : Number.parseInt(b.OwnerID, 10) || b.OwnerID || '';
        const ownerCmp = ownerA > ownerB ? 1 : ownerA < ownerB ? -1 : 0;
        return ownerCmp;
      });
    }

    // If coalescing, group them, otherwise each entry is its own group
    const grouped = this.coalesceEntries
        ? this.groupData(sortedOrOriginal)
        : sortedOrOriginal.map((e) => [e]);

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

      <!-- Toggle for coalescing entries -->
      <label style="margin-left: 1em;">
        <input
          type="checkbox"
          @change=${this.toggleCoalesceEntries}
          ?checked=${this.coalesceEntries}
        />
        Coalesce Entries
      </label>

      <table class="table table-dark mt-3">
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
          ${grouped.map((group) => this.renderTableRows(group))}
        </tbody>
      </table>
    `;
  }
}

customElements.define('cluster-tasks', ClusterTasks);
