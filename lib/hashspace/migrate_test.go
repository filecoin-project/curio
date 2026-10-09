package hashspace

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/storiface"
)

func writeClaim(t *testing.T, root string, used int64) {
	t.Helper()
	for _, kind := range spaceKinds {
		n := int64(0)
		if kind == DIR_OPEN {
			n = used
		}
		require.NoError(t, writeLayout(root, kind, Layout{
			Used:        n,
			CommittedAt: time.Now().UTC().Truncate(time.Second),
			Split:       SPLIT,
			Ranges:      []HashRange{{Start: fullCircle, End: fullCircle}},
			Claim:       true,
		}))
	}
}

func TestFirstSetupSeedsClaimsKeepingUsed(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeClaim(t, a, 1000)
	writeClaim(t, b, 2000)
	ok, err := isClaim(a)
	require.NoError(t, err)
	require.True(t, ok)

	st, err := FirstSetup([]Drive{{Root: a, Capacity: 100_000}, {Root: b, Capacity: 300_000}})
	require.NoError(t, err)
	for _, sp := range st.Spaces {
		require.Len(t, sp.Ranges, 2)
		require.ElementsMatch(t, []int{0, 1}, sp.Owner)
	}
	for root, used := range map[string]int64{a: 1000, b: 2000} {
		ok, err := isClaim(root)
		require.NoError(t, err)
		require.False(t, ok)
		layout, err := readLayout(filepath.Join(root, DIR_OPEN, layoutFile))
		require.NoError(t, err)
		require.Equal(t, used, layout.Used)
		require.Len(t, layout.Ranges, 1)
	}
}

func TestDeleteFindsMisplacedFile(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	_, err := FirstSetup([]Drive{{Root: a, Capacity: 1000}, {Root: b, Capacity: 1000}})
	require.NoError(t, err)
	sp := mustLoad(t, DIR_OPEN, a, b)

	// Find a piece owned by a, then put its file on b only.
	var c cid.Cid
	for i := 0; i < 64; i++ {
		c = mustPiece(t, byte(i))
		_, digest, err := cidHashHex(c)
		require.NoError(t, err)
		if d, ok := sp.locate(digest); ok && d.root == a {
			break
		}
	}
	w, err := sp.WriteCIDOn(b, c)
	require.NoError(t, err)
	_, err = w.Write([]byte("misplaced"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	require.NoError(t, sp.DeleteCID(c))
	path, err := piecePath(b, DIR_OPEN, mustHashHex(t, c))
	require.NoError(t, err)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))

	// New writes still land on the range owner.
	w, err = sp.WriteCID(c)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	path, err = piecePath(a, DIR_OPEN, mustHashHex(t, c))
	require.NoError(t, err)
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func testDB(t *testing.T) *harmonydb.DB {
	t.Helper()
	host := os.Getenv("CURIO_HARMONYDB_HOSTS")
	if host == "" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "5433"), time.Second)
	if err != nil {
		t.Skipf("no YugabyteDB at %s:5433: %v", host, err)
	}
	_ = conn.Close()
	db, err := harmonydb.NewFromConfig(harmonydb.Config{
		Hosts:    []string{host},
		Database: "yugabyte",
		Username: "yugabyte",
		Password: "yugabyte",
		Port:     "5433",
		ITestID:  harmonydb.ITestNewID(),
	})
	require.NoError(t, err)
	t.Cleanup(db.ITestDeleteAll)
	return db
}

func TestMigratePieceParkRenamesSoleRefs(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	root := t.TempDir()
	const storageID = "migrate-disk"

	_, err := db.Exec(ctx, `INSERT INTO pdp_services (pubkey, service_label) VALUES ('\x01', 'svc')`)
	require.NoError(t, err)

	park := func(first byte, extraRef bool) (int64, string) {
		digest := make([]byte, HASH_BYTES)
		digest[0] = first
		v1, err := commcid.DataCommitmentV1ToCID(digest)
		require.NoError(t, err)
		v2, err := commcid.DataCommitmentToPieceCidv2(digest, 127)
		require.NoError(t, err)
		var id int64
		require.NoError(t, db.QueryRow(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
			VALUES ($1, 128, 127, TRUE, TRUE) RETURNING id`, v1.String()).Scan(&id))
		var ref int64
		require.NoError(t, db.QueryRow(ctx, `INSERT INTO parked_piece_refs (piece_id, long_term) VALUES ($1, TRUE) RETURNING ref_id`, id).Scan(&ref))
		_, err = db.Exec(ctx, `INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref) VALUES ('svc', $1, $2)`, v1.String(), ref)
		require.NoError(t, err)
		if extraRef {
			_, err = db.Exec(ctx, `INSERT INTO parked_piece_refs (piece_id, long_term) VALUES ($1, FALSE)`, id)
			require.NoError(t, err)
		}
		_, err = db.Exec(ctx, `INSERT INTO sector_location (miner_id, sector_num, sector_filetype, storage_id, is_primary, read_refs)
			VALUES (0, $1, $2, $3, TRUE, 0)`, id, int(storiface.FTPiece), storageID)
		require.NoError(t, err)
		src := filepath.Join(root, storiface.FTPiece.String(), storiface.SectorName(storiface.PieceNumber(id).Ref().ID))
		require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
		require.NoError(t, os.WriteFile(src, make([]byte, 127), 0o644))
		hexHash, _, err := cidHashHex(v2)
		require.NoError(t, err)
		return id, hexHash
	}
	soleID, soleHash := park(0x11, false)
	sharedID, sharedHash := park(0x22, true)

	// An earlier pass renamed this piece, then it gained a second ref: the
	// piece-park copy comes back and the open-pieces file stays.
	gainedID, gainedHash := park(0x33, true)
	gainedSrc := filepath.Join(root, storiface.FTPiece.String(), storiface.SectorName(storiface.PieceNumber(gainedID).Ref().ID))
	gainedDst, err := piecePath(root, DIR_OPEN, gainedHash)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(gainedDst), 0o755))
	require.NoError(t, os.Rename(gainedSrc, gainedDst))

	drives := []LocalDrive{{StorageID: storageID, Root: root}}
	claimed, err := migratePiecePark(ctx, db, drives)
	require.NoError(t, err)
	require.Equal(t, []string{storageID}, claimed)

	dst, err := piecePath(root, DIR_OPEN, soleHash)
	require.NoError(t, err)
	_, err = os.Stat(dst)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, storiface.FTPiece.String(), storiface.SectorName(storiface.PieceNumber(soleID).Ref().ID)))
	require.True(t, os.IsNotExist(err))
	// The shared piece is copied; its piece-park file and location stay.
	_, err = os.Stat(filepath.Join(root, storiface.FTPiece.String(), storiface.SectorName(storiface.PieceNumber(sharedID).Ref().ID)))
	require.NoError(t, err)
	sharedDst, err := piecePath(root, DIR_OPEN, sharedHash)
	require.NoError(t, err)
	info, err := os.Stat(sharedDst)
	require.NoError(t, err)
	require.Equal(t, int64(127), info.Size())

	for _, p := range []string{gainedSrc, gainedDst} {
		info, err := os.Stat(p)
		require.NoError(t, err)
		require.Equal(t, int64(127), info.Size())
	}

	var locs []int64
	require.NoError(t, db.Select(ctx, &locs, `SELECT sector_num FROM sector_location WHERE miner_id = 0 AND storage_id = $1 ORDER BY sector_num`, storageID))
	require.Equal(t, []int64{sharedID, gainedID}, locs)

	ok, err := isClaim(root)
	require.NoError(t, err)
	require.True(t, ok)
	layout, err := readLayout(filepath.Join(root, DIR_OPEN, layoutFile))
	require.NoError(t, err)
	require.Equal(t, int64(3*127), layout.Used)

	// layout.json now exists, so a second start leaves everything alone.
	claimed, err = migratePiecePark(ctx, db, drives)
	require.NoError(t, err)
	require.Empty(t, claimed)
	_, err = os.Stat(dst)
	require.NoError(t, err)
}
