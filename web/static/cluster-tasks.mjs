import {LitElement, html, css} from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import {clusterTaskAge} from './cluster-tasks-age.mjs';
import {RPCCallHTTP} from '/lib/jsonrpc.mjs';
import {
  ClusterTaskFreshnessTicker,
  ClusterTaskPollController,
} from '/cluster-tasks-controller.mjs';
import {
  CLUSTER_TASK_DEFAULTS,
  CLUSTER_TASK_ORDER_POLICY,
  advanceClusterTaskDisplayClock,
  beginClusterTaskRefresh,
  buildClusterTaskLastSuccessPresentation,
  buildClusterTaskRequest,
  buildClusterTaskSections,
  buildClusterTaskTypeOptions,
  cancelClusterTaskRefresh,
  clusterTaskSectionEmptyMessage,
  clusterTaskSnapshotMismatchMessage,
  completeClusterTaskRefresh,
  createClusterTaskDisplayClock,
  createClusterTaskViewState,
  failClusterTaskRefresh,
  freezeClusterTaskDisplayClock,
  formatClusterTaskSectionSummary,
  formatTaskAgeSeconds,
  parseBoundedInteger,
  resetClusterTaskDisplayClock,
  shouldRunClusterTaskFreshness,
} from '/cluster-tasks-model.mjs';

class ClusterTasks extends LitElement {
  static get properties() {
    return {
      viewState: {type: Object},
      maxTasks: {type: Number},
      maxPending: {type: Number},
      maxTasksDraft: {type: String},
      maxPendingDraft: {type: String},
      maxTasksError: {type: String},
      maxPendingError: {type: String},
      includeBackground: {type: Boolean},
      taskName: {type: String},
      coalesceEntries: {type: Boolean},
      paused: {type: Boolean},
      viewVisible: {type: Boolean},
      freshnessNow: {type: Number},
      displayClock: {type: Object},
    };
  }

  static get styles() {
    return css`
      :host {
        display: block;
      }

      .controls {
        display: flex;
        flex-wrap: wrap;
        align-items: flex-end;
        gap: var(--space-3, 12px);
        margin-bottom: var(--space-3, 12px);
      }

      .control {
        display: flex;
        flex-direction: column;
        gap: var(--space-1, 4px);
        min-width: 7rem;
      }

      .control > label,
      .check-control {
        color: var(--color-text-secondary, #8b949e);
        font-size: 11px;
        font-weight: 500;
      }

      .control input[type='number'] {
        width: 7rem;
      }

      .control select {
        min-width: 11rem;
        max-width: 18rem;
      }

      .check-control {
        display: flex;
        align-items: center;
        gap: var(--space-1, 4px);
        min-height: 31px;
        white-space: nowrap;
      }

      .control-error {
        max-width: 12rem;
        color: var(--color-danger-fg, #f85149);
        font-size: 11px;
      }

      .actions {
        display: flex;
        gap: var(--space-2, 8px);
      }

      .status-row {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2, 8px);
        min-height: 24px;
        margin-bottom: var(--space-2, 8px);
        color: var(--color-text-secondary, #8b949e);
        font-size: 12px;
      }

      .status-pill {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        min-width: 5.75rem;
        padding: 2px 8px;
        border-radius: var(--radius-md, 6px);
        font-weight: 600;
      }

      .status-pill.fresh {
        color: var(--color-success-fg, #3fb950);
        background: var(--color-success-muted, rgba(63, 185, 80, 0.15));
      }

      .status-pill.loading {
        color: var(--color-info-fg, #58a6ff);
        background: var(--color-info-muted, rgba(88, 166, 255, 0.15));
      }

      .status-pill.stale,
      .status-pill.paused {
        color: var(--color-warning-fg, #d29922);
        background: var(--color-warning-muted, rgba(210, 153, 34, 0.15));
      }

      .status-pill.failed {
        color: var(--color-danger-fg, #f85149);
        background: var(--color-danger-muted, rgba(248, 81, 73, 0.15));
      }

      .policy {
        margin: 0 0 var(--space-3, 12px);
        color: var(--color-text-secondary, #8b949e);
        font-size: 12px;
      }

      .task-section {
        margin-top: var(--space-4, 16px);
      }

      .section-heading {
        display: flex;
        flex-wrap: wrap;
        align-items: baseline;
        justify-content: space-between;
        gap: var(--space-2, 8px);
        margin-bottom: var(--space-2, 8px);
      }

      .section-heading h3 {
        margin: 0;
        color: var(--color-text-primary, #e6edf3);
        font-size: 14px;
      }

      .section-summary,
      .empty-state {
        color: var(--color-text-secondary, #8b949e);
        font-size: 12px;
      }

      .empty-state {
        padding: var(--space-3, 12px);
        border: 1px dashed var(--color-border-default, #30363d);
        border-radius: var(--radius-md, 6px);
      }

      .table-wrap {
        max-width: 100%;
        overflow-x: auto;
      }

      table {
        width: 100%;
        min-width: 36rem;
        table-layout: fixed;
      }

      th,
      td {
        padding-right: 0.375rem;
        padding-left: 0.375rem;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      th:nth-child(1),
      td:nth-child(1) {
        width: 10ch;
      }

      th:nth-child(2),
      td:nth-child(2) {
        width: 14ch;
        max-width: 18ch;
      }

      th:nth-child(3),
      td:nth-child(3) {
        width: 9ch;
      }

      th:nth-child(4),
      td:nth-child(4) {
        width: 9ch;
      }

      th:nth-child(5),
      td:nth-child(5) {
        width: 10ch;
      }

      th:nth-child(6),
      td:nth-child(6) {
        width: 23ch;
        min-width: 23ch;
        max-width: none;
      }

      td:nth-child(6) {
        overflow: visible;
        text-overflow: clip;
      }

      td:nth-child(6) > a {
        white-space: nowrap;
      }

      .age-column {
        text-align: start;
        font-variant-numeric: tabular-nums;
      }

      .similar-row > td {
        background: var(--color-form-group-2, #21262d);
        line-height: 1.2em;
        text-align: center;
        font-style: italic;
      }

      .message {
        margin-bottom: var(--space-3, 12px);
      }

      .warnings {
        margin: 0;
        padding-left: var(--space-5, 20px);
      }
    `;
  }

  constructor() {
    super();
    this.viewState = createClusterTaskViewState();
    this.maxTasks = CLUSTER_TASK_DEFAULTS.maxTasks;
    this.maxPending = CLUSTER_TASK_DEFAULTS.maxPending;
    this.maxTasksDraft = String(this.maxTasks);
    this.maxPendingDraft = String(this.maxPending);
    this.maxTasksError = '';
    this.maxPendingError = '';
    this.includeBackground = CLUSTER_TASK_DEFAULTS.includeBackground;
    this.taskName = CLUSTER_TASK_DEFAULTS.taskName;
    this.coalesceEntries = CLUSTER_TASK_DEFAULTS.coalesceEntries;
    this.paused = false;
    this.viewVisible = false;
    this.freshnessNow = Date.now();
    this.displayClock = createClusterTaskDisplayClock();
    this.monotonicNow = () => performance.now();

    this.drawer = null;
    this.drawerObserver = null;
    this.resizeObserver = null;
    this.handleDocumentVisibility = () => this.syncActivity();
    this.handleWindowResize = () => this.syncActivity();

    this.pollController = new ClusterTaskPollController({
      request: (signal) => RPCCallHTTP(
          'ClusterTaskSummaryLimited',
          [buildClusterTaskRequest(this.currentControls())],
          {signal},
      ),
      onStart: () => {
        this.viewState = beginClusterTaskRefresh(this.viewState);
      },
      onSuccess: (response) => {
        const completedAt = Date.now();
        this.viewState = completeClusterTaskRefresh(this.viewState, response, completedAt);
        this.displayClock = resetClusterTaskDisplayClock(this.monotonicNow());
        this.applyServerLimits(this.viewState.response.Applied);
        this.freshnessNow = completedAt;
        this.syncFreshnessTicker();
      },
      onError: (error) => {
        this.freezeDisplayClock();
        this.viewState = failClusterTaskRefresh(this.viewState, error);
      },
    });
    this.freshnessTicker = new ClusterTaskFreshnessTicker({
      onTick: (now) => {
        this.freshnessNow = now;
        this.displayClock = advanceClusterTaskDisplayClock(
            this.displayClock,
            this.monotonicNow(),
        );
      },
    });
  }

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener('visibilitychange', this.handleDocumentVisibility);
    window.addEventListener('resize', this.handleWindowResize, {passive: true});
    this.pollController.setActivity({
      mounted: true,
      documentVisible: !document.hidden,
      paused: this.paused,
    });

    this.updateComplete.then(() => {
      if (this.isConnected) {
        this.observeVisibility();
      }
    });
  }

  disconnectedCallback() {
    document.removeEventListener('visibilitychange', this.handleDocumentVisibility);
    window.removeEventListener('resize', this.handleWindowResize);
    this.disconnectVisibilityObservers();
    this.freezeDisplayClock();
    this.freshnessTicker.stop();
    this.pollController.setActivity({mounted: false, viewVisible: false});
    this.viewState = cancelClusterTaskRefresh(this.viewState);
    super.disconnectedCallback();
  }

  updated(changedProperties) {
    if (!changedProperties.has('taskName') && !changedProperties.has('viewState')) {
      return;
    }

    const select = this.renderRoot.querySelector('#task-type');
    if (select && select.value !== this.taskName) {
      select.value = this.taskName;
    }
  }

  currentControls() {
    return {
      maxTasks: this.maxTasks,
      maxPending: this.maxPending,
      includeBackground: this.includeBackground,
      taskName: this.taskName,
    };
  }

  applyServerLimits(applied) {
    const maxTasksDraftWasCommitted = this.maxTasksDraft === String(this.maxTasks);
    const maxPendingDraftWasCommitted = this.maxPendingDraft === String(this.maxPending);

    this.maxTasks = applied.MaxTasks;
    this.maxPending = applied.MaxPending;
    if (maxTasksDraftWasCommitted) {
      this.maxTasksDraft = String(applied.MaxTasks);
      this.maxTasksError = '';
    }
    if (maxPendingDraftWasCommitted) {
      this.maxPendingDraft = String(applied.MaxPending);
      this.maxPendingError = '';
    }
  }

  observeVisibility() {
    this.disconnectVisibilityObservers();
    this.drawer = this.closest('ui-drawer');

    if (this.drawer && 'MutationObserver' in window) {
      this.drawerObserver = new MutationObserver(() => this.syncActivity());
      this.drawerObserver.observe(this.drawer, {
        attributes: true,
        attributeFilter: ['isopen', 'class', 'hidden', 'style'],
      });
    }

    if ('ResizeObserver' in window) {
      this.resizeObserver = new ResizeObserver(() => this.syncActivity());
      this.resizeObserver.observe(this);
      if (this.drawer) {
        this.resizeObserver.observe(this.drawer);
      }
    }

    this.syncActivity();
  }

  disconnectVisibilityObservers() {
    this.drawerObserver?.disconnect();
    this.resizeObserver?.disconnect();
    this.drawerObserver = null;
    this.resizeObserver = null;
    this.drawer = null;
  }

  isActuallyVisible() {
    if (!this.isConnected || this.hidden) {
      return false;
    }

    const drawer = this.drawer || this.closest('ui-drawer');
    if (drawer?.isOpen === false) {
      return false;
    }
    return this.getClientRects().length > 0;
  }

  syncActivity() {
    const viewVisible = this.isActuallyVisible();
    this.viewVisible = viewVisible;
    this.pollController.setActivity({
      mounted: this.isConnected,
      viewVisible,
      documentVisible: !document.hidden,
      paused: this.paused,
    });

    if (!this.pollController.active) {
      this.freezeDisplayClock();
      this.viewState = cancelClusterTaskRefresh(this.viewState);
    }
    this.syncFreshnessTicker();
  }

  freezeDisplayClock() {
    this.displayClock = freezeClusterTaskDisplayClock(this.displayClock);
  }

  syncFreshnessTicker() {
    const shouldTick = shouldRunClusterTaskFreshness({
      isConnected: this.isConnected,
      viewVisible: this.viewVisible,
      documentVisible: !document.hidden,
      paused: this.paused,
      hasSuccessfulLoad: this.viewState.hasSuccessfulLoad,
    });
    if (shouldTick) {
      this.freshnessTicker.start();
    } else {
      this.freshnessTicker.stop();
    }
  }

  refreshNow() {
    if (!this.maxTasksError && !this.maxPendingError) {
      this.pollController.refreshNow();
    }
  }

  togglePause() {
    this.paused = !this.paused;
    this.syncActivity();
  }

  handleTaskNameChange(event) {
    const taskName = event.target.value;
    if (taskName === this.taskName) {
      return;
    }
    this.taskName = taskName;
    this.pollController.refreshNow();
  }

  handleBackgroundChange(event) {
    this.includeBackground = event.target.checked;
    this.pollController.refreshNow();
  }

  handleCoalesceChange(event) {
    this.coalesceEntries = event.target.checked;
  }

  handleMaxTasksInput(event) {
    this.maxTasksDraft = event.target.value;
    this.maxTasksError = '';
  }

  handleMaxPendingInput(event) {
    this.maxPendingDraft = event.target.value;
    this.maxPendingError = '';
  }

  commitMaxTasks() {
    const value = parseBoundedInteger(this.maxTasksDraft, 1, 500);
    if (value === null) {
      this.maxTasksError = 'Enter a whole number from 1 to 500.';
      return;
    }

    let changed = value !== this.maxTasks;
    this.maxTasks = value;
    this.maxTasksDraft = String(value);
    this.maxTasksError = '';

    if (this.maxPending > value) {
      this.maxPending = value;
      this.maxPendingDraft = String(value);
      this.maxPendingError = '';
      changed = true;
    }

    if (changed) {
      this.pollController.refreshNow();
    }
  }

  commitMaxPending() {
    const value = parseBoundedInteger(this.maxPendingDraft, 0, this.maxTasks);
    if (value === null) {
      this.maxPendingError = `Enter a whole number from 0 to ${this.maxTasks}.`;
      return;
    }

    const changed = value !== this.maxPending;
    this.maxPending = value;
    this.maxPendingDraft = String(value);
    this.maxPendingError = '';
    if (changed) {
      this.pollController.refreshNow();
    }
  }

  handleMaxTasksKeyDown(event) {
    if (event.key === 'Enter') {
      event.preventDefault();
      this.commitMaxTasks();
    }
  }

  handleMaxPendingKeyDown(event) {
    if (event.key === 'Enter') {
      event.preventDefault();
      this.commitMaxPending();
    }
  }

  taskTypeOptions() {
    return buildClusterTaskTypeOptions(
        this.taskName,
        this.viewState.response?.TaskTypes,
    );
  }

  formatTimestamp(value) {
    if (!value) {
      return '';
    }
    const timestamp = new Date(value);
    return Number.isNaN(timestamp.getTime()) ? String(value) : timestamp.toLocaleTimeString();
  }

  renderStatus() {
    const state = this.viewState;
    let label = 'Waiting to load';
    let tone = 'loading';

    if (this.paused) {
      label = 'Paused';
      tone = 'paused';
    } else if (state.refreshing && !state.hasSuccessfulLoad) {
      label = 'Loading';
    } else if (state.stale) {
      label = 'Stale';
      tone = 'stale';
    } else if (state.hasSuccessfulLoad && state.response?.Partial && !state.refreshing) {
      label = 'Partial';
      tone = 'stale';
    } else if (state.error && !state.hasSuccessfulLoad) {
      label = 'Refresh failed';
      tone = 'failed';
    } else if (state.refreshing) {
      label = 'Refreshing';
    } else if (state.hasSuccessfulLoad) {
      label = 'Fresh';
      tone = 'fresh';
    }

    const observedAt = this.formatTimestamp(state.response?.ObservedAt);
    const lastSuccess = buildClusterTaskLastSuccessPresentation({
      paused: this.paused,
      now: this.freshnessNow,
      lastSuccessAt: state.lastSuccessAt,
    });
    return html`
      <div class="status-row" role="status" aria-live="polite">
        <span class="status-pill ${tone}">${label}</span>
        ${lastSuccess.text ? html`
          <span title=${lastSuccess.title}>${lastSuccess.text}</span>
        ` : ''}
        ${observedAt ? html`<span>Snapshot observed: ${observedAt}</span>` : ''}
      </div>
    `;
  }

  renderMessages() {
    const state = this.viewState;
    const warnings = state.response?.Warnings || [];
    const snapshotMismatch = clusterTaskSnapshotMismatchMessage({
      controls: this.currentControls(),
      response: state.response,
      refreshing: state.refreshing,
      failed: Boolean(state.error),
      paused: this.paused,
    });
    return html`
      ${snapshotMismatch ? html`
        <div class="alert alert-warning message" role="status">
          ${snapshotMismatch}
        </div>
      ` : ''}
      ${state.error ? html`
        <div class="alert ${state.hasSuccessfulLoad ? 'alert-warning' : 'alert-danger'} message" role="alert">
          ${state.hasSuccessfulLoad
            ? `Refresh failed; showing the last successful snapshot. ${state.error}`
            : `Unable to load Cluster Tasks. ${state.error}`}
        </div>
      ` : ''}
      ${warnings.length > 0 ? html`
        <div class="alert alert-warning message" role="status">
          <ul class="warnings">
            ${warnings.map((warning) => html`
              <li>
                ${warning.TaskName ? `${warning.TaskName}: ` : ''}${warning.Message}
                ${warning.Code ? html` <span title="Warning code">(${warning.Code})</span>` : ''}
              </li>
            `)}
          </ul>
        </div>
      ` : ''}
    `;
  }

  renderControls() {
    const taskTypes = this.taskTypeOptions();
    const controlsInvalid = Boolean(this.maxTasksError || this.maxPendingError);
    return html`
      <div class="controls">
        <div class="control">
          <label for="task-type">Task type</label>
          <select
            id="task-type"
            class="form-select form-select-sm"
            .value=${this.taskName}
            @change=${this.handleTaskNameChange}
          >
            <option value="">All</option>
            ${taskTypes.map((taskType) => html`
              <option value=${taskType.Name}>${taskType.Name}</option>
            `)}
          </select>
        </div>

        <div class="control">
          <label for="max-tasks">Max displayed tasks</label>
          <input
            id="max-tasks"
            class="form-control form-control-sm"
            type="number"
            min="1"
            max="500"
            step="1"
            .value=${this.maxTasksDraft}
            aria-invalid=${this.maxTasksError ? 'true' : 'false'}
            @input=${this.handleMaxTasksInput}
            @change=${this.commitMaxTasks}
            @keydown=${this.handleMaxTasksKeyDown}
          />
          ${this.maxTasksError ? html`<span class="control-error">${this.maxTasksError}</span>` : ''}
        </div>

        <div class="control">
          <label for="max-pending">Pending preview</label>
          <input
            id="max-pending"
            class="form-control form-control-sm"
            type="number"
            min="0"
            max=${this.maxTasks}
            step="1"
            .value=${this.maxPendingDraft}
            aria-invalid=${this.maxPendingError ? 'true' : 'false'}
            @input=${this.handleMaxPendingInput}
            @change=${this.commitMaxPending}
            @keydown=${this.handleMaxPendingKeyDown}
          />
          ${this.maxPendingError ? html`<span class="control-error">${this.maxPendingError}</span>` : ''}
        </div>

        <label class="check-control">
          <input
            class="form-check-input"
            type="checkbox"
            .checked=${this.includeBackground}
            @change=${this.handleBackgroundChange}
          />
          Show background tasks
        </label>

        <label class="check-control">
          <input
            class="form-check-input"
            type="checkbox"
            .checked=${this.coalesceEntries}
            @change=${this.handleCoalesceChange}
          />
          Coalesce Entries
        </label>

        <div class="actions">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            ?disabled=${this.paused || controlsInvalid}
            @click=${this.refreshNow}
          >
            Refresh now
          </button>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            @click=${this.togglePause}
          >
            ${this.paused ? 'Resume' : 'Pause'}
          </button>
        </div>
      </div>
    `;
  }

  renderTableRows(entries) {
    if (entries.length <= 3) {
      return entries.map((entry) => this.renderRow(entry));
    }

    const firstEntry = entries[0];
    const lastEntry = entries[entries.length - 1];
    const middleCount = entries.length - 2;
    return html`
      ${this.renderRow(firstEntry)}
      <tr class="similar-row">
        <td colspan="6">${middleCount} similar tasks</td>
      </tr>
      ${this.renderRow(lastEntry)}
    `;
  }

  renderRow(entry) {
    const hasOwner = entry.OwnerID !== null && entry.OwnerID !== undefined;
    const state = hasOwner && entry.TookState === 'awaiting-start'
      ? 'awaiting-start'
      : entry.State || (hasOwner ? 'running' : 'pending');
    const miner = entry.Miners?.length ? entry.Miners.join(', ') : entry.SpID ? entry.Miner : 'n/a';
    const ageValue = clusterTaskAge(entry, this.displayClock);
    const age = ageValue.text ?? formatTaskAgeSeconds(ageValue.seconds);
    const ageTitle = ageValue.title;
    return html`
      <tr>
        <td title=${miner}>${miner}</td>
        <td title=${entry.Name}>${entry.Name}</td>
        <td><a href="/pages/task/id/?id=${entry.ID}">${entry.ID}</a></td>
        <td>${state}</td>
        <td class="age-column" title=${ageTitle}>${age}</td>
        <td title=${entry.Owner || ''}>
          ${hasOwner
            ? html`<a href="/pages/node_info/?id=${entry.OwnerID}">${entry.Owner}</a>`
            : ''}
        </td>
      </tr>
    `;
  }

  sectionSummary(section, response) {
    return formatClusterTaskSectionSummary(
        section.entries.length,
        section.total,
        response.TotalsAvailable,
    );
  }

  renderSection(section, response) {
    const emptyMessage = clusterTaskSectionEmptyMessage(section.key, response);
    return html`
      <section class="task-section" aria-labelledby="cluster-tasks-${section.key}">
        <div class="section-heading">
          <h3 id="cluster-tasks-${section.key}">${section.title}</h3>
          <span class="section-summary">${this.sectionSummary(section, response)}</span>
        </div>
        ${emptyMessage ? html`
          <div class="empty-state">${emptyMessage}</div>
        ` : html`
          <div class="table-wrap">
            <table class="table table-dark">
              <thead>
                <tr>
                  <th>SpID</th>
                  <th>Task</th>
                  <th>ID</th>
                  <th>State</th>
                  <th class="age-column" title=${section.ageTooltip}>${section.ageLabel}</th>
                  <th>Owner</th>
                </tr>
              </thead>
              <tbody>
                ${section.groups.map((group) => this.renderTableRows(group))}
              </tbody>
            </table>
          </div>
        `}
      </section>
    `;
  }

  render() {
    const response = this.viewState.response;
    const sections = response
      ? buildClusterTaskSections(response, this.coalesceEntries)
      : [];

    return html`
      <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
      <link
        rel="stylesheet"
        href="/ux/main.css"
        onload="document.body.style.visibility = 'initial'"
      />

      ${this.renderControls()}
      ${this.renderStatus()}
      <p class="policy">${CLUSTER_TASK_ORDER_POLICY}</p>
      ${this.renderMessages()}

      ${!this.viewState.hasSuccessfulLoad && this.viewState.refreshing ? html`
        <div class="empty-state" role="status">Loading Cluster Tasks…</div>
      ` : !this.viewState.hasSuccessfulLoad && this.viewState.error ? html`
        <div class="empty-state">No task snapshot is available yet. Refresh will retry automatically.</div>
      ` : sections.map((section) => this.renderSection(section, response))}
    `;
  }
}

customElements.define('cluster-tasks', ClusterTasks);
