import { html, css, LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

class DealSearch extends LitElement {
    static properties = {
        searchTerm: { type: String },
    };

    constructor() {
        super();
        this.searchTerm = '';
    }

    handleInput(event) {
        this.searchTerm = event.target.value;
    }

    handleSearch() {
        if (this.searchTerm.trim() !== '') {
            window.location.href = `/pages/mk20-deal/?id=${encodeURIComponent(this.searchTerm.trim())}`;
        }
        // If searchTerm is empty, do nothing
    }

    render() {
        return html`
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        crossorigin="anonymous"
      />
      <div class="search-container">
        <input 
          autofocus
          type="text"
          placeholder="Enter deal ID"
          @input="${this.handleInput}"
        />
        <button class="btn btn-primary" @click="${this.handleSearch}">
          Search
        </button>
      </div>
    `;
    }

    static styles = css`
    .search-container {
      display: grid;
      grid-template-columns: 1fr max-content;
      grid-column-gap: 0.75rem;
      margin-bottom: 1rem;
    }
    
    .btn {
    padding: 0.4rem 1rem;
    border: none;
    border-radius: 0;
    background-color: var(--color-form-default);
    color: var(--color-text-primary);

    &:hover, &:focus, &:focus-visible {
        background-color: var(--color-form-default-pressed);
        color: var(--color-text-secondary);
    }
  }
  `;
}

customElements.define('deal-search', DealSearch);
