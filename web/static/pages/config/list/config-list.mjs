import { html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import 'https://cdn.jsdelivr.net/npm/axios@1.7.7/dist/axios.min.js';
import { StyledLitElement } from '/ux/StyledLitElement.mjs';
import './list-tile.mjs';

class ConfigList extends StyledLitElement {
  constructor() {
    super();
    this.layers = []; // string[]
    this.topo = [];
    this.loadData();
    this.message = this.readCookie('message');
    this.eraseCookie('message');
    this.addName = '';
  }

  async loadData() {
    Promise.all([
      axios.get('/api/config/layers'),
      axios.get('/api/config/topo'),
      axios.get('/api/config/default')
    ])
      .then(responses => {
        this.layers = responses[0]?.data;
        this.layers.unshift('default');
        this.topo = responses[1]?.data;
        this.requestUpdate();
      })
      .catch(error => {
        console.error('Error fetching data:', error);
      });
  }

  readCookie(name) {
    const cookies = document.cookie.split(';');
    for (let i = 0; i < cookies.length; i++) {
      const cookie = cookies[i].trim();
      if (cookie.startsWith(name + '=')) {
        return cookie.substring(name.length + 1);
      }
    }
    return '';
  }

  eraseCookie(name) {
    document.cookie = name + '=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/;';
  }

  updateName(e) {
    this.addName = e.target.value;
  }

  addLayer() {
    // get a name
    const v = this.addName;
    if (v === '') {
      alert('Error: Layer name cannot be empty');
      return;
    }

    axios.post('/api/config/addlayer', { name: v })
      .then(response => {
        window.location.href = '/config/edit.html?layer=' + v;
      })
      .catch(error => {
        alert('Error adding layer:', error);
      });
  }

  render() {
    return html`
      <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">

      ${this.message ? html`<div class="alert">${this.message}</div>` : ''}

      <ul>
        ${this.layers.map((layer, index) => html`
          <list-tile id=${index}
                     .layerName=${layer}
                     .layerNodes=${this.topo.filter(topo => topo.LayersCSV.split(",").includes(layer))}></list-tile>
        `)}
      </ul>
      <div class="add-layer">
        <input autofocus type="text" id="layername" placeholder="Layer Name" @change=${this.updateName}>
        <button class="btn" @click=${this.addLayer}>Add Layer</button>
      </div>

      <hr>
      <span>
        To delete a layer, use ysqlsh to issue the following command:<br>
        <code lang=sql>DELETE FROM curio.harmony_config WHERE title = 'my_layer_name';</code>
      </span>
    `
  }
}

ConfigList.styles =  css`
  .alert {
    font-size: 24px;
    display: inline-block;
    color: green;
  }

  .add-layer {
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

customElements.define('config-list', ConfigList)