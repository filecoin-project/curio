package hashfolder

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

	st, err := FirstSetup([]Drive{{StorageID: "a", Root: a, Capacity: 100_000}, {StorageID: "b", Root: b, Capacity: 300_000}})
	require.NoError(t, err)
	for _, sp := range st.HashSpaces {
		require.Len(t, sp, 2)
		require.ElementsMatch(t, []string{"a", "b"}, []string{sp[0].StorageID, sp[1].StorageID})
	}
	for root, used := range map[string]int64{a: 1000, b: 2000} {
		ok, err := isClaim(root)
		require.NoError(t, err)
		require.False(t, ok)
		layout, err := readLayout(layoutPath(root, DIR_OPEN))
		require.NoError(t, err)
		require.Equal(t, used, layout.Used)
		require.Len(t, layout.Ranges, 1)
	}
}

func TestDeleteFindsMisplacedFile(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	_, err := FirstSetup([]Drive{{StorageID: "a", Root: a, Capacity: 1000}, {StorageID: "b", Root: b, Capacity: 1000}})
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

	// Partial copies left by an earlier pass are replaced.
	for _, h := range []string{soleHash, sharedHash} {
		p, err := piecePath(root, DIR_OPEN, h)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, make([]byte, 10), 0o644))
	}

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
	soleInfo, err := os.Stat(dst)
	require.NoError(t, err)
	require.Equal(t, int64(127), soleInfo.Size())
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
	layout, err := readLayout(layoutPath(root, DIR_OPEN))
	require.NoError(t, err)
	require.Equal(t, int64(3*127), layout.Used)

	// The layout now exists, so a second start leaves everything alone.
	claimed, err = migratePiecePark(ctx, db, drives)
	require.NoError(t, err)
	require.Empty(t, claimed)
	_, err = os.Stat(dst)
	require.NoError(t, err)
}

func hasLayouts(t *testing.T, root string) bool {
	t.Helper()
	ok, err := hasBothLayouts(root)
	require.NoError(t, err)
	return ok
}

func TestDropUnusableLayouts(t *testing.T) {
	setup := func(t *testing.T) (string, string, []LocalDrive) {
		a, b := t.TempDir(), t.TempDir()
		_, err := FirstSetup([]Drive{{StorageID: "a", Root: a, Capacity: 1000}, {StorageID: "b", Root: b, Capacity: 1000}})
		require.NoError(t, err)
		return a, b, []LocalDrive{{StorageID: "a", Root: a}, {StorageID: "b", Root: b}}
	}
	t.Run("complete setup is kept", func(t *testing.T) {
		a, b, drives := setup(t)
		require.NoError(t, dropUnusableLayouts(drives, true))
		require.True(t, hasLayouts(t, a))
		require.True(t, hasLayouts(t, b))
	})

	t.Run("interrupted setup", func(t *testing.T) {
		a, b, drives := setup(t)
		require.NoError(t, removeLayouts(b))
		// With ranges in the map, a's layouts are the cluster's to fix.
		require.NoError(t, dropUnusableLayouts(drives, false))
		require.True(t, hasLayouts(t, a))
		require.NoError(t, dropUnusableLayouts(drives, true))
		require.False(t, hasLayouts(t, a))
	})

	t.Run("single layout", func(t *testing.T) {
		a, b, drives := setup(t)
		require.NoError(t, os.Remove(layoutPath(a, DIR_ACL)))
		require.NoError(t, dropUnusableLayouts(drives, false))
		require.False(t, hasLayouts(t, a))
		require.True(t, hasLayouts(t, b))
	})

	t.Run("truncated layout", func(t *testing.T) {
		a, b, drives := setup(t)
		require.NoError(t, os.WriteFile(layoutPath(a, DIR_OPEN), []byte(`{"Version":`), 0o644))
		require.NoError(t, dropUnusableLayouts(drives, false))
		require.False(t, hasLayouts(t, a))
		require.True(t, hasLayouts(t, b))
	})

	t.Run("half rewritten claim", func(t *testing.T) {
		a, b, drives := setup(t)
		layout, err := readLayout(layoutPath(a, DIR_OPEN))
		require.NoError(t, err)
		writeClaim(t, a, 500)
		require.NoError(t, writeLayout(a, DIR_OPEN, layout))
		require.NoError(t, dropUnusableLayouts(drives, false))
		require.False(t, hasLayouts(t, a))
		require.True(t, hasLayouts(t, b))
	})

	t.Run("claims are kept", func(t *testing.T) {
		a, b := t.TempDir(), t.TempDir()
		writeClaim(t, a, 100)
		require.NoError(t, dropUnusableLayouts([]LocalDrive{{StorageID: "a", Root: a}, {StorageID: "b", Root: b}}, true))
		ok, err := isClaim(a)
		require.NoError(t, err)
		require.True(t, ok)
	})
}

func TestFirstSetupAfterLostLayouts(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	_, err := FirstSetup([]Drive{{StorageID: "a", Root: a, Capacity: 100_000}, {StorageID: "b", Root: b, Capacity: 100_000}})
	require.NoError(t, err)
	require.NoError(t, removeLayouts(b))
	require.NoError(t, dropUnusableLayouts([]LocalDrive{{StorageID: "a", Root: a}, {StorageID: "b", Root: b}}, true))
	// migratePiecePark claims a drive that holds pieces; a holds 300 bytes.
	writeClaim(t, a, 300)

	st, err := FirstSetup([]Drive{{StorageID: "a", Root: a, Capacity: 100_000}, {StorageID: "b", Root: b, Capacity: 100_000}})
	require.NoError(t, err)
	for _, sp := range st.HashSpaces {
		require.Len(t, sp, 2)
	}
	layout, err := readLayout(layoutPath(a, DIR_OPEN))
	require.NoError(t, err)
	require.False(t, layout.Claim)
	require.Equal(t, int64(300), layout.Used)
	require.True(t, hasLayouts(t, b))
}
