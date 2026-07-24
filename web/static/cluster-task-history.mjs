import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import { pollRPC } from '/lib/poll.mjs';

customElements.define('cluster-task-history', class ClusterTaskHistory extends LitElement {
    static properties = {
        data: { type: Array },
    };

    constructor() {
        super();
        this.data = [];
        pollRPC(async () => {
            this.data = await RPCCall('ClusterTaskHistory', [20, 0]);
        }, 300);
    }
    render() {
        return html`
            <link rel="stylesheet" href="/ux/vendor/bootstrap.min.css">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
                <table class="table table-dark">
                    <thead>
                    <tr>
                        <th>Name</th>
                        <th>ID</th>
                        <th>Executor</th>
                        <th>Posted</th>
                        <th>Start</th>
                        <th>Queued</th>
                        <th>Took</th>
                        <th>Outcome</th>
                        <th>Message</th>
                    </tr>
                    </thead>
                    <tbody>
                    ${this.data.map((item) => html`
                        <tr>
                            <td>${item.Name}</td>
                            <td><a href="/pages/task/id/?id=${item.TaskID}">${item.TaskID}</a></td>
                            <td>${item.CompletedBy}</td>
                            <td>${item.Posted}</td>
                            <td>${item.Start}</td>
                            <td>${item.Queued}</td>
                            <td>${item.Took}</td>
                            <td>
                                ${item.Result ? html`<span class="success">success</span>` : html`<span class="error">error</span>`}
                            </td>
                            <td style="max-width: 25vh">
                                <div style="overflow: hidden; white-space: nowrap; text-overflow: ellipsis" title="${item.Err}">
                                    ${item.Err}
                                </div>
                            </td>
                        </tr>
                    `)}
                    </tbody>
                </table>
        `;
    }
});
