package indexstore

import (
	"context"
	"testing"

	"github.com/filecoin-project/curio/deps/config"
)

func TestReadonlyIndexStoreSkipsCassandra(t *testing.T) {
	t.Parallel()

	store := NewReadonlyIndexStore(config.DefaultCurioConfig())
	if err := store.Start(context.Background(), false); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.session != nil {
		t.Fatal("expected no cassandra session in readonly mode")
	}
	if _, err := store.PiecesContainingMultihash(context.Background(), nil); err == nil {
		t.Fatal("expected error when using readonly index store")
	}
}
