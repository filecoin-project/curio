import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

customElements.define('pdp-guide', class PDPGuideElement extends LitElement {
    static properties = {
        status: { type: Object },
        loading: { type: Boolean },
        error: { type: String },
        busy: { type: String },
        importMaterial: { type: String },
        registerName: { type: String },
        registerDescription: { type: String },
        registerLocation: { type: String },
        createdKey: { type: Object },
        actionMessage: { type: String },
        actionError: { type: String },
        expandedOk: { type: Object },
    };

    static styles = css`
        :host { display: block; color: var(--color-text-primary, #e6edf3); }
        .lead {
            color: var(--color-text-secondary, #8b949e);
            margin: 0 0 20px;
            max-width: 44rem;
        }
        .toolbar {
            display: flex;
            gap: 8px;
            align-items: center;
            margin-bottom: 16px;
        }
        .checklist { display: flex; flex-direction: column; gap: 12px; }
        .item {
            background: var(--color-bg-subtle, #161b22);
            border: 1px solid var(--color-border-default, #30363d);
            border-radius: 8px;
            padding: 16px 20px;
        }
        .item.collapsed {
            cursor: pointer;
        }
        .item.collapsed:hover {
            background: var(--color-bg-elevated, #21262d);
        }
        .item-head {
            display: flex;
            gap: 12px;
            align-items: flex-start;
        }
        .item.collapsed .item-head {
            align-items: center;
        }
        .item-head-main {
            flex: 1;
            min-width: 0;
        }
        .collapse-toggle {
            appearance: none;
            background: transparent;
            border: 0;
            color: var(--color-text-secondary, #8b949e);
            cursor: pointer;
            flex: 0 0 auto;
            font-size: 14px;
            line-height: 1;
            margin-left: auto;
            padding: 4px 2px 4px 8px;
            user-select: none;
        }
        .collapse-toggle:hover {
            color: var(--color-text-primary, #e6edf3);
        }
        .item.collapsed .item-title {
            margin: 0;
        }
        .check {
            appearance: none;
            width: 18px;
            height: 18px;
            margin-top: 2px;
            border: 1px solid var(--color-border-default, #30363d);
            border-radius: 4px;
            background: var(--color-bg-elevated, #21262d);
            flex: 0 0 auto;
            pointer-events: none;
            position: relative;
        }
        .check[data-state="ok"] {
            background: var(--color-success-muted, rgba(63,185,80,.15));
            border-color: var(--color-success-fg, #3fb950);
        }
        .check[data-state="ok"]::after {
            content: '';
            position: absolute;
            left: 5px;
            top: 1px;
            width: 5px;
            height: 10px;
            border: solid var(--color-success-fg, #3fb950);
            border-width: 0 2px 2px 0;
            transform: rotate(45deg);
        }
        .check[data-state="warn"] {
            background: var(--color-warning-muted, rgba(210,153,34,.15));
            border-color: var(--color-warning-fg, #d29922);
        }
        .check[data-state="warn"]::after {
            content: '!';
            position: absolute;
            left: 0;
            right: 0;
            top: 0;
            color: var(--color-warning-fg, #d29922);
            font-size: 11px;
            font-weight: 700;
            line-height: 16px;
            text-align: center;
        }
        .sub .check[data-state="warn"]::after { line-height: 12px; font-size: 10px; }
        .item-title {
            font-size: 16px;
            font-weight: 600;
            margin: 0;
        }
        .item-detail {
            color: var(--color-text-secondary, #8b949e);
            margin: 4px 0 0;
            font-size: 14px;
        }
        .subs {
            list-style: none;
            margin: 12px 0 0;
            padding: 0 0 0 30px;
            display: flex;
            flex-direction: column;
            gap: 8px;
        }
        .sub {
            display: flex;
            gap: 10px;
            align-items: flex-start;
            font-size: 13px;
        }
        .sub .check { width: 14px; height: 14px; margin-top: 2px; border-radius: 3px; }
        .sub .check[data-state="ok"]::after { left: 4px; top: 0; width: 4px; height: 8px; }
        .sub-label { color: var(--color-text-dense, #c9d1d9); }
        .sub-meta {
            color: var(--color-text-secondary, #8b949e);
            font-family: var(--font-mono, ui-monospace, monospace);
            font-size: 12px;
            word-break: break-all;
        }
        .actions {
            margin-top: 14px;
            padding-top: 14px;
            border-top: 1px solid var(--color-border-muted, #21262d);
        }
        .actions label {
            display: block;
            font-size: 12px;
            font-weight: 500;
            color: var(--color-text-secondary, #8b949e);
            margin-bottom: 4px;
        }
        .actions textarea,
        .actions input {
            width: 100%;
            background: var(--color-bg-elevated, #21262d);
            color: var(--color-text-primary, #e6edf3);
            border: 1px solid var(--color-border-default, #30363d);
            border-radius: 6px;
            padding: 8px 10px;
            font-family: var(--font-mono, ui-monospace, monospace);
            font-size: 13px;
            margin-bottom: 10px;
        }
        .actions .row-btns { display: flex; flex-wrap: wrap; gap: 8px; }
        .hint {
            font-size: 12px;
            color: var(--color-text-secondary, #8b949e);
            margin: -4px 0 10px;
        }
        .banner {
            border-radius: 8px;
            padding: 10px 12px;
            margin-bottom: 12px;
            font-size: 13px;
        }
        .banner.ok { background: var(--color-success-muted); color: var(--color-success-fg); }
        .banner.err { background: var(--color-danger-muted); color: var(--color-danger-fg); }
        .banner.warn { background: var(--color-warning-muted); color: var(--color-warning-fg); }
        .modal-backdrop {
            position: fixed; inset: 0; background: rgba(0,0,0,.6);
            display: flex; align-items: center; justify-content: center; z-index: 1050;
        }
        .modal-card {
            background: var(--color-bg-subtle, #161b22);
            border: 1px solid var(--color-border-default, #30363d);
            border-radius: 8px;
            width: 40rem;
            max-width: 95vw;
            padding: 20px;
        }
        .mono { font-family: var(--font-mono, ui-monospace, monospace); font-size: 13px; }
    `;

    constructor() {
        super();
        this.status = null;
        this.loading = true;
        this.error = '';
        this.busy = '';
        this.importMaterial = '';
        this.registerName = '';
        this.registerDescription = '';
        this.registerLocation = '';
        this.createdKey = null;
        this.actionMessage = '';
        this.actionError = '';
        this.expandedOk = {};
        this.refresh();
    }

    isCollapsed(key, checked) {
        return !!checked && !this.expandedOk[key];
    }

    toggleExpanded(key) {
        this.expandedOk = { ...this.expandedOk, [key]: !this.expandedOk[key] };
    }

    onCollapsedActivate(key, e) {
        if (e.type === 'keydown' && e.key !== 'Enter' && e.key !== ' ') return;
        if (e.type === 'keydown') e.preventDefault();
        this.toggleExpanded(key);
    }

    wrapItem(key, ok, title, body, warn = false) {
        // Only fully-green checks collapse; warn (!) stays open.
        const checked = !!ok && !warn;
        const collapsed = this.isCollapsed(key, checked);
        return html`
            <div class="item ${collapsed ? 'collapsed' : ''}"
                 role=${collapsed ? 'button' : undefined}
                 tabindex=${collapsed ? '0' : undefined}
                 aria-expanded=${checked ? String(!collapsed) : undefined}
                 @click=${collapsed ? (e) => this.onCollapsedActivate(key, e) : undefined}
                 @keydown=${collapsed ? (e) => this.onCollapsedActivate(key, e) : undefined}>
                <div class="item-head">
                    <span class="check" data-state=${this.stateOf(ok, warn)} aria-hidden="true"></span>
                    <div class="item-head-main">
                        <h2 class="item-title">${title}</h2>
                    </div>
                    ${checked ? html`
                        <button class="collapse-toggle" type="button"
                                aria-label=${collapsed ? 'Expand' : 'Collapse'}
                                @click=${(e) => { e.stopPropagation(); this.toggleExpanded(key); }}>
                            ${collapsed ? '▾' : '▴'}
                        </button>
                    ` : ''}
                </div>
                ${collapsed ? '' : body}
            </div>
        `;
    }

    async refresh() {
        this.loading = true;
        this.error = '';
        try {
            this.status = await RPCCall('PDPGuideStatus', []);
        } catch (e) {
            this.error = e?.message || String(e);
            this.status = null;
        } finally {
            this.loading = false;
        }
    }

    stateOf(ok, warn = false) {
        if (ok) return 'ok';
        if (warn) return 'warn';
        return 'fail';
    }

    async createWallet() {
        this.busy = 'create';
        this.actionError = '';
        this.actionMessage = '';
        try {
            this.createdKey = await RPCCall('CreatePDPKey', []);
            await this.refresh();
            this.actionMessage = 'Wallet created. Save the private key — it is shown only once.';
        } catch (e) {
            this.actionError = e?.message || String(e);
        } finally {
            this.busy = '';
        }
    }

    async importWallet(e) {
        e.preventDefault();
        this.busy = 'import';
        this.actionError = '';
        this.actionMessage = '';
        try {
            const address = await RPCCall('ImportPDPKey', [this.importMaterial.trim()]);
            this.importMaterial = '';
            await this.refresh();
            this.actionMessage = `Imported wallet ${address}.`;
        } catch (err) {
            this.actionError = err?.message || String(err);
        } finally {
            this.busy = '';
        }
    }

    async registerProvider(e) {
        e.preventDefault();
        this.busy = 'register';
        this.actionError = '';
        this.actionMessage = '';
        try {
            await RPCCall('FSRegister', [
                this.registerName.trim(),
                this.registerDescription.trim(),
                this.registerLocation.trim(),
            ]);
            await this.refresh();
            this.actionMessage = 'Registration submitted. Wait a few epochs for on-chain confirmation.';
        } catch (err) {
            this.actionError = err?.message || String(err);
        } finally {
            this.busy = '';
        }
    }

    renderWallet(wallet) {
        const configured = !!wallet?.configured;
        const balanceKnown = !!wallet?.balanceKnown;
        const funded = !!wallet?.funded;
        const actor = !!wallet?.actorExists;
        const ok = !!wallet?.ok;
        const balanceLabel = !configured
            ? '—'
            : !balanceKnown
                ? (wallet.balance || 'Err')
                : funded
                    ? (wallet.balance || 'funded')
                    : 'Unfunded';
        return this.wrapItem('wallet', ok, 'Have a wallet with balance', html`
            <p class="item-detail" style="margin-left: 30px;">${wallet?.detail || ''}</p>
            <ul class="subs">
                <li class="sub">
                    <span class="check" data-state=${this.stateOf(configured)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">PDP signing key configured</div>
                        ${wallet?.address ? html`<div class="sub-meta">${wallet.address}</div>` : ''}
                    </div>
                </li>
                <li class="sub">
                    <span class="check" data-state=${this.stateOf(!!wallet?.filAddress)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">Filecoin address (f4 / delegated)</div>
                        ${wallet?.filAddress ? html`<div class="sub-meta">${wallet.filAddress}</div>` : html`<div class="sub-meta">Derived after key import/create</div>`}
                    </div>
                </li>
                <li class="sub">
                    <span class="check" data-state=${this.stateOf(actor, configured && !actor)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">On-chain actor lookup</div>
                        <div class="sub-meta">${actor ? 'Actor present' : 'No actor yet (fund the address to create it)'}</div>
                    </div>
                </li>
                <li class="sub">
                    <span class="check" data-state=${this.stateOf(funded, configured && !balanceKnown)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">Balance</div>
                        <div class="sub-meta">${balanceLabel}</div>
                    </div>
                </li>
            </ul>
            ${!configured ? html`
                <div class="actions">
                    <div class="row-btns" style="margin-bottom: 12px;">
                        <button class="btn btn-primary btn-sm" ?disabled=${!!this.busy}
                                @click=${() => this.createWallet()}>
                            ${this.busy === 'create' ? 'Creating…' : 'Create wallet'}
                        </button>
                    </div>
                    <form @submit=${(e) => this.importWallet(e)}>
                        <label for="import-key">Import hex key or lotus wallet export</label>
                        <p class="hint">
                            Hex secp256k1 key, or output of
                            <span class="mono">lotus wallet export &lt;f4-address&gt;</span>
                            (converted to hex automatically).
                        </p>
                        <textarea id="import-key" rows="3" .value=${this.importMaterial}
                                  @input=${(e) => { this.importMaterial = e.target.value; }}
                                  placeholder="hex private key or lotus wallet export"></textarea>
                        <div class="row-btns">
                            <button class="btn btn-success btn-sm" type="submit" ?disabled=${!!this.busy || !this.importMaterial.trim()}>
                                ${this.busy === 'import' ? 'Importing…' : 'Import key'}
                            </button>
                        </div>
                    </form>
                </div>
            ` : ''}
        `);
    }

    renderStorage(storage) {
        const warn = storage?.meetsMinimum && !storage?.meetsRecommended;
        const ok = !!storage?.ok;
        return this.wrapItem('storage', ok, 'Have 20 GiB on /data or mounted storage', html`
            <p class="item-detail" style="margin-left: 30px;">${storage?.detail || ''}</p>
            <ul class="subs">
                <li class="sub">
                    <span class="check" data-state=${this.stateOf((storage?.pathCount || 0) > 0)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">Store-capable paths</div>
                        <div class="sub-meta">${storage?.pathCount ?? 0} path(s)</div>
                    </div>
                </li>
                <li class="sub">
                    <span class="check" data-state=${this.stateOf(storage?.meetsMinimum)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">At least 20 GiB available</div>
                        <div class="sub-meta">${storage?.availableHuman || '0 B'} available / ${storage?.capacityHuman || '0 B'} capacity</div>
                    </div>
                </li>
                <li class="sub">
                    <span class="check" data-state=${this.stateOf(storage?.meetsRecommended, storage?.meetsMinimum && !storage?.meetsRecommended)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">Recommended 100 GiB+</div>
                        <div class="sub-meta">${storage?.meetsRecommended ? 'Meets recommended capacity' : 'Under 100 GiB — consider adding storage'}</div>
                    </div>
                </li>
            </ul>
            ${!storage?.ok ? html`
                <div class="actions">
                    <p class="hint">
                        Curio-PDP scans writable mounts under <span class="mono">/data</span>
                        (or <span class="mono">DATA_STORAGE</span> / <span class="mono">[Subsystems].DataPath</span>).
                    </p>
                    <a class="btn btn-secondary btn-sm" href="https://docs.curiostorage.org/curio-pdp#storage" target="_blank" rel="noopener">
                        Storage docs for Curio-PDP
                    </a>
                </div>
            ` : warn ? html`
                <div class="actions">
                    <div class="banner warn">Available capacity is under 100 GiB. PDP will work, but add storage before taking significant load.</div>
                </div>
            ` : ''}
        `, warn);
    }

    renderDNS(dns) {
        const ok = !!dns?.ok;
        return this.wrapItem('dns', ok, 'Have a publicly reachable DNS name', html`
            <p class="item-detail" style="margin-left: 30px;">${dns?.detail || ''}</p>
            <ul class="subs">
                <li class="sub">
                    <span class="check" data-state=${this.stateOf(dns?.domainConfigured)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">HTTP.DomainName configured</div>
                        <div class="sub-meta">${dns?.domainName || 'not set'}${dns?.httpEnabled ? '' : ' (HTTP.Enable is false)'}</div>
                    </div>
                </li>
                <li class="sub">
                    <span class="check" data-state=${this.stateOf(dns?.reachable)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">Reachable via /pdp/ping</div>
                        <div class="sub-meta">${dns?.serviceURL || ''}${dns?.reachableDetail ? ` — ${dns.reachableDetail}` : ''}</div>
                    </div>
                </li>
            </ul>
            ${dns?.suggestTunnel ? html`
                <div class="actions">
                    <p class="hint">
                        If inbound ports are blocked, a tunnel is one possible design for exposing the PDP API.
                        Cloudflare Tunnel is an example — install and run <span class="mono">cloudflared</span>
                        yourself, point a public hostname at this node's HTTP listen address, and set
                        <span class="mono">HTTP.DomainName</span> to that hostname.
                    </p>
                    <a class="btn btn-secondary btn-sm"
                       href="https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/"
                       target="_blank" rel="noopener">
                        Cloudflare Tunnel documentation
                    </a>
                </div>
            ` : ''}
        `);
    }

    renderRegistry(registry, ready) {
        const ok = !!registry?.ok;
        return this.wrapItem('registry', ok, 'Register with FOC', html`
            <p class="item-detail" style="margin-left: 30px;">${registry?.detail || ''}</p>
            <ul class="subs">
                <li class="sub">
                    <span class="check" data-state=${this.stateOf(registry?.registered)} aria-hidden="true"></span>
                    <div>
                        <div class="sub-label">Service provider registry</div>
                        <div class="sub-meta">
                            ${registry?.registered
                                ? `Provider #${registry.providerId}${registry.name ? ` — ${registry.name}` : ''}`
                                : 'Not registered'}
                        </div>
                    </div>
                </li>
            </ul>
            ${!registry?.registered ? html`
                <div class="actions">
                    ${!ready ? html`
                        <div class="banner warn">Complete wallet, storage, and public DNS before registering.</div>
                    ` : html`
                        <form @submit=${(e) => this.registerProvider(e)}>
                            <label for="reg-name">Provider name</label>
                            <input id="reg-name" .value=${this.registerName}
                                   @input=${(e) => { this.registerName = e.target.value; }} required>
                            <label for="reg-desc">Description</label>
                            <input id="reg-desc" .value=${this.registerDescription}
                                   @input=${(e) => { this.registerDescription = e.target.value; }} required>
                            <label for="reg-loc">Location</label>
                            <input id="reg-loc" .value=${this.registerLocation}
                                   @input=${(e) => { this.registerLocation = e.target.value; }}
                                   placeholder="C=US;ST=California;L=San Francisco" required>
                            <p class="hint">Location format: <span class="mono">C=US;ST=California;L=San Francisco</span></p>
                            <div class="row-btns">
                                <button class="btn btn-primary btn-sm" type="submit" ?disabled=${!!this.busy}>
                                    ${this.busy === 'register' ? 'Registering…' : 'Register with FOC'}
                                </button>
                                <a class="btn btn-secondary btn-sm" href="/pages/pdp/">Open PDP page</a>
                            </div>
                        </form>
                    `}
                </div>
            ` : html`
                <div class="actions">
                    <a class="btn btn-secondary btn-sm" href="/pages/pdp/">Manage registration on PDP page</a>
                </div>
            `}
        `);
    }

    renderCreatedKeyModal() {
        if (!this.createdKey) return '';
        return html`
            <div class="modal-backdrop">
                <div class="modal-card">
                    <h2 class="item-title">Save your PDP wallet key</h2>
                    <div class="banner err" style="margin-top: 12px;">
                        Copy and store this private key now. It will not be shown again.
                    </div>
                    <p><strong>Ethereum address:</strong> <span class="mono">${this.createdKey.address}</span></p>
                    ${this.createdKey.filAddress ? html`
                        <p><strong>Filecoin address:</strong> <span class="mono">${this.createdKey.filAddress}</span></p>
                    ` : ''}
                    <label>Private key (hex)</label>
                    <textarea class="mono" rows="3" readonly>${this.createdKey.privateKeyHex}</textarea>
                    <div class="row-btns" style="margin-top: 12px;">
                        <button class="btn btn-primary btn-sm" @click=${() => { this.createdKey = null; }}>
                            I have saved the key
                        </button>
                    </div>
                </div>
            </div>
        `;
    }

    render() {
        const s = this.status;
        // Reachability can fail due to hairpin NAT; domain config is enough to submit registration.
        const readyForRegister = !!(s?.wallet?.ok && s?.storage?.ok && s?.dns?.domainConfigured);

        return html`
            <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
            <link rel="stylesheet" href="/ux/main.css">

            <p class="lead">
                Server-verified checklist for running as a Filecoin Onchain Cloud (FOC) PDP provider.
                Items update from internal checks — not by clicking the boxes.
            </p>

            <div class="toolbar">
                <button class="btn btn-secondary btn-sm" ?disabled=${this.loading || !!this.busy}
                        @click=${() => this.refresh()}>
                    ${this.loading ? 'Checking…' : 'Re-check'}
                </button>
            </div>

            ${this.actionMessage ? html`<div class="banner ok">${this.actionMessage}</div>` : ''}
            ${this.actionError ? html`<div class="banner err">${this.actionError}</div>` : ''}
            ${this.error ? html`<div class="banner err">${this.error}</div>` : ''}

            ${this.loading && !s ? html`<p class="item-detail">Running checks…</p>` : html`
                <div class="checklist">
                    ${this.renderWallet(s?.wallet)}
                    ${this.renderStorage(s?.storage)}
                    ${this.renderDNS(s?.dns)}
                    ${this.renderRegistry(s?.registry, readyForRegister)}
                </div>
            `}

            ${this.renderCreatedKeyModal()}
        `;
    }
});
