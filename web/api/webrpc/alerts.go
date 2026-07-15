package webrpc

import (
	"context"
	"strings"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/alertmanager/plugin"
)

// AlertMute represents a muted alert pattern
type AlertMute struct {
	ID        int64      `db:"id" json:"ID"`
	AlertName string     `db:"alert_name" json:"AlertName"`
	Pattern   *string    `db:"pattern" json:"Pattern"`
	Reason    string     `db:"reason" json:"Reason"`
	MutedBy   string     `db:"muted_by" json:"MutedBy"`
	MutedAt   time.Time  `db:"muted_at" json:"MutedAt"`
	ExpiresAt *time.Time `db:"expires_at" json:"ExpiresAt"`
	Active    bool       `db:"active" json:"Active"`
}

type AlertOngoingEntry struct {
	AlertName     string    `db:"alert_name" json:"AlertName"`
	Message       string    `db:"message" json:"Message"`
	CreatedAt     time.Time `db:"created_at" json:"CreatedAt"`
	LastUpdatedAt time.Time `db:"last_updated_at" json:"LastUpdatedAt"`
}

func (a *WebRPC) AlertOngoingList(ctx context.Context) ([]AlertOngoingEntry, error) {
	var alerts []AlertOngoingEntry
	err := a.Deps.DB.Select(ctx, &alerts, `
		SELECT
			system || '_' || subsystem || '_' || condition AS alert_name,
			message,
			created_at,
			last_seen_at AS last_updated_at
		FROM alert_conditions
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, xerrors.Errorf("getting ongoing alerts: %w", err)
	}
	return alerts, nil
}

// AlertMuteList returns all active and inactive alert mutes
func (a *WebRPC) AlertMuteList(ctx context.Context) ([]AlertMute, error) {
	var mutes []AlertMute
	err := a.Deps.DB.Select(ctx, &mutes, `
		SELECT id, alert_name, pattern, reason, muted_by, muted_at, expires_at, active
		FROM alert_mutes
		ORDER BY active DESC, muted_at DESC
	`)
	if err != nil {
		return nil, xerrors.Errorf("getting alert mutes: %w", err)
	}
	return mutes, nil
}

// AlertMuteAdd adds a new alert mute
func (a *WebRPC) AlertMuteAdd(ctx context.Context, alertName string, pattern *string, reason string, mutedBy string, expiresInHours *int) error {
	var expiresAt *time.Time
	if expiresInHours != nil && *expiresInHours > 0 {
		expiresAt = new(time.Now().Add(time.Duration(*expiresInHours) * time.Hour))
	}

	_, err := a.Deps.DB.Exec(ctx, `
		INSERT INTO alert_mutes (alert_name, pattern, reason, muted_by, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, alertName, pattern, reason, mutedBy, expiresAt)
	if err != nil {
		return xerrors.Errorf("adding alert mute: %w", err)
	}
	return nil
}

// AlertMuteDeactivate deactivates an alert mute
func (a *WebRPC) AlertMuteDeactivate(ctx context.Context, id int64) error {
	_, err := a.Deps.DB.Exec(ctx, `UPDATE alert_mutes SET active = FALSE WHERE id = $1`, id)
	if err != nil {
		return xerrors.Errorf("deactivating alert mute: %w", err)
	}
	return nil
}

// AlertMuteDelete deletes an alert mute
func (a *WebRPC) AlertMuteDelete(ctx context.Context, id int64) error {
	_, err := a.Deps.DB.Exec(ctx, `DELETE FROM alert_mutes WHERE active = FALSE AND id = $1`, id)
	if err != nil {
		return xerrors.Errorf("deleting alert mute: %w", err)
	}
	return nil
}

// AlertMuteReactivate reactivates an alert mute
func (a *WebRPC) AlertMuteReactivate(ctx context.Context, id int64) error {
	_, err := a.Deps.DB.Exec(ctx, `UPDATE alert_mutes SET active = TRUE WHERE id = $1`, id)
	if err != nil {
		return xerrors.Errorf("reactivating alert mute: %w", err)
	}
	return nil
}

// AlertCategoriesList returns the list of alert categories that can be muted
func (a *WebRPC) AlertCategoriesList(ctx context.Context) ([]string, error) {
	defaults := alertmanager.AlertNames
	return append(defaults, "others"), nil
}

// AlertPendingCount returns the count of pending (unprocessed) alerts from the alerts table
// This is used for the sidebar indicator
func (a *WebRPC) AlertPendingCount(ctx context.Context) (int, error) {
	var count int
	err := a.Deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM alerts`).Scan(&count)
	if err != nil {
		return 0, xerrors.Errorf("counting pending alerts: %w", err)
	}
	return count, nil
}

// AlertSendTest sends a test alert through every enabled alert plugin and records the outcome
// in alert_history. With no plugins enabled it just records locally - an error is
// only returned if a configured plugin fails to deliver.
func (a *WebRPC) AlertSendTest(ctx context.Context) error {
	const testMessage = "Test alert from Curio Web UI - if you see this, your alerting system is working correctly."

	plugins := plugin.LoadAlertPlugins(a.Deps.Cfg.Alerting)

	var failures []string
	for _, p := range plugins {
		if err := p.SendAlert(&plugin.AlertPayload{
			Summary:  "Curio Test Alert",
			Severity: "info",
			Source:   "Curio Web UI",
			Details:  map[string]any{"TestAlert": testMessage},
			Time:     time.Now(),
		}); err != nil {
			failures = append(failures, err.Error())
		}
	}

	sentToPlugins := len(plugins) > 0 && len(failures) == 0
	_, err := a.Deps.DB.Exec(ctx, `
		INSERT INTO alert_history (alert_name, message, machine_name, sent_to_plugins, sent_at)
		VALUES ('TestAlert', $1, 'web-ui', $2, NOW())
	`, testMessage, sentToPlugins)
	if err != nil {
		return xerrors.Errorf("inserting test alert: %w", err)
	}

	if len(failures) > 0 {
		return xerrors.Errorf("test alert recorded, but delivery failed on %d/%d channel(s): %s",
			len(failures), len(plugins), strings.Join(failures, "; "))
	}
	return nil
}

// AlertHistoryEntry represents an alert in the history
type AlertHistoryEntry struct {
	ID             int64      `db:"id" json:"ID"`
	AlertName      string     `db:"alert_name" json:"AlertName"`
	Message        string     `db:"message" json:"Message"`
	MachineName    *string    `db:"machine_name" json:"MachineName"`
	CreatedAt      time.Time  `db:"created_at" json:"CreatedAt"`
	Acknowledged   bool       `db:"acknowledged" json:"Acknowledged"`
	AcknowledgedBy *string    `db:"acknowledged_by" json:"AcknowledgedBy"`
	AcknowledgedAt *time.Time `db:"acknowledged_at" json:"AcknowledgedAt"`
	SentToPlugins  bool       `db:"sent_to_plugins" json:"SentToPlugins"`
	SentAt         *time.Time `db:"sent_at" json:"SentAt"`
	CommentCount   int        `db:"comment_count" json:"CommentCount"`
}

// AlertComment represents a comment on an alert
type AlertComment struct {
	ID        int64     `db:"id" json:"ID"`
	AlertID   int64     `db:"alert_id" json:"AlertID"`
	Comment   string    `db:"comment" json:"Comment"`
	CreatedBy string    `db:"created_by" json:"CreatedBy"`
	CreatedAt time.Time `db:"created_at" json:"CreatedAt"`
}

// AlertHistoryListResult wraps the paginated result
type AlertHistoryListResult struct {
	Alerts              []AlertHistoryEntry `json:"Alerts"`
	Total               int                 `json:"Total"`
	UnacknowledgedTotal int                 `json:"UnacknowledgedTotal"`
}

// AlertHistoryListPaginated returns paginated alert history (JSON-RPC compatible)
func (a *WebRPC) AlertHistoryListPaginated(ctx context.Context, limit int, offset int, includeAcknowledged bool) (*AlertHistoryListResult, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	var total, unacknowledgedTotal int
	if includeAcknowledged {
		err := a.Deps.DB.QueryRow(ctx, `
			SELECT
				COUNT(*)::int,
				COUNT(*) FILTER (WHERE NOT acknowledged)::int
			FROM alert_history
		`).Scan(&total, &unacknowledgedTotal)
		if err != nil {
			return nil, xerrors.Errorf("counting alerts: %w", err)
		}
	} else {
		err := a.Deps.DB.QueryRow(ctx, `SELECT COUNT(*)::int FROM alert_history WHERE NOT acknowledged`).Scan(&unacknowledgedTotal)
		if err != nil {
			return nil, xerrors.Errorf("counting unacknowledged alerts: %w", err)
		}
		total = unacknowledgedTotal
	}

	var alerts []AlertHistoryEntry
	if total > 0 && offset < total {
		var err error
		if includeAcknowledged {
			err = a.Deps.DB.Select(ctx, &alerts, `
				SELECT
					ah.id, ah.alert_name, ah.message, ah.machine_name, ah.created_at,
					ah.acknowledged, ah.acknowledged_by, ah.acknowledged_at,
					ah.sent_to_plugins, ah.sent_at,
					COALESCE((SELECT COUNT(*) FROM alert_comments ac WHERE ac.alert_id = ah.id), 0) as comment_count
				FROM alert_history ah
				ORDER BY ah.created_at DESC LIMIT $1 OFFSET $2
			`, limit, offset)
		} else {
			err = a.Deps.DB.Select(ctx, &alerts, `
				SELECT
					ah.id, ah.alert_name, ah.message, ah.machine_name, ah.created_at,
					ah.acknowledged, ah.acknowledged_by, ah.acknowledged_at,
					ah.sent_to_plugins, ah.sent_at,
					COALESCE((SELECT COUNT(*) FROM alert_comments ac WHERE ac.alert_id = ah.id), 0) as comment_count
				FROM alert_history ah
				WHERE NOT ah.acknowledged
				ORDER BY ah.created_at DESC LIMIT $1 OFFSET $2
			`, limit, offset)
		}
		if err != nil {
			return nil, xerrors.Errorf("getting alert history: %w", err)
		}
	}

	return &AlertHistoryListResult{
		Alerts:              alerts,
		Total:               total,
		UnacknowledgedTotal: unacknowledgedTotal,
	}, nil
}

// AlertAcknowledge marks an alert as acknowledged
func (a *WebRPC) AlertAcknowledge(ctx context.Context, id int64, acknowledgedBy string) error {
	_, err := a.Deps.DB.Exec(ctx, `
		UPDATE alert_history 
		SET acknowledged = TRUE, acknowledged_by = $1, acknowledged_at = NOW()
		WHERE id = $2
	`, acknowledgedBy, id)
	if err != nil {
		return xerrors.Errorf("acknowledging alert: %w", err)
	}
	return nil
}

// AlertAcknowledgeMultiple marks multiple alerts as acknowledged
func (a *WebRPC) AlertAcknowledgeMultiple(ctx context.Context, ids []int64, acknowledgedBy string) error {
	_, err := a.Deps.DB.Exec(ctx, `
		UPDATE alert_history 
		SET acknowledged = TRUE, acknowledged_by = $1, acknowledged_at = NOW()
		WHERE id = ANY($2)
	`, acknowledgedBy, ids)
	if err != nil {
		return xerrors.Errorf("acknowledging alerts: %w", err)
	}
	return nil
}

// AlertCommentAdd adds a comment to an alert
func (a *WebRPC) AlertCommentAdd(ctx context.Context, alertID int64, comment string, createdBy string) error {
	_, err := a.Deps.DB.Exec(ctx, `
		INSERT INTO alert_comments (alert_id, comment, created_by)
		VALUES ($1, $2, $3)
	`, alertID, comment, createdBy)
	if err != nil {
		return xerrors.Errorf("adding comment: %w", err)
	}
	return nil
}

// AlertCommentList returns all comments for an alert
func (a *WebRPC) AlertCommentList(ctx context.Context, alertID int64) ([]AlertComment, error) {
	var comments []AlertComment
	err := a.Deps.DB.Select(ctx, &comments, `
		SELECT id, alert_id, comment, created_by, created_at
		FROM alert_comments
		WHERE alert_id = $1
		ORDER BY created_at ASC
	`, alertID)
	if err != nil {
		return nil, xerrors.Errorf("getting comments: %w", err)
	}
	return comments, nil
}

// AlertUnacknowledgedCount returns count of unacknowledged alerts (for sidebar)
func (a *WebRPC) AlertUnacknowledgedCount(ctx context.Context) (int, error) {
	var count int
	err := a.Deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM alert_history WHERE NOT acknowledged`).Scan(&count)
	if err != nil {
		return 0, xerrors.Errorf("counting unacknowledged alerts: %w", err)
	}
	return count, nil
}

func (a *WebRPC) AlertOngoingCount(ctx context.Context) (int, error) {
	var count int
	err := a.Deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM alert_conditions`).Scan(&count)
	if err != nil {
		return 0, xerrors.Errorf("counting ongoing alerts: %w", err)
	}
	return count, nil
}

func (a *WebRPC) AlertTotalCount(ctx context.Context) (int, error) {
	c1, err := a.AlertUnacknowledgedCount(ctx)
	if err != nil {
		return 0, err
	}
	c2, err := a.AlertOngoingCount(ctx)
	if err != nil {
		return 0, err
	}
	return c1 + c2, nil
}

func (a *WebRPC) AlertAcknowledgeAll(ctx context.Context, acknowledgedBy string) error {
	_, err := a.Deps.DB.Exec(ctx, `UPDATE alert_history SET acknowledged = TRUE, acknowledged_by = $1, acknowledged_at = NOW() WHERE NOT acknowledged`, acknowledgedBy)
	if err != nil {
		return xerrors.Errorf("acknowledging all alerts: %w", err)
	}
	return nil
}
