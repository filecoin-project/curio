import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/lib/clipboard-copy.mjs';

class ActorSummary extends LitElement {
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
        
        /* The hidden tooltip text */
      .deadline-entry-tooltip {
        visibility: hidden;
        background-color: rgba(50, 50, 50, 0.9);
        color: #fff;
        text-align: center;
        padding: 5px 8px;
        border-radius: 4px;
    
        /* Position it above the hovered item */
        position: absolute;
        z-index: 1;
        bottom: 125%;  /* move the tooltip above the entry box */
        left: 50%;     /* center the tooltip horizontally */
        transform: translateX(-50%);
        white-space: nowrap;
    
        /* Fade-in transition */
        opacity: 0;
        transition: opacity 0.2s;
      }
    
      /* The arrow at the bottom of the tooltip */
      .deadline-entry-tooltip::after {
        content: "";
        position: absolute;
        top: 100%; /* arrow should appear at the bottom of the tooltip */
        left: 50%;
        transform: translateX(-50%);
        border-width: 5px;
        border-style: solid;
        border-color: rgba(50, 50, 50, 0.9) transparent transparent transparent;
      }
    
      .address-container {
        display: flex;
        align-items: center;
      }
    `;

    constructor() {
        super();
        this.data = [];
        this.loadData();

        this.openDeadlineIndex = -1; // no tooltip open by default

        // Listen for clicks anywhere in the document
        document.addEventListener('click', this.handleOutsideClick.bind(this));
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        document.removeEventListener('click', this.handleOutsideClick);
    }

    async loadData() {
        this.data = await RPCCall('ActorSummary');
        this.requestUpdate();

        // Poll for updates
        setInterval(async () => {
            this.data = await RPCCall('ActorSummary');
            this.requestUpdate();
        }, 30000);
    }

    renderDeadlines(deadlines) {
        return html`
            <div class="deadline-box">
                ${
                        deadlines.map((d, index) => {
                            const countInfo = d.Count
                                    ? html`
                                        Total: ${d.Count.Total},
                                        Active: ${d.Count.Active},
                                        Live: ${d.Count.Live},
                                        Fault: ${d.Count.Fault},
                                        Recovering: ${d.Count.Recovering}
                                    `
                                    : 'No Count Info';

                            // If this tooltip is open, we show it. Otherwise, hide it.
                            const isOpen = (this.openDeadlineIndex === index);

                            return html`
                                <div
                                        class="deadline-entry-container"
                                        data-deadline-index="${index}"
                                        style="position: relative;"
                                >
                                    <div
                                            class="deadline-entry
                ${d.Current ? 'deadline-entry-cur' : ''}
                ${d.Proven ? 'deadline-proven' : ''}
                ${d.PartFaulty ? 'deadline-partially-faulty' : ''}
                ${d.Faulty ? 'deadline-faulty' : ''}"
                                    ></div>

                                    <!-- The tooltip -->
                                    <div
                                            class="deadline-entry-tooltip"
                                            style="
                  visibility: ${isOpen ? 'visible' : 'hidden'};
                  opacity: ${isOpen ? '1' : '0'};
                "
                                    >
                                        ${countInfo}
                                    </div>
                                </div>
                            `;
                        })
                }
            </div>
        `;
    }


    handleOutsideClick(e) {
        // If we have an open tooltip, and the user clicked outside it, close it
        // We'll detect if they clicked inside a .deadline-entry-container
        // that references the same index. If not, we close.
        const clickedEl = e.composedPath
            ? e.composedPath()[0]
            : e.target; // cross-browser, some use composedPath

        // We'll store a custom data attr (e.g. data-deadline-index)
        // on the container so we know which one was clicked
        if (!clickedEl.closest || !clickedEl.closest('.deadline-entry-container')) {
            // Click is outside any deadline container
            this.openDeadlineIndex = -1;
            this.requestUpdate();
            return;
        }

        // If clicked inside a container, let's see which index
        const container = clickedEl.closest('.deadline-entry-container');
        const idx = parseInt(container.dataset.deadlineIndex, 10);

        // If we clicked the same index that was open, we can close it.
        // Or if it's different, open that new one.
        if (this.openDeadlineIndex === idx) {
            this.openDeadlineIndex = -1; // toggle off
        } else {
            this.openDeadlineIndex = idx; // show new
        }
        this.requestUpdate();
    }


    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Address</th>
                    <th>Source Layer</th>
                    <th>QaP</th>
                    <th>Deadlines</th>
                    <th>Balance</th>
                    <th>Available</th>
                    <th style="min-width: 100px">Wins 1d/7d/30d</th>
                </tr>
                </thead>
                <tbody>
                ${this.data.map(entry => html`
                    <tr>
                        <td>
                            <div  class="address-container">
                                <a href="/pages/actor/?id=${entry.Address}">${entry.Address}</a>
                                <clipboard-copy .text=${entry.Address}></clipboard-copy>
                            </div>
                        </td>
                        <td>
                            ${entry.CLayers.map(layer => html`<span>${layer} </span>`)}
                        </td>
                        <td>${entry.QualityAdjustedPower}</td>
                        <td>${this.renderDeadlines(entry.Deadlines)}</td>
                        <td>${entry.ActorBalance}</td>
                        <td>${entry.ActorAvailable}</td>
                        <td>${entry.Win1}/${entry.Win7}/${entry.Win30}</td>
                    </tr>
                `)}
                </tbody>
            </table>
        `;
    }
}

customElements.define('actor-summary', ActorSummary);
