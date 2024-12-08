import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/epoch.mjs';

class WinStats extends LitElement {
    constructor() {
        super();
        this.data = [];
        this.loadData();
    }

    async loadData() {
        this.data = await RPCCall('WinStats') || [];
        setTimeout(() => this.loadData(), 2 * 60 * 1000); // 2 minutes
        this.requestUpdate();
    }

    async copyToClipboard(block) {
        if (navigator.clipboard && navigator.clipboard.writeText) {
            try {
                await navigator.clipboard.writeText(block);
                // Show notification
                this.showNotification('Block copied to clipboard');
            } catch (err) {
                console.error('Failed to copy using Clipboard API', err);
                this.fallbackCopyTextToClipboard(block);
            }
        } else {
            // Fallback method
            this.fallbackCopyTextToClipboard(block);
        }
    }

    fallbackCopyTextToClipboard(text) {
        const textArea = document.createElement('textarea');
        textArea.value = text;

        // Avoid scrolling to bottom
        textArea.style.position = 'fixed';
        textArea.style.top = '0';
        textArea.style.left = '0';
        textArea.style.width = '1px';
        textArea.style.height = '1px';
        textArea.style.opacity = '0';

        document.body.appendChild(textArea);
        textArea.focus();
        textArea.select();

        try {
            const successful = document.execCommand('copy');
            if (successful) {
                this.showNotification('Block copied to clipboard');
            } else {
                this.showNotification('Failed to copy block');
            }
        } catch (err) {
            console.error('Fallback: Oops, unable to copy', err);
            this.showNotification('Failed to copy block');
        }

        document.body.removeChild(textArea);
    }

    showNotification(message) {
        // Simple notification logic; you might want to customize this
        const notification = document.createElement('div');
        notification.textContent = message;
        notification.style.position = 'fixed';
        notification.style.bottom = '20px';
        notification.style.right = '20px';
        notification.style.background = 'rgba(0,0,0,0.7)';
        notification.style.color = 'white';
        notification.style.padding = '10px';
        notification.style.borderRadius = '5px';
        notification.style.zIndex = '1000';
        document.body.appendChild(notification);
        setTimeout(() => {
            document.body.removeChild(notification);
        }, 2000);
    }

    render() {
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            <table class="table table-dark">
                <thead>
                <tr>
                    <th>Address</th>
                    <th>Epoch</th>
                    <th>Block</th>
                    <th>Task Success</th>
                    <th>Submitted At</th>
                    <th>Compute Time</th>
                    <th>Included</th>
                </tr>
                </thead>
                <tbody>
                    ${this.data.map(entry => html`
                    <tr>
                        <td>${entry.Miner}</td>
                        <td><pretty-epoch .epoch=${entry.Epoch}></pretty-epoch></td>
                        <td>
                            <span 
                                @click=${() => this.copyToClipboard(entry.Block)} 
                                style="cursor:pointer; text-decoration:underline;" 
                                title="${entry.Block}">
                                ...${entry.Block.slice(-10)}
                            </span>
                        </td>
                        <td>${entry.TaskSuccess}</td>
                        <td>${entry.SubmittedAtStr}</td>
                        <td>${entry.ComputeTime}</td>
                        <td>${entry.IncludedStr}</td>
                    </tr>
                    `)}
                </tbody>
            </table>
        `;
    }
}

customElements.define('win-stats', WinStats);