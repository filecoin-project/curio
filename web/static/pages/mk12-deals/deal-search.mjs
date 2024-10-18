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
            window.location.href = `/pages/mk12-deal/?id=${encodeURIComponent(this.searchTerm.trim())}`;
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
          type="text"
          class="form-control"
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
      display: flex;
      align-items: center;
      margin-bottom: 1rem;
    }

    .search-container input {
      flex: 1;
      margin-right: 0.5rem;
    }
  `;
}

customElements.define('deal-search', DealSearch);
