package itests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	carv2 "github.com/ipld/go-car/v2"
	"github.com/stretchr/testify/require"

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
	seedRetrievalFixtures(t, ctx, dir, db, idxStore, spID, minerID, sealProof, sectorSize, seedPlan, fixtures)

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
	runRetrievalScenarios(t, ctx, db, idxStore, dependencies, baseURL, fixtures)
}

type retrievalFixtureSeed struct {
	DealID             string
	SectorNum          abi.SectorNumber
	Fixture            helpers.PieceFixture
	IsAggregate        bool
	AggregateSubPieces []mk20.DataSource
}

type retrievalFixtures struct {
	mk12               helpers.PieceFixture
	aggregateSubpiece  helpers.PieceFixture
	aggregate          helpers.PieceFixture
	aggregateSubPieces []mk20.DataSource
	denylisted         helpers.PieceFixture
	parkNoDeal         helpers.PieceFixture
	parkWithDeal       helpers.PieceFixture
}

type retrievalParkedPieceIDs struct {
	parkOnlyPieceID     int64
	parkWithDealPieceID int64
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

	return retrievalFixtures{
		mk12:               mk12Fixture,
		aggregateSubpiece:  aggregateSubpieceFixture,
		aggregate:          aggregateFixture,
		aggregateSubPieces: aggregateSubPieces,
		denylisted:         helpers.CreatePieceFixture(t, dir, 448),
		parkNoDeal:         helpers.CreatePieceFixture(t, dir, 320),
		parkWithDeal:       helpers.CreatePieceFixture(t, dir, 384),
	}
}

func buildRetrievalSeedPlan(fixtures retrievalFixtures) []retrievalFixtureSeed {
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
			IsAggregate:        true,
			AggregateSubPieces: fixtures.aggregateSubPieces,
		},
		{
			DealID:    "mk12-denylist-itest",
			SectorNum: abi.SectorNumber(201),
			Fixture:   fixtures.denylisted,
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
) {
	t.Helper()

	for _, seed := range seeds {
		require.NoError(t, helpers.WriteUnsealedSectorFixture(dir, minerID, seed.SectorNum, sectorSize, seed.Fixture))
	}

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

	for _, seed := range seeds {
		if seed.IsAggregate {
			require.NoError(t, helpers.AddAggregateIndexFromPiece(t, ctx, idxStore, seed.Fixture, seed.AggregateSubPieces))
			continue
		}
		require.NoError(t, helpers.AddIndexFromCAR(ctx, idxStore, seed.Fixture.PieceCIDV2, seed.Fixture.CarBytes))
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

	return retrievalParkedPieceIDs{
		parkOnlyPieceID:     parkOnlyPieceID,
		parkWithDealPieceID: parkWithDealPieceID,
	}, nil
}

func runRetrievalScenarios(
	t *testing.T,
	ctx context.Context,
	db *harmonydb.DB,
	idxStore *indexstore.IndexStore,
	dependencies *deps.Deps,
	baseURL string,
	fixtures retrievalFixtures,
) {
	t.Helper()

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
}

func seedRetrievalFixtureTx(
	tx *harmonydb.Tx,
	spID int64,
	sealProof abi.RegisteredSealProof,
	seed retrievalFixtureSeed,
) error {
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
		RawSize:       seed.Fixture.RawSize,
		FastRetrieval: true,
		PieceRefID:    nil,
		LegacyDeal:    false,
		LegacyDealID:  int64(0),
	})
}
