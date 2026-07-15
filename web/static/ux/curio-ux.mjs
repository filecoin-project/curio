import { LitElement, css, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { getUIVariant, applySkiffDocumentClass } from '/lib/ui-variant.mjs';

class CurioUX extends LitElement {
  static properties = {
    alertCount: { type: Number },
    variant: { type: String },
    version: { type: String },
    nodeCount: { type: Number },
  };

  static styles = css`
    :host {
      display: flex;
      margin: 0;
      min-height: 100vh;
      background: var(--color-bg-canvas, #0d1117);
    }

    .sidebar {
      display: flex;
      flex-direction: column;
      flex-shrink: 0;
      width: 188px;
      height: 100vh;
      max-height: 100vh;
      position: sticky;
      top: 0;
      padding: var(--space-3, 12px) var(--space-2, 8px);
      background: var(--color-bg-canvas, #0d1117);
      border-right: 1px solid var(--color-border-default, #30363d);
      box-sizing: border-box;
    }

    .brand {
      display: flex;
      align-items: center;
      gap: var(--space-2, 8px);
      margin-bottom: var(--space-3, 12px);
      padding: var(--space-1, 4px) var(--space-2, 8px);
      text-decoration: none;
      color: var(--color-text-primary, #e6edf3);
      flex-shrink: 0;
    }

    .brand:hover {
      text-decoration: none;
      color: var(--color-text-primary, #e6edf3);
    }

    .brand-name {
      font-size: 16px;
      font-weight: 600;
      letter-spacing: -0.01em;
    }

    .nav-list {
      display: flex;
      flex-direction: column;
      gap: 2px;
      flex: 1;
      min-height: 0;
      overflow-y: auto;
    }

    .nav-link {
      display: flex;
      align-items: center;
      gap: var(--space-2, 8px);
      height: 28px;
      padding: 0 var(--space-2, 8px);
      border-radius: var(--radius-md, 6px);
      font-size: 12px;
      font-weight: 400;
      color: #b1bac4;
      text-decoration: none;
      border-left: 2px solid transparent;
      transition: background-color 150ms ease, color 150ms ease;
    }

    .nav-link:hover {
      background: var(--color-bg-elevated, #21262d);
      color: var(--color-text-primary, #e6edf3);
      text-decoration: none;
    }

    .nav-link.active {
      background: var(--color-bg-elevated, #21262d);
      color: var(--color-text-primary, #e6edf3);
      border-left-color: var(--color-accent-emphasis, #5e6ad2);
      font-weight: 500;
    }

    .nav-link svg {
      flex-shrink: 0;
      opacity: 0.7;
    }

    .nav-link.active svg,
    .nav-link:hover svg {
      opacity: 1;
    }

    .sidebar-divider {
      height: 1px;
      margin: var(--space-3, 12px) 0;
      background: var(--color-border-default, #30363d);
      border: 0;
    }

    .nav-section {
      display: flex;
      flex-direction: column;
      gap: 2px;
      margin-bottom: var(--space-3, 12px);
    }

    .nav-section:last-of-type {
      margin-bottom: 0;
    }

    .nav-section-title {
      padding: 0 var(--space-2, 8px);
      margin-bottom: 2px;
      font-size: 10px;
      font-weight: 600;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: #9da5b0;
    }

    .sidebar-footer {
      flex-shrink: 0;
      margin-top: auto;
    }

    .curio-slot {
      flex: 1;
      min-width: 0;
    }

    .drawer {
      display: block;
    }

    .wide-content {
      display: none;
    }

    @media (min-width: 2210px) {
      .drawer {
        display: none;
      }

      .wide-content {
        display: block;
      }
    }

    .alert-indicator {
      display: flex;
      align-items: center;
      gap: var(--space-2, 8px);
      padding: var(--space-2, 8px);
      margin-top: var(--space-2, 8px);
      border-radius: var(--radius-md, 6px);
      font-size: 11px;
      font-weight: 500;
      text-decoration: none;
      border: 1px solid var(--color-border-default, #30363d);
      transition: background-color 150ms ease;
      white-space: nowrap;
    }

    .alert-indicator:hover {
      text-decoration: none;
    }

    .alert-indicator.has-alerts {
      background: var(--color-danger-muted, rgba(248, 81, 73, 0.15));
      color: var(--color-danger-fg, #f85149);
      border-color: var(--color-danger-fg, #f85149);
    }

    .alert-indicator.has-alerts:hover {
      background: rgba(248, 81, 73, 0.25);
      color: var(--color-danger-fg, #f85149);
    }

    .alert-indicator.no-alerts {
      background: var(--color-success-muted, rgba(63, 185, 80, 0.15));
      color: var(--color-success-fg, #3fb950);
      border-color: transparent;
    }

    .alert-indicator.no-alerts:hover {
      background: rgba(63, 185, 80, 0.25);
      color: var(--color-success-fg, #3fb950);
    }

    .alert-dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      flex-shrink: 0;
    }

    .alert-dot.active {
      background: var(--color-danger-fg, #f85149);
    }

    .alert-dot.ok {
      background: var(--color-success-fg, #3fb950);
    }

    .sidebar-meta {
      padding: 8px;
      font-size: 10px;
      line-height: 1.4;
      color: var(--color-text-muted, #656d76);
    }

    .sidebar-meta strong {
      color: var(--color-text-secondary, #8b949e);
      font-weight: 500;
    }
  `;

  constructor() {
    super();
    this.alertCount = 0;
    this.variant = 'curio';
    this.version = '';
    this.nodeCount = 0;
  }

  get isSkiff() {
    return this.variant === 'skiff';
  }

  async connectedCallback() {
    super.connectedCallback();
    document.head.innerHTML += `
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no">
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap" rel="stylesheet">
    <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-QWTKZyjpPEjISv5WaRU9OFeRpok6YctnYmDr5pNlyT2bRjXh0JMhjY6hW+ALEwIH" crossorigin="anonymous">
    <link rel="stylesheet" href="/ux/main.css">
    <link rel="stylesheet" href="/ux/bootstrap-dark.css">
    <link rel="stylesheet" href="/ux/dark-table.css" onload="document.body.style.visibility = 'initial'">
    <link rel="icon" type="image/svg+xml" href="/favicon.svg">`;

    document.documentElement.lang = 'en';
    document.documentElement.dataset.bsTheme = 'dark';
    document.documentElement.classList.add('dark');

    document.body.style.padding = '0';

    try {
      this.variant = await getUIVariant();
      applySkiffDocumentClass(this.variant);
      this.requestUpdate();
    } catch (_) {
      // keep default curio chrome
    }

    this.loadAlertStatus();
    this.loadSidebarMeta();
  }

  async loadSidebarMeta() {
    try {
      const [version, machines] = await Promise.all([
        RPCCall('Version'),
        RPCCall('ClusterMachines'),
      ]);
      this.version = version || '';
      this.nodeCount = Array.isArray(machines) ? machines.length : 0;
    } catch (_) {
      this.version = '';
      this.nodeCount = 0;
    }
    setTimeout(() => this.loadSidebarMeta(), 60000);
  }

  async loadAlertStatus() {
    try {
      const count = await RPCCall('AlertTotalCount');
      this.alertCount = count || 0;
    } catch (e) {
      this.alertCount = 0;
    }
    setTimeout(() => this.loadAlertStatus(), 30000);
  }

  render() {
    const active = window.location.pathname;
    return html`
      ${this.renderMenu(active)}
      ${this.message ? html`<div class="alert alert-primary" role="alert">${this.message}</div>` : html``}
      <slot class="curio-slot"></slot>
    `;
  }

  renderMenu(active) {
    const brand = this.isSkiff ? 'Curio PDP' : 'Curio';
    const icon = {
      overview: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M8.354 1.146a.5.5 0 0 0-.708 0l-6 6A.5.5 0 0 0 1.5 7.5v7a.5.5 0 0 0 .5.5h4.5a.5.5 0 0 0 .5-.5v-4h2v4a.5.5 0 0 0 .5.5H14a.5.5 0 0 0 .5-.5v-7a.5.5 0 0 0-.146-.354L13 5.793V2.5a.5.5 0 0 0-.5-.5h-1a.5.5 0 0 0-.5.5v1.293L8.354 1.146zM2.5 14V7.707l5.5-5.5 5.5 5.5V14H10v-4a.5.5 0 0 0-.5-.5h-3a.5.5 0 0 0-.5.5v4H2.5z"/></svg>`,
      chain: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M6 12.5a.5.5 0 0 1 .5-.5h3a.5.5 0 0 1 0 1h-3a.5.5 0 0 1-.5-.5M3 8.062C3 6.76 4.235 5.765 5.53 5.886a26.6 26.6 0 0 0 4.94 0A2.02 2.02 0 0 1 12.5 6.5c.28 0 .5.224.5.498v1.153c0 .271-.215.494-.51.562a26 26 0 0 1-4.942 0A2 2 0 0 1 3 9.374zm4.542-.827a1 1 0 0 0 .46-.799 1 1 0 0 0-.117-.87 1 1 0 0 0-.885-.516 26.6 26.6 0 0 0-4.942 0 1 1 0 0 0-.885.516 1 1 0 0 0-.117.87 1 1 0 0 0 .46.799 26.6 26.6 0 0 0 4.942 0"/></svg>`,
      tasks: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M2 1a1 1 0 0 0-1 1v4.586a1 1 0 0 0 .293.707l7 7a1 1 0 0 0 1.414 0l4.586-4.586a1 1 0 0 0 0-1.414l-7-7A1 1 0 0 0 6.586 1zm4.5 2.5a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0"/></svg>`,
      storage: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M0 2a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v1a2 2 0 0 1-2 2H2a2 2 0 0 1-2-2zm0 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v1a2 2 0 0 1-2 2H2a2 2 0 0 1-2-2zm0 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v1a2 2 0 0 1-2 2H2a2 2 0 0 1-2-2z"/></svg>`,
      pdpGuide: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M10.854 7.146a.5.5 0 0 1 0 .708l-3 3a.5.5 0 0 1-.708 0l-1.5-1.5a.5.5 0 1 1 .708-.708L7.5 9.793l2.646-2.647a.5.5 0 0 1 .708 0"/><path d="M4 1.5H3a2 2 0 0 0-2 2V14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V3.5a2 2 0 0 0-2-2h-1v1h1a1 1 0 0 1 1 1V14a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V3.5a1 1 0 0 1 1-1h1z"/><path d="M9.5 1a.5.5 0 0 1 .5.5v1a.5.5 0 0 1-.5.5h-3a.5.5 0 0 1-.5-.5v-1a.5.5 0 0 1 .5-.5zm-3-1A1.5 1.5 0 0 0 5 1.5v1A1.5 1.5 0 0 0 6.5 4h3A1.5 1.5 0 0 0 11 2.5v-1A1.5 1.5 0 0 0 9.5 0z"/></svg>`,
      pdp: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M4.5 11a.5.5 0 1 0 0-1 .5.5 0 0 0 0 1M3 10.5a.5.5 0 1 1-1 0 .5.5 0 0 1 1 0"/><path d="M16 11a2 2 0 0 1-2 2H2a2 2 0 0 1-2-2V9.51c0-.418.105-.83.305-1.197l2.472-4.531A1.5 1.5 0 0 1 4.094 3h7.812a1.5 1.5 0 0 1 1.317.782l2.472 4.53c.2.368.305.78.305 1.198zM3.655 4.26 1.592 8.043Q1.79 8 2 8h12q.21 0 .408.042L12.345 4.26a.5.5 0 0 0-.439-.26H4.094a.5.5 0 0 0-.44.26zM1 10v1a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-1a1 1 0 0 0-1-1H2a1 1 0 0 0-1 1"/></svg>`,
      config: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M8 4a.5.5 0 0 1 .5.5V6a.5.5 0 0 1-1 0V4.5A.5.5 0 0 1 8 4zM3.732 5.732a.5.5 0 0 1 .707 0l.915.914a.5.5 0 1 1-.708.708l-.914-.915a.5.5 0 0 1 0-.707zM2 10a.5.5 0 0 1 .5-.5h1.586a.5.5 0 0 1 0 1H2.5A.5.5 0 0 1 2 10zm9.5 0a.5.5 0 0 1 .5-.5h1.5a.5.5 0 0 1 0 1H12a.5.5 0 0 1-.5-.5zm.754-4.246a.389.389 0 0 0-.527-.02L7.547 9.31a.91.91 0 1 0 1.302 1.258l3.434-4.297a.389.389 0 0 0-.029-.518z"/><path fill-rule="evenodd" d="M0 10a8 8 0 1 1 15.547 2.661c-.442 1.253-1.845 1.602-2.932 1.25C11.309 13.488 9.475 13 8 13c-1.474 0-3.31.488-4.615.911-1.087.352-2.49.003-2.932-1.25A7.988 7.988 0 0 1 0 10zm8-7a7 7 0 0 0-6.603 9.329c.203.575.923.876 1.68.63C4.397 12.533 6.358 12 8 12s3.604.532 4.923.96c.757.245 1.477-.056 1.68-.631A7 7 0 0 0 8 3z"/></svg>`,
      sql: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M0 2a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H2a2 2 0 0 1-2-2V2zm15 2h-4v3h4V4zm0 4h-4v3h4V8zm0 4h-4v3h3a1 1 0 0 0 1-1v-2zm-5 3v-3H6v3h4zm-5 0v-3H1v2a1 1 0 0 0 1 1h3zm-4-4h4V8H1v3zm0-4h4V4H1v3zm5-3v3h4V4H6zm4 4H6v3h4V8z"/></svg>`,
      sectors: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M0 2a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H2a2 2 0 0 1-2-2V2zm15 2h-4v3h4V4zm0 4h-4v3h4V8zm0 4h-4v3h3a1 1 0 0 0 1-1v-2zm-5 3v-3H6v3h4zm-5 0v-3H1v2a1 1 0 0 0 1 1h3zm-4-4h4V8H1v3zm0-4h4V4H1v3zm5-3v3h4V4H6zm4 4H6v3h4V8z"/></svg>`,
      porep: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M1 2.5A1.5 1.5 0 0 1 2.5 1h3A1.5 1.5 0 0 1 7 2.5v3A1.5 1.5 0 0 1 5.5 7h-3A1.5 1.5 0 0 1 1 5.5v-3zM2.5 2a.5.5 0 0 0-.5.5v3a.5.5 0 0 0 .5.5h3a.5.5 0 0 0 .5-.5v-3a.5.5 0 0 0-.5-.5h-3zm6.5.5A1.5 1.5 0 0 1 10.5 1h3A1.5 1.5 0 0 1 15 2.5v3A1.5 1.5 0 0 1 13.5 7h-3A1.5 1.5 0 0 1 9 5.5v-3zm1.5-.5a.5.5 0 0 0-.5.5v3a.5.5 0 0 0 .5.5h3a.5.5 0 0 0 .5-.5v-3a.5.5 0 0 0-.5-.5h-3zM1 10.5A1.5 1.5 0 0 1 2.5 9h3A1.5 1.5 0 0 1 7 10.5v3A1.5 1.5 0 0 1 5.5 15h-3A1.5 1.5 0 0 1 1 13.5v-3zm1.5-.5a.5.5 0 0 0-.5.5v3a.5.5 0 0 0 .5.5h3a.5.5 0 0 0 .5-.5v-3a.5.5 0 0 0-.5-.5h-3zm6.5.5A1.5 1.5 0 0 1 10.5 9h3a1.5 1.5 0 0 1 1.5 1.5v3a1.5 1.5 0 0 1-1.5 1.5h-3A1.5 1.5 0 0 1 9 13.5v-3zm1.5-.5a.5.5 0 0 0-.5.5v3a.5.5 0 0 0 .5.5h3a.5.5 0 0 0 .5-.5v-3a.5.5 0 0 0-.5-.5h-3z"/></svg>`,
      snap: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M4.04 7.43a4 4 0 0 1 7.92 0 .5.5 0 1 1-.99.14 3 3 0 0 0-5.94 0 .5.5 0 1 1-.99-.14M4 9.5a.5.5 0 0 1 .5-.5h7a.5.5 0 0 1 .5.5v4a.5.5 0 0 1-.5.5h-7a.5.5 0 0 1-.5-.5zm1 .5v3h6v-3h-1v.5a.5.5 0 0 1-1 0V10z"/><path d="M6 2.341V2a2 2 0 1 1 4 0v.341c2.33.824 4 3.047 4 5.659v5.5a2.5 2.5 0 0 1-2.5 2.5h-7A2.5 2.5 0 0 1 2 13.5V8a6 6 0 0 1 4-5.659M7 2v.083a6 6 0 0 1 2 0V2a1 1 0 0 0-2 0m1 1a5 5 0 0 0-5 5v5.5A1.5 1.5 0 0 0 4.5 15h7a1.5 1.5 0 0 0 1.5-1.5V8a5 5 0 0 0-5-5"/></svg>`,
      market: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M12.5 16a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7m.354-5.854 1.5 1.5a.5.5 0 0 1-.708.708L13 11.707V14.5a.5.5 0 0 1-1 0v-2.793l-.646.647a.5.5 0 0 1-.708-.708l1.5-1.5a.5.5 0 0 1 .708 0"/><path d="M2 1a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v7.256A4.5 4.5 0 0 0 12.5 8a4.5 4.5 0 0 0-3.59 1.787A.5.5 0 0 0 9 9.5v-1a.5.5 0 0 0-.5-.5h-1a.5.5 0 0 0-.5.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .39-.187A4.5 4.5 0 0 0 8.027 12H6.5a.5.5 0 0 0-.5.5V16H3a1 1 0 0 1-1-1zm2 1.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5h-1a.5.5 0 0 0-.5.5m3 0v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5h-1a.5.5 0 0 0-.5.5m3.5-.5a.5.5 0 0 0-.5.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5zM4 5.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5h-1a.5.5 0 0 0-.5.5M7.5 5a.5.5 0 0 0-.5.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5zm2.5.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5h-1a.5.5 0 0 0-.5.5M4.5 8a.5.5 0 0 0-.5.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5z"/></svg>`,
      mk12: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path fill-rule="evenodd" d="M2.5 12a.5.5 0 0 1 .5-.5h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5m0-4a.5.5 0 0 1 .5-.5h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5m0-4a.5.5 0 0 1 .5-.5h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5"/></svg>`,
      mk20: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path fill-rule="evenodd" d="M5 11.5a.5.5 0 0 1 .5-.5h9a.5.5 0 0 1 0 1h-9a.5.5 0 0 1-.5-.5m0-4a.5.5 0 0 1 .5-.5h9a.5.5 0 0 1 0 1h-9a.5.5 0 0 1-.5-.5m0-4a.5.5 0 0 1 .5-.5h9a.5.5 0 0 1 0 1h-9a.5.5 0 0 1-.5-.5M3.854 2.146a.5.5 0 0 1 0 .708l-1.5 1.5a.5.5 0 0 1-.708 0l-.5-.5a.5.5 0 1 1 .708-.708L2 3.293l1.146-1.147a.5.5 0 0 1 .708 0m0 4a.5.5 0 0 1 0 .708l-1.5 1.5a.5.5 0 0 1-.708 0l-.5-.5a.5.5 0 1 1 .708-.708L2 7.293l1.146-1.147a.5.5 0 0 1 .708 0m0 4a.5.5 0 0 1 0 .708l-1.5 1.5a.5.5 0 0 1-.708 0l-.5-.5a.5.5 0 0 1 .708-.708l.146.147 1.146-1.147a.5.5 0 0 1 .708 0"/></svg>`,
      marketSettings: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M7.068.727c.243-.97 1.62-.97 1.864 0l.071.286a.96.96 0 0 0 1.622.434l.205-.211c.695-.719 1.888-.03 1.613.931l-.08.284a.96.96 0 0 0 1.187 1.187l.283-.081c.96-.275 1.65.918.931 1.613l-.211.205a.96.96 0 0 0 .434 1.622l.286.071c.97.243.97 1.62 0 1.864l-.286.071a.96.96 0 0 0-.434 1.622l.211.205c.719.695.03 1.888-.931 1.613l-.284-.08a.96.96 0 0 0-1.187 1.187l.081.283c.275.96-.918 1.65-1.613.931l-.205-.211a.96.96 0 0 0-1.622.434l-.071.286c-.243.97-1.62.97-1.864 0l-.071-.286a.96.96 0 0 0-1.622-.434l-.205.211c-.695.719-1.888.03-1.613-.931l.08-.284a.96.96 0 0 0-1.186-1.187l-.284.081c-.96.275-1.65-.918-.931-1.613l.211-.205a.96.96 0 0 0-.434-1.622l-.286-.071c-.97-.243-.97-1.62 0-1.864l.286-.071a.96.96 0 0 0 .434-1.622l-.211-.205c-.719-.695-.03-1.888.931-1.613l.284.08a.96.96 0 0 0 1.187-1.186l-.081-.284c-.275-.96.918-1.65 1.613-.931l.205.211a.96.96 0 0 0 1.622-.434zM12.973 8.5H8.25l-2.834 3.779A4.998 4.998 0 0 0 12.973 8.5m0-1a4.998 4.998 0 0 0-7.557-3.779l2.834 3.78zM5.048 3.967l-.087.065zm-.431.355A4.98 4.98 0 0 0 3.002 8c0 1.455.622 2.765 1.615 3.678L7.375 8zm.344 7.646.087.065z"/></svg>`,
      ipni: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path fill-rule="evenodd" d="M0 10.5A1.5 1.5 0 0 1 1.5 9h1A1.5 1.5 0 0 1 4 10.5v1A1.5 1.5 0 0 1 2.5 13h-1A1.5 1.5 0 0 1 0 11.5zm1.5-.5a.5.5 0 0 0-.5.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5zm10.5.5A1.5 1.5 0 0 1 13.5 9h1a1.5 1.5 0 0 1 1.5 1.5v1a1.5 1.5 0 0 1-1.5 1.5h-1a1.5 1.5 0 0 1-1.5-1.5zm1.5-.5a.5.5 0 0 0-.5.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5zM6 4.5A1.5 1.5 0 0 1 7.5 3h1A1.5 1.5 0 0 1 10 4.5v1A1.5 1.5 0 0 1 8.5 7h-1A1.5 1.5 0 0 1 6 5.5zM7.5 4a.5.5 0 0 0-.5.5v1a.5.5 0 0 0 .5.5h1a.5.5 0 0 0 .5-.5v-1a.5.5 0 0 0-.5-.5z"/><path d="M6 4.5H1.866a1 1 0 1 0 0 1h2.668A6.52 6.52 0 0 0 1.814 9H2.5q.186 0 .358.043a5.52 5.52 0 0 1 3.185-3.185A1.5 1.5 0 0 1 6 5.5zm3.957 1.358A1.5 1.5 0 0 0 10 5.5v-1h4.134a1 1 0 1 1 0 1h-2.668a6.52 6.52 0 0 1 2.72 3.5H13.5q-.185 0-.358.043a5.52 5.52 0 0 0-3.185-3.185"/></svg>`,
      snark: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M4 8a1.5 1.5 0 1 1 3 0 1.5 1.5 0 0 1-3 0m7.5-1.5a1.5 1.5 0 1 0 0 3 1.5 1.5 0 0 0 0-3"/><path d="M0 1.5A.5.5 0 0 1 .5 1h1a.5.5 0 0 1 .5.5V4h13.5a.5.5 0 0 1 .5.5v7a.5.5 0 0 1-.5.5H2v2.5a.5.5 0 0 1-1 0V2H.5a.5.5 0 0 1-.5-.5m5.5 4a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5M9 8a2.5 2.5 0 1 0 5 0 2.5 2.5 0 0 0-5 0"/><path d="M3 12.5h3.5v1a.5.5 0 0 1-.5.5H3.5a.5.5 0 0 1-.5-.5zm4 1v-1h4v1a.5.5 0 0 1-.5.5h-3a.5.5 0 0 1-.5-.5"/></svg>`,
      wallets: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M0 3a2 2 0 0 1 2-2h13.5a.5.5 0 0 1 0 1H15v2a1 1 0 0 1 1 1v8.5a1.5 1.5 0 0 1-1.5 1.5h-12A2.5 2.5 0 0 1 0 12.5zm1 1.732V12.5A1.5 1.5 0 0 0 2.5 14h12a.5.5 0 0 0 .5-.5V5H2a2 2 0 0 1-1-.268M1 3a1 1 0 0 0 1 1h12V2H2a1 1 0 0 0-1 1"/></svg>`,
      ipfs: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M8.186 1.113a.5.5 0 0 0-.372 0L1.846 3.5 8 5.961 14.154 3.5zM15 4.239l-6.5 2.6v7.922l6.5-2.6V4.24zM7.5 14.762V6.838L1 4.239v7.923zM7.443.184a1.5 1.5 0 0 1 1.114 0l7.129 2.852A.5.5 0 0 1 16 3.5v8.662a1 1 0 0 1-.629.928l-7.185 2.874a.5.5 0 0 1-.372 0L.63 13.09a1 1 0 0 1-.63-.928V3.5a.5.5 0 0 1 .314-.464z"/></svg>`,
      docs: html`<svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16"><path d="M8.5 2.687c.654-.689 1.782-.886 3.112-.752 1.234.124 2.503.523 3.388.893v9.923c-.918-.35-2.107-.692-3.287-.81-1.094-.111-2.278-.039-3.213.492zM8 1.783C7.015.936 5.587.81 4.287.94c-1.514.153-3.042.672-3.994 1.105A.5.5 0 0 0 0 2.5v11a.5.5 0 0 0 .707.455c.882-.4 2.303-.881 3.68-1.02 1.409-.142 2.59.087 3.223.877a.5.5 0 0 0 .78 0c.633-.79 1.814-1.019 3.222-.877 1.378.139 2.8.62 3.681 1.02A.5.5 0 0 0 16 13.5v-11a.5.5 0 0 0-.293-.455c-.952-.433-2.48-.952-3.994-1.105C10.413.809 8.985.936 8 1.783"/></svg>`,
    };

    const dashboards = [
      ...(this.isSkiff
        ? [{ href: '/pages/pdp-overview/', label: 'PDP Overview', icon: icon.overview }]
        : [
            { href: '/', label: 'Overview', icon: icon.overview },
            { href: '/pages/pdp-overview/', label: 'PDP Overview', icon: icon.overview },
          ]),
      { href: '/pages/chain/', label: 'Chain', icon: icon.chain },
      { href: '/pages/tasks/', label: 'Tasks', icon: icon.tasks },
      { href: '/pages/storage_paths/', label: 'Storage', icon: icon.storage },
    ];

    const settings = [
      { href: '/pages/pdp-guide/', label: 'PDP Guide', icon: icon.pdpGuide },
      { href: '/pages/wallet/', label: 'Wallets', icon: icon.wallets },
      ...(this.isSkiff
        ? [{ href: '/config/edit.html?layer=base', label: 'Configuration', icon: icon.config }]
        : [
            { href: '/pages/config/list/', label: 'Configurations', icon: icon.config },
            { href: '/pages/market-settings/', label: 'Market Settings', icon: icon.marketSettings },
          ]),
    ];

    const tools = [
      { href: '/pages/pdp/', label: 'PDP', icon: icon.pdp },
      { href: '/pages/sql/', label: 'SQL Console', icon: icon.sql },
      ...(!this.isSkiff ? [
        { href: '/sector/', label: 'Sectors', icon: icon.sectors },
        { href: '/pages/pipeline_porep/', label: 'PoRep', icon: icon.porep },
        { href: '/snap/', label: 'Snap', icon: icon.snap },
        { href: '/pages/market/', label: 'Storage Market', icon: icon.market },
        { href: '/pages/mk12-deals/', label: 'MK12', icon: icon.mk12 },
        { href: '/pages/mk20/', label: 'MK20', icon: icon.mk20 },
      ] : []),
      { href: '/pages/ipni/', label: 'IPNI', icon: icon.ipni },
      ...(!this.isSkiff ? [{ href: '/pages/proofshare/', label: 'Snark Market', icon: icon.snark }] : []),
      { href: '/pages/ipfs-content/', label: 'IPFS Content', icon: icon.ipfs },
      { href: 'https://docs.curiostorage.org/', label: 'Docs', external: true, icon: icon.docs },
    ];

    const sections = [
      { title: 'Dashboards', items: dashboards },
      { title: 'Settings', items: settings },
      { title: 'Tools', items: tools },
    ];

    const isActive = (href) => {
      if (href === '/') return active === '/' || active === '';
      if (href.startsWith('/config/edit.html')) return active.startsWith('/config/');
      return active.startsWith(href);
    };

    const renderItem = (item) => html`
      <a href="${item.href}"
         class="nav-link ${isActive(item.href) ? 'active' : ''}"
         ?target=${item.external ? '_blank' : undefined}
         aria-current=${isActive(item.href) ? 'page' : undefined}>
        ${item.icon}
        <span>${item.label}</span>
      </a>`;

    return html`
      <nav class="sidebar">
        <a href="${this.isSkiff ? '/pages/pdp-overview/' : '/'}" class="brand">
          <img src="/favicon.svg" width="24" height="24" alt="">
          <span class="brand-name">${brand}</span>
        </a>
        <hr class="sidebar-divider">
        <div class="nav-list">
          ${sections.map((section) => html`
            <div class="nav-section">
              <div class="nav-section-title">${section.title}</div>
              ${section.items.map(renderItem)}
            </div>`)}
        </div>
        <div class="sidebar-footer">
          <hr class="sidebar-divider">
          <div class="sidebar-meta">
            <div><strong>${this.version || 'curio'}</strong></div>
            <div>${this.nodeCount === 1 ? '1 node' : `${this.nodeCount} nodes`}</div>
          </div>
          <a href="/pages/alerts/" class="alert-indicator ${this.alertCount > 0 ? 'has-alerts' : 'no-alerts'}">
            <span class="alert-dot ${this.alertCount > 0 ? 'active' : 'ok'}"></span>
            <span>${this.alertCount > 0 ? `${this.alertCount} Active Alerts` : 'No Active Alerts'}</span>
          </a>
        </div>
      </nav>
    `;
  }

}

customElements.define('curio-ux', CurioUX);
