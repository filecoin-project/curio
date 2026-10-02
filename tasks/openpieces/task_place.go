// Package openpieces moves PDP pieces between piece-park and the cluster
// open-pieces hash space.
package openpieces

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/hashspace"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/piecestore"
	"github.com/filecoin-project/curio/lib/storiface"
)

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

// Place copies the PDP piece for parked piece ref pieceRef into open-pieces
// before the caller returns. After it returns nil, a client can read the
// piece from the hash space. A nil cluster means this node has not joined
// the hash space, and placement is skipped. When a hash_space_delete row is
// present the bytes are left in piece-park.
func Place(ctx context.Context, db *harmonydb.DB, hs *hashspace.Cluster, local *paths.Local, pieceIO piecestore.PieceIO, pieceRef int64) error {
	if hs == nil {
		return nil
	}
	if local == nil || pieceIO == nil {
		return xerrors.Errorf("open-pieces placement needs local storage and piece IO")
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
	pc, err := /* pieceCidV2 */ func(pdpPieceCID string, rawSize int64) (cid.Cid, error) {
		v1, err := cid.Parse(pdpPieceCID)
		if err != nil {
			return cid.Undef, xerrors.Errorf("parsing piece cid %s: %w", pdpPieceCID, err)
		}
		v2, err := commcid.PieceCidV2FromV1(v1, uint64(rawSize))
		if err != nil {
			return cid.Undef, xerrors.Errorf("piece cid v2 for %s: %w", pdpPieceCID, err)
		}
		return v2, nil
	}(pp.PieceCID, pp.RawSize)
	if err != nil {
		return err
	}

	placed, err := hs.HasFile(ctx, pc)
	if err != nil {
		return xerrors.Errorf("checking open piece: %w", err)
	}
	if !placed {
		digest, err := hashspace.CIDHash(pc)
		if err != nil {
			return err
		}
		target, err := hs.Target(ctx, digest)
		if err != nil {
			return err
		}
		var size int64
		var adopted string
		var existed bool
		for attempt := 0; ; attempt++ {
			busy, err := /* deleteBusy */ func(ctx context.Context, db *harmonydb.DB, pc cid.Cid) (bool, error) {
				var busy bool
				if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $1)`, pc.String()).Scan(&busy); err != nil {
					return false, xerrors.Errorf("checking open-pieces delete of %s: %w", pc, err)
				}
				return busy, nil
			}(ctx, db, pc)
			if err != nil {
				return err
			}
			if busy {
				if attempt >= placeDeleteAttempts {
					return xerrors.Errorf("open-pieces delete of %s is in progress", pc)
				}
				if err := sleepTask(ctx, nil); err != nil {
					return err
				}
				continue
			}
			size, adopted, existed, err = /* writePiece */ func(ctx context.Context, hs *hashspace.Cluster, local *paths.Local, pieceIO piecestore.PieceIO, target string, pc cid.Cid, pp parkedPiece) (int64, string, bool, error) {
				if !hs.HasLocal(target) {
					r, err := pieceIO.PieceReader(ctx, storiface.PieceNumber(pp.ID))
					if err != nil {
						return 0, "", false, xerrors.Errorf("opening piece-park copy of %s: %w", pc, err)
					}
					defer func() { _ = r.Close() }()
					size, existed, err := hs.PutRemote(ctx, target, pc, r)
					if err != nil {
						return 0, "", false, err
					}
					return size, "", existed, nil
				}

				if size, ok, err := hs.StatLocal(target, pc); err != nil {
					return 0, "", false, err
				} else if ok {
					return size, "", true, nil
				}

				if pp.RefCount == 1 {
					if src, ok := local.ExistingLocalFile(storiface.PieceNumber(pp.ID).Ref().ID, storiface.FTPiece); ok {
						size, err := hs.AdoptLocal(target, pc, src)
						switch {
						case err == nil:
							return size, src, false, nil
						case errors.Is(err, os.ErrExist):
							size, _, err := hs.StatLocal(target, pc)
							return size, "", true, err
						case !errors.Is(err, hashspace.ErrCrossDevice):
							return 0, "", false, xerrors.Errorf("renaming %s into open-pieces: %w", pc, err)
						}
					}
				}

				r, err := pieceIO.PieceReader(ctx, storiface.PieceNumber(pp.ID))
				if err != nil {
					return 0, "", false, xerrors.Errorf("opening piece-park copy of %s: %w", pc, err)
				}
				defer func() { _ = r.Close() }()
				size, err := hs.WriteLocal(target, pc, r)
				if err != nil {
					return 0, "", false, err
				}
				return size, "", false, nil
			}(ctx, hs, local, pieceIO, target, pc, pp)
			if err != nil {
				return err
			}
			if size != pp.RawSize {
				undoPlacement(ctx, hs, target, pc, existed, adopted)
				return xerrors.Errorf("open piece %s is %d bytes, expected %d", pc, size, pp.RawSize)
			}
			var refOK, dropBusy bool
			if err := db.QueryRow(ctx, `SELECT
					EXISTS (SELECT 1 FROM pdp_piecerefs WHERE piece_ref = $1),
					EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $2)`,
				pieceRef, pc.String()).Scan(&refOK, &dropBusy); err != nil {
				return xerrors.Errorf("checking placement of %s: %w", pc, err)
			}
			if !refOK {
				undoPlacement(ctx, hs, target, pc, existed, adopted)
				return nil
			}
			if dropBusy {
				undoPlacement(ctx, hs, target, pc, existed, adopted)
				if attempt >= placeDeleteAttempts-1 {
					return xerrors.Errorf("open-pieces delete of %s is in progress", pc)
				}
				if err := sleepTask(ctx, nil); err != nil {
					return err
				}
				continue
			}
			break
		}
		placed, err = hs.HasFile(ctx, pc)
		if err != nil {
			return err
		}
		if !placed {
			return xerrors.Errorf("open piece %s disappeared during placement", pc)
		}
		if err := hs.MaybeRebalance(ctx, target); err != nil {
			log.Warnw("checking open-pieces capacity", "storage", target, "error", err)
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
			if err := /* restorePark */ func(ctx context.Context, hs *hashspace.Cluster, pieceIO piecestore.PieceIO, pc cid.Cid, pp parkedPiece) error {
				locs, err := hs.Locations(ctx, pc.String())
				if err != nil {
					return err
				}
				lastErr := xerrors.Errorf("%s has no open-pieces location", pc)
				for _, l := range locs {
					r, err := hs.Open(ctx, l.StorageID, pc)
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
				return lastErr
			}(ctx, hs, pieceIO, pc, pp); err != nil {
				return xerrors.Errorf("restoring piece-park copy of %s for a new non-PDP ref: %w", pc, err)
			}
		}
	}
	return nil
}

// writePiece puts pc on target. A sole-reference piece-park file on the same
// local filesystem is renamed; otherwise the bytes are copied. A remote
// target is written by streaming a PUT to that node. The second result is
// the piece-park path when the file was renamed. The third reports that the
// open-pieces file was already there.

// undoPlacement removes a file this attempt created. An adopted file goes
// back to its piece-park path; a copy is deleted. A file that was already
// there is left alone.
func undoPlacement(ctx context.Context, hs *hashspace.Cluster, target string, pc cid.Cid, existed bool, adopted string) {
	if existed {
		return
	}
	if adopted != "" {
		if err := hs.ReturnLocal(target, pc, adopted); err != nil {
			log.Errorw("returning open piece to piece-park", "piece", pc, "path", adopted, "error", err)
		}
		return
	}
	if err := hs.Drop(ctx, target, pc); err != nil {
		log.Warnw("dropping open piece after placement rolled back", "piece", pc, "storage", target, "error", err)
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

// restorePark writes the piece-park copy back from open-pieces when a non-PDP
// ref attached while the copy was being removed.
