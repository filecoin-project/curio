import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';

class UploadStatus extends LitElement {
    constructor() {
        super();
        this.loaddata();
    }

    static styles = css`
    .chunk-box {
      display: grid;
      grid-template-columns: repeat(16, auto);
      grid-template-rows: repeat(3, auto);
      grid-gap: 1px;
    }
    .chunk-entry {
      width: 10px;
      height: 10px;
      background-color: grey;
      margin: 1px;
    }
    .chunk-complete {
      background-color: green;
    }
    .chunk-missing
      background-color: red;
    }
    `

    async loaddata() {
        try {
            const params = new URLSearchParams(window.location.search);
            this.data = await RPCCall('ChunkUploadStatus', [params.get('id')]);
            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load upload status:', error);
            alert(`Failed to load upload status: ${error.message}`);
        }
    }

    render() {
        if (!this.data) return html`<p>No data.</p>`;

        return html`
            <link
                    href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
                    rel="stylesheet"
                    crossorigin="anonymous"
            />
            <link rel="stylesheet" href="/ux/main.css" />
            
            <table class="table table-dark table-striped table-sm">
                <tr><th>Identifier</th><td>${this.data.id}</td></tr>
                <tr><th>Total Chunks</th><td>${this.data.status.total_chunks}</td></tr>
                <tr><th>Uploaded</th><td>${this.data.status.uploaded}</td></tr>
                <tr><th>Missing</th><td>${this.data.status.missing}</td></tr>
                <tr><th>Status</th><td>${this.renderChunks(this.data.status)}</td></tr>
            </table>
        `;
    }

    renderChunks(status) {
        const totalChunks = status.total_chunks;
        const missingChunks = status.missing_chunks || [];
        const uploadedChunksSet = new Set();

        if (status.uploaded_chunks) {
            status.uploaded_chunks.forEach(chunk => uploadedChunksSet.add(chunk));
        }

        // Create chunk entries
        const chunkEntries = Array.from({ length: totalChunks }, (_, i) => {
            const chunkIndex = i + 1; // Chunks start from 1
            const isMissing = missingChunks.includes(chunkIndex);
            const isUploaded = uploadedChunksSet.has(chunkIndex);

            return html`
            <div class="chunk-entry 
                ${isMissing ? 'chunk-missing' : ''} 
                ${isUploaded ? 'chunk-complete' : ''}">
            </div>
        `;
        });

        return html`
            <div class="chunk-box">
                ${chunkEntries}
            </div>
        `;
    }
}
customElements.define('upload-status', UploadStatus);

