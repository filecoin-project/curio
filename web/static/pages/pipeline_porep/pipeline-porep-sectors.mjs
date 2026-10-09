import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import { RPCCallHTTP } from '/lib/jsonrpc.mjs';
import { formatDateTwo } from '/lib/dateutil.mjs';
import { visiblePoRepSectors } from './visibility.mjs';
import { PoRepPagePoller } from './page-poller.mjs';
import '/ux/compact-epoch.mjs';
import '/ux/task.mjs';

export const pipelineStyles = css`
    .porep-pipeline-table,
    .porep-state {
      color: #d0d0d0;
    }

    .porep-pipeline-table td,
    .porep-pipeline-table th {
      border-left: none;
      border-collapse: collapse;
      vertical-align: middle;
    }

    .porep-pipeline-table tr:nth-child(odd) {
      border-top: 6px solid #999999;
    }

    .porep-pipeline-table tr:first-child {
      border-top: none;
    }

    .porep-state {
      border-collapse: collapse;
    }

    .porep-state td,
    .porep-state th {
      border-left: 1px solid #f0f0f0;
      border-right: 1px solid #f0f0f0;
      padding: 1px 5px;
      text-align: center;
      font-size: 0.7em;
    }

    .porep-state tr {
      border-top: 1px solid #f0f0f0;
    }

    .porep-state tr:first-child {
      border-top: none;
    }

    .pipeline-active {
      background-color: #303060;
    }

    .pipeline-success {
      background-color: #306030;
    }

    .pipeline-failed {
      background-color: #603030;
    }

    .pipeline-waiting {
      background-color: #808080;
    }

    /* Waiting for precommit or commit states */
    .pipeline-waiting-precommit {
      background-color: #a06010;
    }
    .pipeline-waiting-commit {
      background-color: #a06010;
    }
  `;

class PipelinePorepSectors extends LitElement {
    static properties = {
        data: { type: Array },
        hidePendingSDR: { type: Boolean, attribute: 'hide-pending-sdr' },
        snapshot: { state: true },
        loading: { state: true },
        error: { state: true },
        paused: { state: true },
        offset: { state: true },
    };

    constructor() {
        super();
        this.data = [];
        this.hidePendingSDR = false;
        this.snapshot = null;
        this.loading = true;
        this.error = '';
        this.paused = false;
        this.offset = 0;
        this.lastSuccess = null;
        this.poller = new PoRepPagePoller({
            request: (signal) => RPCCallHTTP('PipelinePorepPage', [{
                Offset: this.offset, HidePendingSDR: this.hidePendingSDR,
            }], {signal}),
            onStart: () => { this.loading = true; },
            onSuccess: (snapshot) => {
                if (!snapshot || !Array.isArray(snapshot.Sectors) || snapshot.Sectors.length > 100) {
                    throw new Error('Invalid PoRep page response');
                }
                this.snapshot = snapshot;
                this.data = snapshot.Sectors;
                this.lastSuccess = new Date();
                this.loading = false;
                this.error = '';
            },
            onError: (error) => {
                this.loading = false;
                this.error = error?.message || String(error);
            },
        });
        this.visibilityChanged = () => this.updateActivity();
    }

    connectedCallback() {
        super.connectedCallback();
        document.addEventListener('visibilitychange', this.visibilityChanged);
        this.updateActivity();
    }

    disconnectedCallback() {
        this.poller.setActive(false);
        document.removeEventListener('visibilitychange', this.visibilityChanged);
        super.disconnectedCallback();
    }

    updateActivity() {
        this.poller.setActive(this.isConnected && !this.paused && !document.hidden);
        this.requestUpdate();
    }

    changePage(offset) {
        this.offset = Math.max(0, offset);
        this.poller.refresh();
    }

    changeFilter(event) {
        this.hidePendingSDR = event.target.checked;
        this.changePage(0);
    }

    static styles = [pipelineStyles];

    render() {
        const snapshot = this.snapshot;
        // While changing pages/filters keep the last successful snapshot and
        // its applied filter together; do not relabel old rows as new results.
        const visibleSectors = visiblePoRepSectors(this.data, snapshot?.HidePendingSDR ?? false);

        return html`
      <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
      <link
        rel="stylesheet"
        href="/ux/main.css"
        onload="document.body.style.visibility = 'initial'"
      />

      <p role="status">
        ${!snapshot ? (this.error ? 'Unable to load PoRep sectors.' : 'Loading PoRep sectors…')
            : this.error ? 'Stale snapshot — refresh failed.'
            : !this.poller.active ? 'Paused snapshot.'
            : this.loading ? 'Refreshing — showing the last successful snapshot.' : 'Snapshot loaded.'}
        ${this.error ? html`<span role="alert">${this.error} Automatic retry when active.</span>` : ''}
        ${this.lastSuccess ? html`Last success: ${this.lastSuccess.toLocaleString()}. Server snapshot: ${snapshot.ObservedAt}.` : ''}
      </p>
      <button @click=${() => this.poller.refresh()} ?disabled=${!this.poller.active}>Refresh now</button>
      <button @click=${() => { this.paused = !this.paused; this.updateActivity(); }}>${this.paused ? 'Resume refresh' : 'Pause refresh'}</button>
      <div style="margin: 1em 0;">
        <strong>Waiting for PreCommit:</strong> ${snapshot?.WaitingForPrecommit ?? '—'}
        &nbsp;&nbsp;|&nbsp;&nbsp;
        <strong>Waiting for Commit:</strong> ${snapshot?.WaitingForCommit ?? '—'}
      </div>

      <!-- Main table: one row per sector -->
      <label>
        <input type="checkbox" .checked=${this.hidePendingSDR}
          @change=${this.changeFilter} />
        Hide unclaimed, unfinished SDR sectors in this view
      </label>
      ${snapshot ? html`<p class="counts">Showing ${visibleSectors.length} of ${this.data.length} sectors on this page;
        ${snapshot.Matching} matching of ${snapshot.Total} total pipeline sectors.
        ${snapshot.Total - snapshot.Matching} filtered on the server; ${this.data.length - visibleSectors.length} hidden locally.
        Counters above include the entire pipeline snapshot, not just this page.</p>
        ${snapshot.Matching === 0 ? html`<p>${snapshot.Total === 0 ? 'No pipeline sectors.' : 'No sectors match this filter.'}</p>` : ''}
        <button @click=${() => this.changePage(snapshot.Offset - snapshot.Limit)} ?disabled=${snapshot.Offset === 0 || this.loading}>Previous</button>
        <button @click=${() => this.changePage(snapshot.Offset + snapshot.Limit)} ?disabled=${snapshot.Offset + snapshot.Limit >= snapshot.Matching || this.loading}>Next</button>
        <button @click=${() => this.changePage(0)} ?disabled=${snapshot.Offset === 0 || this.loading}>First page</button>
        <span>Offset ${snapshot.Offset}; up to ${snapshot.Limit} per page. Active/failed/post-SDR sectors first.</span>` : ''}
      <p>Task labels use this pipeline snapshot (owned does not prove Do entry). Chain/seed readiness is not queried here;
        open Details for on-chain information and task links for history/actions.</p>
      <table class="table table-dark table-striped">
        <thead>
          <tr>
            <th>Miner</th>
            <th>Sector #</th>
            <th>Create Time</th>
            <th>PreCommit ReadyAt</th>
            <th>Commit ReadyAt</th>
            <th>Pipeline</th>
            <th>Details</th>
          </tr>
        </thead>
        <tbody>
          ${visibleSectors.map((sector) => this.renderSectorRow(sector))}
        </tbody>
      </table>
    `;
    }

    renderSectorRow(sector) {
        return html`
      <tr>
        <!-- Miner -->
        <td>${sector.Address}</td>

        <!-- Sector number -->
        <td>${sector.SectorNumber}</td>

        <!-- CreateTime in two lines -->
        <td>
          ${sector.CreateTime
            ? this.renderTwoLineDate(sector.CreateTime)
            : '--'}
        </td>

        <!-- PreCommit ReadyAt in two lines if present -->
        <td>
          ${sector.PreCommitReadyAt
            ? this.renderTwoLineDate(sector.PreCommitReadyAt)
            : '--'}
        </td>

        <!-- Commit ReadyAt in two lines if present -->
        <td>
          ${sector.CommitReadyAt
            ? this.renderTwoLineDate(sector.CommitReadyAt)
            : '--'}
        </td>

        <!-- Pipeline sub-table -->
        <td>${renderSectorPipeline({...sector, TaskSnapshot: true})}</td>

        <!-- Details link -->
        <td>
          <a
            href="/pages/sector/?sp=${sector.Address}&id=${sector.SectorNumber}"
            >DETAILS</a
          >
        </td>
      </tr>
    `;
    }

    /**
     * Renders a date in two lines: first line = date (YYYY-MM-DD),
     * second line = time (HH:mm:ss), with special color if older than 12h.
     */
    renderTwoLineDate(dateString) {
        const [dateStr, timeStr] = formatDateTwo(dateString);
        // If the date is older than 12 hours from now, color it "danger."
        const isOld =
            new Date(dateString).getTime() < Date.now() - 12 * 60 * 60 * 1000;
        const style = isOld
            ? 'white-space: nowrap; color: var(--color-danger-main)'
            : 'white-space: nowrap';

        return html`
      <div style="${style}">${dateStr}</div>
      <div style="${style}">${timeStr}</div>
    `;
    }
}

customElements.define('pipeline-porep-sectors', PipelinePorepSectors);

export function renderSectorPipeline(sector) {
    return html`
      <table class="porep-state porep-pipeline-table">
        <tbody>
          <tr>
            <!-- Row 1 tasks -->
            ${renderSectorState(
        'SDR',
        1,
        sector,
        sector.TaskSDR,
        sector.AfterSDR,
        sector.StartedSDR
    )}
            ${renderSectorState(
        'TreeC',
        1,
        sector,
        sector.TaskTreeC,
        sector.AfterTreeC,
        sector.StartedTreeRC
    )}
            ${renderSectorState(
        'Synthetic',
        2,
        sector,
        sector.TaskSynthetic,
        sector.AfterSynthetic,
        sector.StartedSynthetic
    )}
            ${renderSectorState(
        'PComm Msg',
        2,
        sector,
        sector.TaskPrecommitMsg,
        sector.AfterPrecommitMsg,
        sector.StartedPrecommitMsg
    )}
            ${renderSectorStateNoTask(
        'PComm Wait',
        2,
        sector.AfterPrecommitMsg,
        sector.AfterPrecommitMsgSuccess
    )}
            <td
              rowspan="2"
              class="${sector.AfterPrecommitMsgSuccess
        ? 'pipeline-active'
        : ''} ${sector.AfterSeed ? 'pipeline-success' : ''}"
            >
              <div>Wait Seed</div>
              <div>
                ${sector.AfterSeed
                  ? 'done'
                  : sector.TaskSnapshot ? (sector.SeedEpoch == null ? '—' : `epoch ${sector.SeedEpoch} (readiness unknown)`)
                  : html`<compact-pretty-epoch .epoch=${sector.SeedEpoch}></compact-pretty-epoch>`}
              </div>
            </td>
            ${renderSectorState(
        'PoRep',
        2,
        sector,
        sector.TaskPoRep,
        sector.AfterPoRep,
        sector.StartedPoRep
    )}
            ${renderSectorState(
        'Clear Cache',
        1,
        sector,
        sector.TaskFinalize,
        sector.AfterFinalize,
        sector.StartedFinalize
    )}
            ${renderSectorState(
        'Move Storage',
        1,
        sector,
        sector.TaskMoveStorage,
        sector.AfterMoveStorage,
        sector.StartedMoveStorage
    )}
            <td
              class="${sector.ChainSector == null ? '' : sector.ChainSector
        ? 'pipeline-success'
        : sector.ChainAlloc
            ? 'pipeline-active'
            : 'pipeline-failed'}"
            >
              <div>On Chain</div>
              <div>
                ${sector.ChainSector == null ? 'unknown' : sector.ChainSector
        ? 'yes'
        : sector.ChainAlloc
            ? 'allocated'
            : 'no'}
              </div>
            </td>
            <td
              rowspan="2"
              class="${sector.Failed
        ? 'pipeline-failed'
            : sector.ChainActive == null ? '' : sector.ChainActive
            ? 'pipeline-success'
            : 'pipeline-active'}"
            >
              <div>State</div>
              <div>
                ${sector.Failed
        ? 'Failed'
        : sector.ChainActive == null ? 'Chain status unknown' : sector.ChainActive
            ? 'Sealed'
            : 'Sealing'}
              </div>
            </td>
          </tr>
          <tr>
            <!-- Row 2 tasks -->
            ${renderSectorState(
        'TreeD',
        1,
        sector,
        sector.TaskTreeD,
        sector.AfterTreeD,
        sector.StartedTreeD
    )}
            ${renderSectorState(
        'TreeR',
        1,
        sector,
        sector.TaskTreeR,
        sector.AfterTreeR,
        sector.StartedTreeRC
    )}
            <!-- Commit steps -->
            ${renderSectorState(
        'Commit Msg',
        1,
        sector,
        sector.TaskCommitMsg,
        sector.AfterCommitMsg,
        sector.StartedCommitMsg
    )}
            ${renderSectorStateNoTask(
        'Commit Wait',
        1,
        sector.AfterCommitMsg,
        sector.AfterCommitMsgSuccess
    )}
            <td
              class="${sector.ChainActive == null ? '' : sector.ChainActive
        ? 'pipeline-success'
        : 'pipeline-failed'}"
            >
              <div>Active</div>
              <div>
                ${sector.ChainActive == null ? 'unknown' : sector.ChainActive
        ? 'yes'
        : sector.ChainUnproven
            ? 'unproven'
            : sector.ChainFaulty
                ? 'faulty'
                : 'no'}
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    `;
}

/**
 * Renders a stage cell with a task ID (if present) or a "Done / --" state.
 * Also applies special "waiting" color if:
 *   - "PComm Msg" and sector is waiting for precommit
 *   - "Commit Msg" and sector is waiting for commit
 */
export function renderSectorState(name, rowspan, sector, task, after, started) {
    // 1) "waiting for precommit"
    if (
        name === 'PComm Msg' &&
        sector.AfterSynthetic &&
        sector.PreCommitReadyAt &&
        !sector.AfterPrecommitMsg &&
        !sector.TaskPrecommitMsg
    ) {
        return html`
        <td rowspan="${rowspan}" class="pipeline-waiting-precommit">
          <div>${name}</div>
          <div>Waiting</div>
        </td>
      `;
    }
    // 2) "waiting for commit"
    if (
        name === 'Commit Msg' &&
        sector.CommitReadyAt &&
        !sector.AfterCommitMsg &&
        !sector.TaskCommitMsg
    ) {
        return html`
        <td rowspan="${rowspan}" class="pipeline-waiting-commit">
          <div>${name}</div>
          <div>Waiting</div>
        </td>
      `;
    }

    // Normal logic for tasks with an ID
    if (task) {
        const missing =
            sector.MissingTasks && sector.MissingTasks.includes(task);
        return html`
        <td
          rowspan="${rowspan}"
          class="${sector.TaskSnapshot && after ? 'pipeline-success' : missing
            ? 'pipeline-failed'
            : started
                ? 'pipeline-active'
                : 'pipeline-waiting'}"
        >
          <div>${name}</div>
          <div style="font-size: 0.9em;">
            ${sector.TaskSnapshot ? html`<a href="/pages/task/id/?id=${task}">${task}</a>
                ${after ? 'done' : missing ? 'not queued' : started ? 'owned' : 'queued'}`
                : html`<task-status .taskId=${task}></task-status>`}
          </div>
          ${missing && !sector.TaskSnapshot ? html`<div><b>FAILED</b></div>` : ''}
        </td>
      `;
    }

    // No task ID => either done or not started
    return html`
      <td rowspan="${rowspan}" class="${after ? 'pipeline-success' : ''}">
        <div>${name}</div>
        <div>${after ? 'done' : '--'}</div>
      </td>
    `;
}

/**
 * Renders a stage cell for tasks that don't have an associated Task ID
 * but do have after/active states to display.
 */
export function renderSectorStateNoTask(name, rowspan, active, after) {
    return html`
      <td
        rowspan="${rowspan}"
        class="${active ? 'pipeline-active' : ''} ${after
        ? 'pipeline-success'
        : ''}"
      >
        <div>${name}</div>
        <div>${after ? 'done' : '--'}</div>
      </td>
    `;
}
