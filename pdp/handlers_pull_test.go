package pdp

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/snadrus/must"
	"github.com/stretchr/testify/require"
)

// mockPullStore implements PullStore for testing
type mockPullStore struct {
	// Configuration for mock behavior
	existingPull *PullRecord
	pullPieces   []PullPieceStatus
	createError  error
	backpressure *PullBackpressure

	// Tracking calls
	createPullCalled    bool
	createdPull         *PullRecord
	createdPieces       []PullPiece // v1 CID + raw size + source_url
	getPullPiecesCalled bool
	lastCreatedID       int64
}

func (m *mockPullStore) GetPullByKey(ctx context.Context, service string, hash []byte, dataSetId uint64, recordKeeper string) (*PullRecord, error) {
	return m.existingPull, nil
}

func (m *mockPullStore) CreatePullWithPieces(ctx context.Context, pull *PullRecord, pieces []PullPiece) (int64, *PullBackpressure, error) {
	m.createPullCalled = true
	m.createdPull = pull
	m.createdPieces = pieces
	if m.createError != nil {
		return 0, nil, m.createError
	}
	m.lastCreatedID++
	return m.lastCreatedID, m.backpressure, nil
}

func (m *mockPullStore) GetPullStatus(ctx context.Context, pullID int64) ([]PullPieceStatus, error) {
	m.getPullPiecesCalled = true
	if m.pullPieces != nil {
		return m.pullPieces, nil
	}
	pieces := make([]PullPieceStatus, len(m.createdPieces))
	for i, piece := range m.createdPieces {
		info, err := PieceCidV2FromV1(piece.CidV1, piece.RawSize)
		if err != nil {
			return nil, err
		}
		pieces[i] = PullPieceStatus{PieceCid: info.CidV2.String(), Status: PullStatusPending}
	}
	return pieces, nil
}

// mockValidator implements AddPiecesValidator for testing
type mockValidator struct {
	shouldPass bool
	err        error
	payer      common.Address
	payerErr   error
	payerCalls []uint64
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

func (m *mockValidator) GetDataSetPayer(ctx context.Context, dataSetId uint64) (common.Address, error) {
	m.payerCalls = append(m.payerCalls, dataSetId)
	if m.payerErr != nil {
		return common.Address{}, m.payerErr
	}
	if m.payer != (common.Address{}) {
		return m.payer, nil
	}
	return common.HexToAddress("0x1111111111111111111111111111111111111111"), nil
}

// Valid test PieceCIDv2s
const (
	testCid1 = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"
	testCid2 = "bafkzcibf6x7poaqtihg2pifeyzwfy3ndaumj3ds6c5ddiqewo2dzfzr7pqlery5dwyba"
	testCid3 = "bafkzcibf6x7poaqtzqrdhkbnlu53ftoiiom6rcu7fmwbaa423d5kihygqqhi7m5ypyfq"
)

// Test dataSetId for "add to existing dataset" case (avoids recordKeeper requirement)
var testDataSetId = uint64(1)

func testExtraData(t *testing.T) string {
	t.Helper()

	extraData, err := makeTestExtraData(common.HexToAddress("0x1111111111111111111111111111111111111111"), 1)
	require.NoError(t, err)
	return extraData
}

func testAddPiecesOnlyExtraData(t *testing.T) string {
	t.Helper()

	return "0x" + hex.EncodeToString([]byte("add-pieces-only-extra-data"))
}

func makeTestExtraData(payer common.Address, nonce int64) (string, error) {
	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return "", err
	}
	addressType, err := abi.NewType("address", "", nil)
	if err != nil {
		return "", err
	}
	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return "", err
	}
	stringArrayType, err := abi.NewType("string[]", "", nil)
	if err != nil {
		return "", err
	}

	createArgs := abi.Arguments{
		{Type: addressType},
		{Type: uint256Type},
		{Type: stringArrayType},
		{Type: stringArrayType},
		{Type: bytesType},
	}
	createPayload, err := createArgs.Pack(
		payer,
		big.NewInt(nonce),
		[]string{},
		[]string{},
		[]byte("signature-"+strconv.FormatInt(nonce, 10)),
	)
	if err != nil {
		return "", err
	}

	outerArgs := abi.Arguments{{Type: bytesType}, {Type: bytesType}}
	extraData, err := outerArgs.Pack(createPayload, []byte{})
	if err != nil {
		return "", err
	}

	return "0x" + hex.EncodeToString(extraData), nil
}

func testPullPieceStatus(cidV2Str string, status PullStatus) PullPieceStatus {
	return PullPieceStatus{PieceCid: cidV2Str, Status: status}
}

func TestHandlePull_MethodNotAllowed(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true}, nil)

	req := httptest.NewRequest(http.MethodGet, "/pdp/piece/pull", nil)
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHandlePull_InvalidJSON(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true}, nil)

	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewBufferString("not json"))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid request body")
}

func TestHandlePull_MissingExtraData(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true}, nil)

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
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true}, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true}, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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
	// Pull should not be created when validation fails
	require.False(t, store.createPullCalled)
}

func TestHandlePull_NewRequest_Success(t *testing.T) {
	store := &mockPullStore{}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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
	require.True(t, store.createPullCalled)
	require.NotNil(t, store.createdPull)
	require.Equal(t, "public", store.createdPull.Service) // NullAuth returns "public"
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

func TestHandlePull_ExistingDataSetUsesFWSSPayer(t *testing.T) {
	store := &mockPullStore{}
	payer := common.HexToAddress("0x2222222222222222222222222222222222222222")
	validator := &mockValidator{shouldPass: true, payer: payer}
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testAddPiecesOnlyExtraData(t),
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
	require.True(t, store.createPullCalled)
	require.NotNil(t, store.createdPull)
	require.Equal(t, payer.Hex(), store.createdPull.ClientAddress)
	require.Equal(t, []uint64{testDataSetId}, validator.payerCalls)
}

func TestHandlePull_CreateNew_MissingRecordKeeper(t *testing.T) {
	handler := NewPullHandler(&NullAuth{}, &mockPullStore{}, &mockValidator{shouldPass: true}, nil)

	// dataSetId omitted (nil) requires recordKeeper
	body := PullRequest{
		ExtraData: testExtraData(t),
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
	handler := NewPullHandler(&privateServiceAuth{}, store, validator, nil)

	// dataSetId omitted (nil) with recordKeeper = create new dataset
	body := PullRequest{
		ExtraData:    testExtraData(t),
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
	require.True(t, store.createPullCalled)
	require.NotNil(t, store.createdPull)
	require.Equal(t, uint64(0), store.createdPull.DataSetId) // create-new uses 0
	require.Equal(t, testRecordKeeper, store.createdPull.RecordKeeper)
}

func TestHandlePull_Idempotent(t *testing.T) {
	store := &mockPullStore{
		existingPull: &PullRecord{
			ID:            123,
			Service:       "public",
			ExtraDataHash: []byte("hash"),
		},
		pullPieces: []PullPieceStatus{testPullPieceStatus(testCid1, PullStatusComplete)},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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

	// Should NOT create new pull (idempotent - already exists)
	require.False(t, store.createPullCalled)

	// Should return status of existing pull with v2 CID in response
	var resp PullResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Equal(t, PullStatusComplete, resp.Status)
	require.Len(t, resp.Pieces, 1)
	require.Equal(t, PullStatusComplete, resp.Pieces[0].Status)
	require.Equal(t, testCid1, resp.Pieces[0].PieceCid) // Response should have v2 CID
}

func TestHandlePull_MixedStatuses(t *testing.T) {
	store := &mockPullStore{
		pullPieces: []PullPieceStatus{
			testPullPieceStatus(testCid1, PullStatusComplete),
			testPullPieceStatus(testCid2, PullStatusInProgress),
			testPullPieceStatus(testCid3, PullStatusPending),
		},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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

func TestHandlePull_Backpressure(t *testing.T) {
	store := &mockPullStore{
		backpressure: &PullBackpressure{RetryAfter: 2 * time.Minute},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
		DataSetId: &testDataSetId,
		Pieces: []PullPieceRequest{
			{PieceCid: testCid1, SourceURL: "https://example.com/piece/" + testCid1},
		},
	}
	bodyBytes := must.One(json.Marshal(body))
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	handler.HandlePull(rec, req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "120", rec.Header().Get("Retry-After"))
	require.True(t, store.createPullCalled)
	require.False(t, store.getPullPiecesCalled)
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
	store := &mockPullStore{
		existingPull: &PullRecord{
			ID:            123,
			Service:       "public",
			ExtraDataHash: []byte("hash"),
		},
		pullPieces: []PullPieceStatus{testPullPieceStatus(testCid1, PullStatusRetrying)},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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

func TestHandlePull_FailedFromPullItems(t *testing.T) {
	// Piece already marked as failed in pull_items should show "failed" status
	store := &mockPullStore{
		existingPull: &PullRecord{
			ID:            123,
			Service:       "public",
			ExtraDataHash: []byte("hash"),
		},
		pullPieces: []PullPieceStatus{testPullPieceStatus(testCid1, PullStatusFailed)},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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

func TestHandlePull_OrphanedTaskWithoutTerminalStateIsPending(t *testing.T) {
	// Terminal state is explicit in pdp_piece_pull_items. A missing harmony task
	// is not inferred as failure by the status endpoint.
	store := &mockPullStore{
		existingPull: &PullRecord{
			ID:            123,
			Service:       "public",
			ExtraDataHash: []byte("hash"),
		},
		pullPieces: []PullPieceStatus{testPullPieceStatus(testCid1, PullStatusPending)},
	}
	validator := &mockValidator{shouldPass: true}
	handler := NewPullHandler(&NullAuth{}, store, validator, nil)

	body := PullRequest{
		ExtraData: testExtraData(t),
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
	require.Equal(t, PullStatusPending, resp.Status)
	require.Len(t, resp.Pieces, 1)
	require.Equal(t, PullStatusPending, resp.Pieces[0].Status)
}

func TestComputeOverallStatus_Priority(t *testing.T) {
	// Failed pieces are terminal per-piece results. The batch only becomes
	// terminal after every piece is complete or failed.
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
			name:           "failed does not override active work",
			pieceStatuses:  []PullStatus{PullStatusComplete, PullStatusInProgress, PullStatusRetrying, PullStatusFailed},
			expectedStatus: PullStatusRetrying,
		},
		{
			name:           "partial failure with completed pieces is complete",
			pieceStatuses:  []PullStatus{PullStatusComplete, PullStatusFailed},
			expectedStatus: PullStatusComplete,
		},
		{
			name:           "all failed makes batch failed",
			pieceStatuses:  []PullStatus{PullStatusFailed, PullStatusFailed},
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
