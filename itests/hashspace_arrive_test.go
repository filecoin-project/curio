package itests

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources/ffigpu"
	"github.com/filecoin-project/curio/lib/hashspace"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/openpieces"
)

const (
	hsParkedPieces   = 60
	hsSharedEvery    = 5
	hsResidentPieces = 8
	hsPieceRawSize   = 127
)

type hsDisk struct {
	id   string
	root string
}

func (d hsDisk) drive() hashspace.LocalDrive {
	return hashspace.LocalDrive{StorageID: d.id, Root: d.root}
}

func hsDigest(first byte) []byte {
	d := make([]byte, hashspace.HASH_BYTES)
	d[0] = first
	return d
}

// hsOpenFiles lists the open-pieces files under root by hash with their sizes.
func hsOpenFiles(t *testing.T, root string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	top := filepath.Join(root, hashspace.DIR_OPEN)
	shards, err := os.ReadDir(top)
	require.NoError(t, err)
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(top, shard.Name()))
		require.NoError(t, err)
		for _, f := range files {
			if f.Name()[0] == '.' {
				continue
			}
			info, err := f.Info()
			require.NoError(t, err)
			out[shard.Name()+f.Name()] = info.Size()
		}
	}
	return out
}

// TestHashSpaceArrivingDiskMovesPiecesOff starts with a running cluster of two
// disks, then restarts it with a third disk that holds piece-park pieces. The
// pieces are migrated into open-pieces on the arriving disk, and the rebalance
// moves every piece that another disk owns off of it.
func TestHashSpaceArrivingDiskMovesPiecesOff(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	arriving := hsDisk{id: "hs-arrive-a", root: t.TempDir()}
	resident := []hsDisk{{id: "hs-arrive-b", root: t.TempDir()}, {id: "hs-arrive-c", root: t.TempDir()}}
	all := []hsDisk{arriving, resident[0], resident[1]}

	// The existing cluster, with a few pieces already on its disks.
	existing, err := hashspace.NewCluster(ctx, db, []hashspace.LocalDrive{resident[0].drive(), resident[1].drive()}, nil)
	require.NoError(t, err)
	residentHashes := map[string]string{} // hash -> storage id it was written to
	for i := 0; i < hsResidentPieces; i++ {
		digest := hsDigest(byte(i*32 + 2))
		pc, err := commcid.DataCommitmentToPieceCidv2(digest, hsPieceRawSize)
		require.NoError(t, err)
		owner, err := existing.Target(ctx, digest)
		require.NoError(t, err)
		_, err = existing.WriteLocal(owner, pc, bytes.NewReader(bytes.Repeat([]byte{digest[0]}, hsPieceRawSize)))
		require.NoError(t, err)
		residentHashes[hex.EncodeToString(digest)] = owner
	}
	require.NoError(t, existing.Close())

	// Piece-park pieces on the arriving disk. Every hsSharedEvery-th piece
	// also has a ref outside PDP, so its piece-park copy has to stay.
	_, err = db.Exec(ctx, `INSERT INTO pdp_services (pubkey, service_label) VALUES ('\x01', 'svc')`)
	require.NoError(t, err)
	parkFile := func(id int64) string {
		return filepath.Join(arriving.root, storiface.FTPiece.String(), storiface.SectorName(storiface.PieceNumber(id).Ref().ID))
	}
	parkedHashes := map[string]int64{} // hash -> piece-park id
	var soleIDs, sharedIDs []int64
	for i := 0; i < hsParkedPieces; i++ {
		digest := hsDigest(byte(i*4 + 1))
		v1, err := commcid.DataCommitmentV1ToCID(digest)
		require.NoError(t, err)
		var id, ref int64
		require.NoError(t, db.QueryRow(ctx, `INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
			VALUES ($1, 128, $2, TRUE, TRUE) RETURNING id`, v1.String(), hsPieceRawSize).Scan(&id))
		require.NoError(t, db.QueryRow(ctx, `INSERT INTO parked_piece_refs (piece_id, long_term) VALUES ($1, TRUE) RETURNING ref_id`, id).Scan(&ref))
		_, err = db.Exec(ctx, `INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref) VALUES ('svc', $1, $2)`, v1.String(), ref)
		require.NoError(t, err)
		if i%hsSharedEvery == 0 {
			_, err = db.Exec(ctx, `INSERT INTO parked_piece_refs (piece_id, long_term) VALUES ($1, FALSE)`, id)
			require.NoError(t, err)
			sharedIDs = append(sharedIDs, id)
		} else {
			soleIDs = append(soleIDs, id)
		}
		_, err = db.Exec(ctx, `INSERT INTO sector_location (miner_id, sector_num, sector_filetype, storage_id, is_primary, read_refs)
			VALUES (0, $1, $2, $3, TRUE, 0)`, id, int(storiface.FTPiece), arriving.id)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(parkFile(id)), 0o755))
		require.NoError(t, os.WriteFile(parkFile(id), bytes.Repeat([]byte{digest[0]}, hsPieceRawSize), 0o644))
		parkedHashes[hex.EncodeToString(digest)] = id
	}

	// Restart with the arriving disk. NewCluster migrates piece-park into
	// open-pieces, and no move can start until the task engine below exists.
	cluster, err := hashspace.NewCluster(ctx, db, []hashspace.LocalDrive{arriving.drive(), resident[0].drive(), resident[1].drive()}, nil)
	require.NoError(t, err)

	t.Run("after migration", func(t *testing.T) {
		var misplaced bool
		require.NoError(t, db.QueryRow(ctx, `SELECT has_misplaced FROM hash_space_disk WHERE storage_id = $1`, arriving.id).Scan(&misplaced))
		require.True(t, misplaced, "the arriving disk holds pieces outside its ranges")

		files := hsOpenFiles(t, arriving.root)
		require.Len(t, files, hsParkedPieces)
		for hash := range parkedHashes {
			require.Contains(t, files, hash)
		}

		var locs []int64
		require.NoError(t, db.Select(ctx, &locs, `SELECT sector_num FROM sector_location
			WHERE miner_id = 0 AND storage_id = $1 AND sector_filetype = $2`, arriving.id, int(storiface.FTPiece)))
		require.ElementsMatch(t, sharedIDs, locs, "only pieces with a non-PDP ref keep their piece-park location")
		for _, id := range soleIDs {
			_, err := os.Stat(parkFile(id))
			require.True(t, os.IsNotExist(err), "sole-ref piece %d was renamed out of piece-park", id)
		}
		for _, id := range sharedIDs {
			_, err := os.Stat(parkFile(id))
			require.NoError(t, err, "shared piece %d keeps its piece-park file", id)
		}
	})

	// The move task runs for real: the cluster loop plans the moves once the
	// engine exists, and the engine copies and hands them over.
	engine, err := harmonytask.New(db, []harmonytask.TaskInterface{openpieces.NewMoveTask(db, cluster)},
		"hashspace-itest:1234", noopPeerConnector{}, ffigpu.Inspector{})
	require.NoError(t, err)
	stop := sync.OnceFunc(func() {
		engine.GracefullyTerminate()
		require.NoError(t, cluster.Close())
	})
	t.Cleanup(stop)

	require.Eventually(t, func() bool {
		var moves int
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM hash_space_move_source`).Scan(&moves); err != nil {
			return false
		}
		var misplaced bool
		if err := db.QueryRow(ctx, `SELECT has_misplaced FROM hash_space_disk WHERE storage_id = $1`, arriving.id).Scan(&misplaced); err != nil {
			return false
		}
		return moves == 0 && !misplaced
	}, 3*time.Minute, time.Second, "the arriving disk never finished handing its pieces to their owners")
	stop()

	type rangeRow struct {
		End     []byte `db:"end_hash"`
		Storage string `db:"storage_id"`
	}
	var ranges []rangeRow
	require.NoError(t, db.Select(ctx, &ranges, `SELECT end_hash, storage_id FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, hashspace.DIR_OPEN))
	require.NotEmpty(t, ranges)
	ownerOf := func(digest []byte) string {
		for _, r := range ranges {
			if bytes.Compare(digest, r.End) <= 0 {
				return r.Storage
			}
		}
		return ranges[0].Storage
	}

	t.Run("sql", func(t *testing.T) {
		var moves int
		require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM hash_space_move_source`).Scan(&moves))
		require.Zero(t, moves)

		var flagged int
		require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM hash_space_disk WHERE has_misplaced`).Scan(&flagged))
		require.Zero(t, flagged)

		var disks []string
		require.NoError(t, db.Select(ctx, &disks, `SELECT storage_id FROM hash_space_disk`))
		require.ElementsMatch(t, []string{arriving.id, resident[0].id, resident[1].id}, disks)

		owners := map[string]bool{}
		for _, r := range ranges {
			owners[r.Storage] = true
		}
		require.True(t, owners[resident[0].id] && owners[resident[1].id], "both existing disks keep ranges")

		var locs []int64
		require.NoError(t, db.Select(ctx, &locs, `SELECT sector_num FROM sector_location
			WHERE miner_id = 0 AND storage_id = $1 AND sector_filetype = $2`, arriving.id, int(storiface.FTPiece)))
		require.ElementsMatch(t, sharedIDs, locs, "moving open pieces does not touch piece-park locations")
	})

	t.Run("disk layout", func(t *testing.T) {
		files := map[string]map[string]int64{}
		for _, d := range all {
			files[d.id] = hsOpenFiles(t, d.root)
		}

		var total, movedOff int
		check := func(hash string) {
			digest, err := hex.DecodeString(hash)
			require.NoError(t, err)
			owner := ownerOf(digest)
			for _, d := range all {
				size, present := files[d.id][hash]
				require.Equal(t, d.id == owner, present, "piece %s: owner %s, checking %s", hash, owner, d.id)
				if present {
					require.EqualValues(t, hsPieceRawSize, size)
					total++
				}
			}
		}
		for hash := range parkedHashes {
			check(hash)
			digest, err := hex.DecodeString(hash)
			require.NoError(t, err)
			if ownerOf(digest) != arriving.id {
				movedOff++
			}
		}
		for hash, wrote := range residentHashes {
			check(hash)
			digest, err := hex.DecodeString(hash)
			require.NoError(t, err)
			require.Equal(t, wrote, ownerOf(digest), "resident pieces stay where they were written")
		}
		require.Equal(t, hsParkedPieces+hsResidentPieces, total, "no piece is missing or duplicated")
		require.NotZero(t, movedOff, "some parked pieces belong to the other disks")

		for _, d := range all {
			raw, err := os.ReadFile(filepath.Join(d.root, hashspace.DIR_OPEN, "layout.json"))
			require.NoError(t, err)
			var layout hashspace.Layout
			require.NoError(t, json.Unmarshal(raw, &layout))

			var bytesOnDisk int64
			for _, size := range files[d.id] {
				bytesOnDisk += size
			}
			require.Equal(t, bytesOnDisk, layout.Used, "%s used counter matches its files", d.id)
			require.False(t, layout.Claim, "%s no longer carries a migration claim", d.id)
			require.Empty(t, layout.MoveDests, "%s has no move left", d.id)

			var want []hashspace.HashRange
			for i, r := range ranges {
				if r.Storage != d.id {
					continue
				}
				start := ranges[(i-1+len(ranges))%len(ranges)].End
				want = append(want, hashspace.HashRange{Start: hex.EncodeToString(start), End: hex.EncodeToString(r.End)})
			}
			require.ElementsMatch(t, want, layout.Ranges, "%s layout ranges match hash_space_range", d.id)
		}
	})
}
