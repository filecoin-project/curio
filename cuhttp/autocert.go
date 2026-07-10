package cuhttp

import (
	"context"
	"errors"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// AutocertCache stores Let's Encrypt certificates in harmonydb.
type AutocertCache struct {
	DB *harmonydb.DB
}

func (c AutocertCache) Get(ctx context.Context, key string) ([]byte, error) {
	var ret []byte
	err := c.DB.QueryRow(ctx, `SELECT v FROM autocert_cache WHERE k = $1`, key).Scan(&ret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, autocert.ErrCacheMiss
		}

		log.Warnf("failed to get the value from DB for key %s: %s", key, err)
		return nil, xerrors.Errorf("failed to get the value from DB for key %s: %w", key, err)
	}
	return ret, nil
}

func (c AutocertCache) Put(ctx context.Context, key string, data []byte) error {
	_, err := c.DB.Exec(ctx, `INSERT INTO autocert_cache (k, v) VALUES ($1, $2)
						ON CONFLICT (k) DO UPDATE SET v = EXCLUDED.v`, key, data)
	if err != nil {
		log.Warnf("failed to inset key value pair in DB: %s", err)
		return xerrors.Errorf("failed to inset key value pair in DB: %w", err)
	}
	return nil
}

func (c AutocertCache) Delete(ctx context.Context, key string) error {
	_, err := c.DB.Exec(ctx, `DELETE FROM autocert_cache WHERE k = $1`, key)
	if err != nil {
		log.Warnf("failed to delete key value pair from DB: %s", err)
		return xerrors.Errorf("failed to delete key value pair from DB: %w", err)
	}
	return nil
}

var _ autocert.Cache = AutocertCache{}
