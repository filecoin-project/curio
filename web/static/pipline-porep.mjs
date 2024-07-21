    import { html, css, LitElement } from 'https://cdn.skypack.dev/lit';

    class PipelinePorep extends LitElement {
        constructor() {
            super();
            this.data = [];
        }
    
        async loadData() {
            this.data = await RPCCall('PorepPipelineSummary');
            setTimeout(() => this.loadData(), 5000);
            super.requestUpdate();
        }
    
        connectedCallback() {
            this.loadData();
            this.render();
        }

        render() {
            return html`
                <div class="row">
                <div class="col-md-auto" style="max-width: 1000px">
                    <div class="info-block">
                        <h2>PoRep Pipeline</h2>
                        <table class="table table-dark">
                            <thead>
                            <tr>
                                <th>Address</th>
                                <th>SDR</th>
                                <th>Trees</th>
                                <th>Precommit Msg</th>
                                <th>Wait Seed</th>
                                <th>PoRep</th>
                                <th>Commit Msg</th>
                                <th>Done</th>
                                <th>Failed</th>
                            </tr>
                            </thead>
                            <tbody>
                            ${this.data.map(
                                item => html`
                                
                                    <tr>
                                        <td><b>${item.Actor}</b></td>
                                        <td class=${item.CountSDR !== 0 ? 'success' : ''}>${item.CountSDR}</td>
                                        <td class=${item.CountTrees !== 0 ? 'success' : ''}>${item.CountTrees}</td>
                                        <td class=${item.CountPrecommitMsg !== 0 ? 'success' : ''}>${item.CountPrecommitMsg}</td>
                                        <td class=${item.CountWaitSeed !== 0 ? 'success' : ''}>${item.CountWaitSeed}</td>
                                        <td class=${item.CountPoRep !== 0 ? 'success' : ''}>${item.CountPoRep}</td>
                                        <td class=${item.CountCommitMsg !== 0 ? 'success' : ''}>${item.CountCommitMsg}</td>
                                        <td>${item.CountDone}</td>
                                        <td>${item.CountFailed}</td>
                                    </tr>
                                `
                            )}
                            </tbody>
                        </table>
                    </div>
                </div>
                <div class="col-md-auto">
                </div>
            </div>
            `;
        }
    }

    customElements.define('pipeline-porep', PipelinePorep);
