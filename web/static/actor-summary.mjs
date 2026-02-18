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

        .deadline-entry-container {
            position: relative;
            display: inline-block;
        }

        .deadline-entry {
            width: 10px;
            height: 10px;
            background-color: grey;
            margin: 1px;
            cursor: pointer;
            box-sizing: border-box;
        }

        .deadline-entry:hover {
            outline: 1px solid white;
        }

        .deadline-entry-cur {
            border-bottom: 3px solid deepskyblue;
        }

        .deadline-entry-cur-pending {
            border-bottom: 3px solid deepskyblue;
            animation: blink-pending 1s ease-in-out infinite;
        }

        .deadline-entry-cur-danger {
            border-bottom: 3px solid deepskyblue;
            animation: blink-danger 0.5s ease-in-out infinite;
        }

        @keyframes blink-pending {
            0%, 100% {
                border-bottom-color: deepskyblue;
            }
            50% {
                border-bottom-color: #FF6B00;
            }
        }

        @keyframes blink-danger {
            0%, 100% {
                border-bottom-color: deepskyblue;
            }
            50% {
                border-bottom-color: #DC143C;
            }
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

        .deadline-info-box {
            position: absolute;
            bottom: 100%;
            left: 50%;
            transform: translateX(-50%);
            background-color: var(--color-form-group-1, #1a2a3a);
            border: 1px solid #555;
            padding: 8px 12px;
            z-index: 10000;
            min-width: 180px;
            box-shadow: 0 4px 12px rgba(0,0,0,0.5);
            font-size: 0.85em;
            white-space: nowrap;
            margin-bottom: 4px;
            pointer-events: none;
        }

        .deadline-info-box .info-row {
            margin: 3px 0;
        }

        .deadline-info-box .info-label {
            color: #aaa;
        }

        .deadline-info-box .info-value {
            color: #fff;
            font-weight: 500;
        }

        .deadline-info-box .post-status {
            margin-top: 6px;
            padding-top: 6px;
            border-top: 1px solid #444;
        }

        .deadline-info-box .post-submitted {
            color: #4BB543;
        }

        .deadline-info-box .post-pending {
            color: #FFD600;
        }

        .deadline-info-box .deadline-header {
            font-weight: 600;
            margin-bottom: 6px;
            padding-bottom: 4px;
            border-bottom: 1px solid #444;
        }
    `;

    static properties = {
        data: { type: Array },
        _hoverState: { state: true }  // { actorIndex, deadlineIndex }
    };

    constructor() {
        super();
        this.data = [];
        this._hoverState = null;
        this.loadData();
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

    _handleEntryMouseEnter(actorIndex, deadlineIndex) {
        this._hoverState = { actorIndex, deadlineIndex };
    }

    _handleEntryMouseLeave() {
        this._hoverState = null;
    }

    _handleEntryClick(e, actorAddress, deadlineIndex) {
        e.preventDefault();
        window.location.href = `/pages/deadline/?sp=${actorAddress}&deadline=${deadlineIndex}`;
    }

    renderDeadlines(actorIndex, actorAddress, deadlines) {
        return html`
            <div class="deadline-box">
                ${deadlines.map((d, index) => {
                    const isHovered = this._hoverState?.actorIndex === actorIndex && 
                                     this._hoverState?.deadlineIndex === index;

                    // Determine cursor class: blink if current deadline has pending PoSts
                    // Use danger blink (crimson) if 15+ minutes into deadline without all PoSts submitted
                    let cursorClass = '';
                    if (d.Current) {
                        if (d.PartitionCount > 0 && !d.PartitionsProven) {
                            cursorClass = d.ElapsedMinutes >= 15 ? 'deadline-entry-cur-danger' : 'deadline-entry-cur-pending';
                        } else {
                            cursorClass = 'deadline-entry-cur';
                        }
                    }

                    return html`
                        <div class="deadline-entry-container">
                            <div
                                class="deadline-entry
                                    ${cursorClass}
                                    ${d.Proven ? 'deadline-proven' : ''}
                                    ${d.PartFaulty ? 'deadline-partially-faulty' : ''}
                                    ${d.Faulty ? 'deadline-faulty' : ''}"
                                @mouseenter=${() => this._handleEntryMouseEnter(actorIndex, index)}
                                @mouseleave=${() => this._handleEntryMouseLeave()}
                                @click=${(e) => this._handleEntryClick(e, actorAddress, index)}
                            ></div>
                            ${isHovered ? html`
                                <div class="deadline-info-box">
                                    <div class="deadline-header">
                                        Deadline ${index} ${d.Current ? '(Current)' : ''}
                                    </div>
                                    <div class="info-row">
                                        <span class="info-label">Opens:</span>
                                        <span class="info-value">${d.Current ? 'Now' : d.OpenAt}</span>
                                    </div>
                                    ${d.Count ? html`
                                        <div class="info-row">
                                            <span class="info-label">Partitions:</span>
                                            <span class="info-value">${d.PartitionCount}</span>
                                        </div>
                                        <div class="info-row">
                                            <span class="info-label">Total:</span>
                                            <span class="info-value">${d.Count.Total}</span>
                                        </div>
                                        <div class="info-row">
                                            <span class="info-label">Live:</span>
                                            <span class="info-value">${d.Count.Live}</span>
                                        </div>
                                        <div class="info-row">
                                            <span class="info-label">Active:</span>
                                            <span class="info-value">${d.Count.Active}</span>
                                        </div>
                                        ${d.Count.Fault > 0 ? html`
                                            <div class="info-row">
                                                <span class="info-label">Faulty:</span>
                                                <span class="info-value" style="color: #B63333;">${d.Count.Fault}</span>
                                            </div>
                                        ` : ''}
                                        ${d.Count.Recovering > 0 ? html`
                                            <div class="info-row">
                                                <span class="info-label">Recovering:</span>
                                                <span class="info-value" style="color: #FFD600;">${d.Count.Recovering}</span>
                                            </div>
                                        ` : ''}
                                        ${d.Current && d.PartitionCount > 0 ? html`
                                            <div class="post-status">
                                                <span class="info-label">PoSt:</span>
                                                <span class="${d.PartitionsProven ? 'post-submitted' : 'post-pending'}">
                                                    ${d.PartitionsPosted}/${d.PartitionCount} submitted
                                                </span>
                                            </div>
                                        ` : ''}
                                    ` : html`
                                        <div class="info-row">
                                            <span class="info-label">Empty deadline</span>
                                        </div>
                                    `}
                                </div>
                            ` : ''}
                        </div>
                    `;
                })}
            </div>
        `;
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
                    ${this.data.map((entry, actorIndex) => html`
                        <tr>
                            <td>
                                <div class="address-container">
                                    <a href="/pages/actor/?id=${entry.Address}">${entry.Address}</a>
                                    <clipboard-copy .text=${entry.Address}></clipboard-copy>
                                </div>
                            </td>
                            <td>
                                ${entry.CLayers.map(layer => html`<span>${layer} </span>`)}
                            </td>
                            <td>${entry.QualityAdjustedPower}</td>
                            <td>${this.renderDeadlines(actorIndex, entry.Address, entry.Deadlines)}</td>
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
