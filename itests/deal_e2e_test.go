package itests

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime/codec/dagjson"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/dagsync/ipnisync"
	"github.com/ipni/go-libipni/dagsync/ipnisync/head"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/oklog/ulid"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	verifreg13 "github.com/filecoin-project/go-state-types/builtin/v13/verifreg"
	market9 "github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/itests/helpers"

	miner2 "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/itests/kit"
	"github.com/filecoin-project/lotus/lib/must"
)

type dealVariant struct {
	name        string
	mk20        bool
	isDDO       bool
	verified    bool
	shouldIndex bool
	offline     bool

	dealID       string
	clientAddr   address.Address
	clientIDAddr address.Address
	fixture      helpers.PieceFixture
	signed       *helpers.MK12SignedProposal
	allocationID *verifreg13.AllocationId

	parkedPieceID int64
	pieceRefID    int64
	pieceRefURL   string
}

// TestDealPipelineFullPath runs a full end-to-end storage market pipeline matrix and
// validates that each deal variant reaches its expected final state in DB + chain.
func TestDealPipelineFullPath(t *testing.T) {
	// Give the full integration run a hard upper bound so waits fail deterministically.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Provision funded identities required by the test network bootstrap.
	initialBigBalance := types.MustParseFIL("100fil").Int64()
	rootKey := must.One(key.GenerateKey(types.KTSecp256k1))
	verifierKey := must.One(key.GenerateKey(types.KTSecp256k1))
	verifiedClientKey := must.One(key.GenerateKey(types.KTBLS))
	unverifiedClientKey := must.One(key.GenerateKey(types.KTBLS))

	// Bring up a fresh network + miner + DB backing this test case.
	full, _, db, maddr := helpers.BootstrapNetworkWithNewMiner(t, ctx, "8MiB",
		kit.RootVerifier(rootKey, abi.NewTokenAmount(initialBigBalance)),
		kit.Account(verifierKey, abi.NewTokenAmount(initialBigBalance)),
		kit.Account(verifiedClientKey, abi.NewTokenAmount(initialBigBalance)),
		kit.Account(unverifiedClientKey, abi.NewTokenAmount(initialBigBalance)),
	)

	// Always clean DB rows inserted by this test.
	defer db.ITestDeleteAll()

	// Resolve SP id from miner address for pipeline seed rows.
	minerID, err := address.IDFromAddress(maddr)
	require.NoError(t, err)
	spID := int64(minerID)

	// Build harness base config used by Curio test harness startup.
	baseCfg, err := helpers.SetBaseConfigWithDefaults(t, ctx, db)
	require.NoError(t, err)

	// Allocate an isolated temp dir for parked CAR payloads.
	dir, err := os.MkdirTemp(os.TempDir(), "curio-deal-e2e")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	// Register one verified client and one unverified client used by variant matrix.
	_, verifiedClientAddrs := kit.SetupVerifiedClients(ctx, t, full, rootKey, verifierKey, []*key.Key{verifiedClientKey})
	require.Len(t, verifiedClientAddrs, 1)
	verifiedClientAddr := verifiedClientAddrs[0]

	unverifiedClientAddr, err := full.WalletImport(ctx, &unverifiedClientKey.KeyInfo)
	require.NoError(t, err)

	// Define the coverage matrix, then attach a fixture + per-variant id.
	variants := buildDealVariants(verifiedClientAddr, unverifiedClientAddr)
	assignVariantFixturesAndIDs(t, dir, variants)

	// Compute epochs shared by MK12 proposal/allocation preparation.
	head, err := full.ChainHead(ctx)
	require.NoError(t, err)
	startEpoch := head.Height() + 10000
	dealDuration := abi.ChainEpoch(518400)
	endEpoch := startEpoch + dealDuration

	// Build MK12 signed proposals / DDO allocations that are needed before DB seeding.
	prepareVariantDealArtifacts(ctx, t, full, maddr, uint64(minerID), verifiedClientAddr, startEpoch, endEpoch, variants)

	// Seed pending pipeline rows and write parked piece fixture payloads.
	seedVariantPipelines(ctx, t, db, maddr, spID, startEpoch, endEpoch, dealDuration, dir, variants)

	// Start Curio harness and task runners that execute the seeded pipelines.
	harness := helpers.StartCurioHarnessWithCleanup(ctx, t, dir, db, helpers.NewIndexStore(ctx, t, baseCfg), full, baseCfg.Apis.StorageRPCSecret, helpers.CurioHarnessOptions{
		LogLevels: []helpers.CurioLogLevel{
			{Subsystem: "harmonytask", Level: "INFO"},
			{Subsystem: "cu/seal", Level: "INFO"},
			{Subsystem: "indexing", Level: "INFO"},
			{Subsystem: "storage-market", Level: "INFO"},
		},
	})
	baseURL := "http://" + baseCfg.HTTP.ListenAddress

	mk12IDs := mk12VariantIDs(variants)
	mk20IDs := mk20VariantIDs(variants)

	// On failure, emit a compact cross-pipeline dump to speed up diagnosis.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		sectorRefs, err := collectSectorRefs(dctx, db, spID)
		if err != nil {
			t.Logf("diagnostics collect sectors error: %s", err)
		}
		helpers.DumpDealPipelineDiagnostics(t, dctx, db, mk12IDs, mk20IDs, sectorRefs)
	})

	// Ensure harness sees local storage paths before waiting for completion.
	helpers.RedeclareAllLocalStorage(ctx, t, harness.API)

	// Wait for every variant row to report complete=true in its target pipeline table.
	waitTimeout := computeWaitTimeout(ctx)
	t.Logf("E2E wait timeout=%s", waitTimeout.Round(time.Second))

	require.NoError(t, waitForAllVariantCompletion(t, ctx, db, variants, waitTimeout))

	// Validate sector pipeline finished and no failed sectors remain.
	var sectorCount int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline`).Scan(&sectorCount))
	require.Greater(t, sectorCount, 0, "expected at least one sealing sector")

	var notCommitted int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline WHERE after_commit_msg_success = FALSE`).Scan(&notCommitted))
	require.Equal(t, 0, notCommitted, "all sectors must reach after_commit_msg_success")
	helpers.AssertNoFailedSectors(t, ctx, db)

	// Validate per-variant final pipeline flags and resulting deal/piece metadata rows.
	for _, v := range variants {
		if v.mk20 {
			assertMK20VariantFinalState(t, ctx, db, v)
		} else {
			assertMK12VariantFinalState(t, ctx, db, v)
		}

		var dealCount int
		require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM market_piece_deal WHERE id = $1`, v.dealID).Scan(&dealCount))
		require.Equal(t, 1, dealCount, "market_piece_deal row count for %s", v.name)

		var indexed bool
		require.NoError(t, db.QueryRow(ctx, `SELECT indexed FROM market_piece_metadata WHERE piece_cid = $1 AND piece_size = $2`,
			v.fixture.PieceCIDV1.String(), v.fixture.PieceSize,
		).Scan(&indexed))

		if v.shouldIndex {
			require.True(t, indexed, "indexed metadata expected for %s", v.name)
		} else {
			require.False(t, indexed, "non-index metadata expected for %s", v.name)
		}

		if !v.mk20 {
			assertInitialPieceModel(t, ctx, db, v)
		}

		assertVariantPieceRetrievals(t, baseURL, v)
		assertVariantIPFSBlockRetrieval(t, baseURL, v)
	}

	// Ensure pipeline tables have no leftover incomplete rows after all assertions.
	var stuckMK12 int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk12_deal_pipeline WHERE complete = FALSE`).Scan(&stuckMK12))
	require.Equal(t, 0, stuckMK12, "no mk12 rows should remain incomplete")

	var stuckMK20 int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_pipeline WHERE complete = FALSE`).Scan(&stuckMK20))
	require.Equal(t, 0, stuckMK20, "no mk20 rows should remain incomplete")

	// Confirm verified variants produce an on-chain verified claim for their piece CID.
	for _, v := range variants {
		if v.verified {
			assertVerifiedClaimForPiece(t, ctx, full, maddr, v.fixture.PieceCIDV1, v.name)
		}
	}

	helpers.LogIPNIStatus(t, ctx, db)

	assertIPNIAds(t, ctx, db, spID, baseURL, variants)
}

// buildDealVariants defines the static deal matrix used by this E2E coverage test.
func buildDealVariants(verifiedClientAddr, unverifiedClientAddr address.Address) []*dealVariant {
	return []*dealVariant{
		{name: "mk12-online-index", shouldIndex: true, clientAddr: unverifiedClientAddr},
		{name: "mk12-online-noindex", shouldIndex: false, clientAddr: unverifiedClientAddr},
		{name: "mk12-ddo-index", isDDO: true, shouldIndex: true, clientAddr: verifiedClientAddr},
		{name: "mk12-ddo-noindex", isDDO: true, shouldIndex: false, clientAddr: verifiedClientAddr},
		{name: "mk12-verified-index", verified: true, shouldIndex: true, clientAddr: verifiedClientAddr},
		{name: "mk12-ddo-verified-index", isDDO: true, verified: true, shouldIndex: true, clientAddr: verifiedClientAddr},
		{name: "mk20-online-index", mk20: true, shouldIndex: true, clientAddr: verifiedClientAddr},
		{name: "mk20-offline-noindex", mk20: true, shouldIndex: false, offline: true, clientAddr: verifiedClientAddr},
	}
}

// assignVariantFixturesAndIDs generates a unique CAR fixture and deterministic id type per variant.
func assignVariantFixturesAndIDs(t *testing.T, dir string, variants []*dealVariant) {
	t.Helper()

	for _, v := range variants {
		v.fixture = helpers.CreatePieceFixture(t, dir, 200)
		if v.mk20 {
			v.dealID = ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
		} else {
			v.dealID = uuid.New().String()
		}
	}
}

// prepareVariantDealArtifacts creates MK12 signed proposals or DDO allocations before DB seeding.
func prepareVariantDealArtifacts(
	ctx context.Context,
	t *testing.T,
	full *kit.TestFullNode,
	maddr address.Address,
	minerID uint64,
	verifiedClientAddr address.Address,
	startEpoch abi.ChainEpoch,
	endEpoch abi.ChainEpoch,
	variants []*dealVariant,
) {
	t.Helper()

	for _, v := range variants {
		switch {
		// MK20 uses different seed rows and does not need MK12 proposal/allocation artifacts.
		case v.mk20:
			continue
		// DDO variants require a verified allocation keyed to the piece and client id address.
		case v.isDDO:
			clientIDAddr, err := full.StateLookupID(ctx, v.clientAddr, types.EmptyTSK)
			require.NoError(t, err)
			v.clientIDAddr = clientIDAddr

			_, allocationID := kit.SetupAllocation(ctx, t, full, minerID, abi.PieceInfo{
				Size:     v.fixture.PieceSize,
				PieceCID: v.fixture.PieceCIDV1,
			}, verifiedClientAddr, 0, 0)
			aid := allocationID
			v.allocationID = &aid
		// F05 MK12 variants require a signed deal proposal.
		default:
			providerCollateral, err := helpers.ProviderCollateralBounds(ctx, full, v.fixture.PieceSize, v.verified)
			require.NoError(t, err)

			signed, err := helpers.BuildSignedMK12Proposal(
				ctx,
				full,
				v.clientAddr,
				maddr,
				v.fixture.RootCID,
				v.fixture.PieceCIDV1,
				v.fixture.PieceSize,
				startEpoch,
				endEpoch,
				v.verified,
				providerCollateral,
			)
			require.NoError(t, err)
			v.signed = signed
		}
	}
}

// seedVariantPipelines inserts pending rows for every variant and writes their parked CAR files.
func seedVariantPipelines(
	ctx context.Context,
	t *testing.T,
	db *harmonydb.DB,
	maddr address.Address,
	spID int64,
	startEpoch abi.ChainEpoch,
	endEpoch abi.ChainEpoch,
	dealDuration abi.ChainEpoch,
	dir string,
	variants []*dealVariant,
) {
	t.Helper()

	for _, v := range variants {
		committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// Seed parked piece row and ref so pipeline download steps can resolve local bytes.
			pieceID, err := helpers.InsertCompletedParkedPiece(tx, v.fixture.PieceCIDV1.String(), v.fixture.PieceSize, v.fixture.RawSize, true)
			if err != nil {
				return false, err
			}
			v.parkedPieceID = pieceID

			refID, err := helpers.InsertParkedPieceRef(tx, pieceID, "", nil, true)
			if err != nil {
				return false, err
			}
			v.pieceRefID = refID
			v.pieceRefURL = fmt.Sprintf("pieceref:%d", refID)

			// MK20 variants seed the MK20 pending table.
			if v.mk20 {
				return true, helpers.SeedMK20PendingDeal(tx, helpers.MK20PendingSeed{
					DealID:       v.dealID,
					Client:       v.clientAddr.String(),
					Provider:     maddr,
					Contract:     "0xtest",
					PieceCIDV2:   v.fixture.PieceCIDV2,
					Offline:      v.offline,
					SourceURL:    v.pieceRefURL,
					Indexing:     v.shouldIndex,
					Announce:     v.shouldIndex,
					AllocationID: nil,
					Duration:     dealDuration,
				})
			}

			// DDO MK12 variants seed direct data onboarding rows with allocation metadata.
			if v.isDDO {
				if v.allocationID == nil {
					return false, fmt.Errorf("ddo deal %s missing allocation", v.name)
				}
				return true, helpers.SeedMK12DDOPendingDeal(tx, helpers.MK12DDOPendingSeed{
					UUID:          v.dealID,
					SPID:          spID,
					Client:        v.clientIDAddr.String(),
					PieceCID:      v.fixture.PieceCIDV1.String(),
					PieceSize:     v.fixture.PieceSize,
					RawSize:       v.fixture.RawSize,
					Offline:       false,
					URL:           v.pieceRefURL,
					Announce:      v.shouldIndex,
					FastRetrieval: v.shouldIndex,
					Verified:      v.verified,
					StartEpoch:    startEpoch,
					EndEpoch:      endEpoch,
					AllocationID:  int64(*v.allocationID),
				})
			}

			// Remaining MK12 variants seed classic F05 pending rows with signed proposal payload.
			clientPeerID := fmt.Sprintf("peer-%s", strings.ReplaceAll(v.name, "_", "-"))
			return true, helpers.SeedMK12F05PendingDeal(tx, helpers.MK12F05PendingSeed{
				UUID:          v.dealID,
				SPID:          spID,
				PieceCID:      v.fixture.PieceCIDV1.String(),
				PieceSize:     v.fixture.PieceSize,
				RawSize:       v.fixture.RawSize,
				Offline:       false,
				URL:           v.pieceRefURL,
				Announce:      v.shouldIndex,
				ClientPeerID:  clientPeerID,
				FastRetrieval: v.shouldIndex,
				Signed:        v.signed,
			})
		})
		require.NoError(t, err, "seed variant %s", v.name)
		require.True(t, committed, "seed variant %s transaction committed", v.name)

		// Persist fixture bytes to the location referenced by parked_piece_refs.
		require.NoError(t, helpers.WriteParkedPieceFixture(dir, v.parkedPieceID, v.fixture.CarBytes))
	}
}

// computeWaitTimeout derives an adaptive wait budget from the outer test context deadline.
func computeWaitTimeout(ctx context.Context) time.Duration {
	waitTimeout := 8 * time.Minute
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline) - 45*time.Second
		if remaining < waitTimeout {
			waitTimeout = remaining
		}
	}
	if waitTimeout < 30*time.Second {
		waitTimeout = 30 * time.Second
	}
	return waitTimeout
}

// waitForAllVariantCompletion polls MK12/MK20 tables until each seeded id reports complete=true.
func waitForAllVariantCompletion(t *testing.T, ctx context.Context, db *harmonydb.DB, variants []*dealVariant, timeout time.Duration) error {
	t.Helper()

	deadline := time.Now().Add(timeout)
	mk12IDs := mk12VariantIDs(variants)
	mk20IDs := mk20VariantIDs(variants)
	poll := 0

	check := func() (bool, error) {
		poll++
		// Emit periodic progress snapshots so stalled runs are diagnosable from test logs.
		if poll == 1 || poll%5 == 0 {
			if err := helpers.PipelineProgress(t, ctx, db, mk12IDs, mk20IDs, poll, time.Until(deadline)); err != nil {
				return false, xerrors.Errorf("progress logging: %w", err)
			}
		}

		// Fail early if any sealing sector entered failed=true.
		var failedSectors int
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline WHERE failed = TRUE`).Scan(&failedSectors); err != nil {
			return false, xerrors.Errorf("query failed sectors: %w", err)
		}
		if failedSectors > 0 {
			return false, xerrors.Errorf("%d sectors failed", failedSectors)
		}

		// Require all variant rows to be complete before ending the wait.
		allComplete := true
		for _, v := range variants {
			var complete bool
			var err error
			if v.mk20 {
				// MK20 rows are seeded in market_mk20_pipeline_waiting first; until inserted into
				// market_mk20_pipeline they should be treated as "not complete", not as an error.
				err = db.QueryRow(ctx, `SELECT COALESCE(BOOL_AND(complete), FALSE) FROM market_mk20_pipeline WHERE id = $1`, v.dealID).Scan(&complete)
			} else {
				err = db.QueryRow(ctx, `SELECT complete FROM market_mk12_deal_pipeline WHERE uuid = $1`, v.dealID).Scan(&complete)
			}
			if err != nil {
				return false, xerrors.Errorf("query complete for %s: %w", v.name, err)
			}
			if !complete {
				allComplete = false
			}
		}

		if allComplete {
			return true, nil
		}
		return false, nil
	}

	// Translate helper timeout into clearer test error text and print one final progress dump.
	if err := helpers.WaitForCondition(ctx, timeout, 2*time.Second, check); err != nil {
		_ = helpers.PipelineProgress(t, ctx, db, mk12IDs, mk20IDs, poll, 0)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || err == ctx.Err() {
			return err
		}
		if errors.Is(err, helpers.ErrWaitTimeout) {
			return xerrors.Errorf("timeout waiting for all variants to complete")
		}
		return err
	}

	_ = helpers.PipelineProgress(t, ctx, db, mk12IDs, mk20IDs, poll, 0)
	return nil
}

// assertMK12VariantFinalState verifies that all MK12 stage flags reached terminal success values.
func assertMK12VariantFinalState(t *testing.T, ctx context.Context, db *harmonydb.DB, v *dealVariant) {
	t.Helper()

	var p struct {
		Started      bool          `db:"started"`
		AfterCommp   bool          `db:"after_commp"`
		AfterPSD     bool          `db:"after_psd"`
		AfterFind    bool          `db:"after_find_deal"`
		Sealed       bool          `db:"sealed"`
		Indexed      bool          `db:"indexed"`
		Complete     bool          `db:"complete"`
		Sector       sql.NullInt64 `db:"sector"`
		SectorOffset sql.NullInt64 `db:"sector_offset"`
		IsDDO        bool          `db:"is_ddo"`
	}

	require.NoError(t, db.QueryRow(ctx, `SELECT
		started, after_commp, after_psd, after_find_deal,
		sealed, indexed, complete, sector, sector_offset, is_ddo
	FROM market_mk12_deal_pipeline
	WHERE uuid = $1`, v.dealID).Scan(
		&p.Started, &p.AfterCommp, &p.AfterPSD, &p.AfterFind,
		&p.Sealed, &p.Indexed, &p.Complete, &p.Sector, &p.SectorOffset, &p.IsDDO,
	))
	t.Logf("E2E-ASSERT mk12 variant=%s started=%t commp=%t psd=%t find=%t sealed=%t indexed=%t complete=%t sector=%t offset=%t is_ddo=%t",
		v.name, p.Started, p.AfterCommp, p.AfterPSD, p.AfterFind,
		p.Sealed, p.Indexed, p.Complete, p.Sector.Valid, p.SectorOffset.Valid, p.IsDDO)

	require.True(t, p.Started, "%s should have started=true", v.name)
	require.True(t, p.AfterCommp, "%s should have after_commp=true", v.name)
	require.True(t, p.AfterPSD, "%s should have after_psd=true", v.name)
	require.True(t, p.AfterFind, "%s should have after_find_deal=true", v.name)
	require.True(t, p.Sealed, "%s should have sealed=true", v.name)
	require.True(t, p.Indexed, "%s should have indexed=true", v.name)
	require.True(t, p.Complete, "%s should have complete=true", v.name)
	require.True(t, p.Sector.Valid, "%s should have sector assigned", v.name)
	require.True(t, p.SectorOffset.Valid, "%s should have sector_offset assigned", v.name)
	require.Equal(t, v.isDDO, p.IsDDO, "%s is_ddo mismatch", v.name)
}

// assertMK20VariantFinalState verifies that all MK20 stage flags reached terminal success values.
func assertMK20VariantFinalState(t *testing.T, ctx context.Context, db *harmonydb.DB, v *dealVariant) {
	t.Helper()

	var p struct {
		Started      bool          `db:"started"`
		Downloaded   bool          `db:"downloaded"`
		AfterCommp   bool          `db:"after_commp"`
		Aggregated   bool          `db:"aggregated"`
		Sealed       bool          `db:"sealed"`
		Indexed      bool          `db:"indexed"`
		Complete     bool          `db:"complete"`
		Sector       sql.NullInt64 `db:"sector"`
		SectorOffset sql.NullInt64 `db:"sector_offset"`
	}

	require.NoError(t, db.QueryRow(ctx, `SELECT
		started, downloaded, after_commp, aggregated,
		sealed, indexed, complete, sector, sector_offset
	FROM market_mk20_pipeline
	WHERE id = $1`, v.dealID).Scan(
		&p.Started, &p.Downloaded, &p.AfterCommp, &p.Aggregated,
		&p.Sealed, &p.Indexed, &p.Complete, &p.Sector, &p.SectorOffset,
	))
	t.Logf("E2E-ASSERT mk20 variant=%s started=%t downloaded=%t commp=%t aggregated=%t sealed=%t indexed=%t complete=%t sector=%t offset=%t",
		v.name, p.Started, p.Downloaded, p.AfterCommp, p.Aggregated,
		p.Sealed, p.Indexed, p.Complete, p.Sector.Valid, p.SectorOffset.Valid)

	require.True(t, p.Started, "%s should have started=true", v.name)
	require.True(t, p.Downloaded, "%s should have downloaded=true", v.name)
	require.True(t, p.AfterCommp, "%s should have after_commp=true", v.name)
	require.True(t, p.Aggregated, "%s should have aggregated=true", v.name)
	require.True(t, p.Sealed, "%s should have sealed=true", v.name)
	require.True(t, p.Indexed, "%s should have indexed=true", v.name)
	require.True(t, p.Complete, "%s should have complete=true", v.name)
	require.True(t, p.Sector.Valid, "%s should have sector assigned", v.name)
	require.True(t, p.SectorOffset.Valid, "%s should have sector_offset assigned", v.name)
}

// assertInitialPieceModel validates whether F05 proposal or DDO manifest was persisted for the piece.
func assertInitialPieceModel(t *testing.T, ctx context.Context, db *harmonydb.DB, v *dealVariant) {
	t.Helper()

	var piece struct {
		F05DealID sql.NullInt64   `db:"f05_deal_id"`
		F05Prop   json.RawMessage `db:"f05_deal_proposal"`
		DDOPAM    json.RawMessage `db:"direct_piece_activation_manifest"`
	}
	require.NoError(t, db.QueryRow(ctx, `SELECT f05_deal_id, f05_deal_proposal, direct_piece_activation_manifest
		FROM sectors_sdr_initial_pieces
		WHERE piece_cid = $1 AND piece_size = $2
		ORDER BY created_at DESC
		LIMIT 1`, v.fixture.PieceCIDV1.String(), v.fixture.PieceSize).Scan(
		&piece.F05DealID, &piece.F05Prop, &piece.DDOPAM,
	))

	if v.isDDO {
		require.Len(t, piece.F05Prop, 0, "%s DDO piece must not use f05_deal_proposal", v.name)
		require.NotEmpty(t, piece.DDOPAM, "%s DDO piece must set direct_piece_activation_manifest", v.name)

		var pam miner2.PieceActivationManifest
		require.NoError(t, json.Unmarshal(piece.DDOPAM, &pam))
		if v.verified {
			require.NotNil(t, pam.VerifiedAllocationKey, "%s verified DDO must carry VerifiedAllocationKey", v.name)
			require.NotNil(t, v.allocationID)
			require.EqualValues(t, *v.allocationID, pam.VerifiedAllocationKey.ID)
		}
		return
	}

	require.NotEmpty(t, piece.F05Prop, "%s F05 piece must set f05_deal_proposal", v.name)
	require.Len(t, piece.DDOPAM, 0, "%s F05 piece must not use direct manifest fallback", v.name)
	require.True(t, piece.F05DealID.Valid, "%s F05 piece must set f05_deal_id", v.name)

	var prop market9.DealProposal
	require.NoError(t, json.Unmarshal(piece.F05Prop, &prop))
	require.Equal(t, v.verified, prop.VerifiedDeal, "%s verified flag in f05 proposal", v.name)
}

// assertVerifiedClaimForPiece ensures a verified deal variant produced an on-chain claim for the piece.
func assertVerifiedClaimForPiece(t *testing.T, ctx context.Context, full *kit.TestFullNode, provider address.Address, pieceCID cid.Cid, variant string) {
	t.Helper()

	claims, err := full.StateGetClaims(ctx, provider, types.EmptyTSK)
	require.NoError(t, err)

	found := false
	for _, claim := range claims {
		if claim.Data.Equals(pieceCID) {
			found = true
			break
		}
	}
	require.True(t, found, "expected on-chain verified claim for variant %s piece %s", variant, pieceCID)
}

func assertVariantPieceRetrievals(t *testing.T, baseURL string, v *dealVariant) {
	t.Helper()

	pieceCIDs := []cid.Cid{v.fixture.PieceCIDV1, v.fixture.PieceCIDV2}
	for _, pieceCID := range pieceCIDs {
		status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/piece/"+pieceCID.String(), nil)
		require.Equal(t, http.StatusOK, status, "expected /piece success for %s cid %s", v.name, pieceCID)
		require.Equal(t, v.fixture.CarBytes, body, "unexpected /piece body for %s cid %s", v.name, pieceCID)
		helpers.AssertPieceResponseHeaders(t, headers, pieceCID.String(), len(v.fixture.CarBytes))
	}
}

func assertVariantIPFSBlockRetrieval(t *testing.T, baseURL string, v *dealVariant) {
	t.Helper()

	blockCID, blockData := helpers.SelectFixtureRawBlockCandidate(t, v.fixture)
	status, body, headers := helpers.HTTPGetWithHeaders(t, baseURL, "/ipfs/"+blockCID.String(), map[string]string{
		"Accept": "application/vnd.ipld.raw",
	})

	if v.shouldIndex {
		require.Equal(t, http.StatusOK, status, "expected indexed /ipfs success for %s block %s", v.name, blockCID)
		require.Equal(t, blockData, body, "unexpected /ipfs raw block body for %s block %s", v.name, blockCID)
		require.Contains(t, headers.Get("Content-Type"), "application/vnd.ipld.raw", "unexpected /ipfs content type for %s block %s", v.name, blockCID)
		return
	}

	require.Equal(t, http.StatusNotFound, status, "expected non-indexed /ipfs miss for %s block %s", v.name, blockCID)
}

// collectSectorRefs loads sector refs for diagnostics dumps when the test fails.
func collectSectorRefs(ctx context.Context, db *harmonydb.DB, spID int64) ([]helpers.SectorRef, error) {
	var rows []struct {
		SPID   int64 `db:"sp_id"`
		Number int64 `db:"sector_number"`
	}
	if err := db.Select(ctx, &rows, `SELECT sp_id, sector_number FROM sectors_sdr_pipeline WHERE sp_id = $1 ORDER BY sector_number`, spID); err != nil {
		return nil, err
	}

	refs := make([]helpers.SectorRef, 0, len(rows))
	for _, r := range rows {
		refs = append(refs, helpers.SectorRef{SPID: r.SPID, Number: r.Number})
	}
	return refs, nil
}

// mk12VariantIDs returns only MK12 ids for progress/diagnostic helpers.
func mk12VariantIDs(variants []*dealVariant) []string {
	ids := make([]string, 0, len(variants))
	for _, v := range variants {
		if !v.mk20 {
			ids = append(ids, v.dealID)
		}
	}
	return ids
}

// mk20VariantIDs returns only MK20 ids for progress/diagnostic helpers.
func mk20VariantIDs(variants []*dealVariant) []string {
	ids := make([]string, 0, len(variants))
	for _, v := range variants {
		if v.mk20 {
			ids = append(ids, v.dealID)
		}
	}
	return ids
}

func assertIPNIAds(t *testing.T, ctx context.Context, db *harmonydb.DB, minerID int64, httpURL string, variants []*dealVariant) {
	t.Helper()

	expectedAds := 0
	for _, v := range variants {
		if v.shouldIndex {
			expectedAds++
		}
	}

	var peerID string
	err := db.QueryRow(ctx, `SELECT peer_id FROM ipni_peerid WHERE sp_id = $1`, minerID).Scan(&peerID)
	require.NoError(t, err)
	require.NotEmpty(t, peerID, "expected IPNI peer ID for miner %d", minerID)

	headPath := fmt.Sprintf("/ipni-provider/%s/ipni/v1/ad/head", peerID)
	err = helpers.WaitForCondition(ctx, 90*time.Second, time.Second, func() (bool, error) {
		status, body, _ := helpers.HTTPGetWithHeaders(t, httpURL, headPath, nil)
		if status == http.StatusNoContent {
			return expectedAds == 0, nil
		}
		if status != http.StatusOK {
			return false, fmt.Errorf("unexpected IPNI head status %d", status)
		}

		signedHead, err := head.Decode(bytes.NewReader(body))
		if err != nil {
			return false, fmt.Errorf("decode signed head: %w", err)
		}

		headLink, ok := signedHead.Head.(cidlink.Link)
		if !ok {
			return false, fmt.Errorf("unexpected signed head link type %T", signedHead.Head)
		}

		adCount, err := countIPNIAdsFromHead(t, httpURL, peerID, headLink.Cid)
		if err != nil {
			return false, err
		}
		if adCount != expectedAds {
			t.Logf("IPNI ad count not ready yet: provider=%s have=%d want=%d", peerID, adCount, expectedAds)
			return false, nil
		}

		return true, nil
	})
	require.NoError(t, err)
}

func countIPNIAdsFromHead(t *testing.T, httpURL, peerID string, headCID cid.Cid) (int, error) {
	t.Helper()

	if !headCID.Defined() {
		return 0, nil
	}

	seen := make(map[string]struct{})
	count := 0
	current := headCID

	for current.Defined() {
		if _, ok := seen[current.String()]; ok {
			return 0, fmt.Errorf("detected IPNI ad cycle at %s", current)
		}
		seen[current.String()] = struct{}{}

		status, body, _ := helpers.HTTPGetWithHeaders(t, httpURL, fmt.Sprintf("/ipni-provider/%s/ipni/v1/ad/%s", peerID, current), map[string]string{
			ipnisync.CidSchemaHeader: ipnisync.CidSchemaAdvertisement,
		})
		if status != http.StatusOK {
			return 0, fmt.Errorf("unexpected IPNI ad status %d for %s", status, current)
		}

		builder := schema.AdvertisementPrototype.NewBuilder()
		if err := dagjson.Decode(builder, bytes.NewReader(body)); err != nil {
			return 0, fmt.Errorf("decode advertisement %s: %w", current, err)
		}

		ad, err := schema.UnwrapAdvertisement(builder.Build())
		if err != nil {
			return 0, fmt.Errorf("unwrap advertisement %s: %w", current, err)
		}

		count++
		if !ad.PreviousCid().Defined() {
			t.Logf("Reached the end of the IPNI ad chain: %d ads", count)
			if current == headCID {
				t.Logf("Found only one advertisement: %v", current)
			}
			return 0, nil
		}
		current = ad.PreviousCid()
	}

	return count, nil
}
