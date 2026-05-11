import { html, css } from 'https://cdn.jsdelivr.net/gh/lit/dist@3/all/lit-all.min.js';
import { StyledLitElement } from '/ux/StyledLitElement.mjs';
import RPCCall from '/lib/jsonrpc.mjs';

class AlertsManage extends StyledLitElement {
    static properties = {
        // Mutes
        mutes: { type: Array },
        categories: { type: Array },
        showAddForm: { type: Boolean },
        newAlertName: { type: String },
        newPattern: { type: String },
        newReason: { type: String },
        newExpiresHours: { type: Number },
        
        // Alert history
        alerts: { type: Array },
        alertsTotal: { type: Number },
        alertsPage: { type: Number },
        includeAcknowledged: { type: Boolean },
        
        // UI state
        loading: { type: Boolean },
        error: { type: String },
        activeTab: { type: String },
        
        // Comment modal
        showCommentModal: { type: Boolean },
        commentAlertId: { type: Number },
        commentAlertName: { type: String },
        comments: { type: Array },
        newComment: { type: String },
    };

    constructor() {
        super();
        // Mutes
        this.mutes = [];
        this.categories = [];
        this.showAddForm = false;
        this.newAlertName = '';
        this.newPattern = '';
        this.newReason = '';
        this.newExpiresHours = 0;
        
        // Alert history
        this.alerts = [];
        this.alertsTotal = 0;
        this.alertsPage = 0;
        this.includeAcknowledged = false;
        
        // UI state
        this.loading = true;
        this.error = null;
        this.activeTab = 'history';
        
        // Comment modal
        this.showCommentModal = false;
        this.commentAlertId = null;
        this.commentAlertName = '';
        this.comments = [];
        this.newComment = '';
    }

    connectedCallback() {
        super.connectedCallback();
        this.loadData();
    }

    async loadData() {
        try {
            const [mutes, categories] = await Promise.all([
                RPCCall('AlertMuteList'),
                RPCCall('AlertCategoriesList')
            ]);
            this.mutes = mutes || [];
            this.categories = categories || [];
            await this.loadAlerts();
            this.loading = false;
        } catch (e) {
            this.error = e.message || 'Failed to load alert data';
            this.loading = false;
        }
    }

    async loadAlerts() {
        try {
            const pageSize = 25;
            const result = await RPCCall('AlertHistoryListPaginated', [
                pageSize,
                this.alertsPage * pageSize,
                this.includeAcknowledged
            ]);
            this.alerts = result?.Alerts || [];
            this.alertsTotal = result?.Total || 0;
        } catch (e) {
            console.error('Failed to load alerts:', e);
        }
    }

    async addMute() {
        if (!this.newAlertName || !this.newReason) {
            alert('Please fill in Alert Category and Reason');
            return;
        }

        try {
            const pattern = this.newPattern || null;
            const expiresHours = this.newExpiresHours > 0 ? this.newExpiresHours : null;
            
            await RPCCall('AlertMuteAdd', [
                this.newAlertName,
                pattern,
                this.newReason,
                'web-ui',
                expiresHours
            ]);
            
            this.newAlertName = '';
            this.newPattern = '';
            this.newReason = '';
            this.newExpiresHours = 0;
            this.showAddForm = false;
            
            await this.loadData();
        } catch (e) {
            alert('Failed to add mute: ' + e.message);
        }
    }

    async removeMute(id) {
        if (!confirm('Are you sure you want to deactivate this mute?')) return;
        
        try {
            await RPCCall('AlertMuteRemove', [id]);
            await this.loadData();
        } catch (e) {
            alert('Failed to remove mute: ' + e.message);
        }
    }

    async reactivateMute(id) {
        try {
            await RPCCall('AlertMuteReactivate', [id]);
            await this.loadData();
        } catch (e) {
            alert('Failed to reactivate mute: ' + e.message);
        }
    }

    async sendTestAlert() {
        try {
            await RPCCall('AlertSendTest');
            await this.loadAlerts(); // Refresh to show the new alert
            alert('Test alert created! It should now appear in the Alert History tab and sidebar indicator.');
        } catch (e) {
            alert('Failed to send test alert: ' + e.message);
        }
    }

    async acknowledgeAlert(id) {
        try {
            await RPCCall('AlertAcknowledge', [id, 'web-ui']);
            await this.loadAlerts();
        } catch (e) {
            alert('Failed to acknowledge alert: ' + e.message);
        }
    }

    async acknowledgeAll() {
        const unacked = this.alerts.filter(a => !a.Acknowledged);
        if (unacked.length === 0) return;
        
        if (!confirm(`Acknowledge all ${unacked.length} alerts on this page?`)) return;
        
        try {
            const ids = unacked.map(a => a.ID);
            await RPCCall('AlertAcknowledgeMultiple', [ids, 'web-ui']);
            await this.loadAlerts();
        } catch (e) {
            alert('Failed to acknowledge alerts: ' + e.message);
        }
    }

    async openComments(alert) {
        this.commentAlertId = alert.ID;
        this.commentAlertName = alert.AlertName;
        this.newComment = '';
        try {
            this.comments = await RPCCall('AlertCommentList', [alert.ID]) || [];
        } catch (e) {
            this.comments = [];
        }
        this.showCommentModal = true;
    }

    closeComments() {
        this.showCommentModal = false;
        this.commentAlertId = null;
        this.comments = [];
    }

    async addComment() {
        if (!this.newComment.trim()) return;
        
        try {
            await RPCCall('AlertCommentAdd', [this.commentAlertId, this.newComment.trim(), 'web-ui']);
            this.newComment = '';
            this.comments = await RPCCall('AlertCommentList', [this.commentAlertId]) || [];
            await this.loadAlerts(); // Refresh comment count
        } catch (e) {
            alert('Failed to add comment: ' + e.message);
        }
    }

    formatDate(dateStr) {
        if (!dateStr) return 'Never';
        const date = new Date(dateStr);
        return date.toLocaleString();
    }

    formatTimeAgo(dateStr) {
        if (!dateStr) return '';
        const date = new Date(dateStr);
        const now = new Date();
        const diff = now - date;
        const mins = Math.floor(diff / 60000);
        const hours = Math.floor(mins / 60);
        const days = Math.floor(hours / 24);
        
        if (days > 0) return `${days}d ago`;
        if (hours > 0) return `${hours}h ago`;
        if (mins > 0) return `${mins}m ago`;
        return 'just now';
    }

    async changePage(delta) {
        const newPage = this.alertsPage + delta;
        if (newPage < 0) return;
        const pageSize = 25;
        if (newPage * pageSize >= this.alertsTotal) return;
        this.alertsPage = newPage;
        await this.loadAlerts();
    }

    async toggleIncludeAcknowledged() {
        this.includeAcknowledged = !this.includeAcknowledged;
        this.alertsPage = 0;
        await this.loadAlerts();
    }

    render() {
        if (this.loading) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous">
                <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
                <div style="padding: 20px;">Loading...</div>
            `;
        }

        if (this.error) {
            return html`
                <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous">
                <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
                <div style="padding: 20px; color: #B63333;">Error: ${this.error}</div>
            `;
        }

        return html`
            <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.1.3/dist/css/bootstrap.min.css" rel="stylesheet" crossorigin="anonymous">
            <link rel="stylesheet" href="/ux/main.css" onload="document.body.style.visibility = 'initial'">
            
            <div style="padding: 20px;">
                <h1 style="margin-bottom: 20px;">Alert Management</h1>
                
                <!-- Tabs -->
                <div class="tabs">
                    <button class="tab ${this.activeTab === 'history' ? 'active' : ''}" 
                            @click="${() => this.activeTab = 'history'}">
                        Alert History
                        ${this.alertsTotal > 0 ? html`<span class="tab-badge">${this.alertsTotal}</span>` : ''}
                    </button>
                    <button class="tab ${this.activeTab === 'mutes' ? 'active' : ''}" 
                            @click="${() => this.activeTab = 'mutes'}">
                        Mute Rules
                        ${this.mutes.filter(m => m.Active).length > 0 ? html`<span class="tab-badge">${this.mutes.filter(m => m.Active).length}</span>` : ''}
                    </button>
                    <button class="tab ${this.activeTab === 'settings' ? 'active' : ''}" 
                            @click="${() => this.activeTab = 'settings'}">
                        Settings & Test
                    </button>
                </div>
                
                ${this.activeTab === 'history' ? this.renderHistoryTab() : ''}
                ${this.activeTab === 'mutes' ? this.renderMutesTab() : ''}
                ${this.activeTab === 'settings' ? this.renderSettingsTab() : ''}
            </div>
            
            ${this.showCommentModal ? this.renderCommentModal() : ''}
        `;
    }

    renderHistoryTab() {
        const pageSize = 25;
        const totalPages = Math.ceil(this.alertsTotal / pageSize);
        const unackedCount = this.alerts.filter(a => !a.Acknowledged).length;

        return html`
            <div class="tab-content">
                <div class="history-controls">
                    <label class="checkbox-label">
                        <input type="checkbox" 
                               ?checked="${this.includeAcknowledged}"
                               @change="${this.toggleIncludeAcknowledged}">
                        Show acknowledged alerts
                    </label>
                    ${unackedCount > 0 ? html`
                        <button class="btn btn-small" @click="${this.acknowledgeAll}">
                            Acknowledge All (${unackedCount})
                        </button>
                    ` : ''}
                    <button class="btn btn-small" @click="${() => this.loadAlerts()}">Refresh</button>
                </div>

                ${this.alerts.length === 0 ? html`
                    <div class="empty-state">
                        <p>No alerts found.</p>
                        <p style="color: #666; font-size: 0.9em;">
                            ${this.includeAcknowledged ? 'No alerts have been recorded yet.' : 'All alerts have been acknowledged.'}
                        </p>
                    </div>
                ` : html`
                    <div class="alert-list">
                        ${this.alerts.map(alert => this.renderAlert(alert))}
                    </div>
                    
                    <div class="pagination">
                        <button class="btn btn-small" 
                                ?disabled="${this.alertsPage === 0}"
                                @click="${() => this.changePage(-1)}">Previous</button>
                        <span>Page ${this.alertsPage + 1} of ${totalPages || 1}</span>
                        <button class="btn btn-small" 
                                ?disabled="${(this.alertsPage + 1) * pageSize >= this.alertsTotal}"
                                @click="${() => this.changePage(1)}">Next</button>
                    </div>
                `}
            </div>
        `;
    }

    renderAlert(alert) {
        return html`
            <div class="alert-card ${alert.Acknowledged ? 'acknowledged' : ''}">
                <div class="alert-header">
                    <div class="alert-title">
                        <span class="alert-name">${alert.AlertName}</span>
                        <span class="alert-time">${this.formatTimeAgo(alert.CreatedAt)}</span>
                        ${alert.SentToPlugins ? html`<span class="tag tag-sent">Sent</span>` : html`<span class="tag tag-local">Local Only</span>`}
                        ${alert.Acknowledged ? html`<span class="tag tag-acked">Acked</span>` : ''}
                    </div>
                    <div class="alert-actions">
                        <button class="btn btn-small" @click="${() => this.openComments(alert)}">
                            Comments ${alert.CommentCount > 0 ? `(${alert.CommentCount})` : ''}
                        </button>
                        ${!alert.Acknowledged ? html`
                            <button class="btn btn-small btn-ack" @click="${() => this.acknowledgeAlert(alert.ID)}">
                                Acknowledge
                            </button>
                        ` : ''}
                    </div>
                </div>
                <div class="alert-message">${alert.Message}</div>
                <div class="alert-meta">
                    ${alert.MachineName ? html`Machine: ${alert.MachineName} | ` : ''}
                    Created: ${this.formatDate(alert.CreatedAt)}
                    ${alert.Acknowledged ? html` | Acked by ${alert.AcknowledgedBy} at ${this.formatDate(alert.AcknowledgedAt)}` : ''}
                </div>
            </div>
        `;
    }

    renderCommentModal() {
        return html`
            <div class="modal-overlay" @click="${this.closeComments}">
                <div class="modal" @click="${e => e.stopPropagation()}">
                    <div class="modal-header">
                        <h3>Comments - ${this.commentAlertName}</h3>
                        <button class="modal-close" @click="${this.closeComments}">x</button>
                    </div>
                    <div class="modal-body">
                        ${this.comments.length === 0 ? html`
                            <p style="color: #666;">No comments yet.</p>
                        ` : html`
                            <div class="comment-list">
                                ${this.comments.map(c => html`
                                    <div class="comment">
                                        <div class="comment-meta">${c.CreatedBy} - ${this.formatDate(c.CreatedAt)}</div>
                                        <div class="comment-text">${c.Comment}</div>
                                    </div>
                                `)}
                            </div>
                        `}
                        <div class="comment-form">
                            <textarea placeholder="Add a comment..."
                                      .value="${this.newComment}"
                                      @input="${e => this.newComment = e.target.value}"></textarea>
                            <button class="btn" @click="${this.addComment}">Add Comment</button>
                        </div>
                    </div>
                </div>
            </div>
        `;
    }

    renderMutesTab() {
        const activeMutes = this.mutes.filter(m => m.Active);
        const inactiveMutes = this.mutes.filter(m => !m.Active);

        return html`
            <div class="tab-content">
                <div class="info-box">
                    <h4>About Alert Muting</h4>
                    <p style="margin-bottom: 10px;">
                        Mute specific alerts to prevent them from being sent to your alerting system (PagerDuty, Slack, etc.).
                        Muted alerts are still recorded in the history but not sent externally.
                    </p>
                    <p style="margin-bottom: 10px;"><strong>Available Alert Categories:</strong></p>
                    <div class="category-list">
                        ${this.categories.map(c => html`<span class="category-item">${c}</span>`)}
                    </div>
                </div>

                <div style="margin-bottom: 20px;">
                    <button class="btn" @click="${() => this.showAddForm = !this.showAddForm}">
                        ${this.showAddForm ? 'Cancel' : 'Add New Mute'}
                    </button>
                </div>

                ${this.showAddForm ? html`
                    <div class="add-form">
                        <h3 style="margin-bottom: 15px;">Add Alert Mute</h3>
                        
                        <div class="form-row">
                            <label>Alert Category *</label>
                            <select .value="${this.newAlertName}"
                                    @change="${e => this.newAlertName = e.target.value}">
                                <option value="">Select category...</option>
                                ${this.categories.map(c => html`
                                    <option value="${c}" ?selected="${this.newAlertName === c}">${c}</option>
                                `)}
                            </select>
                        </div>
                        
                        <div class="form-row">
                            <label>Pattern (optional)</label>
                            <input type="text"
                                   placeholder="e.g., 'unsafe to run PoSt' - leave empty to mute entire category"
                                   .value="${this.newPattern}"
                                   @input="${e => this.newPattern = e.target.value}">
                            <small>Substring match against alert message. Leave empty to mute all alerts in this category.</small>
                        </div>
                        
                        <div class="form-row">
                            <label>Reason *</label>
                            <input type="text"
                                   placeholder="e.g., 'Known issue, being worked on'"
                                   .value="${this.newReason}"
                                   @input="${e => this.newReason = e.target.value}">
                        </div>
                        
                        <div class="form-row">
                            <label>Expires In (hours)</label>
                            <input type="number"
                                   placeholder="0 = never expires"
                                   .value="${this.newExpiresHours}"
                                   @input="${e => this.newExpiresHours = parseInt(e.target.value) || 0}">
                            <small>Set to 0 for permanent mute</small>
                        </div>
                        
                        <button class="btn" @click="${this.addMute}">Add Mute</button>
                    </div>
                ` : ''}

                <h3 style="margin-bottom: 15px;">Active Mutes (${activeMutes.length})</h3>
                ${activeMutes.length === 0 ? html`
                    <p style="color: #aaa;">No active mutes</p>
                ` : activeMutes.map(mute => this.renderMute(mute))}

                ${inactiveMutes.length > 0 ? html`
                    <h3 style="margin-top: 30px; margin-bottom: 15px;">Inactive Mutes (${inactiveMutes.length})</h3>
                    ${inactiveMutes.map(mute => this.renderMute(mute))}
                ` : ''}
            </div>
        `;
    }

    renderMute(mute) {
        const isExpired = mute.ExpiresAt && new Date(mute.ExpiresAt) < new Date();
        
        return html`
            <div class="mute-card ${mute.Active ? '' : 'inactive'}">
                <div class="mute-header">
                    <div>
                        <span class="mute-name">${mute.AlertName}</span>
                        <span class="tag ${mute.Active ? 'tag-active' : 'tag-inactive'}">
                            ${mute.Active ? (isExpired ? 'Expired' : 'Active') : 'Inactive'}
                        </span>
                        ${mute.Pattern ? html`<span class="tag tag-pattern">Pattern: "${mute.Pattern}"</span>` : ''}
                    </div>
                    <div>
                        ${mute.Active ? html`
                            <button class="btn btn-small" @click="${() => this.removeMute(mute.ID)}">Deactivate</button>
                        ` : html`
                            <button class="btn btn-small" @click="${() => this.reactivateMute(mute.ID)}">Reactivate</button>
                        `}
                    </div>
                </div>
                <div class="mute-meta">
                    Muted by: ${mute.MutedBy} | 
                    Created: ${this.formatDate(mute.MutedAt)} | 
                    Expires: ${mute.ExpiresAt ? this.formatDate(mute.ExpiresAt) : 'Never'}
                </div>
                <div class="mute-reason">
                    <strong>Reason:</strong> ${mute.Reason}
                </div>
            </div>
        `;
    }

    renderSettingsTab() {
        return html`
            <div class="tab-content">
                <div class="info-box test-alert-box">
                    <h4>Test Alert System</h4>
                    <p style="margin-bottom: 10px;">
                        Create a test alert to verify the alert history and sidebar indicator are working correctly.
                    </p>
                    <p style="margin-bottom: 10px;">
                        <strong>How it works:</strong> The test alert is created immediately and will appear in the Alert History tab
                        and update the sidebar indicator. Use the Acknowledge button to clear it.
                    </p>
                    <p style="margin-bottom: 15px; color: #aaa; font-size: 0.9em;">
                        Note: This test alert is for UI testing only and is not sent to external alerting plugins.
                    </p>
                    <button class="btn" @click="${this.sendTestAlert}">Create Test Alert</button>
                </div>

                <div class="info-box">
                    <h4>Alert System Info</h4>
                    <p><strong>Check Interval:</strong> Every 30 minutes</p>
                    <p><strong>Alert Categories:</strong></p>
                    <ul style="margin: 10px 0; padding-left: 20px;">
                        <li><strong>Balance Check</strong> - Wallet balance monitoring</li>
                        <li><strong>TaskFailures</strong> - Failed tasks in the system</li>
                        <li><strong>PermanentStorageSpace</strong> - Storage capacity warnings</li>
                        <li><strong>WindowPost</strong> - WindowPoSt deadline issues</li>
                        <li><strong>WinningPost</strong> - Block production issues</li>
                        <li><strong>NowCheck</strong> - Immediate alerts from the system</li>
                        <li><strong>ChainSync</strong> - Chain synchronization status</li>
                        <li><strong>MissingSectors</strong> - Missing sector data</li>
                        <li><strong>PendingMessages</strong> - Stuck messages</li>
                    </ul>
                </div>
            </div>
        `;
    }
}

AlertsManage.styles = css`
    :host {
        display: block;
        width: 100%;
    }
    
    /* Tabs */
    .tabs {
        display: flex;
        gap: 5px;
        margin-bottom: 20px;
        border-bottom: 1px solid rgba(255,255,255,0.1);
        padding-bottom: 10px;
    }
    .tab {
        all: unset;
        padding: 10px 20px;
        cursor: pointer;
        border-radius: 4px 4px 0 0;
        background: rgba(255,255,255,0.05);
        color: #aaa;
    }
    .tab:hover {
        background: rgba(255,255,255,0.1);
        color: #fff;
    }
    .tab.active {
        background: rgba(59, 130, 246, 0.3);
        color: #fff;
    }
    .tab-badge {
        margin-left: 8px;
        padding: 2px 8px;
        border-radius: 10px;
        background: rgba(255,255,255,0.2);
        font-size: 0.85em;
    }
    .tab-content {
        padding: 10px 0;
    }
    
    /* Alert history */
    .history-controls {
        display: flex;
        align-items: center;
        gap: 15px;
        margin-bottom: 20px;
    }
    .checkbox-label {
        display: flex;
        align-items: center;
        gap: 8px;
        cursor: pointer;
    }
    .checkbox-label input[type="checkbox"] {
        all: revert;
        width: auto;
        cursor: pointer;
    }
    .empty-state {
        text-align: center;
        padding: 40px;
        color: #aaa;
    }
    .alert-list {
        display: flex;
        flex-direction: column;
        gap: 10px;
    }
    .alert-card {
        background: rgba(255,255,255,0.05);
        border-left: 4px solid #B63333;
        border-radius: 4px;
        padding: 15px;
    }
    .alert-card.acknowledged {
        border-left-color: #4BB543;
        opacity: 0.7;
    }
    .alert-header {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        margin-bottom: 10px;
        flex-wrap: wrap;
        gap: 10px;
    }
    .alert-title {
        display: flex;
        align-items: center;
        gap: 10px;
        flex-wrap: wrap;
    }
    .alert-name {
        font-weight: 600;
        font-size: 1.1em;
    }
    .alert-time {
        color: #aaa;
        font-size: 0.85em;
    }
    .alert-actions {
        display: flex;
        gap: 8px;
    }
    .alert-message {
        background: rgba(0,0,0,0.2);
        padding: 10px;
        border-radius: 4px;
        margin-bottom: 10px;
        white-space: pre-wrap;
        word-break: break-word;
    }
    .alert-meta {
        font-size: 0.85em;
        color: #aaa;
    }
    .pagination {
        display: flex;
        justify-content: center;
        align-items: center;
        gap: 15px;
        margin-top: 20px;
        padding-top: 20px;
        border-top: 1px solid rgba(255,255,255,0.1);
    }
    
    /* Tags */
    .tag {
        display: inline-block;
        padding: 2px 8px;
        border-radius: 4px;
        font-size: 0.8em;
    }
    .tag-sent {
        background: rgba(59, 130, 246, 0.3);
        color: #3B82F6;
    }
    .tag-local {
        background: rgba(170, 170, 170, 0.3);
        color: #aaa;
    }
    .tag-acked {
        background: rgba(75, 181, 67, 0.3);
        color: #4BB543;
    }
    .tag-active {
        background: rgba(75, 181, 67, 0.3);
        color: #4BB543;
    }
    .tag-inactive {
        background: rgba(170, 170, 170, 0.3);
        color: #aaa;
    }
    .tag-pattern {
        background: rgba(59, 130, 246, 0.3);
        color: #3B82F6;
    }
    
    /* Modal */
    .modal-overlay {
        position: fixed;
        top: 0;
        left: 0;
        right: 0;
        bottom: 0;
        background: rgba(0,0,0,0.7);
        display: flex;
        align-items: center;
        justify-content: center;
        z-index: 1000;
    }
    .modal {
        background: #1a1a2e;
        border-radius: 8px;
        width: 90%;
        max-width: 600px;
        max-height: 80vh;
        display: flex;
        flex-direction: column;
    }
    .modal-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 15px 20px;
        border-bottom: 1px solid rgba(255,255,255,0.1);
    }
    .modal-header h3 {
        margin: 0;
    }
    .modal-close {
        all: unset;
        cursor: pointer;
        padding: 5px 10px;
        font-size: 1.2em;
    }
    .modal-body {
        padding: 20px;
        overflow-y: auto;
    }
    .comment-list {
        margin-bottom: 20px;
    }
    .comment {
        background: rgba(255,255,255,0.05);
        padding: 10px;
        border-radius: 4px;
        margin-bottom: 10px;
    }
    .comment-meta {
        font-size: 0.85em;
        color: #aaa;
        margin-bottom: 5px;
    }
    .comment-form textarea {
        width: 100%;
        min-height: 80px;
        margin-bottom: 10px;
        padding: 10px;
        background: rgba(255,255,255,0.08);
        border: 1px solid #A1A1A1;
        color: inherit;
        font-family: inherit;
        resize: vertical;
    }
    
    /* Mutes */
    .mute-card {
        background: rgba(255,255,255,0.05);
        border-radius: 8px;
        padding: 15px;
        margin-bottom: 10px;
    }
    .mute-card.inactive {
        opacity: 0.5;
    }
    .mute-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: 10px;
        flex-wrap: wrap;
        gap: 10px;
    }
    .mute-name {
        font-weight: 600;
        font-size: 1.1em;
        margin-right: 10px;
    }
    .mute-meta {
        font-size: 0.85em;
        color: #aaa;
    }
    .mute-reason {
        background: rgba(0,0,0,0.2);
        padding: 8px 12px;
        border-radius: 4px;
        margin-top: 10px;
    }
    
    /* Forms */
    .add-form {
        background: rgba(255,255,255,0.05);
        border-radius: 8px;
        padding: 20px;
        margin-bottom: 20px;
    }
    .form-row {
        margin-bottom: 15px;
    }
    .form-row label {
        display: block;
        margin-bottom: 5px;
        color: #aaa;
    }
    .form-row small {
        display: block;
        margin-top: 5px;
        color: #666;
    }
    select {
        all: unset;
        box-sizing: border-box;
        width: 100%;
        padding: 0.4rem 1rem;
        border: 1px solid #A1A1A1;
        background-color: rgba(255, 255, 255, 0.08);
        font-size: 1rem;
        cursor: pointer;
    }
    select:hover, select:focus {
        box-shadow: 0 0 0 1px #FFF inset;
    }
    
    /* Info boxes */
    .info-box {
        background: rgba(59, 130, 246, 0.1);
        border: 1px solid rgba(59, 130, 246, 0.3);
        border-radius: 8px;
        padding: 15px;
        margin-bottom: 20px;
    }
    .info-box h4 {
        margin-top: 0;
        color: #3B82F6;
    }
    .test-alert-box {
        background: rgba(255, 193, 7, 0.1);
        border: 1px solid rgba(255, 193, 7, 0.3);
    }
    .test-alert-box h4 {
        color: #FFC107;
    }
    .category-list {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
    }
    .category-item {
        background: rgba(255,255,255,0.1);
        padding: 4px 10px;
        border-radius: 4px;
        font-size: 0.9em;
    }
    
    /* Buttons */
    .btn {
        padding: 0.4rem 1rem;
        border: none;
        border-radius: 0;
        background-color: var(--color-form-default);
        color: var(--color-text-primary);
        cursor: pointer;
    }
    .btn:hover, .btn:focus {
        background-color: var(--color-form-default-pressed);
        color: var(--color-text-secondary);
    }
    .btn:disabled {
        opacity: 0.5;
        cursor: not-allowed;
    }
    .btn-small {
        padding: 0.25rem 0.75rem;
        font-size: 0.9em;
    }
    .btn-ack {
        background-color: rgba(75, 181, 67, 0.3);
    }
    .btn-ack:hover {
        background-color: rgba(75, 181, 67, 0.5);
    }
`;

customElements.define('alerts-manage', AlertsManage);
