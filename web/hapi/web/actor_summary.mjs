import { html, render } from 'https://unpkg.com/lit-html?module';

class ActorSummary extends HTMLElement {
    constructor() {
        super();
        this.attachShadow({ mode: 'open' });
    }

    connectedCallback() {
        this.render();
    }

    render() {
        const data = this.getData(); // Replace with your data source

        const template = html`
            ${data.map((item) => html`
                <tr>
                    <td>${item.Address}</td>
                    <td>
                        ${item.CLayers.map((layer) => html`
                            <span>${layer} </span>
                        `)}
                    </td>
                    <td>${item.QualityAdjustedPower}</td>
                    <td>
                        <div class="deadline-box">
                            ${item.Deadlines.map((deadline) => html`
                                <div class="deadline-entry
                                    ${deadline.Current ? 'deadline-entry-cur' : ''}
                                    ${deadline.Proven ? 'deadline-proven' : ''}
                                    ${deadline.PartFaulty ? 'deadline-partially-faulty' : ''}
                                    ${deadline.Faulty ? 'deadline-faulty' : ''}">
                                </div>
                            `)}
                        </div>
                    </td>
                    <td>${item.ActorBalance}</td>
                    <td>${item.ActorAvailable}</td>
                    <td>${item.WorkerBalance}</td>
                    <td>
                        <table>
                            <tr><td>1day: ${item.Win1}</td></tr>
                            <tr><td>7day: ${item.Win7}</td></tr>
                            <tr><td>30day: ${item.Win30}</td></tr>
                        </table>
                    </td>
                </tr>
            `)}
        `;

        render(template, this.shadowRoot);
    }

    getData() {
        // Replace with your data retrieval logic
        return [
            // Sample data for testing
            {
                Address: 'Address 1',
                CLayers: ['Layer 1', 'Layer 2'],
                QualityAdjustedPower: 100,
                Deadlines: [
                    { Current: true, Proven: false, PartFaulty: false, Faulty: false },
                    { Current: false, Proven: true, PartFaulty: false, Faulty: false },
                ],
                ActorBalance: 500,
                ActorAvailable: 300,
                WorkerBalance: 200,
                Win1: 10,
                Win7: 70,
                Win30: 300,
            },
            {
                Address: 'Address 2',
                CLayers: ['Layer 3', 'Layer 4'],
                QualityAdjustedPower: 200,
                Deadlines: [
                    { Current: true, Proven: false, PartFaulty: false, Faulty: false },
                    { Current: false, Proven: false, PartFaulty: true, Faulty: false },
                ],
                ActorBalance: 800,
                ActorAvailable: 400,
                WorkerBalance: 400,
                Win1: 20,
                Win7: 140,
                Win30: 600,
            },
        ];
    }
}

customElements.define('actor-summary', ActorSummary);