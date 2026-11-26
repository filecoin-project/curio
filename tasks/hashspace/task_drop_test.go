package hashspace

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/hashfolder"
)

// TestDropRemovesACLPiece writes a piece only into acl-pieces, runs HashSpaceDrop,
// and checks the acl-pieces file is gone.
func TestDropRemovesACLPiece(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "sectorstore.json"), []byte(`{"MaxStorage":1000000}`), 0o644))
	drive := hashfolder.LocalDrive{StorageID: "drop-acl-test", Root: root}
	hs, err := hashfolder.NewCluster(ctx, db, []hashfolder.LocalDrive{drive}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, hs.Close()) })

	raw := bytes.Repeat([]byte{0xac}, 127)
	calc := &commp.Calc{}
	_, err = calc.Write(raw)
	require.NoError(t, err)
	digest, _, err := calc.Digest()
	require.NoError(t, err)
	pc, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(len(raw)))
	require.NoError(t, err)

	digestHash, err := hashfolder.CIDHash(pc)
	require.NoError(t, err)
	target, err := hs.ACL.Target(ctx, digestHash)
	require.NoError(t, err)
	_, err = hs.ACL.WriteLocal(target, pc, hashfolder.WithSize(bytes.NewReader(raw), int64(len(raw))))
	require.NoError(t, err)
	inACL, err := hs.ACL.HasFile(ctx, pc)
	require.NoError(t, err)
	require.True(t, inACL)
	inOpen, err := hs.Public.HasFile(ctx, pc)
	require.NoError(t, err)
	require.False(t, inOpen)

	const taskID harmonytask.TaskID = 9001
	_, err = db.Exec(ctx, `INSERT INTO hash_space_delete (piece_cid, task_id) VALUES ($1, $2)`, pc.String(), taskID)
	require.NoError(t, err)

	drop := NewDropTask(db, hs)
	done, err := drop.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)

	inACL, err = hs.ACL.HasFile(ctx, pc)
	require.NoError(t, err)
	require.False(t, inACL, "HashSpaceDrop must remove acl-pieces copies")
	var left int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM hash_space_delete WHERE piece_cid = $1`, pc.String()).Scan(&left))
	require.Equal(t, 0, left)
}

func TestDropPiecePreservesLiveReferencesAndDeletesReleasedFile(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	t.Cleanup(harmonytask.SetEngineForTest(nil))
	const storageID = "drop-test-disk"
	hs, err := hashfolder.NewCluster(t.Context(), db, []hashfolder.LocalDrive{
		{StorageID: storageID, Root: t.TempDir()},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, hs.Close()) })

	body := make([]byte, 127)
	v1 := zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(128).Unpadded())
	v2, err := commcid.PieceCidV2FromV1(v1, uint64(len(body)))
	require.NoError(t, err)
	spaces := []*hashfolder.ClusterSpace{hs.Public, hs.ACL}
	for _, space := range spaces {
		n, err := space.WriteLocal(storageID, v2, bytes.NewReader(body))
		require.NoError(t, err)
		require.EqualValues(t, len(body), n)
	}
	assertReadable := func() {
		t.Helper()
		for _, space := range spaces {
			r, err := space.OpenLocal(storageID, v2)
			require.NoError(t, err, space.Name())
			got, err := io.ReadAll(r)
			require.NoError(t, err)
			require.NoError(t, r.Close())
			require.Equal(t, body, got, space.Name())
		}
	}

	var pieceID, refID, pdpRefID int64
	err = db.QueryRow(t.Context(), `INSERT INTO parked_pieces
		(piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES ($1, 128, $2, TRUE, TRUE) RETURNING id`, v1.String(), len(body)).Scan(&pieceID)
	require.NoError(t, err)
	err = db.QueryRow(t.Context(), `INSERT INTO parked_piece_refs (piece_id, long_term)
		VALUES ($1, TRUE) RETURNING ref_id`, pieceID).Scan(&refID)
	require.NoError(t, err)
	err = db.QueryRow(t.Context(), `INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref)
		VALUES ('public', $1, $2) RETURNING id`, v1.String(), refID).Scan(&pdpRefID)
	require.NoError(t, err)
	var sharedRefID, sharedPDPRefID int64
	err = db.QueryRow(t.Context(), `INSERT INTO parked_piece_refs (piece_id, long_term)
		VALUES ($1, TRUE) RETURNING ref_id`, pieceID).Scan(&sharedRefID)
	require.NoError(t, err)
	err = db.QueryRow(t.Context(), `INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref)
		VALUES ('public', $1, $2) RETURNING id`, v1.String(), sharedRefID).Scan(&sharedPDPRefID)
	require.NoError(t, err)

	const taskID harmonytask.TaskID = 234
	queueDrop := func() {
		t.Helper()
		_, err := db.Exec(t.Context(), `INSERT INTO hash_space_delete (piece_cid, task_id)
			VALUES ($1, $2)`, v2.String(), taskID)
		require.NoError(t, err)
	}
	task := NewDropTask(db, hs)
	queueDrop()
	done, err := task.Do(t.Context(), taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	var queued bool
	err = db.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $1)`, v2.String()).Scan(&queued)
	require.NoError(t, err)
	require.False(t, queued, "a live PDP reference cancels deletion")
	assertReadable()

	// Releasing one reference must leave both copies available to the other.
	_, err = db.Exec(t.Context(), `DELETE FROM pdp_piecerefs WHERE id = $1`, pdpRefID)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
	require.NoError(t, err)
	queueDrop()
	done, err = task.Do(t.Context(), taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	err = db.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $1)`, v2.String()).Scan(&queued)
	require.NoError(t, err)
	require.False(t, queued, "the remaining PDP reference protects both namespaces")
	assertReadable()

	// A non-PDP parked reference also retains the migrated file. Unlike a
	// live PDP reference, it leaves the deletion queued to wait for release.
	_, err = db.Exec(t.Context(), `DELETE FROM pdp_piecerefs WHERE id = $1`, sharedPDPRefID)
	require.NoError(t, err)
	queueDrop()
	waitCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)
	done, err = task.Do(waitCtx, taskID, func() bool { return true })
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, done)
	var started bool
	err = db.QueryRow(t.Context(), `SELECT started FROM hash_space_delete WHERE piece_cid = $1`, v2.String()).Scan(&started)
	require.NoError(t, err)
	require.False(t, started)
	assertReadable()

	_, err = db.Exec(t.Context(), `DELETE FROM parked_piece_refs WHERE ref_id = $1`, sharedRefID)
	require.NoError(t, err)
	done, err = task.Do(t.Context(), taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	for _, space := range spaces {
		_, err = space.OpenLocal(storageID, v2)
		require.ErrorIs(t, err, os.ErrNotExist, "the last reference released the file in %s", space.Name())
	}
	err = db.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $1)`, v2.String()).Scan(&queued)
	require.NoError(t, err)
	require.False(t, queued)
}

func TestDropPieceRemoteClientDeletesRemoteFile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		public  bool
		acl     bool
		failACL bool
	}{
		{name: "public only", public: true},
		{name: "ACL only", acl: true},
		{name: "both copies", public: true, acl: true},
		{name: "ACL failure after public deletion retries", public: true, acl: true, failACL: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			t.Cleanup(harmonytask.SetEngineForTest(nil))
			const storageID = "remote-drop-test-disk"
			owner, err := hashfolder.NewCluster(t.Context(), db, []hashfolder.LocalDrive{
				{StorageID: storageID, Root: t.TempDir()},
			}, nil)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, owner.Close()) })

			body := make([]byte, 127)
			v1 := zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(128).Unpadded())
			v2, err := commcid.PieceCidV2FromV1(v1, uint64(len(body)))
			require.NoError(t, err)
			for _, stored := range []struct {
				space   *hashfolder.ClusterSpace
				present bool
			}{
				{space: owner.Public, present: tc.public},
				{space: owner.ACL, present: tc.acl},
			} {
				if stored.present {
					n, err := stored.space.WriteLocal(storageID, v2, bytes.NewReader(body))
					require.NoError(t, err)
					require.EqualValues(t, len(body), n)
				}
			}
			assertCopy := func(space *hashfolder.ClusterSpace, present bool) {
				t.Helper()
				r, err := space.OpenLocal(storageID, v2)
				if !present {
					require.ErrorIs(t, err, os.ErrNotExist, space.Name())
					return
				}
				require.NoError(t, err, space.Name())
				got, err := io.ReadAll(r)
				require.NoError(t, err)
				require.NoError(t, r.Close())
				require.Equal(t, body, got, space.Name())
			}
			assertCopy(owner.Public, tc.public)
			assertCopy(owner.ACL, tc.acl)

			const authorization = "Bearer remote-drop-test"
			var publicDeletes, aclDeletes atomic.Int64
			var failACL atomic.Bool
			failACL.Store(tc.failACL)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != authorization {
					http.Error(w, "missing storage authentication", http.StatusUnauthorized)
					return
				}
				if r.Method == http.MethodDelete {
					switch r.URL.Query().Get("space") {
					case hashfolder.DIR_OPEN:
						publicDeletes.Add(1)
					case hashfolder.DIR_ACL:
						aclDeletes.Add(1)
						if failACL.Load() {
							http.Error(w, "injected ACL deletion failure", http.StatusInternalServerError)
							return
						}
					default:
						http.Error(w, "unexpected hashspace", http.StatusBadRequest)
						return
					}
				}
				owner.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			_, err = db.Exec(t.Context(), `INSERT INTO storage_path (storage_id, urls)
				VALUES ($1, $2)`, storageID, server.URL+"/remote")
			require.NoError(t, err)

			worker := hashfolder.NewRemoteClient(db, http.Header{
				"Authorization": []string{authorization},
			})
			// Piece cleanup has already released the metadata. Only the durable
			// deletion request and the remote bytes remain for this worker.
			const taskID harmonytask.TaskID = 345
			_, err = db.Exec(t.Context(), `INSERT INTO hash_space_delete (piece_cid, task_id)
				VALUES ($1, $2)`, v2.String(), taskID)
			require.NoError(t, err)
			task := NewDropTask(db, worker)
			done, err := task.Do(t.Context(), taskID, func() bool { return true })
			if tc.failACL {
				require.ErrorContains(t, err, "500")
				require.False(t, done)
				assertCopy(owner.Public, false)
				assertCopy(owner.ACL, true)
				var started bool
				var queuedTask harmonytask.TaskID
				err = db.QueryRow(t.Context(), `SELECT started, task_id
					FROM hash_space_delete WHERE piece_cid = $1`, v2.String()).Scan(&started, &queuedTask)
				require.NoError(t, err)
				require.True(t, started, "a partial deletion must remain queued for retry")
				require.Equal(t, taskID, queuedTask)
				failACL.Store(false)
				done, err = task.Do(t.Context(), taskID, func() bool { return true })
			}
			require.NoError(t, err)
			require.True(t, done)
			require.Positive(t, publicDeletes.Load(), "deletion must address the public namespace")
			require.Positive(t, aclDeletes.Load(), "deletion must address the ACL namespace")
			assertCopy(owner.Public, false)
			assertCopy(owner.ACL, false)
			var queued bool
			err = db.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $1)`, v2.String()).Scan(&queued)
			require.NoError(t, err)
			require.False(t, queued)
		})
	}
}
