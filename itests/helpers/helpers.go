package helpers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/api"
)

func RedeclareAllLocalStorage(ctx context.Context, t *testing.T, capi api.Curio) {
	t.Helper()

	localStorage, err := capi.StorageLocal(ctx)
	require.NoError(t, err)
	for id := range localStorage {
		storID := id
		require.NoError(t, capi.StorageRedeclare(ctx, &storID, false))
	}
}
