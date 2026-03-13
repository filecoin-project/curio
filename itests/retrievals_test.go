package itests

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math/bits"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-units"
	"github.com/gbrlsnchs/jwt/v3"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-data-segment/datasegment"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/cmd/curio/rpc"
	"github.com/filecoin-project/curio/cmd/curio/tasks"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/itests/helpers"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/tasks/indexing"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	miner2 "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/cli/spcli/createminer"
	"github.com/filecoin-project/lotus/itests/kit"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/storage/sealer/fr32"
)

type pieceFixture struct {
	RootCID    cid.Cid
	CarBytes   []byte
	PieceCIDV1 cid.Cid
	PieceCIDV2 cid.Cid
	PieceSize  abi.PaddedPieceSize
	RawSize    int64
}

func TestRetrievals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	full, miner, ensemble := kit.EnsembleMinimal(t,
		kit.LatestActorsAt(-1),
		kit.PresealSectors(2),
		kit.ThroughRPC(),
	)
	ensemble.Start()
	ensemble.BeginMining(100 * time.Millisecond)
	full.WaitTillChain(ctx, kit.HeightAtLeast(12))

	require.NoError(t, miner.LogSetLevel(ctx, "*", "ERROR"))
	require.NoError(t, full.LogSetLevel(ctx, "*", "ERROR"))

	token, err := full.AuthNew(ctx, lapi.AllPermissions)
	require.NoError(t, err)
	fapi := fmt.Sprintf("%s:%s", string(token), full.ListenAddr)

	sharedITestID := harmonydb.ITestNewID()
	db, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID, true)
	require.NoError(t, err)
	defer db.ITestDeleteAll()

	idxStore, err := indexstore.NewIndexStore([]string{helpers.EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, 9042, config.DefaultCurioConfig())
	require.NoError(t, err)
	require.NoError(t, idxStore.Start(ctx, true))

	addr := miner.OwnerKey.Address
	sectorSizeInt, err := units.RAMInBytes("2KiB")
	require.NoError(t, err)

	maddr, err := createminer.CreateStorageMiner(ctx, full, addr, addr, addr, abi.SectorSize(sectorSizeInt), 1, 1.0)
	require.NoError(t, err)
	require.NoError(t, deps.CreateMinerConfig(ctx, full, db, []string{maddr.String()}, fapi))

	baseCfg := config.DefaultCurioConfig()
	var baseText string
	require.NoError(t, db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText))
	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
	require.NoError(t, err)

	httpAddr := helpers.FreeListenAddr(t)
	baseCfg.Subsystems.EnableDealMarket = true
	baseCfg.HTTP.Enable = true
	baseCfg.HTTP.DelegateTLS = true
	baseCfg.HTTP.DomainName = "localhost"
	baseCfg.HTTP.ListenAddress = httpAddr
	baseCfg.HTTP.DenylistServers = config.NewDynamic([]string{})
	baseCfg.Batching.PreCommit.Timeout = time.Second
	baseCfg.Batching.Commit.Timeout = time.Second

	cb, err := config.ConfigUpdate(baseCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO harmony_config (title, config) VALUES ($1, $2) ON CONFLICT (title) DO UPDATE SET config = $2`, "base", string(cb))
	require.NoError(t, err)

	temp := os.TempDir()
	dir, err := os.MkdirTemp(temp, "curio-retrieval-itest")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, storiface.FTUnsealed.String()), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, storiface.FTPiece.String()), 0o755))

	mk12Fixture := createPieceFixture(t, dir, 512)
	aggregateSubpieceFixture := createPieceFixture(t, dir, 127)
	aggregateSiblingFixture := createPieceFixture(t, dir, 127)
	aggregateFixture, aggregateSubPieces := createAggregateFixtureFromSubpieces(t, []pieceFixture{
		aggregateSubpieceFixture,
		aggregateSiblingFixture,
	})
	parkNoDealFixture := createPieceFixture(t, dir, 320)
	parkWithDealFixture := createPieceFixture(t, dir, 384)

	mid, err := address.IDFromAddress(maddr)
	require.NoError(t, err)
	mi, err := full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)
	nv, err := full.StateNetworkVersion(ctx, types.EmptyTSK)
	require.NoError(t, err)
	sealProof, err := miner2.PreferredSealProofTypeFromWindowPoStType(nv, mi.WindowPoStProofType, false)
	require.NoError(t, err)
	sectorSize, err := sealProof.SectorSize()
	require.NoError(t, err)

	// MK12-style fixture: read from a declared unsealed sector and reachable through both piece CID v1 and v2.
	sectorNum := abi.SectorNumber(199)
	require.NoError(t, writeUnsealedSectorFixture(dir, abi.ActorID(mid), sectorNum, abi.SectorSize(sectorSize), mk12Fixture))
	_, err = db.Exec(ctx, `INSERT INTO sectors_meta (
		sp_id, sector_num, reg_seal_proof, ticket_epoch, ticket_value,
		orig_sealed_cid, orig_unsealed_cid, cur_sealed_cid, cur_unsealed_cid,
		seed_epoch, seed_value
	) VALUES ($1, $2, $3, 0, $4, $5, $6, $5, $6, 0, $4)
	ON CONFLICT (sp_id, sector_num) DO NOTHING`,
		mid, int64(sectorNum), sealProof, []byte{0}, mk12Fixture.PieceCIDV1.String(), mk12Fixture.PieceCIDV1.String())
	require.NoError(t, err)
	_, err = db.Exec(ctx, `SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		"mk12-retrieval-itest",
		mk12Fixture.PieceCIDV1.String(),
		true,
		mid,
		int64(sectorNum),
		int64(0),
		int64(mk12Fixture.PieceSize),
		mk12Fixture.RawSize,
		true,
		nil,
		false,
		int64(0),
	)
	require.NoError(t, err)
	require.NoError(t, addIndexFromCAR(ctx, idxStore, mk12Fixture.PieceCIDV2, mk12Fixture.CarBytes))

	// Aggregate retrieval fixture: the subpiece has no direct deal and must be served
	// from an aggregate piece via indexstore aggregate mappings.
	aggregateSectorNum := abi.SectorNumber(200)
	require.NoError(t, writeUnsealedSectorFixture(dir, abi.ActorID(mid), aggregateSectorNum, abi.SectorSize(sectorSize), aggregateFixture))
	_, err = db.Exec(ctx, `INSERT INTO sectors_meta (
		sp_id, sector_num, reg_seal_proof, ticket_epoch, ticket_value,
		orig_sealed_cid, orig_unsealed_cid, cur_sealed_cid, cur_unsealed_cid,
		seed_epoch, seed_value
	) VALUES ($1, $2, $3, 0, $4, $5, $6, $5, $6, 0, $4)
	ON CONFLICT (sp_id, sector_num) DO NOTHING`,
		mid, int64(aggregateSectorNum), sealProof, []byte{0}, aggregateFixture.PieceCIDV1.String(), aggregateFixture.PieceCIDV1.String())
	require.NoError(t, err)
	_, err = db.Exec(ctx, `SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		"mk12-aggregate-itest",
		aggregateFixture.PieceCIDV1.String(),
		true,
		mid,
		int64(aggregateSectorNum),
		int64(0),
		int64(aggregateFixture.PieceSize),
		aggregateFixture.RawSize,
		true,
		nil,
		false,
		int64(0),
	)
	require.NoError(t, err)
	require.NoError(t, addAggregateIndexFromPiece(t, ctx, idxStore, aggregateFixture, aggregateSubPieces))

	// Park-only fixture: no market_piece_deal binding.
	var parkOnlyPieceID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES ($1, $2, $3, TRUE, TRUE)
		RETURNING id`,
		parkNoDealFixture.PieceCIDV1.String(),
		int64(parkNoDealFixture.PieceSize),
		parkNoDealFixture.RawSize,
	).Scan(&parkOnlyPieceID))
	require.NoError(t, writeParkedPieceFixture(dir, parkOnlyPieceID, parkNoDealFixture.CarBytes))

	// Parked piece with deal binding (ULID id + piece_ref).
	var parkWithDealPieceID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES ($1, $2, $3, TRUE, TRUE)
		RETURNING id`,
		parkWithDealFixture.PieceCIDV1.String(),
		int64(parkWithDealFixture.PieceSize),
		parkWithDealFixture.RawSize,
	).Scan(&parkWithDealPieceID))
	require.NoError(t, writeParkedPieceFixture(dir, parkWithDealPieceID, parkWithDealFixture.CarBytes))
	var pieceRefID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
		VALUES ($1, $2, $3::jsonb, TRUE)
		RETURNING ref_id`,
		parkWithDealPieceID, "", "{}",
	).Scan(&pieceRefID))
	_, err = db.Exec(ctx, `SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		parkWithDealFixture.PieceCIDV1.String(),
		false,
		int64(-1),
		int64(-1),
		nil,
		int64(parkWithDealFixture.PieceSize),
		parkWithDealFixture.RawSize,
		true,
		pieceRefID,
		false,
		int64(0),
	)
	require.NoError(t, err)

	capi, dependencies, engineTerm, closure, finishCh := constructCurioWithMarketDeps(ctx, t, dir, db, idxStore, full, baseCfg)
	defer engineTerm()
	defer closure()
	defer func() {
		_ = capi.Shutdown(context.Background())
		<-finishCh
	}()

	baseURL := "http://" + httpAddr
	waitForHTTP(t, baseURL)

	t.Run("mk12 piece retrieval by pieceCIDv1", func(t *testing.T) {
		status, body, headers := httpGetWithHeaders(t, baseURL, "/piece/"+mk12Fixture.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, mk12Fixture.CarBytes, body)
		assertPieceResponseHeaders(t, headers, mk12Fixture.PieceCIDV1.String(), len(mk12Fixture.CarBytes))
	})

	t.Run("mk12 piece retrieval by pieceCIDv2", func(t *testing.T) {
		status, body, headers := httpGetWithHeaders(t, baseURL, "/piece/"+mk12Fixture.PieceCIDV2.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, mk12Fixture.CarBytes, body)
		assertPieceResponseHeaders(t, headers, mk12Fixture.PieceCIDV2.String(), len(mk12Fixture.CarBytes))
	})

	t.Run("ipfs style retrieval", func(t *testing.T) {
		status, body, headers := httpGetWithHeaders(t, baseURL, "/ipfs/"+mk12Fixture.RootCID.String(), map[string]string{
			"Accept": "application/vnd.ipld.car",
		})
		require.Equal(t, http.StatusOK, status)
		assertIPFSCarResponseHeaders(t, headers)

		br, err := carv2.NewBlockReader(bytes.NewReader(body))
		require.NoError(t, err)
		require.Contains(t, br.Roots, mk12Fixture.RootCID)
	})

	t.Run("aggregate retrieval by subpieceCIDv2", func(t *testing.T) {
		status, body, headers := httpGetWithHeaders(t, baseURL, "/piece/"+aggregateSubpieceFixture.PieceCIDV2.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, aggregateSubpieceFixture.CarBytes, body)
		assertPieceResponseHeaders(t, headers, aggregateSubpieceFixture.PieceCIDV2.String(), len(aggregateSubpieceFixture.CarBytes))
	})

	t.Run("parkpiece retrieval false and no-retrieval endpoint behavior", func(t *testing.T) {
		// /piece endpoint always uses retrieval=true, so this parked-only piece must be 404.
		status, _ := httpGet(t, baseURL, "/piece/"+parkNoDealFixture.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusNotFound, status)

		cprTrue := cachedreader.NewCachedPieceReader(
			db,
			dependencies.SectorReader,
			pieceprovider.NewPieceParkReader(dependencies.Stor, dependencies.Si),
			idxStore,
		)
		_, _, err := cprTrue.GetSharedPieceReader(ctx, parkNoDealFixture.PieceCIDV1, true)
		require.Error(t, err)
		require.ErrorIs(t, err, cachedreader.ErrNoDeal)

		cprFalse := cachedreader.NewCachedPieceReader(
			db,
			dependencies.SectorReader,
			pieceprovider.NewPieceParkReader(dependencies.Stor, dependencies.Si),
			idxStore,
		)
		r, sz, err := cprFalse.GetSharedPieceReader(ctx, parkNoDealFixture.PieceCIDV1, false)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()
		require.Equal(t, uint64(parkNoDealFixture.RawSize), sz)
		got, err := io.ReadAll(r)
		require.NoError(t, err)
		require.Equal(t, parkNoDealFixture.CarBytes, got)
	})

	t.Run("parkpiece with deal binding serves piece retrieval", func(t *testing.T) {
		status, body, headers := httpGetWithHeaders(t, baseURL, "/piece/"+parkWithDealFixture.PieceCIDV1.String(), nil)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, parkWithDealFixture.CarBytes, body)
		assertPieceResponseHeaders(t, headers, parkWithDealFixture.PieceCIDV1.String(), len(parkWithDealFixture.CarBytes))
	})
}

func constructCurioWithMarketDeps(ctx context.Context, t *testing.T, dir string, db *harmonydb.DB, idx *indexstore.IndexStore, full v1api.FullNode, cfg *config.CurioConfig) (api.Curio, *deps.Deps, func(), jsonrpc.ClientCloser, <-chan struct{}) {
	ffiselect.IsTest = true

	cctx, err := helpers.CreateMarketCliContext(dir)
	require.NoError(t, err)

	shutdownChan := make(chan struct{})
	{
		var ctxclose func()
		ctx, ctxclose = context.WithCancel(ctx)
		go func() {
			<-shutdownChan
			ctxclose()
		}()
	}

	dependencies := &deps.Deps{
		DB:         db,
		Chain:      full,
		IndexStore: idx,
	}
	require.NoError(t, os.Setenv("CURIO_REPO_PATH", dir))
	require.NoError(t, dependencies.PopulateRemainingDeps(ctx, cctx, false))

	taskEngine, err := tasks.StartTasks(ctx, dependencies, shutdownChan)
	require.NoError(t, err)

	go func() {
		require.NoError(t, rpc.ListenAndServe(ctx, dependencies, shutdownChan))
	}()

	finishCh := node.MonitorShutdown(shutdownChan)

	var machines []string
	require.NoError(t, db.Select(ctx, &machines, `select host_and_port from harmony_machines`))
	require.Len(t, machines, 1)

	laddr, err := net.ResolveTCPAddr("tcp", machines[0])
	require.NoError(t, err)
	ma, err := manet.FromNetAddr(laddr)
	require.NoError(t, err)

	var apiToken []byte
	{
		type jwtPayload struct {
			Allow []auth.Permission
		}
		p := jwtPayload{Allow: lapi.AllPermissions}
		sk, err := base64.StdEncoding.DecodeString(cfg.Apis.StorageRPCSecret)
		require.NoError(t, err)
		apiToken, err = jwt.Sign(&p, jwt.NewHS256(sk))
		require.NoError(t, err)
	}

	ctoken := fmt.Sprintf("%s:%s", string(apiToken), ma)
	require.NoError(t, os.Setenv("CURIO_API_INFO", ctoken))

	capi, ccloser, err := rpc.GetCurioAPI(&cli.Context{})
	require.NoError(t, err)

	scfg := storiface.LocalStorageMeta{
		ID:         storiface.ID(uuid.New().String()),
		Weight:     10,
		CanSeal:    true,
		CanStore:   true,
		MaxStorage: 0,
		Groups:     []string{},
		AllowTo:    []string{},
	}
	require.NoError(t, capi.StorageInit(ctx, dir, scfg))
	require.NoError(t, capi.StorageAddLocal(ctx, dir))

	//_ = logging.SetLogLevel("harmonytask", "ERROR")
	//_ = logging.SetLogLevel("storage-market", "ERROR")

	return capi, dependencies, taskEngine.GracefullyTerminate, ccloser, finishCh
}

func createPieceFixture(t *testing.T, dir string, sourceSize int64) pieceFixture {
	t.Helper()

	srcPath, err := testutils.CreateRandomTmpFile(dir, sourceSize)
	require.NoError(t, err)

	root, carPath, err := testutils.CreateDenseCARWith(dir, srcPath, 64, 8, []carv2.Option{
		blockstore.WriteAsCarV1(true),
	})
	require.NoError(t, err)

	carBytes, err := os.ReadFile(carPath)
	require.NoError(t, err)

	return createRawPieceFixture(t, carBytes, root)
}

func createRawPieceFixture(t *testing.T, raw []byte, root cid.Cid) pieceFixture {
	t.Helper()

	wr := new(commp.Calc)
	defer wr.Reset()

	n, err := wr.Write(raw)
	require.NoError(t, err)
	digest, paddedPieceSize, err := wr.Digest()
	require.NoError(t, err)

	pieceCIDV1, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)
	pieceCIDV2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(n))
	require.NoError(t, err)

	return pieceFixture{
		RootCID:    root,
		CarBytes:   raw,
		PieceCIDV1: pieceCIDV1,
		PieceCIDV2: pieceCIDV2,
		PieceSize:  abi.PaddedPieceSize(paddedPieceSize),
		RawSize:    int64(n),
	}
}

func createAggregateFixtureFromSubpieces(t *testing.T, subpieces []pieceFixture) (pieceFixture, []mk20.DataSource) {
	t.Helper()

	require.GreaterOrEqual(t, len(subpieces), 2)

	deals := make([]abi.PieceInfo, 0, len(subpieces))
	readers := make([]io.Reader, 0, len(subpieces))
	subSources := make([]mk20.DataSource, 0, len(subpieces))

	for _, sp := range subpieces {
		deals = append(deals, abi.PieceInfo{
			PieceCID: sp.PieceCIDV1,
			Size:     sp.PieceSize,
		})
		readers = append(readers, io.LimitReader(bytes.NewReader(sp.CarBytes), sp.RawSize))
		subSources = append(subSources, mk20.DataSource{
			PieceCID: sp.PieceCIDV2,
			Format: mk20.PieceDataFormat{
				Car: &mk20.FormatCar{},
			},
		})
	}

	_, aggregatedRawSize, err := datasegment.ComputeDealPlacement(deals)
	require.NoError(t, err)

	overallSize := abi.PaddedPieceSize(aggregatedRawSize)
	next := 1 << (64 - bits.LeadingZeros64(uint64(overallSize+256)))

	aggr, err := datasegment.NewAggregate(abi.PaddedPieceSize(next), deals)
	require.NoError(t, err)

	outR, err := aggr.AggregateObjectReader(readers)
	require.NoError(t, err)

	aggregateRaw, err := io.ReadAll(outR)
	require.NoError(t, err)

	fixture := createRawPieceFixture(t, aggregateRaw, cid.Undef)
	require.Equal(t, abi.PaddedPieceSize(next), fixture.PieceSize)

	return fixture, subSources
}

func addIndexFromCAR(ctx context.Context, idx *indexstore.IndexStore, pieceCID cid.Cid, carBytes []byte) error {
	recs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})

	var eg errgroup.Group
	eg.Go(func() error {
		return idx.AddIndex(ctx, pieceCID, recs)
	})

	_, interrupted, idxErr := indexing.IndexCAR(bytes.NewReader(carBytes), 4<<20, recs, addFail)
	close(recs)

	addErr := eg.Wait()
	if idxErr != nil {
		return idxErr
	}
	if addErr != nil {
		return addErr
	}
	if interrupted {
		return fmt.Errorf("indexing was interrupted while adding piece %s", pieceCID)
	}
	return nil
}

func addAggregateIndexFromPiece(t *testing.T, ctx context.Context, idx *indexstore.IndexStore, aggregate pieceFixture, subPieces []mk20.DataSource) error {
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
		fmt.Println("adding aggregate index", k, v)
		if err := idx.InsertAggregateIndex(ctx, k, v); err != nil {
			return fmt.Errorf("inserting aggregate index for %s: %w", k, err)
		}
		for i := range v {
			pieces, err := idx.FindPieceInAggregate(ctx, v[i].Cid)
			require.NoError(t, err)
			require.Len(t, pieces, 1)
			require.True(t, aggregate.PieceCIDV2.Equals(pieces[0].Cid))
		}
	}

	return nil
}

func writeUnsealedSectorFixture(dir string, miner abi.ActorID, sector abi.SectorNumber, sectorSize abi.SectorSize, fixture pieceFixture) error {
	if fixture.PieceSize > abi.PaddedPieceSize(sectorSize) {
		return fmt.Errorf("fixture piece too large for sector: piece=%d sector=%d", fixture.PieceSize, sectorSize)
	}

	// Unsealed files are FR32 padded; write the piece at offset 0 and zero-fill the rest of the sector.
	paddedPiece := fr32PadFixture(fixture.CarBytes, fixture.PieceSize)
	sectorData := make([]byte, int(sectorSize))
	copy(sectorData, paddedPiece)

	sectorPath := filepath.Join(
		dir,
		storiface.FTUnsealed.String(),
		storiface.SectorName(abi.SectorID{Miner: miner, Number: sector}),
	)
	return os.WriteFile(sectorPath, sectorData, 0o644)
}

func fr32PadFixture(raw []byte, pieceSize abi.PaddedPieceSize) []byte {
	unpaddedLen := int(pieceSize.Unpadded())
	in := make([]byte, unpaddedLen)
	copy(in, raw)

	out := make([]byte, int(pieceSize))
	inOff := 0
	outOff := 0
	for inOff < len(in) {
		fr32.Pad(in[inOff:inOff+int(fr32.UnpaddedFr32Chunk)], out[outOff:outOff+int(fr32.PaddedFr32Chunk)])
		inOff += int(fr32.UnpaddedFr32Chunk)
		outOff += int(fr32.PaddedFr32Chunk)
	}
	return out
}

func writeParkedPieceFixture(dir string, pieceID int64, data []byte) error {
	path := filepath.Join(
		dir,
		storiface.FTPiece.String(),
		storiface.SectorName(storiface.PieceNumber(pieceID).Ref().ID),
	)
	return os.WriteFile(path, data, 0o644)
}

func waitForHTTP(t *testing.T, baseURL string) {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	require.Eventually(t, func() bool {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/health", nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode == http.StatusOK
	}, 45*time.Second, 250*time.Millisecond)
}

func assertPieceResponseHeaders(t *testing.T, headers http.Header, pieceCID string, expectedBodyLen int) {
	t.Helper()

	require.Equal(t, "Accept-Encoding", headers.Get("Vary"))
	require.Equal(t, "public, max-age=29030400, immutable", headers.Get("Cache-Control"))
	require.Equal(t, pieceCID, headers.Get("Etag"))
	require.Equal(t, strconv.Itoa(expectedBodyLen), headers.Get("Content-Length"))
	require.NotEmpty(t, headers.Get("Content-Type"))
	require.Equal(t, "bytes", headers.Get("Accept-Ranges"))
}

func assertIPFSCarResponseHeaders(t *testing.T, headers http.Header) {
	require.Contains(t, headers.Get("Content-Type"), "application/vnd.ipld.car")
	require.Contains(t, strings.ToLower(headers.Get("Content-Disposition")), "attachment")
	require.NotEmpty(t, headers.Get("Etag"))
}

func httpGet(t *testing.T, baseURL, path string, headers map[string]string) (int, []byte) {
	t.Helper()

	status, body, _ := httpGetWithHeaders(t, baseURL, path, headers)
	return status, body
}

func httpGetWithHeaders(t *testing.T, baseURL, path string, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Accept-Encoding", "identity")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body, resp.Header.Clone()
}
