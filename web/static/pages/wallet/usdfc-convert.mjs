import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

/** Fixed size used only to sample the spot conversion rate. */
const RATE_SAMPLE_USDFC = '1';

customElements.define('usdfc-convert', class UsdfcConvertElement extends LitElement {
    static properties = {
        keyStatus: { type: Object },
        keyStatusLoading: { type: Boolean },
        amount: { type: String },
        slippageBps: { type: Number },
        quote: { type: Object },
        quoteError: { type: String },
        quoting: { type: Boolean },
        converting: { type: Boolean },
        result: { type: Object },
        convertError: { type: String },
        rateFilPerUsdfc: { type: String },
        rateError: { type: String },
    };

    constructor() {
        super();
        this.keyStatus = undefined;
        this.keyStatusLoading = true;
        this.amount = '';
        this.slippageBps = 100;
        this.quote = null;
        this.quoteError = '';
        this.quoting = false;
        this.converting = false;
        this.result = null;
        this.convertError = '';
        this.rateFilPerUsdfc = '';
        this.rateError = '';
        this._quoteDebounce = null;
        this._quoteInterval = null;
        this._rateInterval = null;
        this.loadKeyStatus();
    }

    connectedCallback() {
        super.connectedCallback();
        this._refreshHandle = setInterval(() => this.loadKeyStatus({ silent: true }), 15000);
        if (this.isVisible()) {
            this.startRatePolling();
            if (this.quoteAmount()) {
                this.startQuotePolling();
            }
        }
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this._refreshHandle) {
            clearInterval(this._refreshHandle);
            this._refreshHandle = null;
        }
        this.stopQuotePolling();
        this.stopRatePolling();
        if (this._quoteDebounce) {
            clearTimeout(this._quoteDebounce);
            this._quoteDebounce = null;
        }
    }

    isVisible() {
        return !this.keyStatusLoading && this.keyStatus?.configured && this.keyStatus?.usdfcKnown;
    }

    async loadKeyStatus({ silent = false } = {}) {
        // Only show the loading shell on the first fetch. Periodic refreshes must
        // not remount the form — that blanks inputs and eats in-progress typing.
        if (!silent && this.keyStatus === undefined) {
            this.keyStatusLoading = true;
        }
        try {
            this.keyStatus = await RPCCall('PDPKeyStatus', []);
        } catch (error) {
            console.error('Failed to load PDP key status:', error);
            if (this.keyStatus === undefined) {
                this.keyStatus = null;
            }
        } finally {
            this.keyStatusLoading = false;
        }
        if (this.isVisible()) {
            this.startRatePolling();
        } else {
            this.stopRatePolling();
            this.stopQuotePolling();
        }
    }

    onAmountInput(e) {
        this.amount = e.target.value;
        this.result = null;
        this.convertError = '';
        this.scheduleQuote();
    }

    onSlippageInput(e) {
        const v = parseInt(e.target.value, 10);
        this.slippageBps = Number.isFinite(v) ? v : 100;
        this.scheduleQuote();
    }

    quoteAmount() {
        const amount = (this.amount || '').trim();
        if (!amount || Number(amount) <= 0) {
            return '';
        }
        return amount;
    }

    scheduleQuote() {
        if (this._quoteDebounce) {
            clearTimeout(this._quoteDebounce);
        }
        this.quoteError = '';
        if (!this.quoteAmount()) {
            this.quote = null;
            this.stopQuotePolling();
            return;
        }
        // Keep the previous quote visible while the new one loads so typing
        // does not blank the expected-out display.
        this._quoteDebounce = setTimeout(() => {
            this._quoteDebounce = null;
            this.fetchQuote(true);
            this.startQuotePolling();
        }, 400);
    }

    startQuotePolling() {
        this.stopQuotePolling();
        this._quoteInterval = setInterval(() => this.fetchQuote(false), 30000);
    }

    stopQuotePolling() {
        if (this._quoteInterval) {
            clearInterval(this._quoteInterval);
            this._quoteInterval = null;
        }
    }

    startRatePolling() {
        if (this._rateInterval) {
            return;
        }
        this.fetchRate();
        this._rateInterval = setInterval(() => this.fetchRate(), 30000);
    }

    stopRatePolling() {
        if (this._rateInterval) {
            clearInterval(this._rateInterval);
            this._rateInterval = null;
        }
    }

    formatRate(quote) {
        if (!quote?.amountOutAtto || !quote?.amountInUsdfc) {
            return '';
        }
        const amountIn = Number(quote.amountInUsdfc);
        if (!Number.isFinite(amountIn) || amountIn <= 0) {
            return '';
        }
        // amountOutAtto is attoFIL (1e18). USDFC amount is in whole tokens.
        const outFil = Number(quote.amountOutAtto) / 1e18;
        if (!Number.isFinite(outFil)) {
            return '';
        }
        const rate = outFil / amountIn;
        if (!Number.isFinite(rate) || rate <= 0) {
            return '';
        }
        return rate.toPrecision(6);
    }

    async fetchRate() {
        if (this.converting) {
            return;
        }
        try {
            const quote = await RPCCall('PDPUsdfcFilQuote', [RATE_SAMPLE_USDFC]);
            const rate = this.formatRate(quote);
            if (rate) {
                this.rateFilPerUsdfc = rate;
                this.rateError = '';
            }
        } catch (error) {
            console.error('Rate quote failed:', error);
            // Keep the last good rate; only surface an error if we have none yet.
            if (!this.rateFilPerUsdfc) {
                this.rateError = error.message || String(error);
            }
        }
    }

    async fetchQuote(showSpinner = false) {
        const amount = this.quoteAmount();
        if (!amount || this.converting) {
            return;
        }
        if (showSpinner) {
            this.quoting = true;
        }
        this.quoteError = '';
        try {
            this.quote = await RPCCall('PDPUsdfcFilQuote', [amount]);
            const rate = this.formatRate(this.quote);
            if (rate) {
                this.rateFilPerUsdfc = rate;
                this.rateError = '';
            }
        } catch (error) {
            console.error('Quote failed:', error);
            this.quoteError = error.message || String(error);
        } finally {
            if (showSpinner) {
                this.quoting = false;
            }
        }
    }

    async convert() {
        const amount = (this.amount || '').trim();
        if (!amount || Number(amount) <= 0) {
            alert('Enter a positive USDFC amount.');
            return;
        }
        if (!confirm(`Convert ${amount} USDFC to FIL via SushiSwap V3 (slippage ${this.slippageBps} bps)?`)) {
            return;
        }
        this.converting = true;
        this.convertError = '';
        this.result = null;
        try {
            this.result = await RPCCall('PDPConvertUsdfcToFil', [amount, this.slippageBps]);
            await this.loadKeyStatus({ silent: true });
            await this.fetchRate();
        } catch (error) {
            console.error('Convert failed:', error);
            this.convertError = error.message || String(error);
        } finally {
            this.converting = false;
        }
    }

    shortHash(h) {
        if (!h) return '';
        if (h.length <= 14) return h;
        return h.slice(0, 8) + '…' + h.slice(-6);
    }

    filfoxTx(h) {
        return `https://filfox.info/en/message/${h}`;
    }

    render() {
        const visible = this.isVisible();
        if (this.keyStatusLoading) {
            return html`
                <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
                <link rel="stylesheet" href="/ux/main.css">
                <h2>Convert USDFC → FIL</h2>
                <p class="text-muted">Loading wallet status…</p>
            `;
        }
        if (!visible) {
            return html``;
        }

        return html`
            <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
            <link rel="stylesheet" href="/ux/main.css">

            <h2>Convert USDFC → FIL</h2>
            <p class="text-muted">
                Swap PDP wallet USDFC to native FIL via SushiSwap V3 (mainnet).
                Requires FIL for gas. Liquidity is limited — large amounts may slip.
            </p>
            <p class="mb-2">
                Available: <strong>${this.keyStatus.usdfcBalance || '—'}</strong>
                · FIL: <strong>${this.keyStatus.balance || '—'}</strong>
            </p>
            <p class="mb-3">
                Rate:
                ${this.rateFilPerUsdfc
                    ? html`<strong>≈ ${this.rateFilPerUsdfc} FIL / USDFC</strong>
                        <span class="text-muted">(refreshes every 30s)</span>`
                    : this.rateError
                        ? html`<span class="text-warning">${this.rateError}</span>`
                        : html`<span class="text-muted">fetching…</span>`}
            </p>

            <div class="row g-2 align-items-end mb-3" style="max-width: 40rem;">
                <div class="col-md-5">
                    <label class="form-label" for="usdfc-amount">Amount (USDFC)</label>
                    <input id="usdfc-amount" class="form-control" type="text" inputmode="decimal"
                           @input=${this.onAmountInput}
                           ?disabled=${this.converting} placeholder="e.g. 10.5">
                </div>
                <div class="col-md-3">
                    <label class="form-label" for="slippage-bps">Slippage (bps)</label>
                    <input id="slippage-bps" class="form-control" type="number" min="0" max="5000"
                           value="100" @input=${this.onSlippageInput}
                           ?disabled=${this.converting}>
                </div>
                <div class="col-md-4">
                    <button class="btn btn-primary w-100" @click=${this.convert}
                            ?disabled=${this.converting || this.quoting || !(this.amount || '').trim()}>
                        ${this.converting ? 'Converting…' : 'Convert'}
                    </button>
                </div>
            </div>

            ${this.quoting && !this.quote ? html`<p class="text-muted">Fetching quote…</p>` : ''}
            ${this.quoteError ? html`<div class="alert alert-warning">${this.quoteError}</div>` : ''}
            ${this.quote ? html`
                <div class="alert alert-secondary">
                    Expected ≈ <strong>${this.quote.amountOutFil} FIL</strong>
                    for ${this.quote.amountInUsdfc} USDFC
                    (min out uses ${this.slippageBps} bps slippage).
                    ${this.quoting ? html`<span class="text-muted"> Updating…</span>` : ''}
                </div>
            ` : ''}

            ${this.convertError ? html`<div class="alert alert-danger">${this.convertError}</div>` : ''}
            ${this.result ? html`
                <div class="alert alert-success">
                    <div>Conversion submitted.</div>
                    ${this.result.quotedOutFil ? html`<div>Quoted: ${this.result.quotedOutFil} FIL (min ${this.result.minOutFil})</div>` : ''}
                    ${this.result.approveTxHash ? html`
                        <div>Approve:
                            <a href="${this.filfoxTx(this.result.approveTxHash)}" target="_blank" rel="noopener">
                                ${this.shortHash(this.result.approveTxHash)}
                            </a>
                        </div>
                    ` : ''}
                    <div>Swap → FIL:
                        <a href="${this.filfoxTx(this.result.swapTxHash)}" target="_blank" rel="noopener">
                            ${this.shortHash(this.result.swapTxHash)}
                        </a>
                    </div>
                    <div class="mt-1"><a href="/pages/chain/">View pending chain messages</a></div>
                </div>
            ` : ''}
        `;
    }
});
