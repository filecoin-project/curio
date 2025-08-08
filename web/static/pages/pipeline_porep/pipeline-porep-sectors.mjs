import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { formatDateTwo } from '/lib/dateutil.mjs';
import '/ux/compact-epoch.mjs';

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
    };

    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('PipelinePorepSectors');
        // Refresh every 3 seconds
        setTimeout(() => this.loadData(), 3000);
        this.requestUpdate();
    }

    static styles = [pipelineStyles];

    render() {
        // Count how many are "waiting for precommit":
        // (PreCommitReadyAt != null && !AfterPrecommitMsg && !TaskPrecommitMsg)
        const waitingForPrecommitCount = this.data.filter(
            (s) => s.AfterSynthetic && s.PreCommitReadyAt && !s.AfterPrecommitMsg && !s.TaskPrecommitMsg
        ).length;

        // Count how many are "waiting for commit":
        // (CommitReadyAt != null && !AfterCommitMsg && !TaskCommitMsg)
        const waitingForCommitCount = this.data.filter(
            (s) => s.CommitReadyAt && !s.AfterCommitMsg && !s.TaskCommitMsg
        ).length;

        return html`
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
      />
      <link
        rel="stylesheet"
        href="/ux/main.css"
        onload="document.body.style.visibility = 'initial'"
      />

      <!-- Show counters for waiting states -->
      <div style="margin: 1em 0;">
        <strong>Waiting for PreCommit:</strong> ${waitingForPrecommitCount}
        &nbsp;&nbsp;|&nbsp;&nbsp;
        <strong>Waiting for Commit:</strong> ${waitingForCommitCount}
      </div>

      <!-- Main table: one row per sector -->
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
          ${this.data.map((sector) => this.renderSectorRow(sector))}
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
        <td>${renderSectorPipeline(sector)}</td>

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
              class="${sector.ChainSector
        ? 'pipeline-success'
        : sector.ChainAlloc
            ? 'pipeline-active'
            : 'pipeline-failed'}"
            >
              <div>On Chain</div>
              <div>
                ${sector.ChainSector
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
        : sector.ChainActive
            ? 'pipeline-success'
            : 'pipeline-active'}"
            >
              <div>State</div>
              <div>
                ${sector.Failed
        ? 'Failed'
        : sector.ChainActive
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
              class="${sector.ChainActive
        ? 'pipeline-success'
        : 'pipeline-failed'}"
            >
              <div>Active</div>
              <div>
                ${sector.ChainActive
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
          class="${missing
            ? 'pipeline-failed'
            : started
                ? 'pipeline-active'
                : 'pipeline-waiting'}"
        >
          <div>${name}</div>
          <div>
            T:
            <a href="/pages/task/id/?id=${task}">${task}</a>
          </div>
          ${missing ? html`<div><b>FAILED</b></div>` : ''}
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
