package pdp

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestNormalizeDeletePieceIDs(t *testing.T) {
	t.Run("single id passes through", func(t *testing.T) {
		out, err := normalizeDeletePieceIDs([]uint64{5})
		require.NoError(t, err)
		require.Equal(t, []int64{5}, out)
	})

	t.Run("duplicates collapse preserving first-seen order", func(t *testing.T) {
		out, err := normalizeDeletePieceIDs([]uint64{3, 1, 3, 2, 1})
		require.NoError(t, err)
		require.Equal(t, []int64{3, 1, 2}, out)
	})

	t.Run("batch at the cap is accepted", func(t *testing.T) {
		ids := make([]uint64, MaxDeletePiecesBatchSize)
		for i := range ids {
			ids[i] = uint64(i)
		}
		out, err := normalizeDeletePieceIDs(ids)
		require.NoError(t, err)
		require.Len(t, out, MaxDeletePiecesBatchSize)
	})

	t.Run("batch over the cap is rejected", func(t *testing.T) {
		ids := make([]uint64, MaxDeletePiecesBatchSize+1)
		_, err := normalizeDeletePieceIDs(ids)
		require.Error(t, err)
	})

	t.Run("max int64 is accepted", func(t *testing.T) {
		out, err := normalizeDeletePieceIDs([]uint64{math.MaxInt64})
		require.NoError(t, err)
		require.Equal(t, []int64{math.MaxInt64}, out)
	})

	t.Run("id beyond int64 range is rejected", func(t *testing.T) {
		_, err := normalizeDeletePieceIDs([]uint64{uint64(math.MaxInt64) + 1})
		require.Error(t, err)
	})
}

func TestHandleDeleteDataSetPiece_MissingPiece404(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	ensurePullTestDataSet(t, db, testDataSetId, "public")
	p := &PDPService{Auth: &NullAuth{}, db: db}

	body, err := json.Marshal(map[string]any{"pieceIds": []uint64{999001, 999002}})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, "/", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("dataSetId", strconv.FormatUint(testDataSetId, 10))
	rctx.URLParams.Add("pieceID", "999001")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	p.handleDeleteDataSetPiece(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "One or more piece not found")
}
