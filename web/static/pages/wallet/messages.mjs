import { css, html, LitElement } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { formatDate } from '/lib/dateutil.mjs';
import '/ux/yesno.mjs';
import {timeSince} from "../../lib/dateutil.mjs";

class PendingMessages extends LitElement {
    static properties = {
        messages: { type: Array },
        totalCount: { type: Number },
        pageSize: { type: Number },
        currentPage: { type: Number },
        totalPages: { type: Number }
    };

    constructor() {
        super();
        this.messages = [];
        this.totalCount = 0;
        this.pageSize = 10;
        this.currentPage = 1;
        this.totalPages = 0;
        this.loadData();
    }

    async loadData() {
        try {
            const data = await RPCCall('PendingMessages');
            this.messages = data.messages || [];
            this.totalCount = this.messages.length;
            this.totalPages = Math.ceil(this.totalCount / this.pageSize);
            this.currentPage = 1;
            this.requestUpdate();
        } catch (error) {
            console.error('Failed to load pending messages:', error);
            alert('Failed to load pending messages: ' + error.message);
        }
    }

    get pagedMessages() {
        const start = (this.currentPage - 1) * this.pageSize;
        const end = start + this.pageSize;
        return this.messages.slice(start, end);
    }

    nextPage() {
        if (this.currentPage < this.totalPages) {
            this.currentPage++;
        }
    }

    prevPage() {
        if (this.currentPage > 1) {
            this.currentPage--;
        }
    }

    connectedCallback() {
        super.connectedCallback();
        this.loadData();
    }

    render() {
        return html`
      <link
        href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css"
        rel="stylesheet"
        crossorigin="anonymous"
      />
      <link rel="stylesheet" href="/ux/main.css" />

      <div>
        <h2>
          Pending Messages List
          <button class="info-btn">
            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor"
              class="bi bi-info-circle" viewBox="0 0 16 16">
              <path
                d="M8 15A7 7 0 1 1 8 1a7 7 0 0 1 0 14m0 1A8 8 0 1 0 8 0a8 8 0 0 0 0 16"/>
              <path
                d="m8.93 6.588-2.29.287-.082.38.45.083c.294.07.352.176.288.469l-.738 
                3.468c-.194.897.105 1.319.808 1.319.545 0 1.178-.252 
                1.465-.598l.088-.416c-.2.176-.492.246-.686.246-.275 
                0-.375-.193-.304-.533zM9 4.5a1 1 0 1 1-2 0 1 1 0 0 1 2 0"/>
            </svg>
            <span class="tooltip-text">
              List of all pending messages. Certain messages may have been executed, but cannot be found on the connected chain node.
            </span>
          </button>
        </h2>
        <table class="table table-dark table-striped table-sm">
          <thead>
            <tr>
              <th>Created At</th>
              <th>Message / Txn ID</th>
            </tr>
          </thead>
          <tbody>
            ${this.pagedMessages.map(
            (msg) => html`
                <tr>
                    <td>${new Date(msg.added_at).toLocaleString()}</td>
                    <td>${(() => {
                        const ageMinutes = (Date.now() - new Date(msg.added_at).getTime()) / 60000;
                        const color = ageMinutes < 30 ? 'limegreen' : ageMinutes < 60 ? 'goldenrod' : 'crimson';
                        return html`<span style="color: ${color}">${msg.message}</span>`;
                    })()}</td>
                </tr>
            `)}
          </tbody>
        </table>

        <div class="pagination-controls">
          <button class="btn btn-secondary" @click="${this.prevPage}" ?disabled="${this.currentPage <= 1}">
            Previous
          </button>
          <span>Page ${this.currentPage} of ${this.totalPages}</span>
          <button class="btn btn-secondary" @click="${this.nextPage}" ?disabled="${this.currentPage >= this.totalPages}">
            Next
          </button>
        </div>
      </div>
    `;
    }

    static styles = css`
    .pagination-controls {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 1rem;
    }

    .info-btn {
      position: relative;
      border: none;
      background: transparent;
      cursor: pointer;
      color: #17a2b8;
      font-size: 1em;
      margin-left: 8px;
    }

    .tooltip-text {
      display: none;
      position: absolute;
      top: 50%;
      left: 120%;
      transform: translateY(-50%);
      min-width: 440px;
      max-width: 600px;
      background-color: #333;
      color: #fff;
      padding: 8px;
      border-radius: 4px;
      font-size: 0.8em;
      text-align: left;
      white-space: normal;
      z-index: 10;
    }

    .info-btn:hover .tooltip-text {
      display: block;
    }
  `;
}

customElements.define('pending-messages', PendingMessages);
