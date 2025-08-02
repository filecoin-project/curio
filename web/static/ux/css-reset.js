import { css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

export default css `
    *, *::before, *::after {
        box-sizing: border-box;
    }

    * {
        margin: 0;
        padding: 0;
        font-family: 'JetBrains Mono', monospace;
    }

    ul, ol {
        list-style: none;
        padding: 0;
    }

    a {
        text-decoration: none;
    }

    button {
      all: unset;
      display: inline-block;
    }
  
    button:focus {
      outline: revert;
    }
`;
