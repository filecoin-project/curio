import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

class YesNo extends LitElement {
    static properties = {
        value: { type: Boolean }
    };

    static styles = css`
    .yes {
      color: green;
    }
    .no {
      color: red;
    }
  `;

    render() {
        return html`
      <span class="${this.value ? 'yes' : 'no'}">
        ${this.value ? 'Yes' : 'No'}
      </span>
    `;
    }
}
customElements.define('yes-no', YesNo);

class FailOk extends LitElement {
    static properties = {
        value: { type: Boolean }
    };

    static styles = css`
    .success {
      color: green;
    }
    .failed {
      color: red;
    }
  `;

    render() {
        return html`
      <span class="${this.value ? 'success' : 'failed'}">
        ${this.value ? 'Success' : 'Failed'}
      </span>
    `;
    }
}
customElements.define('fail-ok', FailOk);

class DoneNotDone extends LitElement {
    static properties = {
        value: { type: Boolean }
    };

    static styles = css`
    .done {
      color: green;
    }
    .not-done {
      color: #FFD700; /* Gold color for better readability on white background */
    }
  `;

    render() {
        return html`
      <span class="${this.value ? 'done' : 'not-done'}">
        ${this.value ? 'Done' : 'Not Done'}
      </span>
    `;
    }
}

customElements.define('done-not-done', DoneNotDone);

