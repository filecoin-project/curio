package indexstore

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
)

// NewReadonlyIndexStore returns an index store that skips Cassandra/YCQL in readonly database mode.
func NewReadonlyIndexStore(cfg *config.CurioConfig) *IndexStore {
	return &IndexStore{
		readonly: true,
		settings: settings{
			InsertBatchSize:   cfg.Market.StorageMarketConfig.Indexing.InsertBatchSize,
			InsertConcurrency: cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency,
		},
	}
}

func (i *IndexStore) requireSession() error {
	if i.readonly || i.session == nil {
		return xerrors.Errorf("cassandra index store is unavailable in readonly database mode")
	}
	return nil
}
