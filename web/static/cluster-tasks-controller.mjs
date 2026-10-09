export const CLUSTER_TASK_SETTLE_MS = 5000;
export const CLUSTER_TASK_TIMEOUT_MS = 15000;
export const CLUSTER_TASK_FRESHNESS_TICK_MS = 1000;

export class ClusterTaskFreshnessTicker {
  constructor({
    onTick,
    tickMs = CLUSTER_TASK_FRESHNESS_TICK_MS,
    nowFn = Date.now,
    setTimeoutFn = globalThis.setTimeout.bind(globalThis),
    clearTimeoutFn = globalThis.clearTimeout.bind(globalThis),
  }) {
    if (typeof onTick !== 'function') {
      throw new TypeError('onTick must be a function');
    }

    this.onTick = onTick;
    this.tickMs = tickMs;
    this.nowFn = nowFn;
    this.setTimeoutFn = setTimeoutFn;
    this.clearTimeoutFn = clearTimeoutFn;
    this.active = false;
    this.timer = null;
  }

  start() {
    if (this.active) {
      return false;
    }

    this.active = true;
    this.onTick(this.nowFn());
    this.schedule();
    return true;
  }

  stop() {
    this.active = false;
    if (this.timer !== null) {
      this.clearTimeoutFn(this.timer);
      this.timer = null;
    }
  }

  schedule() {
    this.timer = this.setTimeoutFn(() => {
      this.timer = null;
      if (!this.active) {
        return;
      }
      this.onTick(this.nowFn());
      this.schedule();
    }, this.tickMs);
  }
}

function timeoutError(timeoutMs) {
  const error = new Error(`Cluster task refresh timed out after ${timeoutMs / 1000}s`);
  error.name = 'TimeoutError';
  return error;
}

function cancellationError() {
  const error = new Error('Cluster task refresh canceled');
  error.name = 'AbortError';
  return error;
}

export class ClusterTaskPollController {
  constructor({
    request,
    onStart = () => {},
    onSuccess = () => {},
    onError = () => {},
    settleMs = CLUSTER_TASK_SETTLE_MS,
    timeoutMs = CLUSTER_TASK_TIMEOUT_MS,
    setTimeoutFn = globalThis.setTimeout.bind(globalThis),
    clearTimeoutFn = globalThis.clearTimeout.bind(globalThis),
  }) {
    if (typeof request !== 'function') {
      throw new TypeError('request must be a function');
    }

    this.request = request;
    this.onStart = onStart;
    this.onSuccess = onSuccess;
    this.onError = onError;
    this.settleMs = settleMs;
    this.timeoutMs = timeoutMs;
    this.setTimeoutFn = setTimeoutFn;
    this.clearTimeoutFn = clearTimeoutFn;

    this.mounted = false;
    this.viewVisible = false;
    this.documentVisible = true;
    this.paused = false;

    this.generation = 0;
    this.currentRun = null;
    this.settleTimer = null;
    this.pendingImmediate = false;
  }

  get active() {
    return this.mounted && this.viewVisible && this.documentVisible && !this.paused;
  }

  get running() {
    return this.currentRun !== null;
  }

  setActivity(activity = {}) {
    const wasActive = this.active;

    for (const field of ['mounted', 'viewVisible', 'documentVisible', 'paused']) {
      if (Object.hasOwn(activity, field)) {
        this[field] = Boolean(activity[field]);
      }
    }

    if (!this.active) {
      this.stopActiveWork();
      return;
    }

    if (!wasActive) {
      this.refreshNow();
    }
  }

  refreshNow() {
    if (!this.active) {
      return false;
    }

    this.clearSettleTimer();
    this.pendingImmediate = true;

    if (this.currentRun) {
      this.invalidateCurrentRun();
    } else {
      this.startPendingRun();
    }

    return true;
  }

  stopActiveWork() {
    this.pendingImmediate = false;
    this.clearSettleTimer();
    this.generation += 1;

    if (!this.currentRun) {
      return;
    }

    this.currentRun.invalidated = true;
    this.clearRunTimeout(this.currentRun);
    this.currentRun.controller.abort();
    this.currentRun.interrupt(cancellationError());
  }

  invalidateCurrentRun() {
    const run = this.currentRun;
    if (!run || run.invalidated) {
      return;
    }

    run.invalidated = true;
    this.generation += 1;
    this.clearRunTimeout(run);
    run.controller.abort();
    run.interrupt(cancellationError());
  }

  startPendingRun() {
    if (!this.active || this.currentRun || !this.pendingImmediate) {
      return;
    }

    this.pendingImmediate = false;
    let rejectInterrupt;
    const interruptPromise = new Promise((_, reject) => {
      rejectInterrupt = reject;
    });
    const run = {
      controller: new AbortController(),
      generation: ++this.generation,
      invalidated: false,
      timedOut: false,
      timeoutTimer: null,
      interruptPromise,
      interrupt: (error) => {
        if (rejectInterrupt === null) {
          return;
        }
        const reject = rejectInterrupt;
        rejectInterrupt = null;
        reject(error);
      },
    };

    this.currentRun = run;
    run.timeoutTimer = this.setTimeoutFn(() => {
      if (this.currentRun !== run || run.invalidated) {
        return;
      }
      run.timedOut = true;
      run.controller.abort();
      run.interrupt(timeoutError(this.timeoutMs));
    }, this.timeoutMs);

    run.promise = this.executeRun(run);
  }

  canCommit(run) {
    return this.currentRun === run &&
      run.generation === this.generation &&
      !run.invalidated &&
      this.active;
  }

  async executeRun(run) {
    try {
      this.onStart();
      const requestPromise = this.request(run.controller.signal);
      const result = await Promise.race([requestPromise, run.interruptPromise]);
      if (!this.canCommit(run)) {
        return;
      }
      if (run.timedOut) {
        this.onError(timeoutError(this.timeoutMs), {timedOut: true});
      } else {
        this.onSuccess(result);
      }
    } catch (error) {
      if (!this.canCommit(run)) {
        return;
      }
      if (run.timedOut) {
        this.onError(timeoutError(this.timeoutMs), {timedOut: true});
      } else if (!run.controller.signal.aborted) {
        this.onError(error, {timedOut: false});
      }
    } finally {
      this.clearRunTimeout(run);
      if (this.currentRun !== run) {
        return;
      }

      this.currentRun = null;
      if (!this.active) {
        return;
      }

      if (this.pendingImmediate) {
        this.startPendingRun();
      } else {
        this.scheduleNextRun();
      }
    }
  }

  scheduleNextRun() {
    this.clearSettleTimer();
    this.settleTimer = this.setTimeoutFn(() => {
      this.settleTimer = null;
      if (!this.active || this.currentRun) {
        return;
      }
      this.pendingImmediate = true;
      this.startPendingRun();
    }, this.settleMs);
  }

  clearRunTimeout(run) {
    if (run.timeoutTimer !== null) {
      this.clearTimeoutFn(run.timeoutTimer);
      run.timeoutTimer = null;
    }
  }

  clearSettleTimer() {
    if (this.settleTimer !== null) {
      this.clearTimeoutFn(this.settleTimer);
      this.settleTimer = null;
    }
  }
}
