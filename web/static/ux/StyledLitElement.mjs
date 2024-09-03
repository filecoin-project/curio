import { LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import cssReset from "./css-reset.js";
import components from "./components.js";

// Base class including the default CSS styling
export class StyledLitElement extends LitElement {
  static _styles = [];

  static get styles() {
    const derivedStyles = this._styles || [];
    return [
      cssReset,
      components,
      ...(Array.isArray(derivedStyles) ? derivedStyles : [derivedStyles]),
    ]
  }

  static set styles(styles) {
    this._styles = styles;
  }
}
