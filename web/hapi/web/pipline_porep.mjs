    import { html, css, LitElement } from 'https://cdn.skypack.dev/lit';

    class PipelinePorep extends LitElement {
        static styles = css`
            .success {
                color: green;
            }
        `;

        static properties = {
            data: { type: Array },
        };

        render() {
            return html`
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
            `;
        }
    }

    customElements.define('pipeline-porep', PipelinePorep);
