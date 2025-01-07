import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { formatDateTwo } from '/lib/dateutil.mjs';


class UpgradeSectors extends LitElement {
    static properties = {
        data: { type: Array },
    };

    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('UpgradeSectors');
        // Refresh every 3 seconds
        setTimeout(() => this.loadData(), 3000);
        this.requestUpdate();
    }

    static styles = css`
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
      font-size: 0.8em;
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
    /* New style for "waiting for submit" state */
    .pipeline-waiting-submit {
      background-color: #a06010;
    }
  `;

    render() {
        // Count how many are "waiting for submit":
        // (UpdateReadyAt != null && !AfterSubmit && !TaskIDSubmit)
        const waitingForSubmitCount = this.data.filter(
            (s) => s.UpdateReadyAt && !s.AfterSubmit && !s.TaskIDSubmit
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

            <!-- Show the "waiting for submit" count -->
            <div style="margin: 1em 0;">
                <strong>Waiting for submit:</strong> ${waitingForSubmitCount}
            </div>

            <!-- One row per sector, with Start and ReadyAt in separate columns -->
            <table class="table table-dark table-striped">
                <thead>
                <tr>
                    <th>Miner</th>
                    <th>Sector #</th>
                    <th>Start Time</th>
                    <th>ReadyAt</th>
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
        <td>${sector.Miner}</td>

        <!-- Sector number -->
        <td>${sector.SectorNum}</td>

        <!-- Start time (absolute + since) -->
        <td>
          ${sector.StartTime ? this.renderTwoLineDate(sector.StartTime) : '--'}
        </td>

        <!-- ReadyAt if not null -->
        <td>
          ${sector.UpdateReadyAt ? this.renderTwoLineDate(sector.UpdateReadyAt) : '--'}
        </td>

        <!-- Pipeline (small sub-table) -->
        <td>${this.renderSectorPipeline(sector)}</td>

        <!-- Details link -->
        <td>
          <a href="/pages/sector/?sp=${sector.Miner}&id=${sector.SectorNum}"
            >DETAILS</a
          >
        </td>
      </tr>
    `;
    }

    renderTwoLineDate(date) {
      const [dateStr, timeStr] = formatDateTwo(date);

        const isOld = new Date(date).getTime() < Date.now() - 12 * 60 * 60 * 1000;
        const style = isOld ? 'white-space: nowrap; color: var(--color-danger-main)' : 'white-space: nowrap';
        return html`
            <div style="${style}">${dateStr}</div>
            <div style="${style}">${timeStr}</div>
        `;
    }

    renderSectorPipeline(sector) {
        return html`
            <table class="porep-state porep-pipeline-table">
                <tbody>
                <tr>
                    ${this.renderSectorState(
                            'Encode',
                            sector,
                            sector.TaskIDEncode,
                            sector.AfterEncode,
                            sector.StartedEncode
                    )}
                    ${this.renderSectorState(
                            'Prove',
                            sector,
                            sector.TaskIDProve,
                            sector.AfterProve,
                            sector.StartedProve
                    )}
                    ${this.renderSectorState(
                            'Submit',
                            sector,
                            sector.TaskIDSubmit,
                            sector.AfterSubmit,
                            sector.StartedSubmit
                    )}
                    ${this.renderSectorState(
                            'Move Storage',
                            sector,
                            sector.TaskIDMoveStorage,
                            sector.AfterMoveStorage,
                            sector.StartedMoveStorage
                    )}
                    ${this.renderSectorStateNoTask(
                            'Prove Msg Landed',
                            sector.AfterSubmit,
                            sector.AfterProveSuccess
                    )}
                    <!-- Sector overall state: failed or healthy -->
                    <td class="${sector.Failed ? 'pipeline-failed' : 'pipeline-success'}">
                        <div>State</div>
                        <div>${sector.Failed ? 'Failed' : 'Healthy'}</div>
                    </td>
                </tr>
                </tbody>
            </table>
        `;
    }

    /**
     * Renders a stage cell for the pipeline.
     * If this is the "Submit" stage and the sector is waiting for submit,
     * we apply the orange "pipeline-waiting-submit" style and show "Waiting".
     */
    renderSectorState(name, sector, taskID, after, started) {
        // Special case: waiting for submit
        if (
            name === 'Submit' &&
            sector.UpdateReadyAt &&
            !sector.AfterSubmit &&
            !sector.TaskIDSubmit
        ) {
            return html`
                <td class="pipeline-waiting-submit">
                    <div>${name}</div>
                    <div>Waiting</div>
                </td>
            `;
        }

        // Otherwise, do normal logic
        if (taskID) {
            const missing =
                sector.MissingTasks && sector.MissingTasks.includes(taskID);

            return html`
        <td
          class="${missing
                ? 'pipeline-failed'
                : started
                    ? 'pipeline-active'
                    : 'pipeline-waiting'}"
        >
          <div>${name}</div>
          <div>
            T: <a href="/pages/task/id/?id=${taskID}">${taskID}</a>
          </div>
          ${missing ? html`<div><b>FAILED</b></div>` : ''}
        </td>
      `;
        } else {
            return html`
        <td class="${after ? 'pipeline-success' : 'pipeline-waiting'}">
          <div>${name}</div>
          <div>${after ? 'Done' : '--'}</div>
        </td>
      `;
        }
    }

    renderSectorStateNoTask(name, active, after) {
        return html`
            <td class="${active ? 'pipeline-active' : ''} ${after
                    ? 'pipeline-success'
                    : ''}">
                <div>${name}</div>
                <div>${after ? 'Done' : '--'}</div>
            </td>
        `;
    }
}

customElements.define('upgrade-sectors', UpgradeSectors);
