// Package openpieces moves PDP pieces between piece-park and the cluster
// hash spaces. Public pieces go to open-pieces. Pieces uploaded with
// read=restricted go to acl-pieces.
package openpieces

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/hashfolder"
	"github.com/filecoin-project/curio/lib/keysmith"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/piecestore"
	"github.com/filecoin-project/curio/lib/storiface"
)

// errPlacementAborted means the PDP ref disappeared while the bytes were
// being copied, and the new file was removed.
var errPlacementAborted = errors.New("piece ref disappeared during placement")

const (
	// waitInterval is how long place and drop pause while the other still
	// owns the piece.
	waitInterval = 5 * time.Second

	// placeDeleteAttempts is how many times placement waits out an in-progress
	// drop before refusing to move the bytes. This is a short wait inside the
	// finalize call, not a harmony-task retry.
	placeDeleteAttempts = 3
)

type parkedPiece struct {
	ID       int64  `db:"id"`
	PieceCID string `db:"piece_cid"`
	RawSize  int64  `db:"piece_raw_size"`
	RefCount int64  `db:"ref_count"`
}

// Place copies the PDP piece for parked piece ref pieceRef into open-pieces,
// or into acl-pieces when pdp_piecerefs.restricted is true. After it returns
// nil, a client can read the piece from that hash space. A nil cluster means
// this node has not joined the hash space, and placement is skipped. When a
// hash_space_delete row is present the bytes are left in piece-park.
func Place(ctx context.Context, db *harmonydb.DB, hs *hashfolder.Cluster, local *paths.Local, pieceIO piecestore.PieceIO, pieceRef int64) error {
	return place(ctx, db, hs, local, pieceIO, pieceRef, false)
}

// PlaceACL copies the piece into acl-pieces. The caller has established that
// the piece was uploaded with read=restricted. An open-pieces copy is removed.
func PlaceACL(ctx context.Context, db *harmonydb.DB, hs *hashfolder.Cluster, local *paths.Local, pieceIO piecestore.PieceIO, pieceRef int64) error {
	return place(ctx, db, hs, local, pieceIO, pieceRef, true)
}

// place writes pieceRef into the hash spaces it belongs in. forceACL puts it
// in acl-pieces even when the restricted column is not yet visible to a
// concurrent reader. The caller has a cluster or has passed a nil cluster,
// which skips the work.
func place(ctx context.Context, db *harmonydb.DB, hs *hashfolder.Cluster, local *paths.Local, pieceIO piecestore.PieceIO, pieceRef int64, forceACL bool) error {
	if hs == nil {
		return nil
	}
	if local == nil || pieceIO == nil {
		return xerrors.Errorf("hash space placement needs local storage and piece IO")
	}
	var parked []parkedPiece
	if err := db.Select(ctx, &parked, `SELECT pp.id, pr.piece_cid, pp.piece_raw_size, pp.ref_count
		FROM pdp_piecerefs pr
		JOIN parked_piece_refs ppr ON ppr.ref_id = pr.piece_ref
		JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE pr.piece_ref = $1`, pieceRef); err != nil {
		return xerrors.Errorf("reading parked piece for ref %d: %w", pieceRef, err)
	}
	if len(parked) == 0 {
		return nil
	}
	pp := parked[0]
	pc, err := pieceCidV2(pp.PieceCID, pp.RawSize)
	if err != nil {
		return err
	}

	wantACL := forceACL
	if !wantACL {
		wantACL, err = pieceRestricted(ctx, db, pieceRef)
		if err != nil {
			return err
		}
	}
	// Restricted pieces never stay in open-pieces.
	wantOpen := !wantACL

	ensure := func(space *hashfolder.ClusterSpace) error {
		placed, err := space.HasFile(ctx, pc)
		if err != nil {
			return xerrors.Errorf("checking %s piece: %w", space.Name(), err)
		}
		if placed {
			return nil
		}
		digest, err := hashfolder.CIDHash(pc)
		if err != nil {
			return err
		}
		target, err := space.Target(ctx, digest)
		if err != nil {
			return err
		}
		var size int64
		var adopted string
		var existed bool
		for attempt := 0; ; attempt++ {
			var busy bool
			if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $1)`, pc.String()).Scan(&busy); err != nil {
				return xerrors.Errorf("checking hash space delete of %s: %w", pc, err)
			}
			if busy {
				if attempt >= placeDeleteAttempts {
					return xerrors.Errorf("hash space delete of %s is in progress", pc)
				}
				if err := sleepTask(ctx, nil); err != nil {
					return err
				}
				continue
			}
			size, adopted, existed, err = writePiece(ctx, hs, local, pieceIO, space, target, pc, pp)
			if err != nil {
				return err
			}
			if size != pp.RawSize {
				undoPlacement(ctx, space, target, pc, existed, adopted)
				return xerrors.Errorf("%s piece %s is %d bytes, expected %d", space.Name(), pc, size, pp.RawSize)
			}
			var refOK, dropBusy bool
			if err := db.QueryRow(ctx, `SELECT
					EXISTS (SELECT 1 FROM pdp_piecerefs WHERE piece_ref = $1),
					EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $2)`,
				pieceRef, pc.String()).Scan(&refOK, &dropBusy); err != nil {
				return xerrors.Errorf("checking placement of %s: %w", pc, err)
			}
			if !refOK {
				undoPlacement(ctx, space, target, pc, existed, adopted)
				return errPlacementAborted
			}
			if dropBusy {
				undoPlacement(ctx, space, target, pc, existed, adopted)
				if attempt >= placeDeleteAttempts-1 {
					return xerrors.Errorf("hash space delete of %s is in progress", pc)
				}
				if err := sleepTask(ctx, nil); err != nil {
					return err
				}
				continue
			}
			break
		}
		placed, err = space.HasFile(ctx, pc)
		if err != nil {
			return err
		}
		if !placed {
			return xerrors.Errorf("%s piece %s disappeared during placement", space.Name(), pc)
		}
		if err := hs.MaybeRebalance(ctx, target); err != nil {
			log.Warnw("checking hash space capacity", "space", space.Name(), "storage", target, "error", err)
		}
		return nil
	}

	if wantOpen {
		if err := ensure(hs.Public); err != nil {
			if errors.Is(err, errPlacementAborted) {
				return nil
			}
			return err
		}
	}
	if wantACL {
		placed, err := hs.ACL.HasFile(ctx, pc)
		if err != nil {
			return xerrors.Errorf("checking acl-pieces for %s: %w", pc, err)
		}
		// The park file is still here the first time an ACL piece is finalized.
		// A copy already in acl-pieces was checked on that write.
		if !placed {
			if err := requireEnvelope(ctx, pieceIO, pp.ID); err != nil {
				return xerrors.Errorf("acl piece %s: %w", pc, err)
			}
		}
		if err := ensure(hs.ACL); err != nil {
			if errors.Is(err, errPlacementAborted) {
				return nil
			}
			return err
		}
		if !wantOpen {
			if err := hs.Public.DeleteCID(ctx, pc.String()); err != nil {
				return xerrors.Errorf("removing open-pieces copy of ACL piece %s: %w", pc, err)
			}
		}
	}

	// Market, sealing and aggregation read the parked piece through their
	// own parked_piece_refs. While they hold one, the piece-park copy stays
	// and the regular piece-park cleanup removes it once every ref, PDP's
	// included, is gone.
	shared, err := hasNonPDPRefs(ctx, db, pp.ID)
	if err != nil {
		return err
	}
	if !shared {
		if err := pieceIO.RemovePiece(ctx, storiface.PieceNumber(pp.ID)); err != nil {
			return xerrors.Errorf("removing piece-park copy of %s: %w", pc, err)
		}
		if shared, err = hasNonPDPRefs(ctx, db, pp.ID); err != nil {
			return err
		}
		if shared {
			if err := restorePark(ctx, hs, pieceIO, pc, pp); err != nil {
				return xerrors.Errorf("restoring piece-park copy of %s for a new non-PDP ref: %w", pc, err)
			}
		}
	}
	return nil
}

// requireEnvelope reads the parked piece and requires a FEE envelope.
// The caller is finalizing a piece into acl-pieces and the park file is still present.
func requireEnvelope(ctx context.Context, pieceIO piecestore.PieceIO, parkedID int64) error {
	rc, err := pieceIO.PieceReader(ctx, storiface.PieceNumber(parkedID))
	if err != nil {
		return xerrors.Errorf("reading parked piece %d: %w", parkedID, err)
	}
	defer func() { _ = rc.Close() }()
	if _, err := keysmith.ReadEnvelopeLabels(rc); err != nil {
		return err
	}
	return nil
}

// pieceRestricted reports whether pieceRef was uploaded with read=restricted.
// pieceRef is parked_piece_refs.ref_id.
func pieceRestricted(ctx context.Context, db *harmonydb.DB, pieceRef int64) (bool, error) {
	var restricted bool
	err := db.QueryRow(ctx, `SELECT restricted FROM pdp_piecerefs WHERE piece_ref = $1`, pieceRef).Scan(&restricted)
	if err != nil {
		return false, xerrors.Errorf("reading pdp_piecerefs.restricted for piece ref %d: %w", pieceRef, err)
	}
	return restricted, nil
}

// pieceCidV2 is the piece CID v2 of a PDP piece CID v1 and its raw size.
func pieceCidV2(pdpPieceCID string, rawSize int64) (cid.Cid, error) {
	v1, err := cid.Parse(pdpPieceCID)
	if err != nil {
		return cid.Undef, xerrors.Errorf("parsing piece cid %s: %w", pdpPieceCID, err)
	}
	v2, err := commcid.PieceCidV2FromV1(v1, uint64(rawSize))
	if err != nil {
		return cid.Undef, xerrors.Errorf("piece cid v2 for %s: %w", pdpPieceCID, err)
	}
	return v2, nil
}

// writePiece puts pc into space on target. A sole-reference piece-park file on
// the same local filesystem is renamed; otherwise the bytes are copied from
// piece-park, or from the other hash space when piece-park is already gone.
// A remote target is written by streaming a PUT to that node. The second
// result is the piece-park path when the file was renamed. The third reports
// that the file was already there.
func writePiece(ctx context.Context, hs *hashfolder.Cluster, local *paths.Local, pieceIO piecestore.PieceIO, space *hashfolder.ClusterSpace, target string, pc cid.Cid, pp parkedPiece) (int64, string, bool, error) {
	openSource := func() (io.ReadCloser, error) {
		r, err := pieceSource(ctx, hs, pieceIO, space, pc, pp)
		if err != nil {
			return nil, xerrors.Errorf("opening copy of %s: %w", pc, err)
		}
		return r, nil
	}
	if !space.HasLocal(target) {
		r, err := openSource()
		if err != nil {
			return 0, "", false, err
		}
		defer func() { _ = r.Close() }()
		size, existed, err := space.PutRemote(ctx, target, pc, hashfolder.WithSize(r, pp.RawSize))
		if err != nil {
			return 0, "", false, err
		}
		return size, "", existed, nil
	}

	if size, ok, err := space.StatLocal(target, pc); err != nil {
		return 0, "", false, err
	} else if ok && size >= pp.RawSize {
		return size, "", true, nil
	} else if ok {
		if err := space.DropLocal(target, pc); err != nil {
			return 0, "", false, xerrors.Errorf("dropping partial copy of %s: %w", pc, err)
		}
	}

	if pp.RefCount == 1 {
		if src, ok := local.ExistingLocalFile(storiface.PieceNumber(pp.ID).Ref().ID, storiface.FTPiece); ok {
			size, err := space.AdoptLocal(target, pc, src)
			switch {
			case err == nil:
				return size, src, false, nil
			case errors.Is(err, os.ErrExist):
				size, _, err := space.StatLocal(target, pc)
				return size, "", true, err
			case !errors.Is(err, hashfolder.ErrCrossDevice):
				return 0, "", false, xerrors.Errorf("renaming %s into %s: %w", pc, space.Name(), err)
			}
		}
	}

	r, err := openSource()
	if err != nil {
		return 0, "", false, err
	}
	defer func() { _ = r.Close() }()
	size, err := space.WriteLocal(target, pc, hashfolder.WithSize(r, pp.RawSize))
	if err != nil {
		return 0, "", false, err
	}
	return size, "", false, nil
}

// pieceSource opens the bytes for pc. Piece-park is used when it still has
// the file. Otherwise the other hash space is read. space is the one being written.
func pieceSource(ctx context.Context, hs *hashfolder.Cluster, pieceIO piecestore.PieceIO, space *hashfolder.ClusterSpace, pc cid.Cid, pp parkedPiece) (io.ReadCloser, error) {
	r, err := pieceIO.PieceReader(ctx, storiface.PieceNumber(pp.ID))
	if err == nil {
		return r, nil
	}
	other := hs.ACL
	if space == hs.ACL {
		other = hs.Public
	}
	locs, lerr := other.Locations(ctx, pc.String())
	if lerr != nil || len(locs) == 0 {
		return nil, err
	}
	last := err
	for _, l := range locs {
		r, oerr := other.Open(ctx, l.StorageID, pc)
		if oerr != nil {
			last = oerr
			continue
		}
		return r, nil
	}
	return nil, last
}

// undoPlacement removes a file this attempt created in space. An adopted file
// goes back to its piece-park path; a copy is deleted. A file that was already
// there is left alone.
func undoPlacement(ctx context.Context, space *hashfolder.ClusterSpace, target string, pc cid.Cid, existed bool, adopted string) {
	if existed {
		return
	}
	if adopted != "" {
		if err := space.ReturnLocal(target, pc, adopted); err != nil {
			log.Errorw("returning piece to piece-park", "space", space.Name(), "piece", pc, "path", adopted, "error", err)
		}
		return
	}
	if err := space.Drop(ctx, target, pc); err != nil {
		log.Warnw("dropping piece after placement rolled back", "space", space.Name(), "piece", pc, "storage", target, "error", err)
	}
}

func sleepTask(ctx context.Context, stillOwned func() bool) error {
	if stillOwned != nil && !stillOwned() {
		return xerrors.Errorf("lost ownership while waiting")
	}
	timer := time.NewTimer(waitInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	if stillOwned != nil && !stillOwned() {
		return xerrors.Errorf("lost ownership while waiting")
	}
	return nil
}

func hasNonPDPRefs(ctx context.Context, db *harmonydb.DB, parkedID int64) (bool, error) {
	var shared bool
	err := db.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM parked_piece_refs r
			WHERE r.piece_id = $1
			  AND NOT EXISTS (SELECT 1 FROM pdp_piecerefs pr WHERE pr.piece_ref = r.ref_id))`, parkedID).Scan(&shared)
	if err != nil {
		return false, xerrors.Errorf("checking non-PDP refs of parked piece %d: %w", parkedID, err)
	}
	return shared, nil
}

// restorePark writes the piece-park copy back from a hash space when a non-PDP
// ref attached while the copy was being removed. Open-pieces is tried first.
func restorePark(ctx context.Context, hs *hashfolder.Cluster, pieceIO piecestore.PieceIO, pc cid.Cid, pp parkedPiece) error {
	lastErr := xerrors.Errorf("%s has no hash space location", pc)
	for _, space := range []*hashfolder.ClusterSpace{hs.Public, hs.ACL} {
		locs, err := space.Locations(ctx, pc.String())
		if err != nil {
			lastErr = err
			continue
		}
		for _, l := range locs {
			r, err := space.Open(ctx, l.StorageID, pc)
			if err != nil {
				lastErr = err
				continue
			}
			err = pieceIO.WritePiece(ctx, nil, storiface.PieceNumber(pp.ID), pp.RawSize, r, storiface.PathStorage)
			_ = r.Close()
			if err != nil {
				lastErr = err
				continue
			}
			return nil
		}
	}
	return lastErr
}
