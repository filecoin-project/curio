import { LitElement, html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import RPCCall from '/lib/jsonrpc.mjs';
import '/ux/task.mjs';
import '/ux/message.mjs';

customElements.define('sector-exp-manager', class SectorExpManager extends LitElement {
    static styles = css`
        .manage-link {
            color: var(--color-primary-light, #8BEFE0);
            cursor: pointer;
            text-decoration: underline;
            font-size: 0.9rem;
            margin-left: 1rem;
            display: inline-block;
        }
        .manage-link:hover {
            color: var(--color-primary-main, #4CAF50);
        }
        .modal {
            position: fixed;
            top: 0;
            left: 0;
            z-index: 1050;
            width: 100%;
            height: 100%;
            overflow: hidden;
            outline: 0;
            display: flex;
            align-items: center;
            justify-content: center;
            backdrop-filter: blur(5px);
        }
        .modal-backdrop {
            position: fixed;
            top: 0;
            left: 0;
            z-index: 1040;
            width: 100vw;
            height: 100vh;
            background-color: rgba(0, 0, 0, 0.5);
        }
        .modal-dialog {
            max-width: 700px;
            margin: 1.75rem auto;
            z-index: 1050;
            max-height: 90vh;
            overflow-y: auto;
        }
        .modal-content {
            background-color: var(--color-form-field, #1d1d21);
            border: 1px solid var(--color-form-default, #808080);
            border-radius: 0.3rem;
            box-shadow: 0 0.5rem 1rem rgba(0, 0, 0, 0.5);
            color: var(--color-text-primary, #FFF);
        }
        .modal-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 1rem;
            border-bottom: 1px solid var(--color-form-default, #808080);
        }
        .modal-body {
            padding: 1rem;
        }
        .modal-footer {
            display: flex;
            align-items: center;
            justify-content: flex-end;
            padding: 1rem;
            border-top: 1px solid var(--color-form-default, #808080);
            gap: 0.5rem;
        }
        .form-group {
            margin-bottom: 1rem;
        }
        .form-label {
            display: block;
            margin-bottom: 0.25rem;
            font-weight: 500;
        }
        .form-control {
            width: 100%;
            padding: 0.375rem 0.75rem;
            border: 1px solid #555;
            background-color: #2a2a2a;
            color: #e0e0e0;
            border-radius: 4px;
        }
        .form-select {
            width: 100%;
            padding: 0.375rem 0.75rem;
            border: 1px solid #555;
            background-color: #2a2a2a;
            color: #e0e0e0;
            border-radius: 4px;
        }
        .btn {
            padding: 0.375rem 0.75rem;
            border: none;
            border-radius: 4px;
            cursor: pointer;
        }
        .btn-primary {
            background-color: var(--color-primary-main, #4CAF50);
            color: white;
        }
        .btn-primary:hover {
            background-color: var(--color-primary-dark, #45a049);
        }
        .btn-secondary {
            background-color: #6c757d;
            color: white;
        }
        .btn-secondary:hover {
            background-color: #5a6268;
        }
        .btn-danger {
            background-color: var(--color-danger-main, #B63333);
            color: white;
        }
        .btn-danger:hover {
            background-color: #a02828;
        }
        .btn-sm {
            padding: 0.25rem 0.5rem;
            font-size: 0.85rem;
        }
        .preset-type-badge {
            padding: 0.25rem 0.5rem;
            border-radius: 3px;
            font-size: 0.8rem;
            display: inline-block;
            margin-right: 0.5rem;
        }
        .badge-extend {
            background-color: #303060;
            color: white;
        }
        .badge-top-up {
            background-color: #a06010;
            color: white;
        }
        .sp-assignments {
            margin-top: 0.5rem;
            padding: 0.5rem;
            background-color: rgba(0, 0, 0, 0.2);
            border-radius: 4px;
        }
        .sp-checkbox-group {
            display: flex;
            flex-wrap: wrap;
            gap: 0.5rem;
            margin-top: 0.5rem;
        }
        .sp-checkbox-label {
            display: flex;
            align-items: center;
            gap: 0.25rem;
            padding: 0.25rem 0.5rem;
            background-color: #2a2a2a;
            border-radius: 4px;
            cursor: pointer;
        }
        .loading {
            text-align: center;
            padding: 2rem;
            color: var(--color-text-dense, #e0e0e0);
        }
        .error {
            color: var(--color-danger-main, #B63333);
            padding: 1rem;
        }
        .info-text {
            font-size: 0.85rem;
            color: var(--color-text-dense, #999);
            font-style: italic;
        }
        .form-check {
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }
        .status-badge {
            padding: 0.25rem 0.5rem;
            border-radius: 3px;
            font-size: 0.8rem;
            display: inline-block;
            white-space: nowrap;
        }
        .status-ok {
            background-color: #28a745;
            color: white;
        }
        .status-needs-action {
            background-color: #ffc107;
            color: black;
        }
        .status-loading {
            background-color: #6c757d;
            color: white;
        }
        .status-error {
            background-color: #dc3545;
            color: white;
        }
    `;

    constructor() {
        super();
        this.presets = null;
        this.spAssignments = null;
        this.availableSPs = [];
        this.loading = true;
        this.error = null;
        this.showPresetModal = false;
        this.editingPreset = null;
        this.formData = {};
        this.expandedPresets = new Set();
        this.conditionStatus = new Map(); // key: "sp_address:preset_name", value: {loading, needsAction}
        this.loadData();
        // Refresh every 30 seconds
        setInterval(() => this.loadData(), 30000);
    }

    async loadData() {
        try {
            this.error = null;
            const [presets, spAssignments, spStats] = await Promise.all([
                RPCCall('SectorExpManagerPresets', []),
                RPCCall('SectorExpManagerSPs', []),
                RPCCall('SectorSPStats', [])
            ]);
            this.presets = presets;
            this.spAssignments = spAssignments;
            this.availableSPs = spStats.map(sp => ({ id: sp.sp_id, address: sp.sp_address }));
            this.loading = false;
            this.requestUpdate();
            
            // Load condition status for each SP assignment
            this.loadConditionStatuses();
        } catch (err) {
            console.error('Failed to load sector expiration manager data:', err);
            this.error = err.message || 'Failed to load data';
            this.loading = false;
            this.requestUpdate();
        }
    }

    async loadConditionStatuses() {
        if (!this.spAssignments) return;
        
        // Load each condition status separately
        for (const assignment of this.spAssignments) {
            const key = `${assignment.sp_address}:${assignment.preset_name}`;
            this.conditionStatus.set(key, { loading: true, needsAction: null });
            this.requestUpdate();
            
            try {
                const needsAction = await RPCCall('SectorExpManagerSPEvalCondition', [
                    assignment.sp_address,
                    assignment.preset_name
                ]);
                this.conditionStatus.set(key, { loading: false, needsAction });
                this.requestUpdate();
            } catch (err) {
                console.error(`Failed to eval condition for ${key}:`, err);
                this.conditionStatus.set(key, { loading: false, needsAction: null, error: err.message });
                this.requestUpdate();
            }
        }
    }

    getConditionStatus(spAddress, presetName) {
        const key = `${spAddress}:${presetName}`;
        return this.conditionStatus.get(key) || { loading: true, needsAction: null };
    }

    openPresetModal(preset = null) {
        this.editingPreset = preset;
        if (preset) {
            this.formData = { ...preset };
        } else {
            this.formData = {
                name: '',
                action_type: 'extend',
                info_bucket_above_days: 0,
                info_bucket_below_days: 14,
                target_expiration_days: 28,
                max_candidate_days: 21,
                top_up_count_low_water_mark: null,
                top_up_count_high_water_mark: null,
                cc: null,
                drop_claims: false
            };
        }
        this.showPresetModal = true;
        this.requestUpdate();
    }

    closePresetModal() {
        this.showPresetModal = false;
        this.editingPreset = null;
        this.formData = {};
        this.requestUpdate();
    }

    handleFormChange(field, value) {
        // Handle number conversions
        if (['info_bucket_above_days', 'info_bucket_below_days', 'target_expiration_days', 
             'max_candidate_days', 'top_up_count_low_water_mark', 'top_up_count_high_water_mark'].includes(field)) {
            const numValue = value === '' ? null : parseInt(value, 10);
            this.formData[field] = numValue;
        } else if (field === 'cc') {
            // Convert to boolean or null
            this.formData[field] = value === '' ? null : (value === 'true');
        } else if (field === 'drop_claims') {
            this.formData[field] = value;
        } else {
            this.formData[field] = value;
        }
        this.requestUpdate();
    }

    async savePreset() {
        try {
            if (this.editingPreset) {
                await RPCCall('SectorExpManagerPresetUpdate', [this.formData]);
            } else {
                await RPCCall('SectorExpManagerPresetAdd', [this.formData]);
            }
            this.closePresetModal();
            await this.loadData();
        } catch (err) {
            console.error('Failed to save preset:', err);
            alert(`Failed to save preset: ${err.message}`);
        }
    }

    async deletePreset(name) {
        if (!confirm(`Delete preset "${name}"? This will also remove all SP assignments using this preset.`)) {
            return;
        }
        try {
            await RPCCall('SectorExpManagerPresetDelete', [name]);
            await this.loadData();
        } catch (err) {
            console.error('Failed to delete preset:', err);
            alert(`Failed to delete preset: ${err.message}`);
        }
    }

    togglePresetExpansion(presetName) {
        if (this.expandedPresets.has(presetName)) {
            this.expandedPresets.delete(presetName);
        } else {
            this.expandedPresets.add(presetName);
        }
        this.requestUpdate();
    }

    async toggleSPAssignment(spAddress, presetName, isAssigned) {
        try {
            if (isAssigned) {
                await RPCCall('SectorExpManagerSPDelete', [spAddress, presetName]);
            } else {
                await RPCCall('SectorExpManagerSPAdd', [spAddress, presetName]);
            }
            await this.loadData();
        } catch (err) {
            console.error('Failed to toggle SP assignment:', err);
            alert(`Failed to toggle SP assignment: ${err.message}`);
        }
    }

    async toggleSPEnabled(spAddress, presetName, enabled) {
        try {
            await RPCCall('SectorExpManagerSPToggle', [spAddress, presetName, enabled]);
            await this.loadData();
        } catch (err) {
            console.error('Failed to toggle SP enabled:', err);
            alert(`Failed to toggle SP enabled: ${err.message}`);
        }
    }

    getSPAssignmentsForPreset(presetName) {
        return this.spAssignments.filter(a => a.preset_name === presetName);
    }

    isSPAssignedToPreset(spAddress, presetName) {
        return this.spAssignments.some(a => a.sp_address === spAddress && a.preset_name === presetName);
    }

    renderPresetModal() {
        if (!this.showPresetModal) return html``;

        const isExtend = this.formData.action_type === 'extend';

        return html`
            <div class="modal-backdrop" @click="${this.closePresetModal}"></div>
            <div class="modal">
                <div class="modal-dialog">
                    <div class="modal-content">
                        <div class="modal-header">
                            <h5>${this.editingPreset ? 'Edit' : 'Add'} Preset</h5>
                            <button type="button" class="btn-secondary" @click="${this.closePresetModal}">×</button>
                        </div>
                        <div class="modal-body">
                            <div class="form-group">
                                <label class="form-label">Name</label>
                                <input 
                                    type="text" 
                                    class="form-control"
                                    .value="${this.formData.name || ''}"
                                    ?disabled="${this.editingPreset !== null}"
                                    @input="${(e) => this.handleFormChange('name', e.target.value)}"
                                />
                            </div>

                            <div class="form-group">
                                <label class="form-label">Action Type</label>
                                <select 
                                    class="form-select"
                                    .value="${this.formData.action_type || 'extend'}"
                                    @change="${(e) => this.handleFormChange('action_type', e.target.value)}"
                                >
                                    <option value="extend">Extend - Roll sectors to target expiration</option>
                                    <option value="top_up">Top Up - Maintain sector count in bucket</option>
                                </select>
                            </div>

                            <div class="form-group">
                                <label class="form-label">Info Bucket Range (Days)</label>
                                <div style="display: flex; gap: 0.5rem; align-items: center;">
                                    <input 
                                        type="number" 
                                        class="form-control"
                                        placeholder="Above"
                                        .value="${this.formData.info_bucket_above_days || 0}"
                                        @input="${(e) => this.handleFormChange('info_bucket_above_days', e.target.value)}"
                                    />
                                    <span>to</span>
                                    <input 
                                        type="number" 
                                        class="form-control"
                                        placeholder="Below"
                                        .value="${this.formData.info_bucket_below_days || ''}"
                                        @input="${(e) => this.handleFormChange('info_bucket_below_days', e.target.value)}"
                                    />
                                </div>
                                <small class="info-text">
                                    ${isExtend 
                                        ? 'If ANY sector expires in this range, trigger extension'
                                        : 'Maintain sector count in this range'}
                                </small>
                            </div>

                            ${isExtend ? html`
                                <div class="form-group">
                                    <label class="form-label">Target Expiration (Days)</label>
                                    <input 
                                        type="number" 
                                        class="form-control"
                                        .value="${this.formData.target_expiration_days || ''}"
                                        @input="${(e) => this.handleFormChange('target_expiration_days', e.target.value)}"
                                    />
                                    <small class="info-text">Extend sectors to this many days from now</small>
                                </div>

                                <div class="form-group">
                                    <label class="form-label">Max Candidate Days</label>
                                    <input 
                                        type="number" 
                                        class="form-control"
                                        .value="${this.formData.max_candidate_days || ''}"
                                        @input="${(e) => this.handleFormChange('max_candidate_days', e.target.value)}"
                                    />
                                    <small class="info-text">Extend all sectors expiring within this many days</small>
                                </div>
                            ` : html`
                                <div class="form-group">
                                    <label class="form-label">Low Water Mark</label>
                                    <input 
                                        type="number" 
                                        class="form-control"
                                        .value="${this.formData.top_up_count_low_water_mark || ''}"
                                        @input="${(e) => this.handleFormChange('top_up_count_low_water_mark', e.target.value)}"
                                    />
                                    <small class="info-text">Trigger top-up when count falls below this</small>
                                </div>

                                <div class="form-group">
                                    <label class="form-label">High Water Mark</label>
                                    <input 
                                        type="number" 
                                        class="form-control"
                                        .value="${this.formData.top_up_count_high_water_mark || ''}"
                                        @input="${(e) => this.handleFormChange('top_up_count_high_water_mark', e.target.value)}"
                                    />
                                    <small class="info-text">Top up to this many sectors</small>
                                </div>
                            `}

                            <div class="form-group">
                                <label class="form-label">Sector Filter</label>
                                <select 
                                    class="form-select"
                                    .value="${this.formData.cc === null ? '' : this.formData.cc.toString()}"
                                    @change="${(e) => this.handleFormChange('cc', e.target.value)}"
                                >
                                    <option value="">Both CC and Deal sectors</option>
                                    <option value="true">CC sectors only</option>
                                    <option value="false">Deal sectors only</option>
                                </select>
                            </div>

                            <div class="form-group">
                                <div class="form-check">
                                    <input 
                                        type="checkbox" 
                                        id="drop_claims"
                                        ?checked="${this.formData.drop_claims}"
                                        @change="${(e) => this.handleFormChange('drop_claims', e.target.checked)}"
                                    />
                                    <label for="drop_claims">Drop Claims</label>
                                </div>
                            </div>
                        </div>
                        <div class="modal-footer">
                            <button type="button" class="btn btn-secondary" @click="${this.closePresetModal}">Cancel</button>
                            <button type="button" class="btn btn-primary" @click="${this.savePreset}">
                                ${this.editingPreset ? 'Update' : 'Add'}
                            </button>
                        </div>
                    </div>
                </div>
            </div>
        `;
    }

    renderPresetDescription(preset) {
        const sectorFilter = preset.cc === true ? '(CC only)' : preset.cc === false ? '(Deals only)' : '(All)';
        const claimsInfo = preset.drop_claims ? ' Drop claims.' : '';
        
        if (preset.action_type === 'extend') {
            return html`
                <div class="info-text" style="margin-top: 0.5rem;">
                    If any sector expires in ${preset.info_bucket_above_days}–${preset.info_bucket_below_days} days, 
                    extend all sectors expiring within ${preset.info_bucket_above_days}–${preset.max_candidate_days || 0} days 
                    to ${preset.target_expiration_days || 0} days from now.
                    ${sectorFilter}${claimsInfo}
                </div>
            `;
        } else {
            return html`
                <div class="info-text" style="margin-top: 0.5rem;">
                    If sector count in ${preset.info_bucket_above_days}–${preset.info_bucket_below_days} days 
                    falls below ${preset.top_up_count_low_water_mark || 0}, 
                    top up to ${preset.top_up_count_high_water_mark || 0} sectors.
                    ${sectorFilter}${claimsInfo}
                </div>
            `;
        }
    }

    renderPresetsTable() {
        if (!this.presets || this.presets.length === 0) {
            return html`<p>No presets configured</p>`;
        }

        return html`
            <table class="table table-dark">
                <thead>
                    <tr>
                        <th>Name</th>
                        <th>Type</th>
                        <th>Description</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.presets.map(preset => {
                        const assignments = this.getSPAssignmentsForPreset(preset.name);
                        const isExpanded = this.expandedPresets.has(preset.name);
                        return html`
                            <tr>
                                <td style="white-space: nowrap;">
                                    <strong>${preset.name}</strong>
                                    ${assignments.length > 0 ? html`
                                        <br>
                                        <small style="color: #999;">${assignments.length} SP(s)</small>
                                    ` : ''}
                                </td>
                                <td>
                                    <span class="preset-type-badge ${preset.action_type === 'extend' ? 'badge-extend' : 'badge-top-up'}">
                                        ${preset.action_type}
                                    </span>
                                </td>
                                <td>
                                    ${this.renderPresetDescription(preset)}
                                </td>
                                <td style="white-space: nowrap;">
                                    <button class="btn btn-sm btn-secondary" @click="${() => this.togglePresetExpansion(preset.name)}">
                                        ${isExpanded ? 'Hide' : 'Manage'} SPs
                                    </button>
                                    <button class="btn btn-sm btn-secondary" @click="${() => this.openPresetModal(preset)}">
                                        Edit
                                    </button>
                                    <button class="btn btn-sm btn-danger" @click="${() => this.deletePreset(preset.name)}">
                                        Delete
                                    </button>
                                </td>
                            </tr>
                            ${isExpanded ? html`
                                <tr>
                                    <td colspan="4">
                                        <div class="sp-assignments">
                                            <strong>Select SPs for this preset:</strong>
                                            <div class="sp-checkbox-group">
                                                ${this.availableSPs.map(sp => {
                                                    const isAssigned = this.isSPAssignedToPreset(sp.address, preset.name);
                                                    return html`
                                                        <label class="sp-checkbox-label">
                                                            <input 
                                                                type="checkbox" 
                                                                ?checked="${isAssigned}"
                                                                @change="${() => this.toggleSPAssignment(sp.address, preset.name, isAssigned)}"
                                                            />
                                                            ${sp.address}
                                                        </label>
                                                    `;
                                                })}
                                            </div>
                                        </div>
                                    </td>
                                </tr>
                            ` : ''}
                        `;
                    })}
                </tbody>
            </table>
        `;
    }

    renderSPAssignmentsTable() {
        if (!this.spAssignments || this.spAssignments.length === 0) {
            return html`<p>No SP assignments configured</p>`;
        }

        return html`
            <table class="table table-dark">
                <thead>
                    <tr>
                        <th>SP</th>
                        <th>Preset</th>
                        <th>Status</th>
                        <th>Enabled</th>
                        <th>Last Run</th>
                        <th>Task</th>
                        <th>Last Message</th>
                        <th>Actions</th>
                    </tr>
                </thead>
                <tbody>
                    ${this.spAssignments.map(assignment => {
                        const status = this.getConditionStatus(assignment.sp_address, assignment.preset_name);
                        return html`
                            <tr>
                                <td style="white-space: nowrap;">${assignment.sp_address}</td>
                                <td style="white-space: nowrap;">${assignment.preset_name}</td>
                                <td style="white-space: nowrap;">
                                    ${status.loading ? html`
                                        <span class="status-badge status-loading">Loading...</span>
                                    ` : status.error ? html`
                                        <span class="status-badge status-error" title="${status.error}">Error</span>
                                    ` : status.needsAction === true ? html`
                                        <span class="status-badge status-needs-action">Needs Extension</span>
                                    ` : status.needsAction === false ? html`
                                        <span class="status-badge status-ok">OK</span>
                                    ` : html`
                                        <span class="status-badge status-loading">-</span>
                                    `}
                                </td>
                                <td>
                                    <input 
                                        type="checkbox" 
                                        ?checked="${assignment.enabled}"
                                        @change="${(e) => this.toggleSPEnabled(assignment.sp_address, assignment.preset_name, e.target.checked)}"
                                    />
                                </td>
                                <td style="white-space: nowrap;">${assignment.last_run_at || '-'}</td>
                                <td style="white-space: nowrap;">
                                    ${assignment.task_id 
                                        ? html`<cu-task task_id="${assignment.task_id}"></cu-task>`
                                        : '-'}
                                </td>
                                <td style="white-space: nowrap;">
                                    ${assignment.last_message_cid
                                        ? html`<fil-message cid="${assignment.last_message_cid}"></fil-message>`
                                        : '-'}
                                </td>
                                <td style="white-space: nowrap;">
                                    <button 
                                        class="btn btn-sm btn-danger" 
                                        @click="${() => this.toggleSPAssignment(assignment.sp_address, assignment.preset_name, true)}"
                                    >
                                        Remove
                                    </button>
                                </td>
                            </tr>
                        `;
                    })}
                </tbody>
            </table>
        `;
    }

    render() {
        if (this.loading) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
                <link rel="stylesheet" href="/ux/main.css">
                <div class="loading">Loading expiration manager data...</div>
            `;
        }

        if (this.error) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
                <link rel="stylesheet" href="/ux/main.css">
                <div class="error">${this.error}</div>
            `;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet">
            <link rel="stylesheet" href="/ux/main.css">
            
            <div class="info-block">
                <h2>
                    Expiration Manager Presets
                    <a class="manage-link" @click="${() => this.openPresetModal()}">+ add preset</a>
                </h2>
                ${this.renderPresetsTable()}
            </div>

            <div class="info-block">
                <h3>Extension Manager SP Assignments</h3>
                ${this.renderSPAssignmentsTable()}
            </div>

            ${this.renderPresetModal()}
        `;
    }
});

