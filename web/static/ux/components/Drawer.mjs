import { html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import { StyledLitElement } from '/ux/StyledLitElement.mjs';

class Drawer extends StyledLitElement {
  static properties = {
    anchor: { type: 'left' | 'right' | 'top' | 'bottom', reflect: true },
    isOpen: { type: Boolean, reflect: true },
    label: { type: String },
    onClose: { type: Function, attribute: false },
  };

  constructor() {
    super();
    this.anchor = 'right';
    this.isOpen = true;
    this.label = 'Drawer';
    this.onClose = null;
  }

  updated(changedProperties) {
    if (changedProperties.has('isOpen')) {
      const dialog = this.shadowRoot.querySelector('dialog');

      if (this.isOpen) {
        dialog.show();
      } else {
        dialog.close();
      }
    }
  }

  handleClose(event) {
    const reason = event.type === 'cancel' ? 'escapeKeyDown' : 'backdropClick';
    this.isOpen = false;

    if (this.onClose) {
      this.onClose(event, reason);
    }
  }

  handleToggle() {
    this.isOpen = !this.isOpen;
  }

  render() {
    return html`
      <div>
        <button class="open-btn" @click=${this.handleToggle}>${this.label}</button>
        <dialog
          class="dialog"
          aria-label=${this.label}
          @close=${this.handleClose}
          @cancel=${this.handleClose}
        >
          <div class="dialog-header">
            <slot name="header"></slot>
            <button class="close-btn" @click=${this.handleClose}>
              <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-x-lg" viewBox="0 0 16 16">
                <path d="M2.146 2.854a.5.5 0 1 1 .708-.708L8 7.293l5.146-5.147a.5.5 0 0 1 .708.708L8.707 8l5.147 5.146a.5.5 0 0 1-.708.708L8 8.707l-5.146 5.147a.5.5 0 0 1-.708-.708L7.293 8z"/>
              </svg>
            </button>
          </div>
          <div class="dialog-content">
            <slot name="content"></slot>
          </div>
        </dialog>
      </div>
    `;
  }
}

// todo: fix the jumping width issue
Drawer.styles = [
  css`
    :host([isOpen]) .open-btn {
      visibility: hidden;
    }
    
    :host([anchor]) .dialog {
      // todo
    }
    
    .open-btn {
      position: fixed;
      top: 0;
      right: 0;
      transform-origin: bottom right;
      transform: rotate(-90deg);
      color: var(--color-text-primary);
      background-color: var(--color-secondary-light);
      border-radius: 8px 8px 0 0;
      padding: 0.75rem 1.2rem;

      &:hover, &:active {
        cursor: pointer;
        background-color: var(--color-secondary-main);
      }
    }
    
    dialog {
      position: fixed;
      top: 0;
      bottom: 0;
      right: 0;
      left: auto;
      width: 40rem;
      min-height: 100vh;
      max-height: 100vh;
      padding: 1rem;
      border: 0;
      background-color: var(--color-fg);
      color: var(--color-text-primary);
      box-shadow: -8px 0 20px 4px var(--color-shadow-main);
      overflow-y: auto;

      .dialog-header {
        display: flex;
        justify-content: space-between;
        align-items: start;
        max-width: inherit;

        ::slotted(*) {
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }

        .close-btn {
          all: unset;
          margin-left: 1.5rem;
          background: transparent;
          color: var(--color-text-primary);

          &:hover, &:active {
            cursor: pointer;
            color: var(--color-text-primary);
            opacity: 0.8;
          }
        }
      }

      .dialog-content {
        //max-height: 100%;
        //overflow-y: auto;
      }
    }
  `
]

customElements.define('ui-drawer', Drawer);
