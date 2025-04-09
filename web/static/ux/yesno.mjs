import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

class YesNo extends LitElement {
    static properties = {
        value: { type: Boolean }
    };

    static styles = css`
    .yes {
      color: var(--color-success-main);
    }
    .no {
      color: var(--color-danger-main);
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
      color: var(--color-success-main);
    }
    .failed {
      color: var(--color-danger-main);
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
      color: var(--color-success-main);
    }
    .not-done {
      color: var(--color-warning-main);
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

class ErrorOrNot extends LitElement {
    static properties = {
        value: { type: Object }
    };

    static styles = css`
    .no-error {
      color: var(--color-success-main);
    }
    .error {
      color: var(--color-warning-main);
    }
  `;

    render() {
        const isValidValue = this.value?.Valid && this.value?.String !== '';
        const result = isValidValue ? 'Yes' : 'No';

        return html`
            <span class="${isValidValue ? 'error' : 'no-error'}">
        ${result}</span>
        `;
    }
}

customElements.define('error-or-not', ErrorOrNot);

