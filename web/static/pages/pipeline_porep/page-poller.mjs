// One abortable HTTP request at a time. The timeout cancels transport; it does
// not race an unbounded WS request and start another copy of the server work.
export class PoRepPagePoller {
    constructor({request, onStart, onSuccess, onError,
        timeoutMs = 60000, settleMs = 5000,
        setTimer = globalThis.setTimeout.bind(globalThis),
        clearTimer = globalThis.clearTimeout.bind(globalThis)}) {
        Object.assign(this, {request, onStart, onSuccess, onError, timeoutMs, settleMs, setTimer, clearTimer});
        this.active = false;
        this.run = null;
        this.timer = null;
        this.pending = false;
        this.generation = 0;
        this.failures = 0;
    }

    setActive(active) {
        if (active === this.active) return;
        this.active = active;
        if (active) this.refresh();
        else {
            this.pending = false;
            this.invalidate();
        }
    }

    invalidate() {
        this.generation++;
        this.clearTimer(this.timer);
        this.timer = null;
        if (this.run) {
            this.clearTimer(this.run.deadline);
            this.run.controller.abort();
        }
    }

    refresh() {
        this.invalidate();
        this.pending = this.active;
        if (!this.run && this.pending) void this.load();
    }

    async load() {
        if (!this.active || this.run) return;
        this.pending = false;
        const run = {controller: new AbortController(), generation: this.generation, timedOut: false};
        this.run = run;
        const current = () => this.active && this.generation === run.generation;
        run.deadline = this.setTimer(() => {
            run.timedOut = true;
            run.controller.abort();
            if (current()) this.onError(new Error('PoRep request timed out; waiting for cancellation before retry'));
        }, this.timeoutMs);
        try {
            this.onStart();
            const result = await this.request(run.controller.signal);
            if (current() && !run.controller.signal.aborted) {
                this.onSuccess(result);
                this.failures = 0;
            }
        } catch (error) {
            if (current()) {
                this.failures++;
                if (!run.timedOut) this.onError(error);
            }
        } finally {
            this.clearTimer(run.deadline);
            this.run = null;
            if (this.active) {
                if (this.pending) void this.load();
                else {
                    const delay = Math.min(30000, this.settleMs * (2 ** Math.min(this.failures, 3)));
                    this.timer = this.setTimer(() => {
                        this.timer = null;
                        void this.load();
                    }, delay);
                }
            }
        }
    }
}
