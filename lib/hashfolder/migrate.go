package hashfolder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/ipfs/go-cid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/storiface"
)

const (
	// MIGRATE_BATCH is how many piece-park files are renamed before their
	// sector_location rows are dropped in one statement.
	MIGRATE_BATCH = 10_000

	// MIGRATE_WORKERS_PER_DISK is how many renames and copies run at once
	// for each disk being migrated.
	MIGRATE_WORKERS_PER_DISK = 5

	// MIGRATE_PROGRESS_INTERVAL is how often migration progress is printed.
	MIGRATE_PROGRESS_INTERVAL = 5 * time.Second

	// MIGRATE_RETRY_MIN and MIGRATE_RETRY_MAX bound the wait before a failed
	// migration pass runs again. Startup waits until a pass succeeds.
	MIGRATE_RETRY_MIN = 5 * time.Second
	MIGRATE_RETRY_MAX = 2 * time.Minute
)

// fullCircle is the start and end of a range covering every hash.
var fullCircle = strings.Repeat("00", HASH_BYTES)

// migratePiecePark moves finalized PDP pieces from the piece-park folders of
// drives into open-pieces, then writes layouts for both spaces claiming
// the whole circle. A piece whose only reference is a PDP ref is renamed,
// which stays on one filesystem and is metadata only. A piece with more
// references is copied, and its piece-park file stays for those readers.
// Only drives with no open-pieces layout take part. One streamed
// query feeds MIGRATE_WORKERS_PER_DISK workers per drive, and progress is
// printed every MIGRATE_PROGRESS_INTERVAL. A failed pass is retried until one
// succeeds or ctx ends; every step is safe to repeat. It returns the storage
// ids that were given a claim. Layouts that cannot be loaded are removed
// first, so a drive that lost its layouts claims the pieces it still holds.
func migratePiecePark(ctx context.Context, db *harmonydb.DB, drives []LocalDrive) ([]string, error) {
	var nRanges int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM hash_space_range`).Scan(&nRanges); err != nil {
		return nil, xerrors.Errorf("counting hash space ranges: %w", err)
	}
	if err := dropUnusableLayouts(drives, nRanges == 0); err != nil {
		return nil, err
	}
	roots := map[string]string{}
	var ids []string
	for _, d := range drives {
		if ok, err := layoutExists(d.Root, DIR_OPEN); err != nil {
			return nil, err
		} else if ok {
			continue
		}
		if deny, err := deniesPiecePark(d.Root); err != nil {
			return nil, err
		} else if deny {
			continue
		}
		roots[d.StorageID] = d.Root
		ids = append(ids, d.StorageID)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	prog := &migrateProgress{start: time.Now()}
	prog.report("In Progress")
	stopReport := make(chan struct{})
	reportDone := make(chan struct{})
	go func() {
		defer close(reportDone)
		ticker := time.NewTicker(MIGRATE_PROGRESS_INTERVAL)
		defer ticker.Stop()
		for {
			select {
			case <-stopReport:
				return
			case <-ticker.C:
				prog.report("In Progress")
			}
		}
	}()
	defer func() {
		close(stopReport)
		<-reportDone
	}()

	wait := MIGRATE_RETRY_MIN
	for attempt := 1; ; attempt++ {
		claimed, err := migratePass(ctx, db, roots, ids, prog)
		if err == nil {
			prog.report("Complete")
			return claimed, nil
		}
		msg := fmt.Sprintf("Piece Migration Retrying in %s: attempt %d failed: %s", wait, attempt, err)
		fmt.Println(msg)
		log.Error(msg)
		select {
		case <-ctx.Done():
			return nil, xerrors.Errorf("piece migration stopped after attempt %d failed: %w", attempt, err)
		case <-time.After(wait):
		}
		wait = min(wait*2, MIGRATE_RETRY_MAX)
	}
}

// migrateProgress counts what migratePiecePark has moved, for its progress
// lines.
type migrateProgress struct {
	start                  time.Time
	bytes, renamed, copied atomic.Int64
}

func (p *migrateProgress) report(state string) {
	msg := fmt.Sprintf("Piece Migration %s: %s (%d bytes) of pieces migrated, %d renamed, %d copied, %s elapsed",
		state, humanize.IBytes(uint64(p.bytes.Load())), p.bytes.Load(), p.renamed.Load(), p.copied.Load(),
		time.Since(p.start).Truncate(time.Second))
	fmt.Println(msg)
	log.Info(msg)
}

// migratePass runs one migration over the drives in roots, keyed by the
// storage ids in ids, and writes a claim on each drive whose folders hold
// pieces. It returns the storage ids that were given a claim.
func migratePass(ctx context.Context, db *harmonydb.DB, roots map[string]string, ids []string, prog *migrateProgress) ([]string, error) {
	for _, root := range roots {
		for i := 0; i < 256; i++ {
			dir := filepath.Join(root, DIR_OPEN, fmt.Sprintf("%02x", i))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, xerrors.Errorf("creating shard %s: %w", dir, err)
			}
		}
	}

	type parked struct {
		ID        int64  `db:"id"`
		PieceCID  string `db:"piece_cid"`
		RawSize   int64  `db:"piece_raw_size"`
		RefCount  int64  `db:"ref_count"`
		StorageID string `db:"storage_id"`
	}
	// returnTo is set when this file came out of piece-park and is
	// copied back if the piece keeps a piece-park location; dropSrc is
	// a piece-park duplicate of bytes already in open-pieces.
	type renamed struct {
		id                     int64
		storageID              string
		dst, returnTo, dropSrc string
		size                   int64
	}

	g, gctx := errgroup.WithContext(ctx)
	work := make(chan parked, MIGRATE_WORKERS_PER_DISK*len(ids)*4)
	renames := make(chan renamed, MIGRATE_BATCH)

	// Stream every eligible piece on these drives to the workers.
	g.Go(func() error {
		defer close(work)
		q, err := db.Query(gctx, `SELECT pp.id, pp.piece_cid, pp.piece_raw_size, pp.ref_count, sl.storage_id
	FROM parked_pieces pp
	JOIN sector_location sl ON sl.miner_id = 0 AND sl.sector_num = pp.id
		AND sl.sector_filetype = $1 AND sl.storage_id = ANY($2)
	WHERE pp.complete AND pp.ref_count >= 1
	  AND EXISTS (SELECT 1 FROM parked_piece_refs r JOIN pdp_piecerefs pr ON pr.piece_ref = r.ref_id
		WHERE r.piece_id = pp.id)`, int(storiface.FTPiece), ids)
		if err != nil {
			return xerrors.Errorf("listing piece-park pieces: %w", err)
		}
		defer q.Close()
		for q.Next() {
			var p parked
			if err := q.StructScan(&p); err != nil {
				return xerrors.Errorf("reading piece-park piece: %w", err)
			}
			select {
			case work <- p:
			case <-gctx.Done():
				return gctx.Err()
			}
		}
		return q.Err()
	})

	// Rename sole-reference pieces and copy shared ones.
	var workers errgroup.Group
	for w := 0; w < MIGRATE_WORKERS_PER_DISK*len(ids); w++ {
		workers.Go(func() error {
			copyBuf := make([]byte, 8<<20)
			for p := range work {
				root := roots[p.StorageID]
				v1, err := cid.Parse(p.PieceCID)
				if err != nil {
					log.Warnw("skipping piece-park migration of unparsable cid", "piece", p.ID, "cid", p.PieceCID, "error", err)
					continue
				}
				v2, err := commcid.PieceCidV2FromV1(v1, uint64(p.RawSize))
				if err != nil {
					log.Warnw("skipping piece-park migration without a v2 cid", "piece", p.ID, "cid", p.PieceCID, "error", err)
					continue
				}
				hexHash, _, err := cidHashHex(v2)
				if err != nil {
					return err
				}
				dst, err := piecePath(root, DIR_OPEN, hexHash)
				if err != nil {
					return err
				}
				src := filepath.Join(root, storiface.FTPiece.String(), storiface.SectorName(storiface.PieceNumber(p.ID).Ref().ID))

				if p.RefCount > 1 {
					// Both copies must end up present: open-pieces for PDP and
					// piece-park for the other refs. An earlier pass may have
					// renamed the file before the piece gained a ref.
					_, dstErr := os.Stat(dst)
					_, srcErr := os.Stat(src)
					from, to := src, dst
					switch {
					case dstErr != nil && !os.IsNotExist(dstErr):
						return dstErr
					case srcErr != nil && !os.IsNotExist(srcErr):
						return srcErr
					case dstErr == nil && srcErr == nil:
						// copyFile keeps a full dst and rewrites a partial one.
					case dstErr == nil:
						from, to = dst, src
					case srcErr != nil:
						log.Warnw("piece-park file missing; skipping migration", "piece", p.ID, "path", src)
						continue
					}
					ok, err := copyPieceFile(from, to, p.RawSize, copyBuf)
					if err != nil {
						return xerrors.Errorf("copying %s to %s: %w", from, to, err)
					}
					if ok {
						prog.copied.Add(1)
						prog.bytes.Add(p.RawSize)
					}
					continue
				}

				r := renamed{id: p.ID, storageID: p.StorageID, dst: dst, size: p.RawSize}
				err = renameNoReplace(src, dst)
				if errors.Is(err, os.ErrExist) {
					// A shorter dst is a partial copy; the piece-park file replaces it.
					if info, serr := os.Stat(dst); serr == nil && info.Size() < p.RawSize {
						if rerr := os.Remove(dst); rerr != nil && !os.IsNotExist(rerr) {
							return xerrors.Errorf("removing partial copy %s: %w", dst, rerr)
						}
						err = renameNoReplace(src, dst)
					}
				}
				switch {
				case err == nil:
					r.returnTo = src
				case errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrExist):
					// An earlier run renamed it before stopping, or the bytes are
					// already in open-pieces. Either way the file at dst is the piece.
					if info, serr := os.Stat(dst); serr != nil || info.Size() != p.RawSize {
						continue
					}
					if errors.Is(err, os.ErrNotExist) {
						r.returnTo = src
					} else {
						r.dropSrc = src
					}
				case isCrossDevice(err):
					continue
				default:
					return xerrors.Errorf("renaming %s into open-pieces: %w", src, err)
				}
				select {
				case renames <- r:
				case <-gctx.Done():
					return gctx.Err()
				}
			}
			return nil
		})
	}
	g.Go(func() error {
		defer close(renames)
		return workers.Wait()
	})

	// Drop piece-park locations of renamed pieces in batches. A piece that
	// gained a ref after the listing keeps its piece-park location.
	g.Go(func() error {
		copyBuf := make([]byte, 8<<20)
		flush := func(batch []renamed) error {
			if len(batch) == 0 {
				return nil
			}
			byStorage := map[string][]int64{}
			for _, r := range batch {
				byStorage[r.storageID] = append(byStorage[r.storageID], r.id)
			}
			type key struct {
				storageID string
				id        int64
			}
			migrated := map[key]struct{}{}
			_, err := db.BeginTransaction(gctx, func(tx *harmonydb.Tx) (bool, error) {
				clear(migrated)
				for storageID, pieceIDs := range byStorage {
					var kept []int64
					if err := tx.Select(&kept, `DELETE FROM sector_location sl
				USING parked_pieces pp
				WHERE sl.miner_id = 0 AND sl.sector_filetype = $1 AND sl.storage_id = $2
				  AND sl.sector_num = ANY($3) AND pp.id = sl.sector_num AND pp.ref_count = 1
				  AND NOT EXISTS (SELECT 1 FROM parked_piece_refs r WHERE r.piece_id = pp.id
					AND NOT EXISTS (SELECT 1 FROM pdp_piecerefs pr WHERE pr.piece_ref = r.ref_id))
				RETURNING sl.sector_num`, int(storiface.FTPiece), storageID, pieceIDs); err != nil {
						return false, err
					}
					if len(kept) == 0 {
						continue
					}
					if _, err := tx.Exec(`DELETE FROM hash_space_place hp USING pdp_piecerefs pr, parked_piece_refs r
				WHERE hp.pdp_pieceref = pr.id AND r.ref_id = pr.piece_ref AND r.piece_id = ANY($1)`, kept); err != nil {
						return false, err
					}
					for _, id := range kept {
						migrated[key{storageID, id}] = struct{}{}
					}
				}
				return true, nil
			}, harmonydb.OptionRetry())
			if err != nil {
				return xerrors.Errorf("dropping migrated piece-park locations: %w", err)
			}
			for _, r := range batch {
				if _, ok := migrated[key{r.storageID, r.id}]; ok {
					prog.renamed.Add(1)
					prog.bytes.Add(r.size)
					if r.dropSrc != "" {
						if err := os.Remove(r.dropSrc); err != nil && !os.IsNotExist(err) {
							log.Warnw("removing piece-park duplicate of an open piece", "piece", r.id, "path", r.dropSrc, "error", err)
						}
					}
					continue
				}
				// The piece gained a ref after the listing: keep the open-pieces
				// file and put a copy back for piece-park readers.
				if r.returnTo != "" {
					if _, err := copyPieceFile(r.dst, r.returnTo, r.size, copyBuf); err != nil {
						return xerrors.Errorf("copying %s back to piece-park: %w", r.dst, err)
					}
				}
				prog.copied.Add(1)
				prog.bytes.Add(r.size)
			}
			return nil
		}
		batch := make([]renamed, 0, MIGRATE_BATCH)
		for r := range renames {
			batch = append(batch, r)
			if len(batch) == MIGRATE_BATCH {
				if err := flush(batch); err != nil {
					return err
				}
				batch = batch[:0]
			}
		}
		return flush(batch)
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Summing each folder also counts files an earlier run renamed before it
	// stopped short of writing the layouts, and files left by layouts that
	// dropUnusableLayouts removed.
	claimed := make([]bool, len(ids))
	var sums errgroup.Group
	for i, storageID := range ids {
		root := roots[storageID]
		sums.Go(func() error {
			var used [2]int64
			for s, kind := range spaceKinds {
				dir := filepath.Join(root, kind)
				if ok, err := fileExists(dir); err != nil {
					return err
				} else if !ok {
					continue
				}
				n, err := sumInterval(dir, "", "")
				if err != nil {
					return err
				}
				used[s] = n
			}
			if used[0]+used[1] == 0 {
				return nil
			}
			now := time.Now().UTC().Truncate(time.Second)
			for s, kind := range spaceKinds {
				if err := writeLayout(root, kind, Layout{
					Used:        used[s],
					CommittedAt: now,
					Ranges:      []HashRange{{Start: fullCircle, End: fullCircle}},
					Claim:       true,
				}); err != nil {
					return err
				}
			}
			claimed[i] = true
			log.Infow("hash space claim written after piece-park migration", "storage", storageID, "open_bytes", used[0], "acl_bytes", used[1])
			return nil
		})
	}
	if err := sums.Wait(); err != nil {
		return nil, err
	}
	var out []string
	for i, ok := range claimed {
		if ok {
			out = append(out, ids[i])
		}
	}
	return out, nil
}

// copyPieceFile copies src onto dst and syncs it. A dst at least size bytes
// long is kept; a shorter one is a partial copy and is rewritten. It
// reports false without error when src is missing, dst is kept, or src is
// not size bytes; a dst it wrote is removed on failure.
func copyPieceFile(src, dst string, size int64, buf []byte) (bool, error) {
	in, err := os.Open(src)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = in.Close() }()
	if info, err := os.Stat(dst); err == nil && info.Size() >= size {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	n, err := io.CopyBuffer(out, in, buf)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != size {
		log.Warnw("skipping piece copy of the wrong size", "path", src, "size", n, "expected", size)
		_ = os.Remove(dst)
		return false, nil
	}
	if err != nil {
		_ = os.Remove(dst)
		return false, err
	}
	return true, nil
}

// dropUnusableLayouts removes both layouts of each drive whose layout set
// cannot be loaded: one space has no layout, a layout does not decode, or
// only one of the two is a claim. A crash while layouts are written leaves
// such a set. With mapEmpty, the cluster map has no ranges yet, so the
// layouts can only come from FirstSetup; when the drives' non-claim layouts
// do not tile the circle in each space, that setup stopped partway and all
// of them are removed. migratePiecePark then claims the pieces each drive
// still holds.
func dropUnusableLayouts(drives []LocalDrive, mapEmpty bool) error {
	var kept []LocalDrive
	for _, d := range drives {
		reason, err := layoutProblem(d.Root)
		if err != nil {
			return err
		}
		if reason == "" {
			kept = append(kept, d)
			continue
		}
		log.Warnw("removing unusable hash space layouts; the drive's pieces will be claimed", "root", d.Root, "reason", reason)
		if err := removeLayouts(d.Root); err != nil {
			return err
		}
	}
	if !mapEmpty {
		return nil
	}
	var loaded []LocalDrive
	var perSpace [2][]hashInterval
	for _, d := range kept {
		if ok, err := layoutExists(d.Root, DIR_OPEN); err != nil {
			return err
		} else if !ok {
			continue
		}
		if claim, err := isClaim(d.Root); err != nil {
			return err
		} else if claim {
			continue
		}
		loaded = append(loaded, d)
		for s, kind := range spaceKinds {
			layout, _, err := openLayout(d.Root, kind)
			if err != nil {
				return err
			}
			ivs, err := decodeIntervals(layout.Ranges)
			if err != nil {
				return err
			}
			perSpace[s] = append(perSpace[s], ivs...)
		}
	}
	if len(loaded) == 0 || (tilesCircle(perSpace[0]) && tilesCircle(perSpace[1])) {
		return nil
	}
	for _, d := range loaded {
		log.Warnw("removing hash space layouts left by an interrupted first setup; the drive's pieces will be claimed", "root", d.Root)
		if err := removeLayouts(d.Root); err != nil {
			return err
		}
	}
	return nil
}

// layoutProblem returns why root's layouts cannot be loaded, or "" when they
// can or root has none.
func layoutProblem(root string) (string, error) {
	var have, claim [2]bool
	for s, kind := range spaceKinds {
		ok, err := layoutExists(root, kind)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		have[s] = true
		layout, _, err := openLayout(root, kind)
		if errors.Is(err, errBadLayout) {
			return err.Error(), nil
		}
		if err != nil {
			return "", err
		}
		if _, err := decodeIntervals(layout.Ranges); err != nil {
			return xerrors.Errorf("%s ranges: %w", kind, err).Error(), nil
		}
		claim[s] = layout.Claim
	}
	switch {
	case have[0] != have[1]:
		return "only one hash space has a layout", nil
	case claim[0] != claim[1]:
		return "only one hash space layout is a claim", nil
	}
	return "", nil
}

// tilesCircle reports whether ivs cover the hash circle exactly once: in end
// order, each interval starts where the one before it ends.
func tilesCircle(ivs []hashInterval) bool {
	if len(ivs) == 0 {
		return false
	}
	sorted := append([]hashInterval(nil), ivs...)
	sort.Slice(sorted, func(a, b int) bool { return bytes.Compare(sorted[a].end, sorted[b].end) < 0 })
	for i, iv := range sorted {
		prev := sorted[(i-1+len(sorted))%len(sorted)]
		if !bytes.Equal(iv.start, prev.end) {
			return false
		}
	}
	return true
}

// isClaim reports whether root's open-pieces layout is a claim written by
// migratePiecePark.
func isClaim(root string) (bool, error) {
	if ok, err := layoutExists(root, DIR_OPEN); err != nil || !ok {
		return false, err
	}
	layout, _, err := openLayout(root, DIR_OPEN)
	if err != nil {
		return false, err
	}
	return layout.Claim, nil
}
