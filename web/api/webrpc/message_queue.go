package webrpc

import (
	"context"
	"time"

	"golang.org/x/xerrors"
)

type MessageQueueSummary struct {
	FilPendingCount int                  `json:"filPendingCount"`
	EthPendingCount int                  `json:"ethPendingCount"`
	FilPending      []PendingMessageItem `json:"filPending"`
	EthPending      []EthPendingMessage  `json:"ethPending"`
}

type PendingMessageItem struct {
	MessageCid string    `db:"message_cid" json:"messageCid"`
	AddedAt    time.Time `db:"added_at" json:"addedAt"`
}

type EthPendingMessage struct {
	TxHash     string     `json:"txHash"`
	SendReason string     `json:"sendReason"`
	SendTime   *time.Time `json:"sendTime"`
}

func (a *WebRPC) MessageQueueSummary(ctx context.Context) (MessageQueueSummary, error) {
	var filPending []PendingMessageItem
	err := a.Deps.DB.Select(ctx, &filPending, `
		SELECT signed_message_cid AS message_cid, created_at AS added_at
		FROM message_waits
		WHERE executed_tsk_cid IS NULL
		ORDER BY created_at DESC
		LIMIT 20`)
	if err != nil {
		return MessageQueueSummary{}, xerrors.Errorf("fil pending messages: %w", err)
	}

	var ethRows []struct {
		TxHash     string     `db:"tx_hash"`
		SendReason string     `db:"send_reason"`
		SendTime   *time.Time `db:"send_time"`
	}
	err = a.Deps.DB.Select(ctx, &ethRows, `
		SELECT mwe.signed_tx_hash AS tx_hash, mse.send_reason, mse.send_time
		FROM message_waits_eth mwe
		LEFT JOIN message_sends_eth mse ON mwe.signed_tx_hash = lower(trim(both from mse.signed_hash))
		WHERE mwe.tx_status = 'pending'
		ORDER BY mse.send_time DESC NULLS LAST, mwe.signed_tx_hash
		LIMIT 20`)
	if err != nil {
		return MessageQueueSummary{}, xerrors.Errorf("eth pending messages: %w", err)
	}

	var filCount int
	err = a.Deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM message_waits WHERE executed_tsk_cid IS NULL`).Scan(&filCount)
	if err != nil {
		return MessageQueueSummary{}, xerrors.Errorf("fil pending count: %w", err)
	}

	var ethCount int
	err = a.Deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM message_waits_eth WHERE tx_status = 'pending'`).Scan(&ethCount)
	if err != nil {
		return MessageQueueSummary{}, xerrors.Errorf("eth pending count: %w", err)
	}

	ethPending := make([]EthPendingMessage, 0, len(ethRows))
	for _, row := range ethRows {
		ethPending = append(ethPending, EthPendingMessage{
			TxHash:     row.TxHash,
			SendReason: row.SendReason,
			SendTime:   row.SendTime,
		})
	}

	if filPending == nil {
		filPending = []PendingMessageItem{}
	}
	if ethPending == nil {
		ethPending = []EthPendingMessage{}
	}

	return MessageQueueSummary{
		FilPendingCount: filCount,
		EthPendingCount: ethCount,
		FilPending:      filPending,
		EthPending:      ethPending,
	}, nil
}
