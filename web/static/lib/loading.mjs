import { html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js'

/** Shared styles for Lit shadow roots. */
export const loadingStyles = css`
  .cu-loading {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--color-text-secondary, #8b949e);
    vertical-align: middle;
  }
  .cu-loading .spinner-border {
    display: inline-block;
    width: 1rem;
    height: 1rem;
    vertical-align: text-bottom;
    border: 0.15em solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: cu-spinner 0.75s linear infinite;
  }
  .cu-loading .spinner-border-sm {
    width: 0.75rem;
    height: 0.75rem;
    border-width: 0.12em;
  }
  .cu-loading-label { font-size: 13px; }
  .cu-loading-block {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 12px 0;
    color: var(--color-text-secondary, #8b949e);
  }
  .cu-loading .visually-hidden {
    position: absolute !important;
    width: 1px; height: 1px;
    padding: 0; margin: -1px;
    overflow: hidden; clip: rect(0,0,0,0);
    white-space: nowrap; border: 0;
  }
  @keyframes cu-spinner {
    to { transform: rotate(360deg); }
  }
`

/** Plain CSS for light-DOM pages that inject a <style> tag. */
export const loadingCssText = `
  .cu-loading {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--color-text-secondary, #8b949e);
    vertical-align: middle;
  }
  .cu-loading .spinner-border {
    display: inline-block;
    width: 1rem;
    height: 1rem;
    vertical-align: text-bottom;
    border: 0.15em solid currentColor;
    border-right-color: transparent;
    border-radius: 50%;
    animation: cu-spinner 0.75s linear infinite;
  }
  .cu-loading .spinner-border-sm {
    width: 0.75rem;
    height: 0.75rem;
    border-width: 0.12em;
  }
  .cu-loading-label { font-size: 13px; }
  .cu-loading-block {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 12px 0;
    color: var(--color-text-secondary, #8b949e);
  }
  .cu-loading .visually-hidden {
    position: absolute !important;
    width: 1px; height: 1px;
    padding: 0; margin: -1px;
    overflow: hidden; clip: rect(0,0,0,0);
    white-space: nowrap; border: 0;
  }
  @keyframes cu-spinner {
    to { transform: rotate(360deg); }
  }
`

/**
 * Inline loading indicator. Prefer this over "—" / "…" while data is still fetching.
 * @param {{ label?: string, size?: 'sm' | 'md' }} [opts]
 */
export function loadingSpinner({ label = '', size = 'sm' } = {}) {
  const sizeClass = size === 'sm' ? 'spinner-border-sm' : ''
  return html`
    <span class="cu-loading" role="status" aria-live="polite" aria-label=${label || 'Loading'}>
      <span class="spinner-border ${sizeClass}" aria-hidden="true"></span>
      ${label ? html`<span class="cu-loading-label">${label}</span>` : html`<span class="visually-hidden">Loading</span>`}
    </span>
  `
}

/** Full-width / section loading row. */
export function loadingBlock(label = 'Loading…') {
  return html`
    <div class="cu-loading-block" role="status" aria-live="polite">
      <span class="spinner-border" aria-hidden="true"></span>
      <span>${label}</span>
    </div>
  `
}
