<template id="sector-info-template">
    <style>
        .btn {
            /* Add your button styles here */
        }
        .table {
            /* Add your table styles here */
        }
    </style>
    <h2>Sector <span id="sector-number"></span></h2>
    <div>
        <details>
            <summary class="btn btn-warning">Remove <span id="remove-status"></span></summary>
            <button class="btn btn-danger" onclick="confirmRemove()">Confirm Remove</button>
        </details>
        <button class="btn btn-primary" id="resume-button">Resume</button>
    </div>
    <div>
        <h3>PoRep Pipeline</h3>
        <sector-porep-state></sector-porep-state>
    </div>
    <div>
        <h3>Pieces</h3>
        <table class="table table-dark">
            <thead>
                <tr>
                    <th>Piece Index</th>
                    <th>Piece CID</th>
                    <th>Piece Size</th>
                    <th>Data URL</th>
                    <th>Data Raw Size</th>
                    <th>Delete On Finalize</th>
                    <th>F05 Publish CID</th>
                    <th>F05 Deal ID</th>
                    <th>Direct Piece Activation Manifest</th>
                    <th>PiecePark ID</th>
                    <th>PP URL</th>
                    <th>PP Created At</th>
                    <th>PP Complete</th>
                    <th>PP Cleanup Task</th>
                </tr>
            </thead>
            <tbody id="pieces-table-body"></tbody>
        </table>
    </div>
    <div>
        <h3>Storage</h3>
        <table class="table table-dark">
            <thead>
                <tr>
                    <th>Path Type</th>
                    <th>File Type</th>
                    <th>Path ID</th>
                    <th>Host</th>
                </tr>
            </thead>
            <tbody id="storage-table-body"></tbody>
        </table>
    </div>
    <div>
        <h3>Tasks</h3>
        <table class="porep-state">
            <thead>
                <tr>
                    <th>Task Type</th>
                    <th>Task ID</th>
                    <th>Posted</th>
                    <th>Worker</th>
                </tr>
            </thead>
            <tbody id="tasks-table-body"></tbody>
        </table>
    </div>
    <div>
        <h3>Current task history</h3>
        <table class="table table-dark">
            <thead>
                <tr>
                    <th>Task ID</th>
                    <th>Task Type</th>
                    <th>Completed By</th>
                    <th>Result</th>
                    <th>Started</th>
                    <th>Took</th>
                    <th>Error</th>
                </tr>
            </thead>
            <tbody id="task-history-table-body"></tbody>
        </table>
    </div>
</template>

<script>
    class SectorInfo extends HTMLElement {
        constructor() {
            super();
            this.attachShadow({ mode: 'open' });
            this.shadowRoot.appendChild(sectorInfoTemplate.content.cloneNode(true));
        }

        connectedCallback() {
            this.render();
        }

        render() {
            const sectorNumberElement = this.shadowRoot.getElementById('sector-number');
            const removeStatusElement = this.shadowRoot.getElementById('remove-status');
            const resumeButton = this.shadowRoot.getElementById('resume-button');
            const piecesTableBody = this.shadowRoot.getElementById('pieces-table-body');
            const storageTableBody = this.shadowRoot.getElementById('storage-table-body');
            const tasksTableBody = this.shadowRoot.getElementById('tasks-table-body');
            const taskHistoryTableBody = this.shadowRoot.getElementById('task-history-table-body');

            // Replace the following lines with your data binding logic
            sectorNumberElement.textContent = '123';
            removeStatusElement.textContent = '(THIS SECTOR IS NOT FAILED!)';
            resumeButton.style.display = 'none';

            // Render pieces
            const pieces = [
                { PieceIndex: 1, PieceCid: 'abc', PieceSize: 100, DataUrl: 'http://example.com', DataRawSize: 200 },
                { PieceIndex: 2, PieceCid: 'def', PieceSize: 150, DataUrl: 'http://example.com', DataRawSize: 250 }
            ];
            pieces.forEach(piece => {
                const row = document.createElement('tr');
                row.innerHTML = `
                    <td>${piece.PieceIndex}</td>
                    <td>${piece.PieceCid}</td>
                    <td>${piece.PieceSize}</td>
                    <td>${piece.DataUrl}</td>
                    <td>${piece.DataRawSize}</td>
                    <td></td>
                    <td></td>
                    <td></td>
                    <td></td>
                    <td></td>
                    <td></td>
                    <td></td>
                    <td></td>
                    <td></td>
                `;
                piecesTableBody.appendChild(row);
            });

            // Render storage
            const storage = [
                { PathType: 'Type 1', FileType: 'File 1', StorageID: '123', Urls: ['http://example.com'] },
                { PathType: 'Type 2', FileType: 'File 2', StorageID: '456', Urls: ['http://example.com', 'http://example.org'] }
            ];
            storage.forEach(item => {
                const row = document.createElement('tr');
                row.innerHTML = `
                    <td>${item.PathType}</td>
                    <td>${item.FileType}</td>
                    <td>${item.StorageID}</td>
                    <td>${item.Urls.map(url => `<p>${url}</p>`).join('')}</td>
                `;
                storageTableBody.appendChild(row);
            });

            // Render tasks
            const tasks = [
                { Name: 'Task 1', ID: '123', SincePosted: '2 hours ago', Owner: 'John Doe', OwnerID: '456' },
                { Name: 'Task 2', ID: '789', SincePosted: '1 hour ago', Owner: 'Jane Smith', OwnerID: '012' }
            ];
            tasks.forEach(task => {
                const row = document.createElement('tr');
                row.innerHTML = `
                    <td>${task.Name}</td>
                    <td>${task.ID}</td>
                    <td>${task.SincePosted}</td>
                    <td>${task.Owner ? `<a href="/hapi/node/${task.OwnerID}">${task.Owner}</a>` : ''}</td>
                `;
                tasksTableBody.appendChild(row);
            });

            // Render task history
            const taskHistory = [
                { PipelineTaskID: '123', Name: 'Task 1', CompletedBy: 'John Doe', Result: true, WorkStart: '2022-01-01', Took: '2 hours', Err: '' },
                { PipelineTaskID: '456', Name: 'Task 2', CompletedBy: 'Jane Smith', Result: false, WorkStart: '2022-01-02', Took: '1 hour', Err: 'Error message' }
            ];
            taskHistory.forEach(history => {
                const row = document.createElement('tr');
                row.innerHTML = `
                    <td>${history.PipelineTaskID}</td>
                    <td>${history.Name}</td>
                    <td>${history.CompletedBy}</td>
                    <td>${history.Result ? 'Success' : 'Failed'}</td>
                    <td>${history.WorkStart}</td>
                    <td>${history.Took}</td>
                    <td>${history.Err}</td>
                `;
                taskHistoryTableBody.appendChild(row);
            });
        }

        confirmRemove() {
            // Add your confirm remove logic here
        }
    }

    customElements.define('sector-info', SectorInfo);
</script>
