import { html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import { StyledLitElement } from '/ux/StyledLitElement.mjs';

/**
 * A Drawer component that displays content in a scrollable panel.
 *
 * @element ui-drawer
 *
 * @property {String} anchor - The side from which the drawer appears. Valid values: `left`, `right`, `top`, `bottom`. Default is `right`.
 * @property {Boolean} isOpen - Indicates whether the drawer is open. Default is `true`.
 * @property {String} label - The label for the drawer's open button.
 * @property {Function} onClose - Callback function invoked when the drawer is closed.
 *
 * @slot title - Content to be placed in the drawer's heading.
 * @slot content - Main content of the drawer.
 *
 * @example
 * <ui-drawer label="Menu">
 *   <h2 slot="title">Menu</h2>
 *   <div slot="content">
 *     ...
 *   </div>
 * </ui-drawer>
 */
class Drawer extends StyledLitElement {
  static properties = {
    anchor: { type: String, reflect: true },
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
    this.dialog = null;
  }

  set anchor(value) {
    const validAnchors = ['left', 'right', 'top', 'bottom'];
    this._anchor = validAnchors.includes(value) ? value : 'right';
  }

  get anchor() {
    return this._anchor;
  }

  firstUpdated() {
    this.dialog = this.shadowRoot.querySelector('dialog');
  }

  updated(changedProperties) {
    if (changedProperties.has('isOpen') && this.dialog) {
      if (this.isOpen) {
        this.dialog.show();
      } else {
        this.dialog.close();
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
          anchor=${this.anchor}
          @close=${this.handleClose}
          @cancel=${this.handleClose}
        >
          <div class="dialog-heading">
            <slot name="title"></slot>
            <button class="close-btn" @click=${this.handleClose} aria-label="Close">
              <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" class="bi bi-x-lg" viewBox="0 0 16 16">
                <path d="M2.146 2.854a.5.5 0 1 1 .708-.708L8 7.293l5.146-5.147a.5.5 0 0 1 .708.708L8.707 8l5.147 5.146a.5.5 0 0 1-.708.708L8 8.707l-5.146 5.147a.5.5 0 0 1-.708-.708L7.293 8z"/>
              </svg>
            </button>
          </div>
          <div>
            <slot name="content"></slot>
          </div>
        </dialog>
      </div>
    `;
  }
}

Drawer.styles = [
  css`
    :host([isOpen]) .open-btn {
      visibility: hidden;
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
      font-size: 0.9rem;

      &:hover, &:active {
        cursor: pointer;
        background-color: var(--color-secondary-main);
      }
    }
    
    dialog {
      position: fixed;
      padding: 1rem;
      border: 0;
      background-color: var(--color-fg);
      color: var(--color-text-primary);
      overflow-y: auto;
      z-index: 10;

      &[anchor="right"] {
        top: 0;
        bottom: 0;
        right: 0;
        left: auto;
        width: 40rem;
        min-height: 100vh;
        max-height: 100vh;
        box-shadow: -8px 0 20px 4px var(--color-shadow-main);
      }
      
      &[anchor="left"] {
        top: 0;
        bottom: 0;
        right: auto;
        left: 0;
        width: 40rem;
        min-height: 100vh;
        max-height: 100vh;
        box-shadow: 8px 0 20px 4px var(--color-shadow-main);
      }

      &[anchor="top"] {
        top: 0;
        bottom: auto;
        right: 0;
        left: 0;
        min-width: 100vw;
        max-width: 100vw;
        min-height: 10rem;
        max-height: 50vh;
        box-shadow: 0 8px 20px 4px var(--color-shadow-main);
        padding: 1rem 2rem;
      }
      
      &[anchor="bottom"] {
        top: auto;
        bottom: 0;
        right: 0;
        left: 0;
        min-width: 100vw;
        max-width: 100vw;
        min-height: 10rem;
        max-height: 50vh;
        box-shadow: 0 -8px 20px 4px var(--color-shadow-main);
        padding: 1rem 2rem;
      }

      .dialog-heading {
        display: flex;
        justify-content: space-between;
        align-items: baseline;
        max-width: inherit;

        ::slotted(*) {
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }

        .close-btn {
          width: 1.5rem;
          height: 1.5rem;
          margin-left: 1.5rem;
          text-align: center;
          background: transparent;
          color: var(--color-text-primary);
          
          &:hover, &:active {
            cursor: pointer;
            color: var(--color-text-primary);
            opacity: 0.8;
          }
        }
      }
    }
  `
]

customElements.define('ui-drawer', Drawer);
