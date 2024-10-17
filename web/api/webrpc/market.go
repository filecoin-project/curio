package webrpc

import (
	"context"
	"fmt"
)

type StorageAsk struct {
	SpID          int64 `db:"sp_id" json:"sp_id"`
	Price         int64 `db:"price" json:"price"`
	VerifiedPrice int64 `db:"verified_price" json:"verified_price"`
	MinSize       int64 `db:"min_size" json:"min_size"`
	MaxSize       int64 `db:"max_size" json:"max_size"`
	CreatedAt     int64 `db:"created_at" json:"created_at"`
	Expiry        int64 `db:"expiry" json:"expiry"`
	Sequence      int64 `db:"sequence" json:"sequence"`
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
