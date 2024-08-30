import { css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

export default css `
    *, *::before, *::after {
        box-sizing: border-box;
    }

    * {
        margin: 0;
        padding: 0;
    }

    ul, ol {
        list-style: none;
    }

    a {
        text-decoration: none;
    }
`;