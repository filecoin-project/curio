import { html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import { StyledLitElement } from '/ux/StyledLitElement.mjs';

class DrawerBase extends StyledLitElement {
  static properties = {
    anchor: { type: 'left' | 'right' | 'top' | 'bottom' },
    isOpen: { type: Boolean, reflect: true },
    label: { type: String },
    onClose: { type: Function, attribute: false },
  };

  constructor() {
    super();
    this.isOpen = true; //todo
    this.label = 'DrawerBase';
    this.onClose = null;
  }

  updated(changedProperties) {
    if (changedProperties.has('isOpen')) {
      const dialog = this.shadowRoot.querySelector('dialog');

      if (this.isOpen) {
        dialog.showModal();
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
        <div class="dialog-wrapper">
<!--            <button class="btn" ><<</button>-->
            <ui-drawer-button targetSelector="#ui-dialog" @click=${this.handleToggle}></ui-drawer-button>
            <dialog
                    id="ui-dialog"
                    class="dialog"
                    aria-label=${this.label}
                    @close=${this.handleClose}
                    @cancel=${this.handleClose}
            >
                <slot name="close-button"></slot>
                <slot></slot>
            </dialog>
        </div>
    `;
  }
}

// todo: fix the jumping width issue
DrawerBase.styles = [
  css`
    .dialog-wrapper {
      //position: relative;
      //display: flex;
      border: 2px solid #1DC8CC;
    }
    
    .btn {
      position: relative;
      right: 5rem;
      background-color: red;
      z-index: 10000;
    }
    
    dialog {
      position: fixed;
      left: auto;
      right: 0;
      top: 0;
      width: max-content;
      min-height: 100vh;
      padding: 1rem;
      border: 0;
      background-color: #28282a; /* todo */
      color: var(--color-text-primary);
      box-shadow: -10px 0 20px 4px rgba(84, 59, 147, 0.8);
    }

    dialog::backdrop {
      //background: rgba(0, 0, 0, 0.5);
    }

    ::slotted([slot="close-button"]) {
      position: absolute;
      top: 1rem;
      right: 1rem;
    }
  `
]

customElements.define('ui-drawer-base', DrawerBase);