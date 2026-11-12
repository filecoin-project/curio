package curioalerting

import (
	"context"
	"encoding/json"
	"strings"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var log = logging.Logger("curio/alerting")

type AlertingSystem struct {
	db *harmonydb.DB
}

func NewAlertingSystem(db *harmonydb.DB) *AlertingSystem {
	return &AlertingSystem{db: db}
}

var _ AlertingInterface = (*AlertingSystem)(nil)

type AlertingInterface interface {
	EmitEvent(ctx context.Context, event AlertEvent) error
	ActivateCondition(ctx context.Context, condition AlertCondition, message string) error
	ResolveCondition(ctx context.Context, condition AlertCondition) error
}

// Keys of AlertEvent.Context; the alert page matches these literal values.
const (
	CONTEXT_KEY_DATASET = "dataset"
	CONTEXT_KEY_RAIL    = "rail"
)

type AlertEvent struct {
	System    string
	Subsystem string
	Message   string

	// Context maps a CONTEXT_KEY_* to the id of an object the event is about.
	Context map[string]string
}

type AlertCondition struct {
	System    string
	Subsystem string
	Condition string
}

func (as *AlertingSystem) EmitEvent(ctx context.Context, event AlertEvent) error {
	if err := requireAlertField("system", event.System); err != nil {
		return err
	}
	if err := requireAlertField("subsystem", event.Subsystem); err != nil {
		return err
	}
	if err := requireAlertField("message", event.Message); err != nil {
		return err
	}

	var eventContext []byte
	if len(event.Context) > 0 {
		var err error
		if eventContext, err = encodeEventContext(event.Context); err != nil {
			log.Warnw("recording alert event without its context", "system", event.System, "subsystem", event.Subsystem, "error", err)
		}
	}

	_, err := as.db.Exec(ctx, `
		INSERT INTO alert_history (
			alert_name, message, machine_name, sent_to_plugins,
			kind, system, subsystem, condition, context
		)
		VALUES ($1, $2, NULL, FALSE, 'event', $3, $4, NULL, $5::jsonb)
	`, alertName(event.System, event.Subsystem, ""), event.Message, event.System, event.Subsystem, eventContext)
	if err != nil {
		return xerrors.Errorf("recording alert event: %w", err)
	}
	return nil
}

// encodeEventContext encodes context as JSON, rejecting NUL characters unsupported by jsonb.
func encodeEventContext(eventContext map[string]string) ([]byte, error) {
	for key, value := range eventContext {
		if strings.ContainsRune(key, 0) || strings.ContainsRune(value, 0) {
			return nil, xerrors.Errorf("context entry %q contains NUL", key)
		}
	}
	return json.Marshal(eventContext)
}

func (as *AlertingSystem) ActivateCondition(ctx context.Context, condition AlertCondition, message string) error {
	if err := validateCondition(condition.System, condition.Subsystem, condition.Condition); err != nil {
		return err
	}

	if err := requireAlertField("message", message); err != nil {
		return err
	}

	_, err := as.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		_, err := tx.Exec(`
			INSERT INTO alert_conditions (
				system, subsystem, condition, message, created_at, last_seen_at, repeat_count, last_notified_at
			)
			VALUES ($1, $2, $3, $4, NOW(), NOW(), 0, NULL)
			ON CONFLICT (system, subsystem, condition) DO UPDATE
			SET message = EXCLUDED.message,
				last_seen_at = NOW(),
				repeat_count = alert_conditions.repeat_count + 1
		`, condition.System, condition.Subsystem, condition.Condition, message)
		if err != nil {
			return false, xerrors.Errorf("upserting alert condition: %w", err)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("activating alert condition: %w", err)
	}
	return nil
}

func (as *AlertingSystem) ResolveCondition(ctx context.Context, resolution AlertCondition) error {
	if err := validateCondition(resolution.System, resolution.Subsystem, resolution.Condition); err != nil {
		return err
	}

	_, err := as.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		_, err := tx.Exec(`
			WITH resolved AS (
				DELETE FROM alert_conditions
				WHERE system = $1
				  AND subsystem = $2
				  AND condition = $3
				RETURNING system, subsystem, condition, message, created_at, last_seen_at, repeat_count
			)
			INSERT INTO alert_history (
				alert_name, message, machine_name, sent_to_plugins,
				kind, system, subsystem, condition,
				condition_created_at, condition_last_seen_at, condition_resolved_at, condition_repeat_count
			)
			SELECT $4, message, NULL, FALSE,
				'condition', system, subsystem, condition,
				created_at, last_seen_at, NOW(), repeat_count
			FROM resolved
		`, resolution.System, resolution.Subsystem, resolution.Condition, alertName(resolution.System, resolution.Subsystem, resolution.Condition))
		if err != nil {
			return false, xerrors.Errorf("moving resolved alert condition to history: %w", err)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("resolving alert condition: %w", err)
	}
	return nil
}

func validateCondition(system, subsystem, condition string) error {
	if err := requireAlertField("system", system); err != nil {
		return err
	}
	if err := requireAlertField("subsystem", subsystem); err != nil {
		return err
	}
	if err := requireAlertField("condition", condition); err != nil {
		return err
	}
	return nil
}

func requireAlertField(name, value string) error {
	if value == "" {
		return xerrors.Errorf("alert %s is required", name)
	}
	return nil
}

func alertName(system, subsystem, condition string) string {
	if condition == "" {
		return system + "_" + subsystem
	}
	return system + "_" + subsystem + "_" + condition
}
