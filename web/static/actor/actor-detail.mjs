    import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
    import '/actor-summary.mjs'; // <sector-expirations>

    import RPCCall from '/lib/jsonrpc.mjs';
    customElements.define('actor-detail',class Actor extends LitElement {
    connectedCallback() {
        super.connectedCallback();
        this.loadData();
    }
    static styles = css`
    .deadline-box {
            display: grid;
            grid-template-columns: repeat(16, auto);
            grid-template-rows: repeat(3, auto);
            grid-gap: 1px;
        }

        .deadline-entry {
            width: 10px;
            height: 10px;
            background-color: grey;
            margin: 1px;
        }

        .deadline-entry-cur {
            border-bottom: 3px solid deepskyblue;
            height: 7px;
        }

        .deadline-proven {
            background-color: green;
        }

        .deadline-partially-faulty {
            background-color: yellow;
        }

        .deadline-faulty {
            background-color: red;
        }
        
        .address-container {
          display: flex;
          align-items: center;
        }
     .dash-tile {
        display: flex;
        flex-direction: column;
        padding: 0.75rem;
        background: #3f3f3f;

        & b {
          padding-bottom: 0.5rem;
          color: deeppink;
        }
      }
    `;

    async loadData() {
        this.data = await RPCCall('ActorDetail', [this.id]);
        this.requestUpdate();
    }
    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            ${!this.data ? html`<div>Loading...</div>` : [this.data].map(actorInfo => html`
                <section class="section">
                <div class="row">
                    <h1>Actor overview</h1>
                </div>
                <div class="row">
                    <div class="col-md-auto">
                        <div class="info-block dash-tile">
                            <h3>Actor</h3>
                            <table><tbody>
                                <tr><td>Address:</td><td>${actorInfo.Summary.Address}</td></tr>
                                <tr><td>CLayers:</td><td>${actorInfo.Summary.CLayers}</td></tr>
                                <tr><td>QualityAdjustedPower:</td><td>${actorInfo.Summary.QualityAdjustedPower}</td></tr>
                                <tr><td>RawBytePower:</td><td>${actorInfo.Summary.RawBytePower}</td></tr>
                                <tr><td>Deadlines:</td><td>${this.renderDeadlines(actorInfo.Summary.Deadlines)}</td></tr>
                                <tr><td>ActorBalance:</td><td>${actorInfo.Summary.ActorBalance}</td></tr>
                                <tr><td>ActorAvailable:</td><td>${actorInfo.Summary.ActorAvailable}</td></tr>
                                <tr><td>WorkerBalance:</td><td>${actorInfo.Summary.WorkerBalance}</td></tr>
                                <tr><td>Wins 24h:</td><td>${actorInfo.Summary.Win1}</td></tr>
                                <tr><td>Win 7 day:</td><td>${actorInfo.Summary.Win7}</td></tr>
                                <tr><td>Win 30 day:</td><td>${actorInfo.Summary.Win30}</td></tr>
                            </tbody></table>
                        </div>
                    </div>
                    <div class="col-md-auto">
                        <div class="info-block dash-tile">
                            <h3>Wallets</h3>
                        </div>
                        <table class="table table-dark">
                            <thead>
                            <tr>
                                <th scope="col">Type</th>
                                <th scope="col">Address</th>
                                <th scope="col">Balance</th>
                            </tr>
                            </thead>
                            <tbody>
                            ${this.data.Wallets.map((wAry) => wAry.map(obj => html`
                            <tr>
                                <td>${obj.Type}</td>
                                <td>${obj.Address}</td>
                                <td>${obj.Balance}</td>
                            </tr>
                            `))}
                            </tbody>
                        </table>
                    </div>
                </div>
                <div class="row">
                    <div class="col-md-auto">
                        <div class="info-block dash-tile">
                            <h3>Expirations</h3>
                            <div style="background: #3f3f3f; height: 200px; width: 90vw;"></div>
                            <sector-expirations address="${actorInfo.Summary.Address}"></sector-expirations>
                        </div>
                    </div>
                </div>
            </section>        
        `)};
    `}
        renderDeadlines(deadlines) {
          return html`
              <div class="deadline-box">
                  ${deadlines.map(d => html`
                      <div class="deadline-entry
                          ${d.Current ? 'deadline-entry-cur' : ''}
                          ${d.Proven ? 'deadline-proven' : ''}
                          ${d.PartFaulty ? 'deadline-partially-faulty' : ''}
                          ${d.Faulty ? 'deadline-faulty' : ''}
                      "></div>
                  `)}
              </div>
          `;
      }
}
);