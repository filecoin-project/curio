import { LitElement, html } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';

customElements.define('config-list', class NodeInfoElement extends LitElement {

    // todo
    constructor() {
        super();
        this.layers = [];
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
            <table>
                ${this.layers.map((layer, index) => html`
                  <tr>
                    <td style="width: 50%"><a href="/config/edit.html?layer=${layer}"><button>${layer}</button></a></td>
                    <td>
                    Used By: ${(f=> f.length?f.map(topo => html`${topo.Server}`):'-')(
                        this.topo.filter(topo => topo.LayersCSV.split(",").includes(layer)))} 
                    </td>
                  </tr>
              `)}
            </table>
            <input autofocus type="text" id="layername" placeholder="Layer Name" @change=${this.updateName}>
            <button class="button" @click=${this.addLayer}>Add Layer</button>
            <hr>
            <span>
            To delete a layer, use ysqlsh to issue the following command:<br>
            <code lang=sql>DELETE FROM curio.harmony_config WHERE title = 'my_layer_name';</code>
          </span>
        `
    }
});