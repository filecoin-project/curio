import { css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

export default css `
    *, *::before, *::after {
        box-sizing: border-box;
    }

    * {
        margin: 0;
        padding: 0;
        font-family: var(--font-sans, 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif);
    }

    ul, ol {
        list-style: none;
        padding: 0;
    }

    a {
        text-decoration: none;
        color: var(--color-accent-fg, #4493f8);
    }

    button {
      all: unset;
      display: inline-block;
      cursor: pointer;
    }
  
    button:focus {
      outline: revert;
    }
`;
