package pdp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	pullStressGlobalLimit     = pullGlobalPendingLimit
	pullStressPerClientLimit  = pullPerClientPendingLimit
	pullStressSoloClientLimit = pullGlobalPendingLimit * pullSoloClientPendingPercentage / 100
)

type pdpPullStressStats struct {
	mu                      sync.Mutex
	accepted                int
	rejected                int
	completedGroups         int
	acceptedAfterCompletion int
	maxGlobalPending        int
	maxClientPending        int
	violations              []string
}

func TestHandlePull_BackpressureStress(t *testing.T) {
	const (
		clientCount       = 20
		attemptsPerClient = 100
		piecePoolSize     = 512
		maxBatchSize      = 8
	)

	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	rawSizes := []uint64{127, 254, 508, 1016, 2032, 4096, 16384, 65536}
	piecePool := make([]string, piecePoolSize)
	for i := range piecePool {
		piecePool[i] = generatedPieceCIDV2(t, i+1, rawSizes[i%len(rawSizes)])
	}

	stats := &pdpPullStressStats{}
	handler := NewPullHandler(&NullAuth{}, NewDBPullStore(db), &mockValidator{shouldPass: true})
	dataSetID := testDataSetId

	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	errCh := make(chan error, clientCount*attemptsPerClient)
	var wg sync.WaitGroup

	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-testCtx.Done():
				return
			case <-ticker.C:
				if err := stats.recordDBCounts(testCtx, db, "monitor"); err != nil {
					if testCtx.Err() != nil {
						return
					}
					errCh <- err
					return
				}
			}
		}
	}()

	completeDone := make(chan struct{})
	go func() {
		defer close(completeDone)
		rng := rand.New(rand.NewSource(9001))
		for {
			select {
			case <-testCtx.Done():
				return
			default:
			}

			time.Sleep(time.Duration(2+rng.Intn(5)) * time.Millisecond)
			completed, err := completeRandomPullPieceGroup(testCtx, db, rng)
			if err != nil {
				if testCtx.Err() != nil {
					return
				}
				errCh <- err
				return
			}
			if completed {
				stats.recordCompletion()
				if err := stats.recordDBCounts(testCtx, db, "complete"); err != nil {
					if testCtx.Err() != nil {
						return
					}
					errCh <- err
					return
				}
			}
		}
	}()

	for clientIdx := 0; clientIdx < clientCount; clientIdx++ {
		wg.Add(1)
		go func(clientIdx int) {
			defer wg.Done()

			rng := rand.New(rand.NewSource(int64(1000 + clientIdx)))
			payer := common.BigToAddress(big.NewInt(int64(clientIdx + 1)))
			for attempt := 0; attempt < attemptsPerClient; attempt++ {
				select {
				case <-testCtx.Done():
					return
				default:
				}

				nonce := int64((clientIdx+1)*1_000_000 + attempt)
				extraData, err := makeTestExtraData(payer, nonce)
				if err != nil {
					errCh <- err
					return
				}

				pieces := randomPullPieces(rng, piecePool, maxBatchSize)
				bodyBytes, err := json.Marshal(PullRequest{
					ExtraData: extraData,
					DataSetId: &dataSetID,
					Pieces:    pieces,
				})
				if err != nil {
					errCh <- err
					return
				}

				req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
				rec := httptest.NewRecorder()
				handler.HandlePull(rec, req)
				switch rec.Code {
				case http.StatusOK:
					stats.recordAccepted()
				case http.StatusTooManyRequests:
					stats.recordRejected()
				default:
					errCh <- fmt.Errorf("unexpected status %d: %s", rec.Code, rec.Body.String())
					return
				}

				if err := stats.recordDBCounts(testCtx, db, "request"); err != nil {
					errCh <- err
					return
				}
				time.Sleep(time.Duration(rng.Intn(3)) * time.Millisecond)
			}
		}(clientIdx)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-testCtx.Done():
		require.NoError(t, testCtx.Err())
	}
	cancel()
	<-monitorDone
	<-completeDone
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
	require.NoError(t, stats.recordDBCounts(ctx, db, "final"))

	snapshot := stats.snapshot()
	require.Empty(t, snapshot.violations)
	require.Greater(t, snapshot.accepted, 0)
	require.Greater(t, snapshot.rejected, 0)
	require.Greater(t, snapshot.completedGroups, 0)
	require.Greater(t, snapshot.acceptedAfterCompletion, 0)
	require.LessOrEqual(t, snapshot.maxGlobalPending, pullStressGlobalLimit)
	require.LessOrEqual(t, snapshot.maxClientPending, pullStressSoloClientLimit)
	require.GreaterOrEqual(t, snapshot.maxGlobalPending, pullStressGlobalLimit/2)
	require.GreaterOrEqual(t, snapshot.maxClientPending, pullStressPerClientLimit/2)
}

func TestHandlePull_BackpressureSoloClientCanUseNinetyPercent(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	handler := NewPullHandler(&NullAuth{}, NewDBPullStore(db), &mockValidator{shouldPass: true})
	dataSetID := testDataSetId
	rawSizes := []uint64{127, 254, 508, 1016}
	piecePool := make([]string, pullStressSoloClientLimit+1)
	for i := range piecePool {
		piecePool[i] = generatedPieceCIDV2(t, i+10_000, rawSizes[i%len(rawSizes)])
	}

	payer := common.HexToAddress("0x1111111111111111111111111111111111111111")
	nonce := submitPullStressRequestChunked(t, handler, payer, 1, dataSetID, piecePool[:pullStressSoloClientLimit])

	rec := submitPullStressRequest(t, handler, payer, nonce, dataSetID, pullPiecesFromCIDs(piecePool[pullStressSoloClientLimit:]))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "60", rec.Header().Get("Retry-After"))
}

func TestHandlePull_BackpressureClientCanBorrowUnusedSlots(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	handler := NewPullHandler(&NullAuth{}, NewDBPullStore(db), &mockValidator{shouldPass: true})
	dataSetID := testDataSetId
	rawSizes := []uint64{127, 254, 508, 1016}
	piecePool := make([]string, 31)
	for i := range piecePool {
		piecePool[i] = generatedPieceCIDV2(t, i+20_000, rawSizes[i%len(rawSizes)])
	}

	firstClient := common.HexToAddress("0x1111111111111111111111111111111111111111")
	secondClient := common.HexToAddress("0x2222222222222222222222222222222222222222")
	rec := submitPullStressRequest(t, handler, firstClient, 1, dataSetID, pullPiecesFromCIDs(piecePool[:20]))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = submitPullStressRequest(t, handler, secondClient, 2, dataSetID, pullPiecesFromCIDs(piecePool[20:]))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestHandlePull_BackpressureClientLimitAppliesNearGlobalReserve(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	handler := NewPullHandler(&NullAuth{}, NewDBPullStore(db), &mockValidator{shouldPass: true})
	dataSetID := testDataSetId
	rawSizes := []uint64{127, 254, 508, 1016}
	piecePool := make([]string, pullStressSoloClientLimit+1)
	for i := range piecePool {
		piecePool[i] = generatedPieceCIDV2(t, i+30_000, rawSizes[i%len(rawSizes)])
	}

	firstClient := common.HexToAddress("0x1111111111111111111111111111111111111111")
	secondClient := common.HexToAddress("0x2222222222222222222222222222222222222222")
	nonce := submitPullStressRequestChunked(t, handler, firstClient, 1, dataSetID, piecePool[:pullStressSoloClientLimit-pullStressPerClientLimit])

	rec := submitPullStressRequest(t, handler, secondClient, nonce, dataSetID, pullPiecesFromCIDs(piecePool[pullStressSoloClientLimit-pullStressPerClientLimit:]))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "60", rec.Header().Get("Retry-After"))
}

func generatedPieceCIDV2(t *testing.T, seed int, rawSize uint64) string {
	t.Helper()

	var commP [32]byte
	binary.BigEndian.PutUint64(commP[:8], uint64(seed))
	binary.BigEndian.PutUint64(commP[8:16], rawSize)
	pieceCID, err := commcid.DataCommitmentToPieceCidv2(commP[:], rawSize)
	require.NoError(t, err)
	return pieceCID.String()
}

func randomPullPieces(rng *rand.Rand, piecePool []string, maxBatchSize int) []PullPieceRequest {
	batchSize := 1 + rng.Intn(maxBatchSize)
	pieces := make([]PullPieceRequest, 0, batchSize)
	seen := map[string]struct{}{}
	for len(pieces) < batchSize {
		pieceCID := piecePool[rng.Intn(len(piecePool))]
		if _, ok := seen[pieceCID]; ok {
			continue
		}
		seen[pieceCID] = struct{}{}
		pieces = append(pieces, PullPieceRequest{
			PieceCid:  pieceCID,
			SourceURL: "https://source.example/piece/" + pieceCID,
		})
	}
	return pieces
}

func completeRandomPullPieceGroup(ctx context.Context, db *harmonydb.DB, rng *rand.Rand) (bool, error) {
	var groups []struct {
		PieceCID     string `db:"piece_cid"`
		PieceRawSize uint64 `db:"piece_raw_size"`
	}
	err := db.Select(ctx, &groups, `
		SELECT piece_cid, piece_raw_size
		FROM pdp_piece_pull_items
		WHERE complete = FALSE
			AND failed = FALSE
		GROUP BY piece_cid, piece_raw_size
	`)
	if err != nil {
		return false, err
	}
	if len(groups) == 0 {
		return false, nil
	}

	group := groups[rng.Intn(len(groups))]
	_, err = db.Exec(ctx, `
		UPDATE pdp_piece_pull_items
		SET complete = TRUE
		WHERE piece_cid = $1
			AND piece_raw_size = $2
			AND complete = FALSE
			AND failed = FALSE
	`, group.PieceCID, group.PieceRawSize)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *pdpPullStressStats) recordAccepted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accepted++
	if s.completedGroups > 0 {
		s.acceptedAfterCompletion++
	}
}

func (s *pdpPullStressStats) recordRejected() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejected++
}

func (s *pdpPullStressStats) recordCompletion() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completedGroups++
}

func (s *pdpPullStressStats) recordDBCounts(ctx context.Context, db *harmonydb.DB, event string) error {
	globalPending, maxClientPending, err := pullStressActiveCounts(ctx, db)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if globalPending > s.maxGlobalPending {
		s.maxGlobalPending = globalPending
	}
	if maxClientPending > s.maxClientPending {
		s.maxClientPending = maxClientPending
	}
	if globalPending > pullStressGlobalLimit {
		s.violations = append(s.violations, fmt.Sprintf("%s: global pending %d > %d", event, globalPending, pullStressGlobalLimit))
	}
	if maxClientPending > pullStressSoloClientLimit {
		s.violations = append(s.violations, fmt.Sprintf("%s: per-client pending %d > %d", event, maxClientPending, pullStressSoloClientLimit))
	}
	return nil
}

func pullPiecesFromCIDs(pieceCIDs []string) []PullPieceRequest {
	pieces := make([]PullPieceRequest, len(pieceCIDs))
	for i, pieceCID := range pieceCIDs {
		pieces[i] = PullPieceRequest{
			PieceCid:  pieceCID,
			SourceURL: "https://source.example/piece/" + pieceCID,
		}
	}
	return pieces
}

func submitPullStressRequest(t *testing.T, handler *PullHandler, payer common.Address, nonce int64, dataSetID uint64, pieces []PullPieceRequest) *httptest.ResponseRecorder {
	t.Helper()

	extraData, err := makeTestExtraData(payer, nonce)
	require.NoError(t, err)

	bodyBytes, err := json.Marshal(PullRequest{
		ExtraData: extraData,
		DataSetId: &dataSetID,
		Pieces:    pieces,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/pdp/piece/pull", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()
	handler.HandlePull(rec, req)
	return rec
}

// submitPullStressRequestChunked submits cids in MaxAddPiecesBatchSize chunks
// (each with its own nonce), requiring 200 OK for each, and returns the next
// unused nonce. Pending state accumulates across chunks just as it would for a
// client streaming batches at the request size limit.
func submitPullStressRequestChunked(t *testing.T, handler *PullHandler, payer common.Address, nonce int64, dataSetID uint64, cids []string) int64 {
	t.Helper()

	for len(cids) > 0 {
		batch := min(len(cids), MaxAddPiecesBatchSize)
		rec := submitPullStressRequest(t, handler, payer, nonce, dataSetID, pullPiecesFromCIDs(cids[:batch]))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		cids = cids[batch:]
		nonce++
	}
	return nonce
}

func (s *pdpPullStressStats) snapshot() pdpPullStressStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pdpPullStressStats{
		accepted:                s.accepted,
		rejected:                s.rejected,
		completedGroups:         s.completedGroups,
		acceptedAfterCompletion: s.acceptedAfterCompletion,
		maxGlobalPending:        s.maxGlobalPending,
		maxClientPending:        s.maxClientPending,
		violations:              append([]string(nil), s.violations...),
	}
}

func pullStressActiveCounts(ctx context.Context, db *harmonydb.DB) (int, int, error) {
	var globalPending int
	err := db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM (
			SELECT DISTINCT piece_cid, piece_raw_size
			FROM pdp_piece_pull_items
			WHERE complete = FALSE
				AND failed = FALSE
		) active
	`).Scan(&globalPending)
	if err != nil {
		return 0, 0, err
	}

	var maxClientPending int
	err = db.QueryRow(ctx, `
		SELECT COALESCE(MAX(piece_count), 0)
		FROM (
			SELECT client_address, COUNT(*) AS piece_count
			FROM (
				SELECT DISTINCT pp.client_address, fi.piece_cid, fi.piece_raw_size
				FROM pdp_piece_pull_items fi
				JOIN pdp_piece_pulls pp ON pp.id = fi.fetch_id
				WHERE fi.complete = FALSE
					AND fi.failed = FALSE
			) active
			GROUP BY client_address
		) client_counts
	`).Scan(&maxClientPending)
	if err != nil {
		return 0, 0, err
	}

	return globalPending, maxClientPending, nil
}
