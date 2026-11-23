package hashspace

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/hashfolder"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/piecestore"
)

func TestPlacementDecision(t *testing.T) {
	tests := []struct {
		name                  string
		forceACL, forceOpen   bool
		anySecured, anyPublic bool
		wantACL, wantOpen     bool
	}{
		{name: "upload before a data set claims it", wantOpen: true},
		{name: "public data set", anyPublic: true, wantOpen: true},
		{name: "restricted data set", anySecured: true, wantACL: true},
		{name: "restricted and public data sets", anySecured: true, anyPublic: true, wantACL: true, wantOpen: true},
		{name: "create-and-add restricted before the row exists", forceACL: true, wantACL: true},
		{name: "create-and-add public before the row exists", forceOpen: true, wantOpen: true},
		{name: "restricted claim of a piece that is also public", forceACL: true, anyPublic: true, wantACL: true, wantOpen: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotACL, gotOpen := placementDecision(tt.forceACL, tt.forceOpen, tt.anySecured, tt.anyPublic)
			if gotACL != tt.wantACL || gotOpen != tt.wantOpen {
				t.Fatalf("placementDecision(%v, %v, %v, %v) = (%v, %v), want (%v, %v)",
					tt.forceACL, tt.forceOpen, tt.anySecured, tt.anyPublic,
					gotACL, gotOpen, tt.wantACL, tt.wantOpen)
			}
		})
	}
}

func TestPlaceACLRetainsPublicCopyForOtherReferences(t *testing.T) {
	for _, tc := range []struct {
		name           string
		otherRef       bool
		separateParked bool
		unrelatedCID   bool
		claimPublic    bool
		wantPublic     bool
	}{
		{name: "sole ACL reference removes public copy"},
		{name: "public reference on same parked piece", otherRef: true, claimPublic: true, wantPublic: true},
		{name: "public reference on different parked piece", otherRef: true, separateParked: true, claimPublic: true, wantPublic: true},
		{name: "unclaimed reference keeps public copy", otherRef: true, wantPublic: true},
		{name: "unrelated CID does not retain public copy", otherRef: true, unrelatedCID: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			t.Cleanup(harmonytask.SetEngineForTest(nil))
			root := t.TempDir()
			index := paths.NewDBIndex(nil, db)
			local, err := paths.NewLocal(t.Context(), &paths.BasicLocalStorage{
				PathToJSON: filepath.Join(root, "storage.json"),
			}, index, "")
			require.NoError(t, err)
			remote, err := paths.NewRemote(local, index, nil, 20, &paths.DefaultPartialFileHandler{})
			require.NoError(t, err)
			pieceIO := piecestore.New(remote, local, index)
			const storageID = "placement-test-disk"
			hs, err := hashfolder.NewCluster(t.Context(), db, []hashfolder.LocalDrive{
				{StorageID: storageID, Root: root},
			}, nil)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, hs.Close()) })

			body := make([]byte, 127)
			v1 := zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(128).Unpadded())
			v2, err := commcid.PieceCidV2FromV1(v1, uint64(len(body)))
			require.NoError(t, err)
			// Repeat placement starts with both previously placed copies and
			// does not revalidate the existing ACL copy's envelope.
			for _, space := range []*hashfolder.ClusterSpace{hs.Public, hs.ACL} {
				n, err := space.WriteLocal(storageID, v2, bytes.NewReader(body))
				require.NoError(t, err)
				require.EqualValues(t, len(body), n)
			}

			insertParked := func(pieceCID string, padded, raw int64, longTerm bool) int64 {
				t.Helper()
				var id int64
				err := db.QueryRow(t.Context(), `INSERT INTO parked_pieces
					(piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
					VALUES ($1, $2, $3, TRUE, $4) RETURNING id`, pieceCID, padded, raw, longTerm).Scan(&id)
				require.NoError(t, err)
				return id
			}
			insertRef := func(parkedID int64, pieceCID string, longTerm bool) int64 {
				t.Helper()
				var ref int64
				err := db.QueryRow(t.Context(), `INSERT INTO parked_piece_refs (piece_id, long_term)
					VALUES ($1, $2) RETURNING ref_id`, parkedID, longTerm).Scan(&ref)
				require.NoError(t, err)
				_, err = db.Exec(t.Context(), `INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref)
					VALUES ('public', $1, $2)`, pieceCID, ref)
				require.NoError(t, err)
				return ref
			}
			parkedID := insertParked(v1.String(), 128, int64(len(body)), true)
			aclRef := insertRef(parkedID, v1.String(), true)
			if tc.otherRef {
				otherParkedID, otherCID, longTerm := parkedID, v1.String(), true
				if tc.separateParked {
					// Long-term and short-term rows can share a CID under the
					// active parked-piece uniqueness constraint.
					longTerm = false
					otherParkedID = insertParked(otherCID, 128, int64(len(body)), longTerm)
				}
				if tc.unrelatedCID {
					otherCID = zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(256).Unpadded()).String()
					otherParkedID = insertParked(otherCID, 256, 254, longTerm)
				}
				otherRef := insertRef(otherParkedID, otherCID, longTerm)
				if tc.claimPublic {
					require.NoError(t, PlaceOpen(t.Context(), db, hs, local, pieceIO, otherRef))
				}
			}

			require.NoError(t, PlaceACL(t.Context(), db, hs, local, pieceIO, aclRef))
			for _, expected := range []struct {
				space   *hashfolder.ClusterSpace
				present bool
			}{
				{space: hs.Public, present: tc.wantPublic},
				{space: hs.ACL, present: true},
			} {
				r, err := expected.space.OpenLocal(storageID, v2)
				if !expected.present {
					require.ErrorIs(t, err, os.ErrNotExist, expected.space.Name())
					continue
				}
				require.NoError(t, err, expected.space.Name())
				got, err := io.ReadAll(r)
				require.NoError(t, err)
				require.NoError(t, r.Close())
				require.Equal(t, body, got, expected.space.Name())
			}
		})
	}
}
