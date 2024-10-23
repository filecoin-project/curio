import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
customElements.define('node-info',class NodeInfoElement extends LitElement {
    constructor() {
        super();
        this.loadData();
    }
    async loadData() {
        const id = new URLSearchParams(window.location.search).get('id');
        this.data = await RPCCall('ClusterNodeInfo', [ id|0 ]);
        this.requestUpdate();

        setTimeout(() => this.loadData(), 2500);
    }
    render() {
        if (!this.data) {
            return html`<div>Loading...</div>`;
        }
        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-1BmE4kWBq78iYhFldvKuhfTAU6auU8tT94WrHftjDbrCEXSU1oBoqyl2QvZ6jIW3" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">

            <h2>Info</h2>
            <table class="table table-dark">
                <tr>
                    <td>Name</td>
                    <td>Host</td>
                    <td>ID</td>
                    <td>Last Contact</td>
                    <td>CPU</td>
                    <td>Memory</td>
                    <td>GPU</td>
                    <td>Debug</td>
                </tr>
                <tr>
                    <td>${this.data.Info.Name}</td>
                    <td>${this.data.Info.Host}</td>
                    <td>${this.data.Info.ID}</td>
                    <td>${this.data.Info.LastContact}</td>
                    <td>${this.data.Info.CPU}</td>
                    <td>${this.toHumanBytes(this.data.Info.Memory)}</td>
                    <td>${this.data.Info.GPU}</td>
                    <td>
                        <a href="http://${this.data.Info.Host}/debug/pprof">[pprof]</a>
                        <a href="http://${this.data.Info.Host}/debug/metrics">[metrics]</a>
                    </td>
                </tr>
            </table>
            <hr>
            <h2>Configuration</h2>
            <table class="table table-dark">
                <thead>
                <tr>
                    <td>Order</td><td>Layer</td><td>View</td>
                </tr>
                </thead>
                <tbody>
                ${this.data.Info.Layers.split(',').map((item, i) => html`
                    <tr>
                        <td>${i}</td>
                        <td>${item}</td>
                        <td><a href="/config/edit.html?layer=${item}">[view]</a></td>
                    </tr>
                `)}
                </tbody>
            </table>
            <hr>
            <h2>Storage</h2>
            <table class="table table-dark">
                <tr>
                    <td>ID</td>
                    <td>Type</td>
                    <td>Capacity</td>
                    <td>Available</td>
                    <td>Reserved</td>
                    <td></td>
                </tr>
                ${(this.data.Storage||[]).map((item) => html`
                    <tr>
                        <td>${item.ID}</td>
                        <td>
                            ${!item.CanSeal && !item.CanStore ? 'ReadOnly' : ''}
                            ${item.CanSeal && !item.CanStore ? 'Seal' : ''}
                            ${!item.CanSeal && item.CanStore ? 'Store' : ''}
                            ${item.CanSeal && item.CanStore ? 'Seal+Store' : ''}
                        </td>
                        <td>${this.toHumanBytes(item.Capacity)}</td>
                        <td>${this.toHumanBytes(item.Available)}</td>
                        <td>${this.toHumanBytes(item.Reserved)}</td>
                        <td>
                            <div style="width: 200px; height: 16px; border: #3f3f3f 3px solid;">
                                <div style="float: left; width: ${item.UsedPercent}%; height: 10px; background-color: green"></div>
                                <div style="float: left; width: ${item.ReservedPercent}%; height: 10px; background-color: darkred"></div>
                            </div>
                        </td>
                    </tr>
                `)}
            </table>
            <hr>
            <h2>Tasks</h2>
            <h3>Running</h3>
            <table class="table table-dark">
                <tr>
                    <td>ID</td>
                    <td>Task</td>
                    <td>Posted</td>
                    <td>Sector</td>
                </tr>
                ${(this.data.RunningTasks||[]).map((task) => html`
                    <tr>
                        <td>${task.ID}</td>
                        <td>${task.Task}</td>
                        <td>${task.Posted}</td>
                        <td>${task.PoRepSector ? html`<a href="/pages/sector/?sp=${task.PoRepSectorMiner}&id=${task.PoRepSector}">${task.PoRepSectorMiner}:${task.PoRepSector}</a>` : ''}</td>
                    </tr>
                `)}
            </table>
            <h3>Recently Finished</h3>
            <table class="table table-dark">
                <tr>
                    <td>ID</td>
                    <td>Task</td>
                    <td>Posted</td>
                    <td>Start</td>
                    <td>Queued</td>
                    <td>Took</td>
                    <td>Outcome</td>
                    <td>Message</td>
                </tr>
                ${this.data.FinishedTasks.map((task) => html`
                    <tr>
                        <td>${task.ID}</td>
                        <td>${task.Task}</td>
                        <td>${task.Posted}</td>
                        <td>${task.Start}</td>
                        <td>${task.Queued}</td>
                        <td>${task.Took}</td>
                        <td>${task.Outcome}</td>
                        <td>${task.Message}</td>
                    </tr>
                `)}
            </table>
        `;
    }

    toHumanBytes(bytes) {
        const sizes = ['Bytes', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB', 'ZiB'];
        let sizeIndex = 0;
        for (; bytes >= 1024 && sizeIndex < sizes.length - 1; sizeIndex++) {
            bytes /= 1024;
        }
        return bytes.toFixed(2) + ' ' + sizes[sizeIndex];
    }

    // Define setters for the data properties
    set info(value) {
        this._info = value;
        this.render();
    }

    set storage(value) {
        this._storage = value;
        this.render();
    }

    set runningTasks(value) {
        this._runningTasks = value;
        this.render();
    }

    set finishedTasks(value) {
        this._finishedTasks = value;
        this.render();
    }

    // Define getters for the data properties
    get info() {
        return this._info;
    }

    get storage() {
        return this._storage;
    }

    get runningTasks() {
        return this._runningTasks;
    }

    get finishedTasks() {
        return this._finishedTasks;
    }
} );
