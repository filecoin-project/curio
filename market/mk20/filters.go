package mk20

import (
	"context"
	"database/sql"
	"errors"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
)

func (m *MK20) applyAllowList(ctx context.Context, client string) (bool, error) {
	var allowed sql.NullBool
	err := m.DB.QueryRow(ctx, `SELECT status FROM market_allow_list WHERE wallet = $1`, client).Scan(&allowed)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, xerrors.Errorf("failed to query the allow list status from DB: %w", err)
		}
		return !m.cfg.Market.StorageMarketConfig.MK20.DenyUnknownClients, nil
	}

	if allowed.Valid {
		return allowed.Bool, nil
	}

	return false, nil
}
