import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import './list-tile.mjs';

customElements.define('config-list', class ConfigList extends LitElement {
    static styles = css`
    .add-layer {
        display: grid;
        grid-template-columns: 1fr max-content;
        grid-column-gap: 0.75rem;
        margin-bottom: 1rem;
    }
        
    .list {
        padding: 0; // todo: why the reset is not picking it up?
        margin-bottom: 2.5rem;
    }

    .text-field {
        all: unset;
        box-sizing: border-box;
        
        width: 100%;
        padding: 0.25rem 0.75rem;
        border: 1px solid #A1A1A1;
        background-color: rgba(255, 255, 255, 0.08);
        
        font-size: 1rem;
        
        &:hover {
            box-shadow:0 0 0 1px #FFF inset;
        }
    }
    `;

    // todo
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
                this.layers = responses[0].data;
                this.layers.unshift('default');
                this.topo = responses[1].data;
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
        var v = this.addName;
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
        // todo: add loading screens

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            ${this.message ? html`<div class="alert">${this.message}</div>` : ''}

            <div class="add-layer">
                <input class="text-field" autofocus type="text" id="layername" placeholder="Layer Name" @change=${this.updateName}>
                <button class="button" @click=${this.addLayer}>Add Layer</button>
            </div>
            <ul class="list">
                ${this.layers.map((layer, index) => html`
                    <list-tile id=${index} 
                               .layerName=${layer} 
                               .layerNodes=${this.topo.filter(topo => topo.LayersCSV.split(",").includes(layer))}>
                    </list-tile>
                `)}
            </ul>
            <hr>
            <span>
            To delete a layer, use ysqlsh to issue the following command:<br>
            <code lang=sql>DELETE FROM curio.harmony_config WHERE title = 'my_layer_name';</code>
          </span>
        `
    }
});