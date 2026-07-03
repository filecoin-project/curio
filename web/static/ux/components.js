import { css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

export default css `
  input {
    all: unset;
    box-sizing: border-box;
    width: 100%;
    padding: 0.4rem 0.75rem;
    border: 1px solid var(--color-border-default, #30363d);
    border-radius: var(--radius-md, 6px);
    background-color: var(--color-bg-elevated, #21262d);
    color: var(--color-text-primary, #e6edf3);
    font-size: 14px;
    font-family: var(--font-sans, 'Inter', sans-serif);

    &:hover, &:focus {
        border-color: var(--color-accent-emphasis, #5e6ad2);
        box-shadow: 0 0 0 3px var(--color-accent-muted, rgba(94, 106, 210, 0.15));
    }
  }
`;
