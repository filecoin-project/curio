package pdpv0

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/ipfs/go-cid"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
	ipnitypes "github.com/filecoin-project/curio/market/ipni/types"
)

type pieceGCRemoval struct {
	piece     cid.Cid
	aggregate bool
}

type pieceGCIndexCleaner struct {
	calls  []pieceGCRemoval
	failAt int
}

func (c *pieceGCIndexCleaner) remove(piece cid.Cid, aggregate bool) error {
	c.calls = append(c.calls, pieceGCRemoval{piece: piece, aggregate: aggregate})
	if len(c.calls) == c.failAt {
		return errors.New("injected index cleanup failure")
	}
	return nil
}

func (c *pieceGCIndexCleaner) RemoveIndexes(_ context.Context, piece cid.Cid) error {
	return c.remove(piece, false)
}

func (c *pieceGCIndexCleaner) RemoveAggregateIndex(_ context.Context, piece cid.Cid) error {
	return c.remove(piece, true)
}

type pieceGCFixture struct {
	v1, v2      cid.Cid
	parkedPiece int64
	pieceRef    int64
	pdpRef      int64
	key         crypto.PrivKey
	provider    string
}

func newPieceGCFixture(t *testing.T) (*harmonydb.DB, pieceGCFixture) {
	t.Helper()
	t.Setenv("DEV_CURIO_EXTERNAL_URL", "http://127.0.0.1:12310")
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	piece := pieceGCFixture{v1: zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(notifyTestPaddedSize).Unpadded())}
	piece.v2, err = commcid.PieceCidV2FromV1(piece.v1, uint64(notifyTestRawSize))
	require.NoError(t, err)
	upload := newNotifyUploadFixture("piece-gc")
	upload.pieceCID = piece.v1.String()
	insertParkedPiece(t, db, &upload, true, false, nil)
	piece.parkedPiece, piece.pieceRef = upload.parkedPiece, upload.pieceRef
	err = db.QueryRow(t.Context(), `
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
		VALUES ('public', $1, $2, TIMEZONE('UTC', NOW()) - INTERVAL '3 hours', FALSE) RETURNING id`,
		piece.v1.String(), piece.pieceRef).Scan(&piece.pdpRef)
	require.NoError(t, err)

	piece.key, _, err = crypto.GenerateEd25519Key(rand.Reader)
	require.NoError(t, err)
	keyBytes, err := crypto.MarshalPrivateKey(piece.key)
	require.NoError(t, err)
	provider, err := peer.IDFromPublicKey(piece.key.GetPublic())
	require.NoError(t, err)
	piece.provider = provider.String()
	_, err = db.Exec(t.Context(), `INSERT INTO ipni_peerid (sp_id, priv_key, peer_id) VALUES (-2, $1, $2)`, keyBytes, piece.provider)
	require.NoError(t, err)
	return db, piece
}

func insertPieceGCAdvertisement(t *testing.T, db *harmonydb.DB, piece pieceGCFixture, removal bool) {
	t.Helper()
	mds := metadata.IpfsGatewayHttp{}
	md, err := mds.MarshalBinary()
	require.NoError(t, err)
	pi := ipnitypes.PdpIpniContext{PieceCID: piece.v2, Payload: true}
	contextID, err := pi.Marshal()
	require.NoError(t, err)
	ad := schema.Advertisement{
		Provider:  piece.provider,
		Addresses: []string{"/ip4/127.0.0.1/tcp/12310/http"},
		ContextID: contextID,
		Metadata:  md,
		Entries:   schema.NoEntries,
		IsRm:      removal,
	}
	require.NoError(t, ad.Sign(piece.key))
	node, err := ad.ToNode()
	require.NoError(t, err)
	link, err := ipniculib.NodeToLink(node, schema.Linkproto)
	require.NoError(t, err)
	var inserted bool
	err = db.QueryRow(t.Context(), `SELECT insert_ad_and_update_head_checked($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULL)`,
		link.(cidlink.Link).Cid.String(), contextID, md, piece.v2.String(), piece.v1.String(), notifyTestPaddedSize,
		removal, piece.provider, ad.Addresses[0], ad.Signature, ad.Entries.String()).Scan(&inserted)
	require.NoError(t, err)
	require.True(t, inserted)
}

func assertPieceGCState(t *testing.T, db *harmonydb.DB, piece pieceGCFixture, refsRemain bool, removalAds int) {
	t.Helper()
	var pdpRef, parkedRef bool
	var ads int
	err := db.QueryRow(t.Context(), `SELECT
		EXISTS(SELECT 1 FROM pdp_piecerefs WHERE id = $1),
		EXISTS(SELECT 1 FROM parked_piece_refs WHERE ref_id = $2),
		(SELECT count(*) FROM ipni WHERE piece_cid = $3 AND is_rm = TRUE)`,
		piece.pdpRef, piece.pieceRef, piece.v1.String()).Scan(&pdpRef, &parkedRef, &ads)
	require.NoError(t, err)
	require.Equal(t, refsRemain, pdpRef, "PDP reference")
	require.Equal(t, refsRemain, parkedRef, "parked reference")
	require.Equal(t, removalAds, ads, "removal advertisements")
}

func insertSharedPieceGCReference(t *testing.T, db *harmonydb.DB, piece pieceGCFixture) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(t.Context(), `WITH ref AS (
		INSERT INTO parked_piece_refs (piece_id, long_term) VALUES ($1, TRUE) RETURNING ref_id
	) INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
		SELECT 'public', $2, ref_id, TIMEZONE('UTC', NOW()), FALSE FROM ref RETURNING id`, piece.parkedPiece, piece.v1.String()).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestPieceGCIndexFailuresRetainReferencesForRetry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{name: "legacy indexes", failAt: 1},
		{name: "v2 indexes", failAt: 2},
		{name: "aggregate indexes", failAt: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, piece := newPieceGCFixture(t)
			insertPieceGCAdvertisement(t, db, piece, false)
			cfg, keepHours := &config.HTTPConfig{}, config.NewDynamic(2)
			pieces, err := processIPNICleanup(t.Context(), db, cfg, keepHours)
			require.NoError(t, err)
			require.Len(t, pieces, 1)
			assertPieceGCState(t, db, piece, true, 1)

			want := []pieceGCRemoval{{piece: piece.v1}, {piece: piece.v2}, {piece: piece.v2, aggregate: true}}
			cleaner := &pieceGCIndexCleaner{failAt: tc.failAt}
			err = processIndexCleanup(t.Context(), db, cleaner, pieces)
			require.ErrorContains(t, err, "injected index cleanup failure")
			require.Equal(t, want[:tc.failAt], cleaner.calls)
			assertPieceGCState(t, db, piece, true, 1)

			pieces, err = processIPNICleanup(t.Context(), db, cfg, keepHours)
			require.NoError(t, err)
			require.Len(t, pieces, 1)
			assertPieceGCState(t, db, piece, true, 1)
			cleaner.failAt, cleaner.calls = 0, nil
			require.NoError(t, processIndexCleanup(t.Context(), db, cleaner, pieces))
			require.Equal(t, want, cleaner.calls)
			assertPieceGCState(t, db, piece, false, 1)
		})
	}
}

func TestPieceGCAdvertisementAndSharedReferenceGuards(t *testing.T) {
	for _, tc := range []struct {
		name        string
		advertise   bool
		removal     bool
		shared      bool
		removalAds  int
		deleteIndex bool
	}{
		{name: "existing removal", advertise: true, removal: true, removalAds: 1, deleteIndex: true},
		{name: "no advertisement"},
		{name: "shared reference", advertise: true, shared: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, piece := newPieceGCFixture(t)
			if tc.advertise {
				insertPieceGCAdvertisement(t, db, piece, tc.removal)
			}
			var sharedID int64
			if tc.shared {
				sharedID = insertSharedPieceGCReference(t, db, piece)
			}
			pieces, err := processIPNICleanup(t.Context(), db, &config.HTTPConfig{}, config.NewDynamic(2))
			require.NoError(t, err)
			assertPieceGCState(t, db, piece, !tc.shared, tc.removalAds)
			cleaner := &pieceGCIndexCleaner{}
			require.NoError(t, processIndexCleanup(t.Context(), db, cleaner, pieces))
			if tc.deleteIndex {
				require.Equal(t, []pieceGCRemoval{{piece: piece.v1}, {piece: piece.v2}, {piece: piece.v2, aggregate: true}}, cleaner.calls)
			} else {
				require.Empty(t, cleaner.calls)
			}
			assertPieceGCState(t, db, piece, false, tc.removalAds)
			if tc.shared {
				var exists bool
				require.NoError(t, db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pdp_piecerefs WHERE id = $1)`, sharedID).Scan(&exists))
				require.True(t, exists)
			}
		})
	}
}

func TestPieceGCNewReferenceProtectsIndexesBetweenPhases(t *testing.T) {
	db, piece := newPieceGCFixture(t)
	insertPieceGCAdvertisement(t, db, piece, false)
	pieces, err := processIPNICleanup(t.Context(), db, &config.HTTPConfig{}, config.NewDynamic(2))
	require.NoError(t, err)
	require.Len(t, pieces, 1)
	sharedID := insertSharedPieceGCReference(t, db, piece)
	cleaner := &pieceGCIndexCleaner{}
	require.NoError(t, processIndexCleanup(t.Context(), db, cleaner, pieces))
	require.Empty(t, cleaner.calls)
	assertPieceGCState(t, db, piece, false, 1)
	var exists bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pdp_piecerefs WHERE id = $1)`, sharedID).Scan(&exists))
	require.True(t, exists)
}

func TestPieceGCTwoEligibleReferencesCleanIndexesOnce(t *testing.T) {
	db, piece := newPieceGCFixture(t)
	insertPieceGCAdvertisement(t, db, piece, false)
	sharedID := insertSharedPieceGCReference(t, db, piece)
	_, err := db.Exec(t.Context(), `UPDATE pdp_piecerefs SET created_at = TIMEZONE('UTC', NOW()) - INTERVAL '3 hours' WHERE id = $1`, sharedID)
	require.NoError(t, err)

	assertCounts := func(wantRefs, wantAds int) {
		t.Helper()
		var pdpRefs, parkedRefs, ads int
		err := db.QueryRow(t.Context(), `SELECT
			(SELECT count(*) FROM pdp_piecerefs WHERE piece_cid = $1),
			(SELECT count(*) FROM parked_piece_refs WHERE piece_id = $2),
			(SELECT count(*) FROM ipni WHERE piece_cid = $1 AND is_rm = TRUE)`,
			piece.v1.String(), piece.parkedPiece).Scan(&pdpRefs, &parkedRefs, &ads)
		require.NoError(t, err)
		require.Equal(t, wantRefs, pdpRefs, "PDP references")
		require.Equal(t, wantRefs, parkedRefs, "parked references")
		require.Equal(t, wantAds, ads, "removal advertisements")
	}
	assertCounts(2, 0)
	pieces, err := processIPNICleanup(t.Context(), db, &config.HTTPConfig{}, config.NewDynamic(2))
	require.NoError(t, err)
	require.Len(t, pieces, 1)
	assertCounts(1, 1)

	cleaner := &pieceGCIndexCleaner{}
	require.NoError(t, processIndexCleanup(t.Context(), db, cleaner, pieces))
	require.Equal(t, []pieceGCRemoval{{piece: piece.v1}, {piece: piece.v2}, {piece: piece.v2, aggregate: true}}, cleaner.calls)
	assertCounts(0, 1)
}

func TestPieceGCPendingAddProtectsReferencesBetweenPhases(t *testing.T) {
	db, piece := newPieceGCFixture(t)
	insertPieceGCAdvertisement(t, db, piece, false)
	pieces, err := processIPNICleanup(t.Context(), db, &config.HTTPConfig{}, config.NewDynamic(2))
	require.NoError(t, err)
	require.Len(t, pieces, 1)
	assertPieceGCState(t, db, piece, true, 1)

	const PENDING_HASH = "0x0000000000000000000000000000000000000000000000000000000000000001"
	_, err = db.Exec(t.Context(), `INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending')`, PENDING_HASH)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO pdp_data_set_piece_adds (
		piece, add_message_hash, add_message_index, sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref
	) VALUES ($1, $2, 0, $1, 0, $3, $4)`, piece.v1.String(), PENDING_HASH, notifyTestPaddedSize, piece.pdpRef)
	require.NoError(t, err)

	cleaner := &pieceGCIndexCleaner{}
	require.NoError(t, processIndexCleanup(t.Context(), db, cleaner, pieces))
	require.Empty(t, cleaner.calls)
	assertPieceGCState(t, db, piece, true, 1)
	pieces, err = processIPNICleanup(t.Context(), db, &config.HTTPConfig{}, config.NewDynamic(2))
	require.NoError(t, err)
	require.Empty(t, pieces)
}
