import {LitElement, html, css} from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

class ClipboardCopy extends LitElement {
    static properties = {
        text: { type: String },
    };

    static styles = css`
    :host {
      display: inline-block;
    }
    .copy-icon {
      cursor: pointer;
      margin-left: 0.5em;
      color: #6c757d;
      transition: color 0.3s ease;
    }
    .copy-icon:hover {
      color: #fff;
    }
    .copied {
      color: #28a745;
    }
  `;

    constructor() {
        super();
        this.text = '';
        this.isCopied = false;
    }

    render() {
        return html`
        <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css"
                  integrity="sha512-iecdLmaskl7CVkqkXNQ/ZH/XLlvWZOJyj7Yy7tcenmpD1ypASozpmT/E0iPtmFIB46ZmdtAc9eNBvH0H/ZpiBw=="
                  crossOrigin="anonymous"/>
        <span class="copy-icon ${this.isCopied ? 'copied' : ''}" @click=${this._copyToClipboard} title="Copy to clipboard">
            <i class="far fa-clipboard"></i>
        </span>
    `;
    }

    _copyToClipboard() {
        navigator.clipboard.writeText(this.text).then(() => {
            console.log('Copied to clipboard:', this.text);
            this.isCopied = true;
            this.requestUpdate();
            setTimeout(() => {
                this.isCopied = false;
                this.requestUpdate();
            }, 2000);
        }).catch(err => {
            console.error('Failed to copy:', err);
        });
    }
}
customElements.define('clipboard-copy', ClipboardCopy);
