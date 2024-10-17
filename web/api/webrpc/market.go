package webrpc

import (
	"context"
	"fmt"
	"time"
)

type StorageAsk struct {
	SpID          int64 `db:"sp_id"`
	Price         int64 `db:"price"`
	VerifiedPrice int64 `db:"verified_price"`
	MinSize       int64 `db:"min_size"`
	MaxSize       int64 `db:"max_size"`
	CreatedAt     int64 `db:"created_at"`
	Expiry        int64 `db:"expiry"`
	Sequence      int64 `db:"sequence"`
}

func (a *WebRPC) GetStorageAsk(ctx context.Context, spID int64) (*StorageAsk, error) {
	var asks []StorageAsk
	err := a.deps.DB.Select(ctx, &asks, `
        SELECT sp_id, price, verified_price, min_size, max_size, created_at, expiry, sequence
        FROM market_mk12_storage_ask
        WHERE sp_id = $1
    `, spID)
	if err != nil {
		return nil, err
	}
	if len(asks) == 0 {
		return nil, fmt.Errorf("no storage ask found for sp_id %d", spID)
	}
	return &asks[0], nil
}

func (a *WebRPC) SetStorageAsk(ctx context.Context, ask *StorageAsk) error {
	err := a.deps.DB.QueryRow(ctx, `
        INSERT INTO market_mk12_storage_ask (
            sp_id, price, verified_price, min_size, max_size, created_at, expiry, sequence
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, 0
        )
        ON CONFLICT (sp_id) DO UPDATE SET
            price = EXCLUDED.price,
            verified_price = EXCLUDED.verified_price,
            min_size = EXCLUDED.min_size,
            max_size = EXCLUDED.max_size,
            created_at = EXCLUDED.created_at,
            expiry = EXCLUDED.expiry,
            sequence = market_mk12_storage_ask.sequence + 1
        RETURNING sequence
    `, ask.SpID, ask.Price, ask.VerifiedPrice, ask.MinSize, ask.MaxSize, ask.CreatedAt, ask.Expiry).Scan(&ask.Sequence)
	if err != nil {
		return fmt.Errorf("failed to insert/update storage ask: %w", err)
	}

	return nil
}

type MK12Pipeline struct {
	UUID           string     `db:"uuid" json:"uuid"`
	SpID           int64      `db:"sp_id" json:"sp_id"`
	Started        bool       `db:"started" json:"started"`
	PieceCid       string     `db:"piece_cid" json:"piece_cid"`
	PieceSize      int64      `db:"piece_size" json:"piece_size"`
	RawSize        *int64     `db:"raw_size" json:"raw_size"`
	Offline        bool       `db:"offline" json:"offline"`
	URL            *string    `db:"url" json:"url"`
	Headers        []byte     `db:"headers" json:"headers"`
	CommTaskID     *int64     `db:"commp_task_id" json:"commp_task_id"`
	AfterCommp     bool       `db:"after_commp" json:"after_commp"`
	PSDTaskID      *int64     `db:"psd_task_id" json:"psd_task_id"`
	AfterPSD       bool       `db:"after_psd" json:"after_psd"`
	PSDWaitTime    *time.Time `db:"psd_wait_time" json:"psd_wait_time"`
	FindDealTaskID *int64     `db:"find_deal_task_id" json:"find_deal_task_id"`
	AfterFindDeal  bool       `db:"after_find_deal" json:"after_find_deal"`
	Sector         *int64     `db:"sector" json:"sector"`
	Offset         *int64     `db:"sector_offset" json:"sector_offset"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	Complete       bool       `db:"complete" json:"complete"`
}

func (a *WebRPC) GetDealPipelines(ctx context.Context, limit int, offset int) ([]MK12Pipeline, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var pipelines []MK12Pipeline
	err := a.deps.DB.Select(ctx, &pipelines, `
        SELECT
            uuid,
            sp_id,
            started,
            piece_cid,
            piece_size,
            raw_size,
            offline,
            url,
            headers,
            commp_task_id,
            after_commp,
            psd_task_id,
            after_psd,
            psd_wait_time,
            find_deal_task_id,
            after_find_deal,
            sector,
            sector_offset,
            created_at,
            complete
        FROM market_mk12_deal_pipeline
        ORDER BY created_at DESC
        LIMIT $1 OFFSET $2
    `, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deal pipelines: %w", err)
	}

	return pipelines, nil
}
