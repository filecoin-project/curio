import {html, css} from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import {StyledLitElement} from "../../../ux/StyledLitElement.mjs";
// todo: update global font size to automatically scale for smaller devices

class ListTile extends StyledLitElement {

    static get properties() {
        return {
            layerName: { type: String },
            layerNodes: { type: Array },
        };
    }

    constructor() {
        super();
        this.layerName = '';
        this.layerNodes = [];
    }

    render() {
        return html`
            <a class="anchor" href="/config/edit.html?layer=${this.layerName}">
                <li class="tile">
                    <div>${this.layerName}</div>
                    <div>Used by: ${this.layerNodes.length
                            ? html`<span><b>${this.layerNodes.length}</b>&nbsp;nodes</span>`
                            : html`<span>&ndash;</span>`}
                    </div>
                </li>
            </a>
        `
    }
}

ListTile.styles = [
    css`
        .anchor {
            color: var(--color-text-primary);
            font-size: 1rem;
        }
    
        .tile {
            display: grid;
            grid-template-columns: repeat(2, 1fr);
            grid-column-gap: 0.5rem;
            padding: 0.5rem 1.5rem;
            margin-bottom: 0.5rem;
            
            background-color: rgba(255, 255, 255, 0.06);
            
            &:hover {
                background-color: #432E64;
            }
        }
    `
]

customElements.define('list-tile', ListTile);