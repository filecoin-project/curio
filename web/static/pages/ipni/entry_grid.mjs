import { html, css, LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class EntryGrid extends LitElement {
    static properties = {
        entriesHead: { type: String },
        entryCount: { type: Number },
        entries: { type: Array },
        currentEntry: { type: Object },
        selectedEntry: { type: Object },
        scanningIndex: { type: Number },
        selectedIndex: { type: Number },
        blink: { type: Boolean },
    };

    constructor() {
        super();
        this.entriesHead = '';
        this.entryCount = 0;
        this.entries = [];
        this.currentEntry = null;
        this.selectedEntry = null;
        this.scanningIndex = 0;
        this.selectedIndex = null;
        this.blink = false;
        this._blinkInterval = null;
    }

    connectedCallback() {
        super.connectedCallback();
        console.log('entriesHead:', this.entriesHead);
        console.log('entryCount:', this.entryCount);
        // Start scanning if entriesHead and entryCount are provided
        if (this.entriesHead && this.entryCount > 0) {
            this.startScanning();
        } else {
            console.warn('Scanning not started due to invalid entriesHead or entryCount.');
        }
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this._blinkInterval) {
            clearInterval(this._blinkInterval);
        }
    }

    startScanning() {
        this.entryCount = Number(this.entryCount);
        if (isNaN(this.entryCount) || this.entryCount <= 0) {
            console.error('Invalid entryCount:', this.entryCount);
            return;
        }

        this.entries = Array.from({ length: this.entryCount }, () => ({
            status: 'unscanned',
            cid: null,
            details: null,
        }));

        this.scanningIndex = 0;
        this.blink = false;

        // Start blinking interval
        this._blinkInterval = setInterval(() => {
            this.blink = !this.blink;
            this.requestUpdate();
        }, 300);

        // Begin scanning entries
        this.scanEntries(this.entriesHead);
    }

    async scanEntries(cid) {
        let currentCid = cid;
        while (this.scanningIndex < this.entryCount && currentCid) {
            // Update the currentEntry
            try {
                const entryInfo = await RPCCall('IPNIEntry', [{ "/": currentCid }]);

                // Update the entries array
                this.entries[this.scanningIndex] = {
                    status: entryInfo.Err ? 'error' : 'scanned',
                    cid: currentCid,
                    details: entryInfo,
                };

                // Update currentEntry
                this.currentEntry = entryInfo;

                // Advance to the next entry
                currentCid = entryInfo.PrevCID || null;
                if (currentCid && typeof currentCid === 'object' && currentCid !== null) {
                    currentCid = currentCid['/'] || null;
                }

                this.scanningIndex++;

                // Wait for a short period to simulate scanning delay
                await new Promise((resolve) => setTimeout(resolve, 100)); // 100ms delay
            } catch (error) {
                console.error('Error scanning entry:', error);

                this.entries[this.scanningIndex] = {
                    status: 'error',
                    cid: currentCid,
                    details: { Err: error.message },
                };

                // Cannot proceed further if there's an error
                currentCid = null;
                this.scanningIndex++;
            }
        }

        // Stop blinking when done
        if (this._blinkInterval) {
            clearInterval(this._blinkInterval);
            this.blink = false;
        }

        // Clear currentEntry when scanning is done
        this.currentEntry = null;
        this.scanningIndex = null;
    }

    handleSquareClick(index) {
        const entry = this.entries[index];
        if (entry && (entry.status === 'scanned' || entry.status === 'error')) {
            this.selectedEntry = entry.details;
            this.selectedIndex = index;
        }
    }

    render() {
        return html`
            <div class="entry-details">
                <h3>Current Entry</h3>
                ${this.currentEntry
                        ? html`<pre>${JSON.stringify(this.currentEntry, null, 2)}</pre>`
                        : html`<p>No entry is being scanned currently.</p>`}
            </div>

            ${this.selectedEntry
                    ? html`
                        <div class="selected-entry-details">
                            <h3>Selected Entry</h3>
                            <pre>${JSON.stringify(this.selectedEntry, null, 2)}</pre>
                        </div>
                    `
                    : ''}

            <div class="grid-container">
                ${this.entries.map((entry, index) => {
                    let className = 'grid-item ';
                    if (index === this.scanningIndex) {
                        className += this.blink ? 'scanning' : 'scanning2';
                    } else {
                        if (entry.status === 'unscanned') {
                            className += 'unscanned';
                        } else if (entry.status === 'scanned') {
                            className += 'scanned';
                        } else if (entry.status === 'error') {
                            className += 'error';
                        }
                    }

                    if (index === this.selectedIndex) {
                        className += ' selected'; // Add 'selected' class
                    }

                    return html`
                        <div
                                class="${className}"
                                @click="${() => this.handleSquareClick(index)}"
                                title="Entry ${index + 1}"
                        ></div>
                    `;
                })}
            </div>
        `;
    }

    static styles = css`
        .entry-details,
        .selected-entry-details {
          margin-bottom: 1rem;
        }

        .grid-container {
          display: grid;
          grid-template-columns: repeat(64, 10px);
          grid-gap: 2px;
        }

        .grid-item {
          width: 10px;
          height: 10px;
          background-color: gray;
          cursor: pointer;
          box-sizing: border-box; /* Ensure padding and border are included in total size */
        }

        .grid-item.unscanned {
          background-color: gray;
        }

        .grid-item.scanned {
          background-color: green;
        }

        .grid-item.error {
          background-color: red;
        }

        .grid-item.scanning {
          background-color: blue;
        }
        
        .grid-item.scanning2 {
          background-color: cyan;
        }

        .grid-item.selected {
          outline: 2px solid cyan; /* Use outline to avoid affecting layout */
        }

        pre {
          padding: 1rem;
        }
    `;
}

customElements.define('entry-grid', EntryGrid);
