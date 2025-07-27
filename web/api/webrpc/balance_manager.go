package webrpc

import (
	"context"
	"database/sql"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
)

// BalanceMgrRule represents a balance manager rule with display-friendly fields.
// Watermark values are returned as short FIL strings (e.g. "0.25 FIL").
// Nullable DB fields are returned as pointers so JSON omits them when nil.
// Time values are formatted on the JS side – we simply expose RFC3339 strings.
//
// NOTE: Any new columns that should be exposed can be trivially added here.
type BalanceMgrRule struct {
	ID              int64   `json:"id"`
	SubjectAddress  string  `json:"subject_address"`
	SecondAddress   string  `json:"second_address"`
	ActionType      string  `json:"action_type"`
	LowWatermark    string  `json:"low_watermark"`
	HighWatermark   string  `json:"high_watermark"`
	TaskID          *int64  `json:"task_id,omitempty"`
	LastMsgCID      *string `json:"last_msg_cid,omitempty"`
	LastMsgSentAt   *string `json:"last_msg_sent_at,omitempty"`
	LastMsgLandedAt *string `json:"last_msg_landed_at,omitempty"`
}

// BalanceMgrRules returns all balance-manager rules.
func (a *WebRPC) BalanceMgrRules(ctx context.Context) ([]BalanceMgrRule, error) {
	const q = `SELECT id, subject_address, second_address, action_type, low_watermark_fil_balance, high_watermark_fil_balance,
        last_msg_cid, last_msg_sent_at, last_msg_landed_at, active_task_id FROM balance_manager_addresses ORDER BY id`

	rows, err := a.deps.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BalanceMgrRule
	for rows.Next() {
		var (
			id                                                   int64
			subjectAddr, secondAddr, actionType, lowStr, highStr string
			lastMsgCID                                           sql.NullString
			lastMsgSentAt, lastMsgLandedAt                       sql.NullTime
			taskID                                               sql.NullInt64
		)
		if err := rows.Scan(&id, &subjectAddr, &secondAddr, &actionType, &lowStr, &highStr, &lastMsgCID, &lastMsgSentAt, &lastMsgLandedAt, &taskID); err != nil {
			return nil, err
		}

		// Convert watermarks to short FIL strings.
		lowBig, err := types.ParseFIL(lowStr)
		if err != nil {
			return nil, err
		}
		highBig, err := types.ParseFIL(highStr)
		if err != nil {
			return nil, err
		}

		rule := BalanceMgrRule{
			ID:             id,
			SubjectAddress: subjectAddr,
			SecondAddress:  secondAddr,
			ActionType:     actionType,
			LowWatermark:   types.FIL(lowBig).Short(),
			HighWatermark:  types.FIL(highBig).Short(),
		}
		if lastMsgCID.Valid {
			rule.LastMsgCID = &lastMsgCID.String
		}
		if lastMsgSentAt.Valid {
			s := lastMsgSentAt.Time.Format(time.RFC3339)
			rule.LastMsgSentAt = &s
		}
		if lastMsgLandedAt.Valid {
			s := lastMsgLandedAt.Time.Format(time.RFC3339)
			rule.LastMsgLandedAt = &s
		}
		if taskID.Valid {
			rule.TaskID = &taskID.Int64
		}
		out = append(out, rule)
	}
	return out, nil
}

// BalanceMgrRuleUpdate updates the low / high watermark thresholds for a rule.
// Values should be provided as strings parsable by types.ParseFIL (e.g. "0.5" or "0.5 FIL").
func (a *WebRPC) BalanceMgrRuleUpdate(ctx context.Context, id int64, lowWatermark, highWatermark string) error {
	lowAmt, err := types.ParseFIL(lowWatermark)
	if err != nil {
		return err
	}
	highAmt, err := types.ParseFIL(highWatermark)
	if err != nil {
		return err
	}

	_, err = a.deps.DB.Exec(ctx, `UPDATE balance_manager_addresses SET low_watermark_fil_balance = $1, high_watermark_fil_balance = $2 WHERE id = $3`, lowAmt.String(), highAmt.String(), id)
	return err
}

// BalanceMgrRuleRemove deletes a balance-manager rule.
func (a *WebRPC) BalanceMgrRuleRemove(ctx context.Context, id int64) error {
	_, err := a.deps.DB.Exec(ctx, `DELETE FROM balance_manager_addresses WHERE id = $1`, id)
	return err
}

// BalanceMgrRuleAdd creates a new balance-manager rule.
// Watermarks are provided as FIL strings. Addresses use Fil/ID strings.
func (a *WebRPC) BalanceMgrRuleAdd(ctx context.Context, subject, second, actionType, lowWatermark, highWatermark string) error {
	// Basic sanity – ensure addresses parse but keep original text for insertion.
	if _, err := address.NewFromString(subject); err != nil {
		return xerrors.Errorf("invalid subject address: %w", err)
	}
	if _, err := address.NewFromString(second); err != nil {
		return xerrors.Errorf("invalid second address: %w", err)
	}

	switch actionType {
	case "requester":
	case "active-provider":
	default:
		return xerrors.Errorf("invalid action type: %s", actionType)
	}

	lowAmt, err := types.ParseFIL(lowWatermark)
	if err != nil {
		return xerrors.Errorf("invalid low watermark: %w", err)
	}
	highAmt, err := types.ParseFIL(highWatermark)
	if err != nil {
		return xerrors.Errorf("invalid high watermark: %w", err)
	}

	_, err = a.deps.DB.Exec(ctx, `INSERT INTO balance_manager_addresses (subject_address, second_address, action_type, low_watermark_fil_balance, high_watermark_fil_balance) VALUES ($1,$2,$3,$4,$5)`, subject, second, actionType, lowAmt.String(), highAmt.String())
	return err
}
