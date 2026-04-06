package itests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ipfs/go-cid"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/itests/helpers"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/denylist"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/tasks/indexing"

	miner2 "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

func TestRetrievals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	full, _, db, maddr := helpers.BootstrapNetworkWithNewMiner(t, ctx, "2KiB")
	defer db.ITestDeleteAll()

	idxStore := helpers.NewIndexStore(ctx, t, config.DefaultCurioConfig())

	// Setup dynamic deny list
	denylistData := []byte("[]")
	baseCfg, err := helpers.SetBaseConfigWithDefaults(t, ctx, db)
	require.NoError(t, err)
	denylistServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("etag", "\"retrievals-itest-denylist\"")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(denylistData)
	}))
	defer denylistServer.Close()
	baseCfg.HTTP.DenylistServers = config.NewDynamic([]string{denylistServer.URL})
	require.NoError(t, helpers.UpsertBaseConfig(ctx, db, baseCfg))

	// Setup Piece and Unsealed Storage
	temp := os.TempDir()
	dir, err := os.MkdirTemp(temp, "curio-retrieval-itest")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, storiface.FTUnsealed.String()), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, storiface.FTPiece.String()), 0o755))

	fixtures := buildRetrievalFixtures(t, dir)

	mid, err := address.IDFromAddress(maddr)
	require.NoError(t, err)
	mi, err := full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)
	nv, err := full.StateNetworkVersion(ctx, types.EmptyTSK)
	require.NoError(t, err)
	sealProof, err := miner2.PreferredSealProofTypeFromWindowPoStType(nv, mi.WindowPoStProofType, false)
	require.NoError(t, err)
	sealSectorSize, err := sealProof.SectorSize()
	require.NoError(t, err)
	spID := int64(mid)
	minerID := abi.ActorID(mid)
	sectorSize := sealSectorSize

	seedPlan := buildRetrievalSeedPlan(fixtures)
	seedState := seedRetrievalFixtures(t, ctx, dir, db, idxStore, spID, minerID, sealProof, sectorSize, seedPlan, fixtures)

	denylistData, err = json.Marshal([]struct {
		Anchor string `json:"anchor"`
	}{
		{Anchor: denylist.CIDToHash(fixtures.denylisted.PieceCIDV1)},
		{Anchor: denylist.CIDToHash(fixtures.denylisted.RootCID)},
	})
	require.NoError(t, err)

	harness := helpers.StartCurioHarnessWithCleanup(ctx, t, dir, db, idxStore, full, baseCfg.Apis.StorageRPCSecret, helpers.CurioHarnessOptions{})

	dependencies := harness.Dependencies

	baseURL := "http://" + baseCfg.HTTP.ListenAddress
	helpers.WaitForHTTP(t, baseURL)
	runRetrievalScenarios(t, ctx, dir, db, idxStore, dependencies, baseURL, spID, minerID, sealProof, sectorSize, fixtures, seedState)
}

type retrievalFixtureSeed struct {
	DealID             string
	SectorNum          abi.SectorNumber
	Fixture            helpers.PieceFixture
	IndexAggregate     bool
	AggregateSubPieces []mk20.DataSource
	RawSizeOverride    *int64
	SkipIndex          bool
}

type retrievalFixtures struct {
	mk12                           helpers.PieceFixture
	aggregateSubpiece              helpers.PieceFixture
	aggregate                      helpers.PieceFixture
	aggregateSubPieces             []mk20.DataSource
	denylisted                     helpers.PieceFixture
	parkNoDeal                     helpers.PieceFixture
	parkWithDeal                   helpers.PieceFixture
	pdpParked                      helpers.PieceFixture
	cacheReuse                     helpers.PieceFixture
	rawSizeValid                   helpers.PieceFixture
	rawSizeZero                    helpers.PieceFixture
	missingMetadata                helpers.PieceFixture
	aggregateRetrySub              helpers.PieceFixture
	aggregateRetryFail             helpers.PieceFixture
	aggregateRetryFailSubPieces    []mk20.DataSource
	aggregateRetrySuccess          helpers.PieceFixture
	aggregateRetrySuccessSubPieces []mk20.DataSource
}

type retrievalParkedPieceIDs struct {
	parkOnlyPieceID     int64
	parkWithDealPieceID int64
	pdpPieceID          int64
	cacheReusePieceID   int64
}

type retrievalSeedState struct {
	cacheReusePieceID int64
}

const (
	rawSizeValidSectorNum       abi.SectorNumber = 202
	rawSizeZeroSectorNum        abi.SectorNumber = 203
	missingMetadataSectorNum    abi.SectorNumber = 204
	aggregateRetrySuccessSector abi.SectorNumber = 205
)

func createPaddedRetrievalFixture(t *testing.T, dir string, sourceSize int64) helpers.PieceFixture {
	t.Helper()

	for attempt := int64(0); attempt < 8; attempt++ {
		fixture := helpers.CreatePieceFixture(t, dir, sourceSize+attempt)
		if fixture.RawSize < int64(fixture.PieceSize.Unpadded()) {
			return fixture
		}
	}

	t.Fatalf("failed to create padded retrieval fixture from source size %d", sourceSize)
	return helpers.PieceFixture{}
}

func buildRetrievalFixtures(t *testing.T, dir string) retrievalFixtures {
	t.Helper()

	mk12Fixture := helpers.CreatePieceFixture(t, dir, 512)
	aggregateSubpieceFixture := helpers.CreatePieceFixture(t, dir, 127)
	aggregateSiblingFixture := helpers.CreatePieceFixture(t, dir, 127)
	aggregateFixture, aggregateSubPieces := helpers.CreateAggregateFixtureFromSubpieces(t, []helpers.PieceFixture{
		aggregateSubpieceFixture,
		aggregateSiblingFixture,
	})
	rawSizeValidFixture := createPaddedRetrievalFixture(t, dir, 331)
	rawSizeZeroFixture := createPaddedRetrievalFixture(t, dir, 347)
	aggregateRetrySubFixture := helpers.CreatePieceFixture(t, dir, 141)
	aggregateRetrySiblingA := helpers.CreatePieceFixture(t, dir, 149)
	aggregateRetrySiblingB := helpers.CreatePieceFixture(t, dir, 157)
	aggregateRetryA, aggregateRetryASubPieces := helpers.CreateAggregateFixtureFromSubpieces(t, []helpers.PieceFixture{
		aggregateRetrySubFixture,
		aggregateRetrySiblingA,
	})
	aggregateRetryB, aggregateRetryBSubPieces := helpers.CreateAggregateFixtureFromSubpieces(t, []helpers.PieceFixture{
		aggregateRetrySubFixture,
		aggregateRetrySiblingB,
	})
	aggregateRetryFail := aggregateRetryA
	aggregateRetryFailSubPieces := aggregateRetryASubPieces
	aggregateRetrySuccess := aggregateRetryB
	aggregateRetrySuccessSubPieces := aggregateRetryBSubPieces
	if bytes.Compare(aggregateRetryB.PieceCIDV2.Bytes(), aggregateRetryA.PieceCIDV2.Bytes()) < 0 {
		aggregateRetryFail = aggregateRetryB
		aggregateRetryFailSubPieces = aggregateRetryBSubPieces
		aggregateRetrySuccess = aggregateRetryA
		aggregateRetrySuccessSubPieces = aggregateRetryASubPieces
	}

	return retrievalFixtures{
		mk12:                           mk12Fixture,
		aggregateSubpiece:              aggregateSubpieceFixture,
		aggregate:                      aggregateFixture,
		aggregateSubPieces:             aggregateSubPieces,
		denylisted:                     helpers.CreatePieceFixture(t, dir, 448),
		parkNoDeal:                     helpers.CreatePieceFixture(t, dir, 320),
		parkWithDeal:                   helpers.CreatePieceFixture(t, dir, 384),
		pdpParked:                      helpers.CreatePieceFixture(t, dir, 360),
		cacheReuse:                     helpers.CreatePieceFixture(t, dir, 416),
		rawSizeValid:                   rawSizeValidFixture,
		rawSizeZero:                    rawSizeZeroFixture,
		missingMetadata:                helpers.CreatePieceFixture(t, dir, 352),
		aggregateRetrySub:              aggregateRetrySubFixture,
		aggregateRetryFail:             aggregateRetryFail,
		aggregateRetryFailSubPieces:    aggregateRetryFailSubPieces,
		aggregateRetrySuccess:          aggregateRetrySuccess,
		aggregateRetrySuccessSubPieces: aggregateRetrySuccessSubPieces,
	}
}

func buildRetrievalSeedPlan(fixtures retrievalFixtures) []retrievalFixtureSeed {
	rawSizeZero := int64(0)
	return []retrievalFixtureSeed{
		{
			DealID:    "mk12-retrieval-itest",
			SectorNum: abi.SectorNumber(199),
			Fixture:   fixtures.mk12,
		},
		{
			DealID:             "mk12-aggregate-itest",
			SectorNum:          abi.SectorNumber(200),
			Fixture:            fixtures.aggregate,
			IndexAggregate:     true,
			AggregateSubPieces: fixtures.aggregateSubPieces,
		},
		{
			DealID:    "mk12-denylist-itest",
			SectorNum: abi.SectorNumber(201),
			Fixture:   fixtures.denylisted,
		},
		{
			DealID:          "mk12-valid-raw-size-itest",
			SectorNum:       rawSizeValidSectorNum,
			Fixture:         fixtures.rawSizeValid,
			RawSizeOverride: &fixtures.rawSizeValid.RawSize,
		},
		{
			DealID:          "mk12-zero-raw-size-itest",
			SectorNum:       rawSizeZeroSectorNum,
			Fixture:         fixtures.rawSizeZero,
			RawSizeOverride: &rawSizeZero,
		},
		{
			DealID:    "mk12-aggregate-retry-itest",
			SectorNum: aggregateRetrySuccessSector,
			Fixture:   fixtures.aggregateRetrySuccess,
			SkipIndex: true,
		},
	}
}

func seedRetrievalFixtures(
	t *testing.T,
	ctx context.Context,
	dir string,
	db *harmonydb.DB,
	idxStore *indexstore.IndexStore,
	spID int64,
	minerID abi.ActorID,
	sealProof abi.RegisteredSealProof,
	sectorSize abi.SectorSize,
	seeds []retrievalFixtureSeed,
	fixtures retrievalFixtures,
) retrievalSeedState {
	t.Helper()

	for _, seed := range seeds {
		require.NoError(t, helpers.WriteUnsealedSectorFixture(dir, minerID, seed.SectorNum, sectorSize, seed.Fixture))
	}
	require.NoError(t, helpers.WriteUnsealedSectorFixture(dir, minerID, missingMetadataSectorNum, sectorSize, fixtures.missingMetadata))

	var parkedIDs retrievalParkedPieceIDs
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		for _, seed := range seeds {
			if err := seedRetrievalFixtureTx(tx, spID, sealProof, seed); err != nil {
				return false, err
			}
		}

		ids, err := seedParkedRetrievalFixturesTx(tx, fixtures)
		if err != nil {
			return false, err
		}
		parkedIDs = ids

		return true, nil
	})
	require.NoError(t, err)
	require.True(t, committed)

	require.NoError(t, helpers.WriteParkedPieceFixture(dir, parkedIDs.parkOnlyPieceID, fixtures.parkNoDeal.CarBytes))
	require.NoError(t, helpers.WriteParkedPieceFixture(dir, parkedIDs.parkWithDealPieceID, fixtures.parkWithDeal.CarBytes))
	require.NoError(t, helpers.WriteParkedPieceFixture(dir, parkedIDs.pdpPieceID, fixtures.pdpParked.CarBytes))
	require.NoError(t, helpers.WriteParkedPieceFixture(dir, parkedIDs.cacheReusePieceID, fixtures.cacheReuse.CarBytes))

	for _, seed := range seeds {
		if seed.SkipIndex {
			continue
		}
		if seed.IndexAggregate {
			require.NoError(t, helpers.AddAggregateIndexFromPiece(t, ctx, idxStore, seed.Fixture, seed.AggregateSubPieces))
			continue
		}
		require.NoError(t, helpers.AddIndexFromCAR(ctx, idxStore, seed.Fixture.PieceCIDV2, seed.Fixture.CarBytes))
	}

	require.NoError(t, addAggregateIndexWithoutUniquenessCheck(ctx, idxStore, fixtures.aggregateRetryFail, fixtures.aggregateRetryFailSubPieces))
	require.NoError(t, addAggregateIndexWithoutUniquenessCheck(ctx, idxStore, fixtures.aggregateRetrySuccess, fixtures.aggregateRetrySuccessSubPieces))

	return retrievalSeedState{
		cacheReusePieceID: parkedIDs.cacheReusePieceID,
	}
}

func seedParkedRetrievalFixturesTx(tx *harmonydb.Tx, fixtures retrievalFixtures) (retrievalParkedPieceIDs, error) {
	parkOnlyPieceID, err := helpers.InsertCompletedParkedPiece(tx, fixtures.parkNoDeal.PieceCIDV1.String(), fixtures.parkNoDeal.PieceSize, fixtures.parkNoDeal.RawSize, true)
	if err != nil {
		return retrievalParkedPieceIDs{}, err
	}

	parkWithDealPieceID, err := helpers.InsertCompletedParkedPiece(tx, fixtures.parkWithDeal.PieceCIDV1.String(), fixtures.parkWithDeal.PieceSize, fixtures.parkWithDeal.RawSize, true)
	if err != nil {
		return retrievalParkedPieceIDs{}, err
	}

	pieceRefID, err := helpers.InsertParkedPieceRef(tx, parkWithDealPieceID, "", nil, true)
	if err != nil {
		return retrievalParkedPieceIDs{}, err
	}

	if err := helpers.ProcessPieceDealTx(tx, helpers.ProcessPieceDealParams{
		DealID:        "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		PieceCID:      fixtures.parkWithDeal.PieceCIDV1.String(),
		BoostDeal:     false,
		SPID:          int64(-1),
		SectorNum:     int64(-1),
		PieceOffset:   nil,
		PieceLength:   int64(fixtures.parkWithDeal.PieceSize),
		RawSize:       fixtures.parkWithDeal.RawSize,
		FastRetrieval: true,
		PieceRefID:    pieceRefID,
		LegacyDeal:    false,
		LegacyDealID:  int64(0),
	}); err != nil {
		return retrievalParkedPieceIDs{}, err
	}

	pdpPieceID, err := helpers.InsertCompletedParkedPiece(tx, fixtures.pdpParked.PieceCIDV1.String(), fixtures.pdpParked.PieceSize, fixtures.pdpParked.RawSize, true)
	if err != nil {
		return retrievalParkedPieceIDs{}, err
	}
	pdpPieceRefID, err := helpers.InsertParkedPieceRef(tx, pdpPieceID, "", nil, true)
	if err != nil {
		return retrievalParkedPieceIDs{}, err
	}
	if _, err := tx.Exec(`INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, $2)`, []byte("retrievals-itest-pdp"), "retrievals-itest-pdp"); err != nil {
		return retrievalParkedPieceIDs{}, err
	}
	if _, err := tx.Exec(`INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at) VALUES ($1, $2, $3, NOW())`, "retrievals-itest-pdp", fixtures.pdpParked.PieceCIDV1.String(), pdpPieceRefID); err != nil {
		return retrievalParkedPieceIDs{}, err
	}

	cacheReusePieceID, err := helpers.InsertCompletedParkedPiece(tx, fixtures.cacheReuse.PieceCIDV1.String(), fixtures.cacheReuse.PieceSize, fixtures.cacheReuse.RawSize, true)
	if err != nil {
		return retrievalParkedPieceIDs{}, err
	}
	cacheReusePieceRefID, err := helpers.InsertParkedPieceRef(tx, cacheReusePieceID, "", nil, true)
	if err != nil {
		return retrievalParkedPieceIDs{}, err
	}
	if err := helpers.ProcessPieceDealTx(tx, helpers.ProcessPieceDealParams{
		DealID:        "01ARZ3NDEKTSV4RRFFQ69G5FB0",
		PieceCID:      fixtures.cacheReuse.PieceCIDV1.String(),
		BoostDeal:     false,
		SPID:          int64(-1),
		SectorNum:     int64(-1),
		PieceOffset:   nil,
		PieceLength:   int64(fixtures.cacheReuse.PieceSize),
		RawSize:       fixtures.cacheReuse.RawSize,
		FastRetrieval: true,
		PieceRefID:    cacheReusePieceRefID,
		LegacyDeal:    false,
		LegacyDealID:  int64(0),
	}); err != nil {
		return retrievalParkedPieceIDs{}, err
	}

	return retrievalParkedPieceIDs{
		parkOnlyPieceID:     parkOnlyPieceID,
		parkWithDealPieceID: parkWithDealPieceID,
		pdpPieceID:          pdpPieceID,
		cacheReusePieceID:   cacheReusePieceID,
	}, nil
}

func runRetrievalScenarios(
	t *testing.T,
	ctx context.Context,
	dir string,
	db *harmonydb.DB,
	idxStore *indexstore.IndexStore,
	dependencies *deps.Deps,
	baseURL string,
	spID int64,
	minerID abi.ActorID,
	sealProof abi.RegisteredSealProof,
	sectorSize abi.SectorSize,
	fixtures retrievalFixtures,
	seedState retrievalSeedState,
) {
	t.Helper()

	nextSectorNum := abi.SectorNumber(300)
	nextSector := func() abi.SectorNumber {
		sectorNum := nextSectorNum
		nextSectorNum++
		return sectorNum
	}
	t.Run("mk12 piece retrieval by pieceCIDv1", func(t *testing.T) {
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.mk12.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, fixtures.mk12.CarBytes, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.mk12.PieceCIDV1.String(), len(fixtures.mk12.CarBytes))
	})

	t.Run("mk12 piece retrieval by pieceCIDv2", func(t *testing.T) {
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.mk12.PieceCIDV2.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, fixtures.mk12.CarBytes, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.mk12.PieceCIDV2.String(), len(fixtures.mk12.CarBytes))
	})

	t.Run("mk12 piece retrieval HEAD by pieceCIDv2", func(t *testing.T) {
		status, body, headers := helpers.HTTPRequestWithHeaders(t, http.MethodHead, baseURL, "/piece/"+fixtures.mk12.PieceCIDV2.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Empty(t, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.mk12.PieceCIDV2.String(), len(fixtures.mk12.CarBytes))
	})

	t.Run("mk12 piece retrieval range by pieceCIDv2", func(t *testing.T) {
		rangeStart := 0
		rangeEnd := 63
		require.Greater(t, len(fixtures.mk12.CarBytes), rangeEnd)
		status, body, headers := helpers.HTTPRequestWithHeaders(t, http.MethodGet, baseURL, "/piece/"+fixtures.mk12.PieceCIDV2.String(), map[string]string{
			"Range": fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd),
		})
		require.Equal(t, http.StatusPartialContent, status)

		expected := fixtures.mk12.CarBytes[rangeStart : rangeEnd+1]
		require.Equal(t, expected, body)
		require.Equal(t, fmt.Sprintf("bytes %d-%d/%d", rangeStart, rangeEnd, len(fixtures.mk12.CarBytes)), headers.Get("Content-Range"))
		require.Equal(t, strconv.Itoa(len(expected)), headers.Get("Content-Length"))
		require.Equal(t, "bytes", headers.Get("Accept-Ranges"))
	})

	t.Run("ipfs style retrieval", func(t *testing.T) {
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/ipfs/"+fixtures.mk12.RootCID.String(), map[string]string{
			"Accept": "application/vnd.ipld.car",
		})
		require.Equal(t, http.StatusOK, status)
		helpers.AssertIPFSCarResponseHeaders(t, headers)

		br, err := carv2.NewBlockReader(bytes.NewReader(body))
		require.NoError(t, err)
		require.Contains(t, br.Roots, fixtures.mk12.RootCID)
	})

	t.Run("aggregate retrieval by subpieceCIDv2", func(t *testing.T) {
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.aggregateSubpiece.PieceCIDV2.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, fixtures.aggregateSubpiece.CarBytes, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.aggregateSubpiece.PieceCIDV2.String(), len(fixtures.aggregateSubpiece.CarBytes))
	})

	t.Run("pdp parked piece retrieval by pieceCIDv1", func(t *testing.T) {
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.pdpParked.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, fixtures.pdpParked.CarBytes, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.pdpParked.PieceCIDV1.String(), len(fixtures.pdpParked.CarBytes))
	})

	newCachedPieceReader := func() *cachedreader.CachedPieceReader {
		return cachedreader.NewCachedPieceReader(
			db,
			dependencies.SectorReader,
			pieceprovider.NewPieceParkReader(dependencies.Stor, dependencies.Si),
			idxStore,
		)
	}

	t.Run("parkpiece retrieval false and no-retrieval endpoint behavior", func(t *testing.T) {
		// /piece endpoint always uses retrieval=true, so this parked-only piece must be 404.
		status, _ := helpers.HTTPGet(t, baseURL, "/piece/"+fixtures.parkNoDeal.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusNotFound, status)

		cprTrue := cachedreader.NewCachedPieceReader(
			db,
			dependencies.SectorReader,
			pieceprovider.NewPieceParkReader(dependencies.Stor, dependencies.Si),
			idxStore,
		)
		_, _, err := cprTrue.GetSharedPieceReader(ctx, fixtures.parkNoDeal.PieceCIDV1, true)
		require.Error(t, err)
		require.ErrorIs(t, err, cachedreader.ErrNoDeal)

		cprFalse := cachedreader.NewCachedPieceReader(
			db,
			dependencies.SectorReader,
			pieceprovider.NewPieceParkReader(dependencies.Stor, dependencies.Si),
			idxStore,
		)
		r, sz, err := cprFalse.GetSharedPieceReader(ctx, fixtures.parkNoDeal.PieceCIDV1, false)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()
		require.Equal(t, uint64(fixtures.parkNoDeal.RawSize), sz)
		got, err := io.ReadAll(r)
		require.NoError(t, err)
		require.Equal(t, fixtures.parkNoDeal.CarBytes, got)
	})

	t.Run("parkpiece with deal binding serves piece retrieval", func(t *testing.T) {
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.parkWithDeal.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, fixtures.parkWithDeal.CarBytes, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.parkWithDeal.PieceCIDV1.String(), len(fixtures.parkWithDeal.CarBytes))
	})

	t.Run("mk20 piece retrieval returns 500 when piece_ref backing is missing", func(t *testing.T) {
		fixture := helpers.CreatePieceFixture(t, dir, 448)
		seedStandaloneMK20Deal(t, ctx, db, "01ARZ3NDEKTSV4RRFFQ69G5FA2", fixture, int64(987654321))

		status, body, _ := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixture.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusInternalServerError, status)
		require.Empty(t, body)
	})

	t.Run("aggregate retrieval returns 404 when parent piece is unavailable", func(t *testing.T) {
		subpieceFixture := helpers.CreatePieceFixture(t, dir, 129)
		siblingFixture := helpers.CreatePieceFixture(t, dir, 137)
		aggregateFixture, aggregateSubPieces := helpers.CreateAggregateFixtureFromSubpieces(t, []helpers.PieceFixture{
			subpieceFixture,
			siblingFixture,
		})
		require.NoError(t, helpers.AddAggregateIndexFromPiece(t, ctx, idxStore, aggregateFixture, aggregateSubPieces))

		status, body, _ := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+subpieceFixture.PieceCIDV2.String(), nil)
		require.Equal(t, http.StatusNotFound, status)
		require.Empty(t, body)
	})

	t.Run("piece retrieval reuses cached reader across requests", func(t *testing.T) {
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.cacheReuse.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, fixtures.cacheReuse.CarBytes, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.cacheReuse.PieceCIDV1.String(), len(fixtures.cacheReuse.CarBytes))

		piecePath := filepath.Join(
			dir,
			storiface.FTPiece.String(),
			storiface.SectorName(storiface.PieceNumber(seedState.cacheReusePieceID).Ref().ID),
		)
		require.NoError(t, os.Remove(piecePath))

		status, body, headers = helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.cacheReuse.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, fixtures.cacheReuse.CarBytes, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.cacheReuse.PieceCIDV1.String(), len(fixtures.cacheReuse.CarBytes))
	})

	t.Run("mk12 piece retrieval returns 500 when sector read fails", func(t *testing.T) {
		fixture := helpers.CreatePieceFixture(t, dir, 300)
		seedStandaloneSectorDeal(t, ctx, dir, db, spID, minerID, sealProof, sectorSize, nextSector(), "mk12-invalid-size-itest", fixture, abi.PaddedPieceSize(1), fixture.RawSize, false)

		status, body, _ := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixture.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusInternalServerError, status)
		require.Empty(t, body)
	})

	t.Run("aggregate retrieval succeeds when one parent fails and another parent is readable", func(t *testing.T) {
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.aggregateRetrySub.PieceCIDV2.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, fixtures.aggregateRetrySub.CarBytes, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.aggregateRetrySub.PieceCIDV2.String(), len(fixtures.aggregateRetrySub.CarBytes))
	})

	t.Run("piece retrieval serves only raw_size when raw_size is valid", func(t *testing.T) {
		require.Less(t, fixtures.rawSizeValid.RawSize, int64(fixtures.rawSizeValid.PieceSize.Unpadded()))
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.rawSizeValid.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, body, int(fixtures.rawSizeValid.RawSize))
		require.Equal(t, fixtures.rawSizeValid.CarBytes, body)
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.rawSizeValid.PieceCIDV1.String(), len(body))
	})

	t.Run("piece retrieval falls back to pieceSize.Unpadded when raw_size is not valid", func(t *testing.T) {
		require.Less(t, fixtures.rawSizeZero.RawSize, int64(fixtures.rawSizeZero.PieceSize.Unpadded()))
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+fixtures.rawSizeZero.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, body, int(fixtures.rawSizeZero.PieceSize.Unpadded()))
		require.Equal(t, fixtures.rawSizeZero.CarBytes, body[:len(fixtures.rawSizeZero.CarBytes)])
		require.True(t, allZeroBytes(body[len(fixtures.rawSizeZero.CarBytes):]))
		helpers.AssertPieceResponseHeaders(t, headers, fixtures.rawSizeZero.PieceCIDV1.String(), len(body))
	})

	t.Run("denylist blocks /piece path", func(t *testing.T) {
		require.Eventually(t, func() bool {
			status, _ := helpers.HTTPGet(t, baseURL, "/piece/"+fixtures.denylisted.PieceCIDV1.String(), nil)
			return status == http.StatusUnavailableForLegalReasons
		}, 15*time.Second, 250*time.Millisecond)
	})

	t.Run("denylist blocks /piece path with pieceCIDv2", func(t *testing.T) {
		require.Eventually(t, func() bool {
			status, _ := helpers.HTTPGet(t, baseURL, "/piece/"+fixtures.denylisted.PieceCIDV2.String(), nil)
			return status == http.StatusUnavailableForLegalReasons
		}, 15*time.Second, 250*time.Millisecond)
	})

	t.Run("denylist blocks /ipfs path", func(t *testing.T) {
		require.Eventually(t, func() bool {
			status, _ := helpers.HTTPGet(t, baseURL, "/ipfs/"+fixtures.denylisted.RootCID.String(), map[string]string{
				"Accept": "application/vnd.ipld.car",
			})
			return status == http.StatusUnavailableForLegalReasons
		}, 15*time.Second, 250*time.Millisecond)
	})

	t.Run("CachedPieceReader tests", func(t *testing.T) {
		cachedSectionReaderType := cachedSectionReaderReflectType(t, ctx, newCachedPieceReader(), fixtures.parkWithDeal.PieceCIDV1)

		t.Run("missing v1 metadata caches size error until a fresh reader is used", func(t *testing.T) {
			cpr := newCachedPieceReader()
			_, _, err := cpr.GetSharedPieceReader(ctx, fixtures.missingMetadata.PieceCIDV1, false)
			require.Error(t, err)
			require.ErrorContains(t, err, "failed to determine piece size")

			seedStandaloneSectorDeal(t, ctx, dir, db, spID, minerID, sealProof, sectorSize, missingMetadataSectorNum, "mk12-missing-metadata-repair-itest", fixtures.missingMetadata, fixtures.missingMetadata.PieceSize, fixtures.missingMetadata.RawSize, false)

			_, _, err = cpr.GetSharedPieceReader(ctx, fixtures.missingMetadata.PieceCIDV1, false)
			require.Error(t, err)
			require.ErrorContains(t, err, "failed to determine piece size")

			repairedCPR := newCachedPieceReader()
			r, sz, err := repairedCPR.GetSharedPieceReader(ctx, fixtures.missingMetadata.PieceCIDV1, false)
			require.NoError(t, err)
			defer func() { _ = r.Close() }()
			require.Equal(t, uint64(fixtures.missingMetadata.RawSize), sz)

			got, err := io.ReadAll(r)
			require.NoError(t, err)
			require.Equal(t, fixtures.missingMetadata.CarBytes, got)
		})

		t.Run("piece metadata without parked backing returns a piece park miss", func(t *testing.T) {
			fixture := helpers.CreatePieceFixture(t, dir, 288)

			_, err := db.Exec(ctx, `
				INSERT INTO market_piece_metadata (piece_cid, piece_size, indexed)
				VALUES ($1, $2, TRUE)`,
				fixture.PieceCIDV1.String(),
				int64(fixture.PieceSize),
			)
			require.NoError(t, err)

			cpr := newCachedPieceReader()
			_, _, err = cpr.GetSharedPieceReader(ctx, fixture.PieceCIDV1, false)
			require.Error(t, err)
			require.ErrorContains(t, err, "failed to find piece in parked_pieces")
		})

		t.Run("cached reader waiter respects canceled context", func(t *testing.T) {
			cpr := cachedreader.NewCachedPieceReader(nil, nil, nil, nil)
			ready := make(chan struct{})
			entry := newCachedSectionReaderValue(t, cachedSectionReaderType, cpr, fixtures.mk12.PieceCIDV1, nil, 0, ready, nil, nil, 0, false)
			cacheSetEntry(t, cpr, "pieceReaderCache", fixtures.mk12.PieceCIDV1, entry)

			waitCtx, cancel := context.WithCancel(ctx)
			cancel()

			_, _, err := cpr.GetSharedPieceReader(waitCtx, fixtures.mk12.PieceCIDV1, false)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, 0, hiddenIntField(entry, "refs"))
		})

		t.Run("cached reader waiter returns cached setup error", func(t *testing.T) {
			cpr := cachedreader.NewCachedPieceReader(nil, nil, nil, nil)
			ready := make(chan struct{})
			close(ready)
			expectedErr := errors.New("cached reader setup failed")
			entry := newCachedSectionReaderValue(t, cachedSectionReaderType, cpr, fixtures.mk12.PieceCIDV1, nil, 0, ready, expectedErr, nil, 0, false)
			cacheSetEntry(t, cpr, "pieceReaderCache", fixtures.mk12.PieceCIDV1, entry)

			_, _, err := cpr.GetSharedPieceReader(ctx, fixtures.mk12.PieceCIDV1, false)
			require.ErrorIs(t, err, expectedErr)
			require.Equal(t, 0, hiddenIntField(entry, "refs"))
		})

		t.Run("cached reader close without expiry decrements refs only", func(t *testing.T) {
			cpr := cachedreader.NewCachedPieceReader(nil, nil, nil, nil)
			reader := newTrackingStorifaceReader([]byte("cached data"))
			entry := newCachedSectionReaderValue(t, cachedSectionReaderType, cpr, fixtures.mk12.PieceCIDV1, reader, 11, closedChan(), nil, func() {}, 2, false)

			require.NoError(t, callHiddenClose(entry))
			require.Equal(t, 1, hiddenIntField(entry, "refs"))
			require.Equal(t, int32(0), reader.closeCount.Load())
		})

		t.Run("cached reader close after expiry closes underlying reader", func(t *testing.T) {
			cpr := cachedreader.NewCachedPieceReader(nil, nil, nil, nil)
			reader := newTrackingStorifaceReader([]byte("cached data"))
			var cancelCalls atomic.Int32
			entry := newCachedSectionReaderValue(t, cachedSectionReaderType, cpr, fixtures.mk12.PieceCIDV1, reader, 11, closedChan(), nil, func() {
				cancelCalls.Add(1)
			}, 1, true)

			require.NoError(t, callHiddenClose(entry))
			require.Equal(t, 0, hiddenIntField(entry, "refs"))
			require.Equal(t, int32(1), reader.closeCount.Load())
			require.Equal(t, int32(1), cancelCalls.Load())
		})

		t.Run("reader cache expiry closes unreferenced reader", func(t *testing.T) {
			cpr := cachedreader.NewCachedPieceReader(nil, nil, nil, nil)
			setCacheTTL(t, cpr, "pieceReaderCache", 50*time.Millisecond)

			reader := newTrackingStorifaceReader([]byte("cached data"))
			var cancelCalls atomic.Int32
			entry := newCachedSectionReaderValue(t, cachedSectionReaderType, cpr, fixtures.mk12.PieceCIDV1, reader, 11, closedChan(), nil, func() {
				cancelCalls.Add(1)
			}, 0, false)
			cacheSetEntry(t, cpr, "pieceReaderCache", fixtures.mk12.PieceCIDV1, entry)

			require.Eventually(t, func() bool {
				return reader.closeCount.Load() == 1 && cancelCalls.Load() == 1
			}, time.Second, 10*time.Millisecond)
		})

		t.Run("reader cache expiry defers cleanup while referenced", func(t *testing.T) {
			cpr := cachedreader.NewCachedPieceReader(nil, nil, nil, nil)
			setCacheTTL(t, cpr, "pieceReaderCache", 50*time.Millisecond)

			reader := newTrackingStorifaceReader([]byte("cached data"))
			var cancelCalls atomic.Int32
			entry := newCachedSectionReaderValue(t, cachedSectionReaderType, cpr, fixtures.mk12.PieceCIDV1, reader, 11, closedChan(), nil, func() {
				cancelCalls.Add(1)
			}, 1, false)
			cacheSetEntry(t, cpr, "pieceReaderCache", fixtures.mk12.PieceCIDV1, entry)

			require.Eventually(t, func() bool {
				return hiddenBoolField(entry, "expired")
			}, time.Second, 10*time.Millisecond)
			require.Equal(t, int32(0), reader.closeCount.Load())
			require.Equal(t, int32(0), cancelCalls.Load())

			require.NoError(t, callHiddenClose(entry))
			require.Equal(t, int32(1), reader.closeCount.Load())
			require.Equal(t, int32(1), cancelCalls.Load())
		})

		t.Run("error cache entry expires", func(t *testing.T) {
			fixture := helpers.CreatePieceFixture(t, dir, 355)
			cpr := newCachedPieceReader()
			setCacheTTL(t, cpr, "pieceErrorCache", 50*time.Millisecond)

			_, _, err := cpr.GetSharedPieceReader(ctx, fixture.PieceCIDV1, false)
			require.Error(t, err)
			_, found := cacheGetEntry(t, cpr, "pieceErrorCache", fixture.PieceCIDV1)
			require.True(t, found)

			require.Eventually(t, func() bool {
				_, found := cacheGetEntry(t, cpr, "pieceErrorCache", fixture.PieceCIDV1)
				return !found
			}, time.Second, 10*time.Millisecond)
		})
	})
}

func seedRetrievalFixtureTx(
	tx *harmonydb.Tx,
	spID int64,
	sealProof abi.RegisteredSealProof,
	seed retrievalFixtureSeed,
) error {
	rawSize := seed.Fixture.RawSize
	if seed.RawSizeOverride != nil {
		rawSize = *seed.RawSizeOverride
	}

	_, err := tx.Exec(`INSERT INTO sectors_meta (
		sp_id, sector_num, reg_seal_proof, ticket_epoch, ticket_value,
		orig_sealed_cid, orig_unsealed_cid, cur_sealed_cid, cur_unsealed_cid,
		seed_epoch, seed_value
	) VALUES ($1, $2, $3, 0, $4, $5, $6, $5, $6, 0, $4)
	ON CONFLICT (sp_id, sector_num) DO NOTHING`,
		spID,
		int64(seed.SectorNum),
		sealProof,
		[]byte{0},
		seed.Fixture.PieceCIDV1.String(),
		seed.Fixture.PieceCIDV1.String(),
	)
	if err != nil {
		return err
	}

	return helpers.ProcessPieceDealTx(tx, helpers.ProcessPieceDealParams{
		DealID:        seed.DealID,
		PieceCID:      seed.Fixture.PieceCIDV1.String(),
		BoostDeal:     true,
		SPID:          spID,
		SectorNum:     int64(seed.SectorNum),
		PieceOffset:   int64(0),
		PieceLength:   int64(seed.Fixture.PieceSize),
		RawSize:       rawSize,
		FastRetrieval: true,
		PieceRefID:    nil,
		LegacyDeal:    false,
		LegacyDealID:  int64(0),
	})
}

func seedStandaloneParkedPiece(t *testing.T, ctx context.Context, dir string, db *harmonydb.DB, fixture helpers.PieceFixture) int64 {
	t.Helper()

	var pieceID int64
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		id, err := helpers.InsertCompletedParkedPiece(tx, fixture.PieceCIDV1.String(), fixture.PieceSize, fixture.RawSize, true)
		if err != nil {
			return false, err
		}
		pieceID = id
		return true, nil
	})
	require.NoError(t, err)
	require.True(t, committed)
	require.NoError(t, helpers.WriteParkedPieceFixture(dir, pieceID, fixture.CarBytes))

	return pieceID
}

func seedStandaloneParkedPieceRef(t *testing.T, ctx context.Context, db *harmonydb.DB, pieceID int64) int64 {
	t.Helper()

	var pieceRefID int64
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		refID, err := helpers.InsertParkedPieceRef(tx, pieceID, "", nil, true)
		if err != nil {
			return false, err
		}
		pieceRefID = refID
		return true, nil
	})
	require.NoError(t, err)
	require.True(t, committed)

	return pieceRefID
}

func seedStandalonePDPPieceRef(t *testing.T, ctx context.Context, db *harmonydb.DB, serviceLabel, pieceCID string, pieceRefID int64) {
	t.Helper()

	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if _, err := tx.Exec(`INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, $2)`, []byte(serviceLabel), serviceLabel); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at) VALUES ($1, $2, $3, NOW())`, serviceLabel, pieceCID, pieceRefID); err != nil {
			return false, err
		}
		return true, nil
	})
	require.NoError(t, err)
	require.True(t, committed)
}

func seedStandaloneMK20Deal(t *testing.T, ctx context.Context, db *harmonydb.DB, dealID string, fixture helpers.PieceFixture, pieceRefID any) {
	t.Helper()

	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return true, helpers.ProcessPieceDealTx(tx, helpers.ProcessPieceDealParams{
			DealID:        dealID,
			PieceCID:      fixture.PieceCIDV1.String(),
			BoostDeal:     false,
			SPID:          int64(-1),
			SectorNum:     int64(-1),
			PieceOffset:   nil,
			PieceLength:   int64(fixture.PieceSize),
			RawSize:       fixture.RawSize,
			FastRetrieval: true,
			PieceRefID:    pieceRefID,
			LegacyDeal:    false,
			LegacyDealID:  int64(0),
		})
	})
	require.NoError(t, err)
	require.True(t, committed)
}

func seedStandaloneSectorDeal(
	t *testing.T,
	ctx context.Context,
	dir string,
	db *harmonydb.DB,
	spID int64,
	minerID abi.ActorID,
	sealProof abi.RegisteredSealProof,
	sectorSize abi.SectorSize,
	sectorNum abi.SectorNumber,
	dealID string,
	fixture helpers.PieceFixture,
	pieceLength abi.PaddedPieceSize,
	rawSize int64,
	writeSector bool,
) {
	t.Helper()

	if writeSector {
		require.NoError(t, helpers.WriteUnsealedSectorFixture(dir, minerID, sectorNum, sectorSize, fixture))
	}

	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		_, err := tx.Exec(`INSERT INTO sectors_meta (
			sp_id, sector_num, reg_seal_proof, ticket_epoch, ticket_value,
			orig_sealed_cid, orig_unsealed_cid, cur_sealed_cid, cur_unsealed_cid,
			seed_epoch, seed_value
		) VALUES ($1, $2, $3, 0, $4, $5, $6, $5, $6, 0, $4)
		ON CONFLICT (sp_id, sector_num) DO NOTHING`,
			spID,
			int64(sectorNum),
			sealProof,
			[]byte{0},
			fixture.PieceCIDV1.String(),
			fixture.PieceCIDV1.String(),
		)
		if err != nil {
			return false, err
		}

		return true, helpers.ProcessPieceDealTx(tx, helpers.ProcessPieceDealParams{
			DealID:        dealID,
			PieceCID:      fixture.PieceCIDV1.String(),
			BoostDeal:     true,
			SPID:          spID,
			SectorNum:     int64(sectorNum),
			PieceOffset:   int64(0),
			PieceLength:   int64(pieceLength),
			RawSize:       rawSize,
			FastRetrieval: true,
			PieceRefID:    nil,
			LegacyDeal:    false,
			LegacyDealID:  int64(0),
		})
	})
	require.NoError(t, err)
	require.True(t, committed)
}

func addAggregateIndexWithoutUniquenessCheck(ctx context.Context, idx *indexstore.IndexStore, aggregate helpers.PieceFixture, subPieces []mk20.DataSource) error {
	recs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})

	var eg errgroup.Group
	eg.Go(func() error {
		return idx.AddIndex(ctx, aggregate.PieceCIDV2, recs)
	})

	blocks, aggidx, interrupted, idxErr := indexing.IndexAggregate(
		aggregate.PieceCIDV2,
		bytes.NewReader(aggregate.CarBytes),
		aggregate.PieceSize,
		subPieces,
		recs,
		addFail,
	)
	close(recs)

	addErr := eg.Wait()
	if idxErr != nil {
		return idxErr
	}
	if addErr != nil {
		return addErr
	}
	if interrupted {
		return fmt.Errorf("aggregate indexing was interrupted for piece %s", aggregate.PieceCIDV2)
	}
	if blocks <= 0 {
		return fmt.Errorf("aggregate piece %s produced no indexed blocks", aggregate.PieceCIDV2)
	}

	for k, v := range aggidx {
		if err := idx.InsertAggregateIndex(ctx, k, v); err != nil {
			return fmt.Errorf("inserting aggregate index for %s: %w", k, err)
		}
	}

	return nil
}

type trackingStorifaceReader struct {
	*bytes.Reader
	closeCount atomic.Int32
}

func newTrackingStorifaceReader(data []byte) *trackingStorifaceReader {
	return &trackingStorifaceReader{Reader: bytes.NewReader(data)}
}

func (r *trackingStorifaceReader) Close() error {
	r.closeCount.Add(1)
	return nil
}

func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func hiddenField(v reflect.Value, name string) reflect.Value {
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	field := v.FieldByName(name)
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
}

func setCacheTTL(t *testing.T, cpr *cachedreader.CachedPieceReader, cacheFieldName string, ttl time.Duration) {
	t.Helper()

	cacheField := hiddenField(reflect.ValueOf(cpr), cacheFieldName)
	ttlCacheField := hiddenField(cacheField, "cache")
	results := ttlCacheField.MethodByName("SetTTL").Call([]reflect.Value{reflect.ValueOf(ttl)})
	if len(results) == 1 && !results[0].IsNil() {
		t.Fatalf("set cache ttl: %v", results[0].Interface())
	}
}

func cacheSetEntry(t *testing.T, cpr *cachedreader.CachedPieceReader, cacheFieldName string, pieceCID cid.Cid, value reflect.Value) {
	t.Helper()

	cacheField := hiddenField(reflect.ValueOf(cpr), cacheFieldName)
	cacheField.MethodByName("Set").Call([]reflect.Value{reflect.ValueOf(pieceCID), value})
}

func cacheGetEntry(t *testing.T, cpr *cachedreader.CachedPieceReader, cacheFieldName string, pieceCID cid.Cid) (any, bool) {
	t.Helper()

	cacheField := hiddenField(reflect.ValueOf(cpr), cacheFieldName)
	results := cacheField.MethodByName("Get").Call([]reflect.Value{reflect.ValueOf(pieceCID)})
	found := results[1].Bool()
	if !found {
		return nil, false
	}
	return results[0].Interface(), true
}

func cachedSectionReaderReflectType(t *testing.T, ctx context.Context, cpr *cachedreader.CachedPieceReader, pieceCID cid.Cid) reflect.Type {
	t.Helper()

	reader, _, err := cpr.GetSharedPieceReader(ctx, pieceCID, true)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	entry, found := cacheGetEntry(t, cpr, "pieceReaderCache", pieceCID)
	require.True(t, found)
	return reflect.TypeOf(entry)
}

func newCachedSectionReaderValue(
	t *testing.T,
	readerType reflect.Type,
	cpr *cachedreader.CachedPieceReader,
	pieceCID cid.Cid,
	reader storiface.Reader,
	rawSize uint64,
	ready chan struct{},
	err error,
	cancel func(),
	refs int,
	expired bool,
) reflect.Value {
	t.Helper()

	entry := reflect.New(readerType.Elem())
	if reader == nil {
		hiddenField(entry, "reader").Set(reflect.Zero(hiddenField(entry, "reader").Type()))
	} else {
		hiddenField(entry, "reader").Set(reflect.ValueOf(reader))
	}
	hiddenField(entry, "cpr").Set(reflect.ValueOf(cpr))
	hiddenField(entry, "pieceCid").Set(reflect.ValueOf(pieceCID))
	hiddenField(entry, "rawSize").SetUint(rawSize)
	hiddenField(entry, "ready").Set(reflect.ValueOf(ready))
	if err == nil {
		hiddenField(entry, "err").Set(reflect.Zero(hiddenField(entry, "err").Type()))
	} else {
		hiddenField(entry, "err").Set(reflect.ValueOf(err))
	}
	if cancel == nil {
		hiddenField(entry, "cancel").Set(reflect.Zero(hiddenField(entry, "cancel").Type()))
	} else {
		hiddenField(entry, "cancel").Set(reflect.ValueOf(cancel))
	}
	hiddenField(entry, "refs").SetInt(int64(refs))
	hiddenField(entry, "expired").SetBool(expired)

	return entry
}

func hiddenIntField(v reflect.Value, name string) int {
	return int(hiddenField(v, name).Int())
}

func hiddenBoolField(v reflect.Value, name string) bool {
	return hiddenField(v, name).Bool()
}

func callHiddenClose(v reflect.Value) error {
	results := v.MethodByName("Close").Call(nil)
	if len(results) == 1 && !results[0].IsNil() {
		return results[0].Interface().(error)
	}
	return nil
}

func allZeroBytes(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}
