package pdp

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/storiface"
)

type uploadTestPieceIO struct {
	writes       int
	removes      int
	declaredSize int64
	verifySize   bool
	storageType  storiface.PathType
	pieceID      storiface.PieceNumber
}

func (m *uploadTestPieceIO) WritePiece(context.Context, *harmonytask.TaskID, storiface.PieceNumber, int64, io.Reader, storiface.PathType) error {
	return errors.New("unexpected WritePiece call")
}

func (m *uploadTestPieceIO) WriteUploadPiece(_ context.Context, pieceID storiface.PieceNumber, size int64, data io.Reader, storageType storiface.PathType, verifySize bool) (abi.PieceInfo, uint64, error) {
	m.writes++
	m.declaredSize = size
	m.verifySize = verifySize
	m.storageType = storageType
	m.pieceID = pieceID

	body, err := io.ReadAll(data)
	if err != nil {
		return abi.PieceInfo{}, 0, err
	}
	calc := &commp.Calc{}
	defer calc.Reset()
	if _, err := calc.Write(body); err != nil {
		return abi.PieceInfo{}, 0, err
	}
	digest, paddedSize, err := calc.Digest()
	if err != nil {
		return abi.PieceInfo{}, 0, err
	}
	pieceCID, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		return abi.PieceInfo{}, 0, err
	}
	return abi.PieceInfo{PieceCID: pieceCID, Size: abi.PaddedPieceSize(paddedSize)}, uint64(len(body)), nil
}

func (m *uploadTestPieceIO) PieceReader(context.Context, storiface.PieceNumber) (io.ReadCloser, error) {
	return nil, errors.New("unexpected PieceReader call")
}

func (m *uploadTestPieceIO) RemovePiece(_ context.Context, pieceID storiface.PieceNumber) error {
	m.removes++
	m.pieceID = pieceID
	return nil
}

func testPieceCIDs(t *testing.T, body []byte) (string, string, int64) {
	t.Helper()
	calc := &commp.Calc{}
	defer calc.Reset()
	_, err := calc.Write(body)
	require.NoError(t, err)
	digest, paddedSize, err := calc.Digest()
	require.NoError(t, err)
	pieceCIDV1, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)
	pieceCIDV2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(len(body)))
	require.NoError(t, err)
	return pieceCIDV1.String(), pieceCIDV2.String(), int64(paddedSize)
}

func createClassicUpload(t *testing.T, service *PDPService, pieceCIDV2 string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece", bytes.NewBufferString(`{"pieceCid":"`+pieceCIDV2+`"}`))
	rec := httptest.NewRecorder()
	service.handlePiecePost(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return path.Base(rec.Header().Get("Location"))
}

func putClassicUpload(service *PDPService, uploadID string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/pdp/piece/upload/"+uploadID, bytes.NewReader(body))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("uploadUUID", uploadID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rec := httptest.NewRecorder()
	service.handlePieceUpload(rec, req)
	return rec
}

func TestExactSizeReader(t *testing.T) {
	t.Run("exact", func(t *testing.T) {
		reader := &exactSizeReader{r: bytes.NewReader([]byte("abcd")), remaining: 4}
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, []byte("abcd"), body)
	})

	t.Run("short", func(t *testing.T) {
		reader := &exactSizeReader{r: bytes.NewReader([]byte("abc")), remaining: 4}
		body, err := io.ReadAll(reader)
		require.ErrorIs(t, err, errPieceTooShort)
		require.Equal(t, []byte("abc"), body)
	})

	t.Run("leaves trailing byte", func(t *testing.T) {
		body := NewTimeoutLimitReader(bytes.NewReader([]byte("abcde")), time.Second)
		reader := &exactSizeReader{r: body, remaining: 4}
		read, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, []byte("abcd"), read)
		hasExtra, err := readHasExtraByte(body)
		require.NoError(t, err)
		require.True(t, hasExtra)
	})

	t.Run("timeout limit overrun is a trailing byte", func(t *testing.T) {
		body := NewTimeoutLimitReader(bytes.NewReader([]byte("x")), time.Second)
		body.totalBytes = int64(PieceSizeMaxLimit)
		hasExtra, err := readHasExtraByte(body)
		require.NoError(t, err)
		require.True(t, hasExtra)
	})
}

func TestNeedsSaveCacheBoundary(t *testing.T) {
	maxRawBelowThreshold := minPaddedPieceSizeForCache * 127 / 256
	require.False(t, needsSaveCache(maxRawBelowThreshold))
	require.True(t, needsSaveCache(maxRawBelowThreshold+1))
}

func TestHandlePieceUploadWritesDirectlyAndPublishes(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	body := bytes.Repeat([]byte{0xab}, 1024)
	pieceCIDV1, pieceCIDV2, paddedSize := testPieceCIDs(t, body)
	pio := &uploadTestPieceIO{}
	service := &PDPService{Auth: &NullAuth{}, db: db, pieceIO: pio}
	uploadID := createClassicUpload(t, service, pieceCIDV2)

	rec := putClassicUpload(service, uploadID, body)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, 1, pio.writes)
	require.Equal(t, int64(len(body)), pio.declaredSize)
	require.True(t, pio.verifySize)
	require.Equal(t, storiface.PathStorage, pio.storageType)

	var parkedPieceID, pieceRefID int64
	var complete, skip, cache bool
	var dataURL sql.NullString
	err = db.QueryRow(t.Context(), `
		SELECT pp.id, ppr.ref_id, pp.complete, pp.skip, ppr.data_url, pr.needs_save_cache
		FROM pdp_piecerefs pr
		JOIN parked_piece_refs ppr ON ppr.ref_id = pr.piece_ref
		JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE pr.service = 'public' AND pr.piece_cid = $1
	`, pieceCIDV1).Scan(&parkedPieceID, &pieceRefID, &complete, &skip, &dataURL, &cache)
	require.NoError(t, err)
	require.Equal(t, int64(pio.pieceID), parkedPieceID)
	require.True(t, complete)
	require.True(t, skip)
	require.False(t, dataURL.Valid)
	require.False(t, cache)

	var padded int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT piece_padded_size FROM parked_pieces WHERE id = $1`, parkedPieceID).Scan(&padded))
	require.Equal(t, paddedSize, padded)

	var uploadExists bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pdp_piece_uploads WHERE id = $1)`, uploadID).Scan(&uploadExists))
	require.False(t, uploadExists)
}

func TestHandlePieceUploadRejectsInvalidLengthsAndReleasesClaim(t *testing.T) {
	tests := []struct {
		name string
		body func([]byte) []byte
	}{
		{name: "short", body: func(body []byte) []byte { return body[:len(body)-1] }},
		{name: "oversized", body: func(body []byte) []byte { return append(append([]byte{}, body...), 0xff) }},
		{name: "cid mismatch", body: func(body []byte) []byte {
			wrong := append([]byte{}, body...)
			wrong[0] ^= 0xff
			return wrong
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)

			expected := bytes.Repeat([]byte{0xcd}, 1024)
			_, pieceCIDV2, _ := testPieceCIDs(t, expected)
			pio := &uploadTestPieceIO{}
			service := &PDPService{Auth: &NullAuth{}, db: db, pieceIO: pio}
			uploadID := createClassicUpload(t, service, pieceCIDV2)

			rec := putClassicUpload(service, uploadID, test.body(expected))
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

			var pieceRef sql.NullInt64
			require.NoError(t, db.QueryRow(t.Context(), `SELECT piece_ref FROM pdp_piece_uploads WHERE id = $1`, uploadID).Scan(&pieceRef))
			require.False(t, pieceRef.Valid)
			require.Equal(t, 1, pio.removes)

			retry := putClassicUpload(service, uploadID, expected)
			require.Equal(t, http.StatusNoContent, retry.Code, retry.Body.String())
		})
	}
}

func TestHandlePiecePostPublishesAlreadyStoredPiece(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	body := bytes.Repeat([]byte{0xef}, 1024)
	pieceCIDV1, pieceCIDV2, paddedSize := testPieceCIDs(t, body)
	_, err = db.Exec(t.Context(), `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term, skip)
		VALUES ($1, $2, $3, TRUE, TRUE, TRUE)
	`, pieceCIDV1, paddedSize, len(body))
	require.NoError(t, err)

	service := &PDPService{Auth: &NullAuth{}, db: db, pieceIO: &uploadTestPieceIO{}}
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece", bytes.NewBufferString(`{"pieceCid":"`+pieceCIDV2+`"}`))
	rec := httptest.NewRecorder()
	service.handlePiecePost(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var refs, uploads int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT COUNT(*) FROM pdp_piecerefs WHERE piece_cid = $1`, pieceCIDV1).Scan(&refs))
	require.NoError(t, db.QueryRow(t.Context(), `SELECT COUNT(*) FROM pdp_piece_uploads WHERE piece_cid = $1`, pieceCIDV1).Scan(&uploads))
	require.Equal(t, int64(1), refs)
	require.Zero(t, uploads)
}

func TestSecondPendingUploadReusesCompletedPiece(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	body := bytes.Repeat([]byte{0x17}, 1024)
	pieceCIDV1, pieceCIDV2, _ := testPieceCIDs(t, body)
	pio := &uploadTestPieceIO{}
	service := &PDPService{Auth: &NullAuth{}, db: db, pieceIO: pio}
	firstUpload := createClassicUpload(t, service, pieceCIDV2)
	secondUpload := createClassicUpload(t, service, pieceCIDV2)

	first := putClassicUpload(service, firstUpload, body)
	require.Equal(t, http.StatusNoContent, first.Code, first.Body.String())
	second := putClassicUpload(service, secondUpload, body)
	require.Equal(t, http.StatusNoContent, second.Code, second.Body.String())
	require.Equal(t, 1, pio.writes)

	var refs, uploads int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT COUNT(*) FROM pdp_piecerefs WHERE piece_cid = $1`, pieceCIDV1).Scan(&refs))
	require.NoError(t, db.QueryRow(t.Context(), `SELECT COUNT(*) FROM pdp_piece_uploads WHERE piece_cid = $1`, pieceCIDV1).Scan(&uploads))
	require.Equal(t, int64(2), refs)
	require.Zero(t, uploads)
}

func TestActiveSameCIDUploadRejectsSecondClaim(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	body := bytes.Repeat([]byte{0x31}, 1024)
	pieceCIDV1, pieceCIDV2, paddedSize := testPieceCIDs(t, body)
	pio := &uploadTestPieceIO{}
	service := &PDPService{Auth: &NullAuth{}, db: db, pieceIO: pio}
	firstUpload := createClassicUpload(t, service, pieceCIDV2)
	secondUpload := createClassicUpload(t, service, pieceCIDV2)

	firstClaim, err := service.claimDirectUpload(t.Context(), firstUpload, "public", pieceCIDV1, int64(len(body)), paddedSize)
	require.NoError(t, err)
	require.True(t, firstClaim.created)

	second := putClassicUpload(service, secondUpload, body)
	require.Equal(t, http.StatusConflict, second.Code, second.Body.String())
	require.Zero(t, pio.writes)
	require.Zero(t, pio.removes)

	var firstRef, secondRef sql.NullInt64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT piece_ref FROM pdp_piece_uploads WHERE id = $1`, firstUpload).Scan(&firstRef))
	require.NoError(t, db.QueryRow(t.Context(), `SELECT piece_ref FROM pdp_piece_uploads WHERE id = $1`, secondUpload).Scan(&secondRef))
	require.True(t, firstRef.Valid)
	require.Equal(t, firstClaim.pieceRefID, firstRef.Int64)
	require.False(t, secondRef.Valid)

	var complete bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT complete FROM parked_pieces WHERE id = $1`, firstClaim.parkedPieceID).Scan(&complete))
	require.False(t, complete)
}

func TestCleanupExpiredDirectUploadClaims(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	body := bytes.Repeat([]byte{0x42}, 1024)
	pieceCIDV1, pieceCIDV2, paddedSize := testPieceCIDs(t, body)
	pio := &uploadTestPieceIO{}
	service := &PDPService{Auth: &NullAuth{}, db: db, pieceIO: pio}
	uploadID := createClassicUpload(t, service, pieceCIDV2)
	claim, err := service.claimDirectUpload(t.Context(), uploadID, "public", pieceCIDV1, int64(len(body)), paddedSize)
	require.NoError(t, err)
	require.True(t, claim.created)

	_, err = db.Exec(t.Context(), `UPDATE pdp_piece_uploads SET created_at = NOW() - INTERVAL '2 hours' WHERE id = $1`, uploadID)
	require.NoError(t, err)
	require.NoError(t, service.cleanupExpiredDirectUploadClaims(t.Context()))

	var pieceRef sql.NullInt64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT piece_ref FROM pdp_piece_uploads WHERE id = $1`, uploadID).Scan(&pieceRef))
	require.False(t, pieceRef.Valid)

	var refExists bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM parked_piece_refs WHERE ref_id = $1)`, claim.pieceRefID).Scan(&refExists))
	require.False(t, refExists)

	var parkedExists bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM parked_pieces WHERE id = $1)`, claim.parkedPieceID).Scan(&parkedExists))
	require.False(t, parkedExists)
	require.Equal(t, 1, pio.removes)
	require.Equal(t, storiface.PieceNumber(claim.parkedPieceID), pio.pieceID)

	retry := putClassicUpload(service, uploadID, body)
	require.Equal(t, http.StatusNoContent, retry.Code, retry.Body.String())
}

func TestCleanupExpiredDirectUploadClaimsLeavesFreshClaim(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	body := bytes.Repeat([]byte{0x53}, 1024)
	pieceCIDV1, pieceCIDV2, paddedSize := testPieceCIDs(t, body)
	pio := &uploadTestPieceIO{}
	service := &PDPService{Auth: &NullAuth{}, db: db, pieceIO: pio}
	uploadID := createClassicUpload(t, service, pieceCIDV2)
	claim, err := service.claimDirectUpload(t.Context(), uploadID, "public", pieceCIDV1, int64(len(body)), paddedSize)
	require.NoError(t, err)

	require.NoError(t, service.cleanupExpiredDirectUploadClaims(t.Context()))

	var pieceRef sql.NullInt64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT piece_ref FROM pdp_piece_uploads WHERE id = $1`, uploadID).Scan(&pieceRef))
	require.True(t, pieceRef.Valid)
	require.Equal(t, claim.pieceRefID, pieceRef.Int64)

	var refExists, parkedExists bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM parked_piece_refs WHERE ref_id = $1)`, claim.pieceRefID).Scan(&refExists))
	require.NoError(t, db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM parked_pieces WHERE id = $1)`, claim.parkedPieceID).Scan(&parkedExists))
	require.True(t, refExists)
	require.True(t, parkedExists)
	require.Zero(t, pio.removes)
}
