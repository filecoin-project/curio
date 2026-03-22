package ittestgroup6

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/go-units"
	"github.com/gbrlsnchs/jwt/v3"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/oklog/ulid"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
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
	"github.com/filecoin-project/curio/itest/helpers"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/tasks/seal"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	miner2 "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/cli/spcli/createminer"
	"github.com/filecoin-project/lotus/itests/kit"
	"github.com/filecoin-project/lotus/node"
)

// sealPollStatus is used to poll the sealing pipeline state.
type sealPollStatus struct {
	SpID                     int64  `db:"sp_id"`
	SectorNumber             int64  `db:"sector_number"`
	AfterSDR                 bool   `db:"after_sdr"`
	AfterTreeD               bool   `db:"after_tree_d"`
	AfterTreeC               bool   `db:"after_tree_c"`
	AfterTreeR               bool   `db:"after_tree_r"`
	AfterSynth               bool   `db:"after_synth"`
	AfterPrecommitMsg        bool   `db:"after_precommit_msg"`
	AfterPrecommitMsgSuccess bool   `db:"after_precommit_msg_success"`
	AfterPoRep               bool   `db:"after_porep"`
	AfterFinalize            bool   `db:"after_finalize"`
	AfterMoveStorage         bool   `db:"after_move_storage"`
	AfterCommitMsg           bool   `db:"after_commit_msg"`
	AfterCommitMsgSuccess    bool   `db:"after_commit_msg_success"`
	Failed                   bool   `db:"failed"`
	FailedReason             string `db:"failed_reason"`
}

// dealVariant describes one deal configuration to test through the full pipeline.
type dealVariant struct {
	name        string
	mk20        bool
	shouldIndex bool
	announce    bool
	isDDO       bool
	offline     bool

	// filled during setup
	fixture       pieceFixture
	sectorNumber  abi.SectorNumber
	parkedPieceID int64
	pieceRefID    int64
	dealUUID      string
}

type pieceFixture struct {
	RootCID    cid.Cid
	CarBytes   []byte
	PieceCIDV1 cid.Cid
	PieceCIDV2 cid.Cid
	PieceSize  abi.PaddedPieceSize
	RawSize    int64
}

// TestDealPipelineVariants tests the full deal pipeline from sealing through indexing
// to completion for various deal configurations:
//   - MK12 online deals (with/without indexing)
//   - MK12 DDO deals (with/without indexing)
//   - MK20 online deals (with/without indexing)
//   - MK20 offline deals (with/without indexing)
//
// This is a regression test for the LEFT JOIN fix in the indexing task's CanAccept query.
// When should_index=false, deals without unsealed sector copies must still be picked up
// by the indexing task. Before the fix, the JOIN on sector_location filtered them out.
func TestDealPipelineVariants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// ── 1. Lotus ensemble ──

	full, miner, ensemble := kit.EnsembleMinimal(t,
		kit.LatestActorsAt(-1),
		kit.PresealSectors(32),
		kit.ThroughRPC(),
	)
	ensemble.Start()
	ensemble.BeginMining(100 * time.Millisecond)
	full.WaitTillChain(ctx, kit.HeightAtLeast(15))

	require.NoError(t, miner.LogSetLevel(ctx, "*", "ERROR"))
	require.NoError(t, full.LogSetLevel(ctx, "*", "ERROR"))

	token, err := full.AuthNew(ctx, lapi.AllPermissions)
	require.NoError(t, err)
	fapi := fmt.Sprintf("%s:%s", string(token), full.ListenAddr)

	// ── 2. HarmonyDB ──

	sharedITestID := harmonydb.ITestNewID()
	t.Logf("sharedITestID: %s", sharedITestID)
	db, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID, true)
	require.NoError(t, err)
	defer db.ITestDeleteAll()

	// ── 3. Index Store ──

	idxStore, err := indexstore.NewIndexStore([]string{helpers.IndexstoreHost()}, 9042, config.DefaultCurioConfig())
	require.NoError(t, err)
	require.NoError(t, idxStore.Start(ctx, true))

	// ── 4. Miner ──

	addr := miner.OwnerKey.Address
	sectorSizeInt, err := units.RAMInBytes("2KiB")
	require.NoError(t, err)
	maddr, err := createminer.CreateStorageMiner(ctx, full, addr, addr, addr, abi.SectorSize(sectorSizeInt), 0, 1.0)
	require.NoError(t, err)
	require.NoError(t, deps.CreateMinerConfig(ctx, full, db, []string{maddr.String()}, fapi))
	t.Logf("Created miner: %s", maddr)

	// ── 5. Config: enable market, disable IPNI ──

	baseCfg := config.DefaultCurioConfig()
	var baseText string
	require.NoError(t, db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText))
	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
	require.NoError(t, err)

	baseCfg.Subsystems.EnableDealMarket = true
	baseCfg.HTTP.Enable = true
	baseCfg.HTTP.DelegateTLS = true
	baseCfg.HTTP.DomainName = "localhost"
	baseCfg.HTTP.ListenAddress = helpers.FreeListenAddr(t)
	baseCfg.HTTP.DenylistServers = config.NewDynamic([]string{})
	baseCfg.Batching.PreCommit.Timeout = time.Second
	baseCfg.Batching.Commit.Timeout = time.Second
	baseCfg.Market.StorageMarketConfig.IPNI.Disable = true // IPNI disabled: indexing task sets complete=TRUE directly

	cb, err := config.ConfigUpdate(baseCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO harmony_config (title, config) VALUES ($1, $2) ON CONFLICT (title) DO UPDATE SET config = $2`, "base", string(cb))
	require.NoError(t, err)

	// ── 6. Start Curio ──

	dir, err := os.MkdirTemp(os.TempDir(), "curio-deal-pipeline")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	capi, engineTerm, closure, finishCh := constructCurioWithMarketAndSeal(ctx, t, dir, db, idxStore, full, maddr, baseCfg)
	defer engineTerm()
	defer closure()

	mid, err := address.IDFromAddress(maddr)
	require.NoError(t, err)

	mi, err := full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)
	nv, err := full.StateNetworkVersion(ctx, types.EmptyTSK)
	require.NoError(t, err)
	wpt := mi.WindowPoStProofType
	spt, err := miner2.PreferredSealProofTypeFromWindowPoStType(nv, wpt, false)
	require.NoError(t, err)

	// ── 7. Define deal variants ──

	variants := []dealVariant{
		{name: "mk12-online-index", shouldIndex: true, announce: false},
		{name: "mk12-online-noindex", shouldIndex: false, announce: false},
		{name: "mk12-ddo-index", shouldIndex: true, announce: false, isDDO: true},
		{name: "mk12-ddo-noindex", shouldIndex: false, announce: false, isDDO: true},
		{name: "mk20-online-index", mk20: true, shouldIndex: true, announce: false},
		{name: "mk20-online-noindex", mk20: true, shouldIndex: false, announce: false},
		{name: "mk20-offline-index", mk20: true, shouldIndex: true, announce: false, offline: true},
		{name: "mk20-offline-noindex", mk20: true, shouldIndex: false, announce: false, offline: true},
	}

	// ── 8. Create piece fixtures ──

	for i := range variants {
		v := &variants[i]
		v.fixture = createPieceFixture(t, dir, 200)
		if v.mk20 {
			v.dealUUID = ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
		} else {
			v.dealUUID = uuid.New().String()
		}
		t.Logf("Variant %q: uuid=%s pieceCIDv1=%s pieceSize=%d rawSize=%d",
			v.name, v.dealUUID, v.fixture.PieceCIDV1, v.fixture.PieceSize, v.fixture.RawSize)
	}

	// ── 9. Set up sectors, parked pieces, and pipeline entries ──

	for i := range variants {
		v := &variants[i]

		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// Allocate sector number
			nums, err := seal.AllocateSectorNumbers(ctx, full, tx, maddr, 1)
			if err != nil {
				return false, xerrors.Errorf("allocating sector numbers: %w", err)
			}
			if len(nums) != 1 {
				return false, xerrors.Errorf("expected 1 sector number, got %d", len(nums))
			}
			v.sectorNumber = nums[0]

			// Create sector in sealing pipeline
			_, err = tx.Exec(`INSERT INTO sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) VALUES ($1, $2, $3)`,
				mid, v.sectorNumber, spt)
			if err != nil {
				return false, xerrors.Errorf("inserting sector: %w", err)
			}

			// Create parked piece (complete=true so sealing can read it immediately)
			err = tx.QueryRow(`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
				VALUES ($1, $2, $3, TRUE, TRUE) RETURNING id`,
				v.fixture.PieceCIDV1.String(), v.fixture.PieceSize, v.fixture.RawSize).Scan(&v.parkedPieceID)
			if err != nil {
				return false, xerrors.Errorf("inserting parked piece: %w", err)
			}

			// Create parked piece reference
			err = tx.QueryRow(`INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
				VALUES ($1, '', '{}', TRUE) RETURNING ref_id`,
				v.parkedPieceID).Scan(&v.pieceRefID)
			if err != nil {
				return false, xerrors.Errorf("inserting parked piece ref: %w", err)
			}

			piecerefURL := fmt.Sprintf("pieceref:%d", v.pieceRefID)

			// Insert piece into sector initial pieces.
			// data_delete_on_finalize: when true, the unsealed sector copy is NOT kept after finalize.
			// For should_index=false deals, we set data_delete_on_finalize=true (no unsealed copy kept).
			// This is the key regression scenario: no sector_location entry for unsealed => LEFT JOIN needed.
			_, err = tx.Exec(`INSERT INTO sectors_sdr_initial_pieces
				(sp_id, sector_number, piece_index, piece_cid, piece_size, data_url, data_headers, data_raw_size, data_delete_on_finalize)
				VALUES ($1, $2, 0, $3, $4, $5, '{}', $6, $7)`,
				mid, v.sectorNumber, v.fixture.PieceCIDV1.String(), v.fixture.PieceSize,
				piecerefURL, v.fixture.RawSize, !v.shouldIndex)
			if err != nil {
				return false, xerrors.Errorf("inserting initial piece: %w", err)
			}

			// Insert deal pipeline entry
			if v.mk20 {
				// MK20: need market_mk20_deal record for DealFromDB() in indexing task
				ds := mk20.DataSource{
					PieceCID: v.fixture.PieceCIDV2,
					Format: mk20.PieceDataFormat{
						Car: &mk20.FormatCar{},
					},
				}
				dataJSON, merr := json.Marshal(ds)
				if merr != nil {
					return false, xerrors.Errorf("marshaling data source: %w", merr)
				}

				_, err = tx.Exec(`INSERT INTO market_mk20_deal (id, client, data, ddo_v1, retrieval_v1, pdp_v1)
					VALUES ($1, 'test-client', $2::jsonb, 'null', 'null', 'null')`,
					v.dealUUID, string(dataJSON))
				if err != nil {
					return false, xerrors.Errorf("inserting mk20 deal: %w", err)
				}

				_, err = tx.Exec(`INSERT INTO market_mk20_pipeline
					(id, sp_id, contract, client, piece_cid_v2, piece_cid, piece_size, raw_size,
					 offline, url, indexing, announce, duration,
					 started, downloaded, after_commp, aggregated,
					 sector, reg_seal_proof, sector_offset)
					VALUES ($1, $2, 'f0100', 'test-client', $3, $4, $5, $6,
					 $7, $8, $9, $10, 518400,
					 TRUE, TRUE, TRUE, TRUE,
					 $11, $12, 0)`,
					v.dealUUID, mid,
					v.fixture.PieceCIDV2.String(), v.fixture.PieceCIDV1.String(),
					v.fixture.PieceSize, v.fixture.RawSize,
					v.offline, piecerefURL, v.shouldIndex, v.announce,
					v.sectorNumber, spt)
				if err != nil {
					return false, xerrors.Errorf("inserting mk20 pipeline: %w", err)
				}
			} else {
				// MK12: insert pipeline entry (LEFT JOINs in indexing task make backing deal optional)
				_, err = tx.Exec(`INSERT INTO market_mk12_deal_pipeline
					(uuid, sp_id, piece_cid, piece_size, raw_size, offline, url,
					 should_index, announce, is_ddo,
					 started, after_commp, after_psd, after_find_deal,
					 sector, reg_seal_proof, sector_offset)
					VALUES ($1, $2, $3, $4, $5, FALSE, $6,
					 $7, $8, $9,
					 TRUE, TRUE, TRUE, TRUE,
					 $10, $11, 0)`,
					v.dealUUID, mid,
					v.fixture.PieceCIDV1.String(), v.fixture.PieceSize, v.fixture.RawSize,
					piecerefURL,
					v.shouldIndex, v.announce, v.isDDO,
					v.sectorNumber, spt)
				if err != nil {
					return false, xerrors.Errorf("inserting mk12 pipeline: %w", err)
				}
			}

			return true, nil
		})
		require.NoError(t, err, "variant %q transaction", v.name)
		require.True(t, comm, "variant %q transaction committed", v.name)

		// Write parked piece data to disk
		require.NoError(t, writeParkedPieceFixture(dir, v.parkedPieceID, v.fixture.CarBytes),
			"variant %q write parked piece", v.name)

		t.Logf("Variant %q: sector=%d parkedPiece=%d pieceRef=%d deleteOnFinalize=%v",
			v.name, v.sectorNumber, v.parkedPieceID, v.pieceRefID, !v.shouldIndex)
	}

	// ── 10. Wait for all sectors to seal ──

	t.Log("Waiting for all sectors to reach after_commit_msg_success...")

	expectedSectors := len(variants)

	require.Eventuallyf(t, func() bool {
		h, err := full.ChainHead(ctx)
		require.NoError(t, err)

		var statuses []sealPollStatus
		err = db.Select(ctx, &statuses, `SELECT
			sp_id, sector_number,
			after_sdr, after_tree_d, after_tree_c, after_tree_r, after_synth,
			after_precommit_msg, after_precommit_msg_success,
			after_porep, after_finalize, after_move_storage,
			after_commit_msg, after_commit_msg_success,
			failed, failed_reason
			FROM sectors_sdr_pipeline ORDER BY sector_number`)
		require.NoError(t, err)

		allDone := true
		for _, s := range statuses {
			if s.Failed {
				t.Fatalf("Sector %d failed: %s", s.SectorNumber, s.FailedReason)
			}
			if !s.AfterCommitMsgSuccess {
				allDone = false
			}
		}

		t.Logf("height=%d sectors=%d/%d sealed", h.Height(), countSealed(statuses), expectedSectors)
		for _, s := range statuses {
			t.Logf("  sector=%d SDR=%t TreeD=%t TreeC=%t TreeR=%t Synth=%t PC=%t PCSuc=%t PoRep=%t Fin=%t Move=%t Commit=%t CommitSuc=%t",
				s.SectorNumber, s.AfterSDR, s.AfterTreeD, s.AfterTreeC, s.AfterTreeR, s.AfterSynth,
				s.AfterPrecommitMsg, s.AfterPrecommitMsgSuccess,
				s.AfterPoRep, s.AfterFinalize, s.AfterMoveStorage,
				s.AfterCommitMsg, s.AfterCommitMsgSuccess)
		}
		return allDone && len(statuses) == expectedSectors
	}, 10*time.Minute, 2*time.Second, "sectors did not finish sealing")

	t.Log("All sectors sealed successfully")

	// ── 11. Wait for all deal pipeline entries to reach complete=TRUE ──

	t.Log("Waiting for all deal pipeline entries to reach complete=TRUE...")

	require.Eventuallyf(t, func() bool {
		allComplete := true

		for i := range variants {
			v := &variants[i]
			var complete bool
			var err error

			if v.mk20 {
				err = db.QueryRow(ctx, `SELECT complete FROM market_mk20_pipeline WHERE id = $1`, v.dealUUID).Scan(&complete)
			} else {
				err = db.QueryRow(ctx, `SELECT complete FROM market_mk12_deal_pipeline WHERE uuid = $1`, v.dealUUID).Scan(&complete)
			}
			require.NoError(t, err, "variant %q query complete", v.name)

			if !complete {
				allComplete = false
				// Log current state for debugging
				if v.mk20 {
					var sealed, indexed bool
					var indexingTaskID sql.NullInt64
					_ = db.QueryRow(ctx, `SELECT sealed, indexed, indexing_task_id FROM market_mk20_pipeline WHERE id = $1`,
						v.dealUUID).Scan(&sealed, &indexed, &indexingTaskID)
					t.Logf("  %s: complete=false sealed=%t indexed=%t indexing_task_id=%s",
						v.name, sealed, indexed, nullInt64Str(indexingTaskID))
				} else {
					var sealed, indexed bool
					var indexingTaskID sql.NullInt64
					_ = db.QueryRow(ctx, `SELECT sealed, indexed, indexing_task_id FROM market_mk12_deal_pipeline WHERE uuid = $1`,
						v.dealUUID).Scan(&sealed, &indexed, &indexingTaskID)
					t.Logf("  %s: complete=false sealed=%t indexed=%t indexing_task_id=%s",
						v.name, sealed, indexed, nullInt64Str(indexingTaskID))
				}
			}
		}

		return allComplete
	}, 5*time.Minute, 2*time.Second, "not all deal pipeline entries reached complete=TRUE")

	t.Log("All deal pipeline entries reached complete=TRUE")

	// ── 12. Assert final state ──

	for i := range variants {
		v := &variants[i]
		t.Run(v.name, func(t *testing.T) {
			if v.mk20 {
				assertMk20PipelineClean(t, ctx, db, v)
			} else {
				assertMk12PipelineClean(t, ctx, db, v)
			}

			// Verify market_piece_deal entry exists (created by process_piece_deal)
			var dealCount int
			err := db.QueryRow(ctx, `SELECT COUNT(*) FROM market_piece_deal WHERE id = $1`, v.dealUUID).Scan(&dealCount)
			require.NoError(t, err)
			require.Equal(t, 1, dealCount, "market_piece_deal entry should exist for %s", v.name)

			// Verify market_piece_metadata entry
			var metaIndexed bool
			err = db.QueryRow(ctx, `SELECT indexed FROM market_piece_metadata WHERE piece_cid = $1 AND piece_size = $2`,
				v.fixture.PieceCIDV1.String(), v.fixture.PieceSize).Scan(&metaIndexed)
			require.NoError(t, err, "market_piece_metadata should exist for %s", v.name)

			if v.shouldIndex {
				// For should_index=true, the piece may or may not be actually indexed depending on
				// whether the reader was available. MK12 deals with unsealed copies get indexed=true.
				// MK20 deals read from parked pieces and should also be indexed=true.
				t.Logf("  %s: market_piece_metadata.indexed=%t (shouldIndex=true)", v.name, metaIndexed)
			} else {
				// For should_index=false, the piece is never actually indexed.
				// This is the key regression scenario: the indexing task must still complete
				// even without an unsealed sector copy (LEFT JOIN fix in CanAccept).
				require.False(t, metaIndexed, "should_index=false deals must have indexed=false in metadata")
			}
		})
	}

	// ── 13. Verify no stuck or failed pipeline entries ──

	var stuckMk12 int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk12_deal_pipeline WHERE complete = FALSE`).Scan(&stuckMk12))
	require.Equal(t, 0, stuckMk12, "no mk12 pipeline entries should be stuck")

	var stuckMk20 int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_pipeline WHERE complete = FALSE`).Scan(&stuckMk20))
	require.Equal(t, 0, stuckMk20, "no mk20 pipeline entries should be stuck")

	var failedSectors int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline WHERE failed = TRUE`).Scan(&failedSectors))
	require.Equal(t, 0, failedSectors, "no sectors should be failed")

	t.Log("All assertions passed - pipeline state is clean")

	_ = capi.Shutdown(ctx)
	<-finishCh
}

// assertMk12PipelineClean verifies the mk12 deal pipeline entry is in a clean final state.
func assertMk12PipelineClean(t *testing.T, ctx context.Context, db *harmonydb.DB, v *dealVariant) {
	t.Helper()

	var p struct {
		Complete       bool          `db:"complete"`
		Sealed         bool          `db:"sealed"`
		Indexed        bool          `db:"indexed"`
		IndexingTaskID sql.NullInt64 `db:"indexing_task_id"`
		ShouldIndex    bool          `db:"should_index"`
		Sector         sql.NullInt64 `db:"sector"`
		SectorOffset   sql.NullInt64 `db:"sector_offset"`
	}
	err := db.QueryRow(ctx, `SELECT complete, sealed, indexed, indexing_task_id, should_index, sector, sector_offset
		FROM market_mk12_deal_pipeline WHERE uuid = $1`, v.dealUUID).Scan(
		&p.Complete, &p.Sealed, &p.Indexed, &p.IndexingTaskID, &p.ShouldIndex, &p.Sector, &p.SectorOffset)
	require.NoError(t, err)

	require.True(t, p.Complete, "mk12 deal %s should be complete", v.name)
	require.True(t, p.Sealed, "mk12 deal %s should be sealed", v.name)
	require.True(t, p.Indexed, "mk12 deal %s should be indexed (pipeline flag)", v.name)
	require.False(t, p.IndexingTaskID.Valid, "mk12 deal %s indexing_task_id should be NULL (cleared)", v.name)
	require.True(t, p.Sector.Valid, "mk12 deal %s should have a sector", v.name)
	require.True(t, p.SectorOffset.Valid, "mk12 deal %s should have a sector_offset", v.name)
}

// assertMk20PipelineClean verifies the mk20 deal pipeline entry is in a clean final state.
func assertMk20PipelineClean(t *testing.T, ctx context.Context, db *harmonydb.DB, v *dealVariant) {
	t.Helper()

	var p struct {
		Complete       bool          `db:"complete"`
		Sealed         bool          `db:"sealed"`
		Indexed        bool          `db:"indexed"`
		IndexingTaskID sql.NullInt64 `db:"indexing_task_id"`
		Indexing       bool          `db:"indexing"`
		Sector         sql.NullInt64 `db:"sector"`
		SectorOffset   sql.NullInt64 `db:"sector_offset"`
	}
	err := db.QueryRow(ctx, `SELECT complete, sealed, indexed, indexing_task_id, indexing, sector, sector_offset
		FROM market_mk20_pipeline WHERE id = $1`, v.dealUUID).Scan(
		&p.Complete, &p.Sealed, &p.Indexed, &p.IndexingTaskID, &p.Indexing, &p.Sector, &p.SectorOffset)
	require.NoError(t, err)

	require.True(t, p.Complete, "mk20 deal %s should be complete", v.name)
	require.True(t, p.Sealed, "mk20 deal %s should be sealed", v.name)
	require.True(t, p.Indexed, "mk20 deal %s should be indexed (pipeline flag)", v.name)
	require.False(t, p.IndexingTaskID.Valid, "mk20 deal %s indexing_task_id should be NULL (cleared)", v.name)
	require.True(t, p.Sector.Valid, "mk20 deal %s should have a sector", v.name)
	require.True(t, p.SectorOffset.Valid, "mk20 deal %s should have a sector_offset", v.name)
}

// constructCurioWithMarketAndSeal builds a Curio node with both market and sealing enabled.
// Combines the patterns from ConstructCurioTest (seal/devnet) and ConstructCurioWithMarket (market).
func constructCurioWithMarketAndSeal(ctx context.Context, t *testing.T, dir string, db *harmonydb.DB, idx *indexstore.IndexStore, full v1api.FullNode, maddr address.Address, cfg *config.CurioConfig) (api.Curio, func(), jsonrpc.ClientCloser, <-chan struct{}) {
	ffiselect.IsTest = true
	seal.SetDevnet(true) // required for sealing in test mode

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

	dependencies := &deps.Deps{}
	dependencies.DB = db
	dependencies.Chain = full
	dependencies.IndexStore = idx
	require.NoError(t, os.Setenv("CURIO_REPO_PATH", dir))
	require.NoError(t, dependencies.PopulateRemainingDeps(ctx, cctx, false))

	taskEngine, err := tasks.StartTasks(ctx, dependencies, shutdownChan)
	require.NoError(t, err)

	go func() {
		err = rpc.ListenAndServe(ctx, dependencies, shutdownChan)
		require.NoError(t, err)
	}()

	finishCh := node.MonitorShutdown(shutdownChan)

	var machines []string
	require.NoError(t, db.Select(ctx, &machines, `select host_and_port from harmony_machines`))
	require.Len(t, machines, 1)
	helpers.WaitForTCP(t, machines[0], 30*time.Second)

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

	_ = logging.SetLogLevel("harmonytask", "DEBUG")
	_ = logging.SetLogLevel("cu/seal", "DEBUG")
	_ = logging.SetLogLevel("indexing", "DEBUG")
	_ = logging.SetLogLevel("storage-market", "DEBUG")

	return capi, taskEngine.GracefullyTerminate, ccloser, finishCh
}

// ── Piece fixture helpers ──

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

func writeParkedPieceFixture(dir string, pieceID int64, data []byte) error {
	path := filepath.Join(
		dir,
		storiface.FTPiece.String(),
		storiface.SectorName(storiface.PieceNumber(pieceID).Ref().ID),
	)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ── Utility helpers ──

func countSealed(statuses []sealPollStatus) int {
	n := 0
	for _, s := range statuses {
		if s.AfterCommitMsgSuccess {
			n++
		}
	}
	return n
}

func nullInt64Str(n sql.NullInt64) string {
	if !n.Valid {
		return "NULL"
	}
	return fmt.Sprintf("%d", n.Int64)
}
