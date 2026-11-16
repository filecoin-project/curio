package cuhttp

import (
	"context"
	"crypto/tls"
	"errors"
	"sync"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
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

var sharedManagers sync.Map // domain -> *autocert.Manager

// TLSEnabled reports whether Curio should terminate TLS with autocert for this HTTP config.
func TLSEnabled(cfg *config.HTTPConfig) bool {
	return cfg != nil && cfg.Enable && cfg.DomainName != "" && !cfg.DelegateTLS
}

// SharedCertManager returns the process-wide autocert manager for DomainName.
// Public HTTPS and the admin GUI share one manager so TLS-ALPN renewals on :443
// cover certificates presented on :4701. Returns nil when TLS is not configured.
func SharedCertManager(cfg *config.HTTPConfig, db *harmonydb.DB) *autocert.Manager {
	if !TLSEnabled(cfg) || db == nil {
		return nil
	}
	if v, ok := sharedManagers.Load(cfg.DomainName); ok {
		return v.(*autocert.Manager)
	}
	m := &autocert.Manager{
		Cache:      AutocertCache{DB: db},
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(cfg.DomainName),
	}
	actual, _ := sharedManagers.LoadOrStore(cfg.DomainName, m)
	return actual.(*autocert.Manager)
}

// SharedTLSConfig returns TLSConfig from SharedCertManager, or nil when TLS is off.
func SharedTLSConfig(cfg *config.HTTPConfig, db *harmonydb.DB) *tls.Config {
	m := SharedCertManager(cfg, db)
	if m == nil {
		return nil
	}
	return m.TLSConfig()
}
