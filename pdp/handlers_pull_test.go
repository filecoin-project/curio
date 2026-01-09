package pdp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/snadrus/must"
	"github.com/stretchr/testify/require"
)

// mockPullStore implements PullStore for testing
type mockPullStore struct {
	// Configuration for mock behavior
	existingPull     *PullRecord
	pieceStatuses    map[string]*PieceStatus    // keyed by v1 CID string (from parked_pieces)
	pullItemStatuses map[string]*PullItemStatus // keyed by v1 CID string (from pdp_piece_pull_items)
	pullPieces       []PullPiece                // v1 CID + raw size + source_url
	createError      error

	// Failure tracking configuration
	exhaustedTasks map[int64]string // taskID -> error reason (for CheckTaskExhaustedRetries)

	// Tracking calls
	createFetchCalled         bool
	createdFetch              *PullRecord
	createdPieces             []PullPiece // v1 CID + raw size + source_url
	getStatusesCalled         bool
	getPullItemStatusesCalled bool
	getFetchPiecesCalled      bool
	lastCreatedID             int64
	markedFailed              map[string]string // v1 CID -> reason
}

func (m *mockPullStore) GetPullByKey(ctx context.Context, service string, hash []byte, dataSetId uint64, recordKeeper string) (*PullRecord, error) {
	return m.existingPull, nil
}

func (m *mockPullStore) CreatePullWithPieces(ctx context.Context, fetch *PullRecord, pieces []PullPiece) (int64, error) {
	m.createFetchCalled = true
	m.createdFetch = fetch
	m.createdPieces = pieces
	if m.createError != nil {
		return 0, m.createError
	}
	m.lastCreatedID++
	return m.lastCreatedID, nil
}

func (m *mockPullStore) GetPieceStatuses(ctx context.Context, pieceCids []cid.Cid) (map[string]*PieceStatus, error) {
	m.getStatusesCalled = true
	if m.pieceStatuses == nil {
		return make(map[string]*PieceStatus), nil
	}
	return m.pieceStatuses, nil
}

func (m *mockPullStore) GetPullItemStatuses(ctx context.Context, fetchID int64, pieceCids []cid.Cid) (map[string]*PullItemStatus, error) {
	m.getPullItemStatusesCalled = true
	if m.pullItemStatuses == nil {
		return make(map[string]*PullItemStatus), nil
	}
	return m.pullItemStatuses, nil
}

func (m *mockPullStore) GetPullPieces(ctx context.Context, fetchID int64) ([]PullPiece, error) {
	m.getFetchPiecesCalled = true
	if m.pullPieces != nil {
		return m.pullPieces, nil
	}
	// Return the pieces that were created
	return m.createdPieces, nil
}

func (m *mockPullStore) MarkPieceFailed(ctx context.Context, fetchID int64, pieceCid string, reason string) error {
	if m.markedFailed == nil {
		m.markedFailed = make(map[string]string)
	}
	m.markedFailed[pieceCid] = reason
	return nil
}

func (m *mockPullStore) CheckTaskExhaustedRetries(ctx context.Context, taskID int64) (bool, string, error) {
	if m.exhaustedTasks != nil {
		if reason, ok := m.exhaustedTasks[taskID]; ok {
			return true, reason, nil
		}
	}
	return false, "", nil
}

// mockValidator implements AddPiecesValidator for testing
type mockValidator struct {
	shouldPass bool
	err        error
}

func (m *mockValidator) ValidateAddPieces(ctx context.Context, params *AddPiecesValidatorParams) error {
	if m.shouldPass {
		return nil
	}
	if m.err != nil {
		return m.err
	}
	return errors.New("validation failed")
}

// Valid test PieceCIDv2s
const (
	testCid1 = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"
	testCid2 = "bafkzcibf6x7poaqtihg2pifeyzwfy3ndaumj3ds6c5ddiqewo2dzfzr7pqlery5dwyba"
	testCid3 = "bafkzcibf6x7poaqtzqrdhkbnlu53ftoiiom6rcu7fmwbaa423d5kihygqqhi7m5ypyfq"
)

// Test dataSetId for "add to existing dataset" case (avoids recordKeeper requirement)
var testDataSetId = uint64(1)

// testParsePieceCidV2 is a test helper that parses a v2 CID and returns PullPiece (v1 + raw size)
func testParsePieceCidV2(t *testing.T, cidV2Str string) PullPiece {
	t.Helper()
	info, err := ParsePieceCidV2(cidV2Str)
	require.NoError(t, err)
	return PullPiece{CidV1: info.CidV1, RawSize: info.RawSize}
}

func TestHandlePull_MethodNotAllowed(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true})

	req := httptest.NewRequest(http.MethodGet, "/pdp/piece/pull", nil)
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHandlePull_InvalidJSON(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true})

	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid request body")
}

func TestHandlePull_MissingExtraData(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true})

	body := PullRequest{
		ExtraData: "",
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "extraData is required")
}

func TestHandlePull_NoPieces(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true})

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces:    []PullPieceRequest{},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "at least one piece")
}

func TestHandlePull_InvalidSourceURL(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true})

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "http://example.com/piece/" + testCid1}, // HTTP not HTTPS
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "HTTPS")
}

func TestHandlePull_ValidatorFails(t *testing.T) {
	store := &mockPullStore{}
	validator := &mockValidator{shouldPass: false, err: errors.New("contract validation failed")}
	handler := NewPullHandler(&NullAuth{}, store, validator)

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "extraData validation failed")
	// Store should not be called when validation fails
	require.False(t, store.createFetchCalled)
}

func TestHandlePull_NewRequest_Success(t *testing.T) {
	store := &mockPullStore{}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator)

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
			{PieceCid: testCid2, SourceURL: "https://example.com/piece/" + testCid2},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// Verify store was called
	require.True(t, store.createFetchCalled)
	require.NotNil(t, store.createdFetch)
	require.Equal(t, "public", store.createdFetch.Service) // NullAuth returns "public"
	require.Len(t, store.createdPieces, 2)

	// Verify pieces include source URLs
	require.NotEmpty(t, store.createdPieces[0].SourceURL)
	require.NotEmpty(t, store.createdPieces[1].SourceURL)

	// Verify response returns v2 CIDs
	var resp PullResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, PullStatusPending, resp.Status) // New pieces should be pending
	require.Len(t, resp.Pieces, 2)
	// Response should contain original v2 CIDs
	cidSet := map[string]bool{}
	for _, p := range resp.Pieces {
		cidSet[p.PieceCid] = true
	}
	require.True(t, cidSet[testCid1])
	require.True(t, cidSet[testCid2])
}

func TestHandlePull_CreateNew_MissingRecordKeeper(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true})

	// dataSetId omitted (nil) requires recordKeeper
	body := PullRequest{
		ExtraData: "0x1234",
		// DataSetId: nil (omitted = create new)
		// RecordKeeper: nil (missing - should fail)
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "recordKeeper is required")
}

// Test recordKeeper address (any valid address works for non-public services)
var testRecordKeeper = "0x5615dEB798BB3E4dFa0139dFa1b3D433Cc23b72f"

// privateServiceAuth returns a non-public service name to bypass AllowedRecordKeepers check
type privateServiceAuth struct{}

func (a *privateServiceAuth) AuthService(r *http.Request) (string, error) {
	return "test-service", nil
}

func TestHandlePull_CreateNew_Success(t *testing.T) {
	store := &mockPullStore{}
	validator := &mockValidator{shouldPass: true}
	// Use non-public service auth to test create-new flow without AllowedRecordKeepers restriction
	handler := NewPullHandler(&privateServiceAuth{}, store, validator)

	// dataSetId omitted (nil) with recordKeeper = create new dataset
	body := PullRequest{
		ExtraData:    "0x1234",
		RecordKeeper: &testRecordKeeper,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// Verify store was called with correct values
	require.True(t, store.createFetchCalled)
	require.NotNil(t, store.createdFetch)
	require.Equal(t, uint64(0), store.createdFetch.DataSetId) // create-new uses 0
	require.Equal(t, testRecordKeeper, store.createdFetch.RecordKeeper)
}

func TestHandlePull_Idempotent(t *testing.T) {
	// Get v1 CID info for test setup
	piece1 := testParsePieceCidV2(t, testCid1)
	v1Str1 := piece1.CidV1.String()

	store := &mockPullStore{
		existingPull: &PullRecord{
			ID:            123,
			Service:       "public",
			ExtraDataHash: []byte("hash"),
		},
		pullPieces: []PullPiece{piece1},
		pieceStatuses: map[string]*PieceStatus{
			v1Str1: {PieceCid: v1Str1, Complete: true},
		},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator)

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// Should NOT create new fetch (idempotent - already exists)
	require.False(t, store.createFetchCalled)

	// Should return status of existing fetch with v2 CID in response
	var resp PullResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, PullStatusComplete, resp.Status)
	require.Len(t, resp.Pieces, 1)
	require.Equal(t, PullStatusComplete, resp.Pieces[0].Status)
	require.Equal(t, testCid1, resp.Pieces[0].PieceCid) // Response should have v2 CID
}

func TestHandlePull_MixedStatuses(t *testing.T) {
	// Get v1 CID info for test setup
	piece1 := testParsePieceCidV2(t, testCid1)
	piece2 := testParsePieceCidV2(t, testCid2)
	piece3 := testParsePieceCidV2(t, testCid3)
	v1Str1 := piece1.CidV1.String()
	v1Str2 := piece2.CidV1.String()

	taskID := int64(123)
	store := &mockPullStore{
		pullPieces: []PullPiece{piece1, piece2, piece3},
		pieceStatuses: map[string]*PieceStatus{
			v1Str1: {PieceCid: v1Str1, Complete: true},
			v1Str2: {PieceCid: v1Str2, Complete: false, TaskID: &taskID, TaskExists: true}, // inProgress
			// piece3 not in map - should be pending
		},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator)

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
			{PieceCid: testCid2, SourceURL: "https://example.com/piece/" + testCid2},
			{PieceCid: testCid3, SourceURL: "https://example.com/piece/" + testCid3},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp PullResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, PullStatusInProgress, resp.Status) // Has inProgress piece
	require.Len(t, resp.Pieces, 3)

	// Find each piece status - response should have v2 CIDs
	statusMap := make(map[string]PullStatus)
	for _, p := range resp.Pieces {
		statusMap[p.PieceCid] = p.Status
	}
	require.Equal(t, PullStatusComplete, statusMap[testCid1])
	require.Equal(t, PullStatusInProgress, statusMap[testCid2])
	require.Equal(t, PullStatusPending, statusMap[testCid3])
}

func TestHandlePull_CreateError(t *testing.T) {
	store := &mockPullStore{
		createError: errors.New("database error"),
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator)

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestPadPieceSize(t *testing.T) {
	// Test vectors from go-fil-commp-hashhash/testdata/zero.txt
	// These are authoritative values: PayloadSize -> PieceSize
	// FR32 padding: ceil(raw * 127/128) rounded to next power of 2
	tests := []struct {
		name     string
		rawSize  int64
		expected int64
	}{
		// Edge cases
		{name: "zero", rawSize: 0, expected: 0},

		// From commp testdata/zero.txt - boundary tests
		{name: "127 -> 128 (exactly 1 FR32 block)", rawSize: 127, expected: 128},
		{name: "254 -> 256 (exactly 2 FR32 blocks)", rawSize: 254, expected: 256},
		{name: "255 -> 512 (just over 2 blocks)", rawSize: 255, expected: 512},
		{name: "508 -> 512 (exactly 4 FR32 blocks)", rawSize: 508, expected: 512},
		{name: "509 -> 1024 (just over 4 blocks)", rawSize: 509, expected: 1024},
		{name: "1016 -> 1024 (exactly 8 FR32 blocks)", rawSize: 1016, expected: 1024},
		{name: "1017 -> 2048 (just over 8 blocks)", rawSize: 1017, expected: 2048},
		{name: "1024 -> 2048", rawSize: 1024, expected: 2048},

		// Additional cases from testdata
		{name: "96 -> 128", rawSize: 96, expected: 128},
		{name: "192 -> 256", rawSize: 192, expected: 256},
		{name: "384 -> 512", rawSize: 384, expected: 512},
		{name: "768 -> 1024", rawSize: 768, expected: 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PadPieceSize(tt.rawSize)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestHandlePull_RetryingStatus(t *testing.T) {
	// Piece with task that has retries > 0 should show "retrying" status
	piece1 := testParsePieceCidV2(t, testCid1)
	v1Str1 := piece1.CidV1.String()
	taskID := int64(999)

	store := &mockPullStore{
		existingPull: &PullRecord{
			ID:            123,
			Service:       "public",
			ExtraDataHash: []byte("hash"),
		},
		pullPieces: []PullPiece{piece1},
		pieceStatuses: map[string]*PieceStatus{
			v1Str1: {
				PieceCid:   v1Str1,
				Complete:   false,
				TaskID:     &taskID,
				TaskExists: true,
				Retries:    2, // Has been retried twice
			},
		},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator)

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp PullResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, PullStatusRetrying, resp.Status)
	require.Len(t, resp.Pieces, 1)
	require.Equal(t, PullStatusRetrying, resp.Pieces[0].Status)
}

func TestHandlePull_FailedFromFetchItems(t *testing.T) {
	// Piece already marked as failed in fetch_items should show "failed" status
	piece1 := testParsePieceCidV2(t, testCid1)

	store := &mockPullStore{
		existingPull: &PullRecord{
			ID:            123,
			Service:       "public",
			ExtraDataHash: []byte("hash"),
		},
		pullPieces: []PullPiece{
			{
				CidV1:      piece1.CidV1,
				RawSize:    piece1.RawSize,
				Failed:     true,
				FailReason: "CommP mismatch: expected X, got Y",
			},
		},
		// No piece status needed - Failed flag in PullPiece takes priority
		pieceStatuses: map[string]*PieceStatus{},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator)

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp PullResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, PullStatusFailed, resp.Status)
	require.Len(t, resp.Pieces, 1)
	require.Equal(t, PullStatusFailed, resp.Pieces[0].Status)
}

func TestHandlePull_OrphanedTaskExhausted(t *testing.T) {
	// Piece with orphaned task (task deleted, exhausted retries) should show "failed"
	// and trigger MarkPieceFailed call
	piece1 := testParsePieceCidV2(t, testCid1)
	v1Str1 := piece1.CidV1.String()
	taskID := int64(888)

	store := &mockPullStore{
		existingPull: &PullRecord{
			ID:            123,
			Service:       "public",
			ExtraDataHash: []byte("hash"),
		},
		pullPieces: []PullPiece{piece1},
		pieceStatuses: map[string]*PieceStatus{
			v1Str1: {
				PieceCid:   v1Str1,
				Complete:   false,
				TaskID:     &taskID,
				TaskExists: false, // Task was deleted
				Retries:    0,
			},
		},
		exhaustedTasks: map[int64]string{
			taskID: "size mismatch: expected 1000, got 500", // Task failed permanently
		},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator)

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp PullResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, PullStatusFailed, resp.Status)
	require.Len(t, resp.Pieces, 1)
	require.Equal(t, PullStatusFailed, resp.Pieces[0].Status)

	// Verify MarkPieceFailed was called with the error reason (prefixed with StorePiece:)
	require.NotNil(t, store.markedFailed)
	require.Equal(t, "StorePiece: size mismatch: expected 1000, got 500", store.markedFailed[v1Str1])
}

func TestHandlePull_OrphanedTaskNotExhausted(t *testing.T) {
	// Piece with orphaned task but no exhaustion record - should show "failed"
	// because the piece is stuck (poller won't pick it up with task_id still set)
	piece1 := testParsePieceCidV2(t, testCid1)
	v1Str1 := piece1.CidV1.String()
	taskID := int64(777)

	store := &mockPullStore{
		existingPull: &PullRecord{
			ID:            123,
			Service:       "public",
			ExtraDataHash: []byte("hash"),
		},
		pullPieces: []PullPiece{piece1},
		pieceStatuses: map[string]*PieceStatus{
			v1Str1: {
				PieceCid:   v1Str1,
				Complete:   false,
				TaskID:     &taskID,
				TaskExists: false, // Task was deleted
				Retries:    0,
			},
		},
		// exhaustedTasks is nil - no history found (purged or never ran)
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator)

	body := PullRequest{
		ExtraData: "0x1234",
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp PullResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, PullStatusFailed, resp.Status)
	require.Len(t, resp.Pieces, 1)
	require.Equal(t, PullStatusFailed, resp.Pieces[0].Status)

	// Verify MarkPieceFailed was called with orphan message (for StorePiece task)
	require.NotNil(t, store.markedFailed)
	require.Equal(t, "StorePiece task orphaned without failure record", store.markedFailed[v1Str1])
}

func TestComputeOverallStatus_Priority(t *testing.T) {
	// Test that status priority is: failed > retrying > inProgress > pending > complete
	tests := []struct {
		name           string
		pieceStatuses  []PullStatus
		expectedStatus PullStatus
	}{
		{
			name:           "all complete",
			pieceStatuses:  []PullStatus{PullStatusComplete, PullStatusComplete},
			expectedStatus: PullStatusComplete,
		},
		{
			name:           "one pending makes overall pending",
			pieceStatuses:  []PullStatus{PullStatusComplete, PullStatusPending},
			expectedStatus: PullStatusPending,
		},
		{
			name:           "inProgress overrides pending",
			pieceStatuses:  []PullStatus{PullStatusPending, PullStatusInProgress, PullStatusComplete},
			expectedStatus: PullStatusInProgress,
		},
		{
			name:           "retrying overrides inProgress",
			pieceStatuses:  []PullStatus{PullStatusInProgress, PullStatusRetrying, PullStatusComplete},
			expectedStatus: PullStatusRetrying,
		},
		{
			name:           "failed overrides all",
			pieceStatuses:  []PullStatus{PullStatusComplete, PullStatusInProgress, PullStatusRetrying, PullStatusFailed},
			expectedStatus: PullStatusFailed,
		},
		{
			name:           "empty pieces is pending",
			pieceStatuses:  []PullStatus{},
			expectedStatus: PullStatusPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &PullResponse{
				Pieces: make([]PullPieceStatus, len(tt.pieceStatuses)),
			}
			for i, s := range tt.pieceStatuses {
				resp.Pieces[i] = PullPieceStatus{PieceCid: "test", Status: s}
			}
			resp.ComputeOverallStatus()
			require.Equal(t, tt.expectedStatus, resp.Status)
		})
	}
}
