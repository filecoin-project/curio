import { css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

export default css `
  input {
    all: unset;
    box-sizing: border-box;

    width: 100%;
    padding: 0.4rem 1rem;
    border: 1px solid #A1A1A1;
    background-color: rgba(255, 255, 255, 0.08);

    font-size: 1rem;

    &:hover, &:focus {
        box-shadow:0 0 0 1px #FFF inset;
    }
  }
`;
