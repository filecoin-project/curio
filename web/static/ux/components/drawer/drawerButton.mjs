import { html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import { StyledLitElement } from '/ux/StyledLitElement.mjs';

export class DrawerButton extends StyledLitElement {
  static properties = {
    targetSelector: { type: String },
  };

  constructor() {
    super();
    this.targetSelector = '';
    this._updatePosition = this._updatePosition.bind(this);
  }

  firstUpdated() {
    this._setupObserver();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this._cleanup();
  }

  _setupObserver() {
    const targetElement = document.querySelector(this.targetSelector);

    if (!targetElement) {
      console.error(`Target element with selector "${this.targetSelector}" not found`);
      return;
    }
    this.observer = new IntersectionObserver(() => this._updatePosition());
    this.observer.observe(targetElement);

    window.addEventListener('resize', this._updatePosition);
    window.addEventListener('scroll', this._updatePosition);
    this._updatePosition();
  }

  _updatePosition() {
    const targetElement = document.querySelector(this.targetSelector);

    if (!targetElement) return;

    const targetRect = targetElement.getBoundingClientRect();
    const scrollX = window.scrollX || window.pageXOffset;
    const scrollY = window.scrollY || window.pageYOffset;

    this.style.left = `${targetRect.left + scrollX}px`;
    this.style.top = `${targetRect.top + scrollY}px`;
  }

  _cleanup() {
    if (this.observer) {
      this.observer.disconnect();
    }
    window.removeEventListener('resize', this._updatePosition);
    window.removeEventListener('scroll', this._updatePosition);
  }

  render() {
    return html`
      <button><<</button>
    `;
  }
}

DrawerButton.styles = [
  css`
    button {
      background-color: red;
      width: 500px;
      height: 100px;
    }
  `
]

customElements.define('ui-drawer-button', DrawerButton);