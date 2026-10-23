package hashfolder

import (
	"context"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

// SNAPSHOT_RETRIES bounds how often a snapshot is reloaded because the map
// version changed while it was being read.
const SNAPSHOT_RETRIES = 3

// mapSnapshot is the hash-space map as of one refresh. ranges and moves are
// open-pieces; aclRanges and aclMoves are acl-pieces. Reads use it so that
// finding the disks that may hold a piece needs no database round-trip.
type mapSnapshot struct {
	ranges    []rangeRow
	moves     []moveSourceRow
	aclRanges []rangeRow
	aclMoves  []moveSourceRow
	misplaced []string
	urls      map[string]string
}

type diskRow struct {
	StorageID    string `db:"storage_id"`
	HasMisplaced bool   `db:"has_misplaced"`
	URLs         string `db:"urls"`
}

// loadSnapshot reads the open-pieces and acl-pieces maps. Moves are read
// before ranges: a move that completes in between is then listed together
// with its new owner, which only adds a place, instead of hiding one.
func (c *Cluster) loadSnapshot(ctx context.Context) (*mapSnapshot, error) {
	snap := &mapSnapshot{urls: map[string]string{}}
	err := c.db.Select(ctx, &snap.moves, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE space = $1`, DIR_OPEN)
	if err != nil {
		return nil, xerrors.Errorf("reading hash space moveSources: %w", err)
	}
	err = c.db.Select(ctx, &snap.ranges, `SELECT end_hash, storage_id, size FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, DIR_OPEN)
	if err != nil {
		return nil, xerrors.Errorf("reading hash space ranges: %w", err)
	}
	err = c.db.Select(ctx, &snap.aclMoves, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE space = $1`, DIR_ACL)
	if err != nil {
		return nil, xerrors.Errorf("reading acl-pieces moveSources: %w", err)
	}
	err = c.db.Select(ctx, &snap.aclRanges, `SELECT end_hash, storage_id, size FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, DIR_ACL)
	if err != nil {
		return nil, xerrors.Errorf("reading acl-pieces ranges: %w", err)
	}
	var disks []diskRow
	err = c.db.Select(ctx, &disks, `SELECT d.storage_id, d.has_misplaced, COALESCE(sp.urls, '') AS urls
		FROM hash_space_disk d LEFT JOIN storage_path sp ON sp.storage_id = d.storage_id
		ORDER BY d.storage_id`)
	if err != nil {
		return nil, xerrors.Errorf("reading hash space disks: %w", err)
	}
	for _, d := range disks {
		if d.HasMisplaced {
			snap.misplaced = append(snap.misplaced, d.StorageID)
		}
		snap.urls[d.StorageID] = d.URLs
	}
	return snap, nil
}

// refreshSnapshot replaces the read snapshot. It runs on every node, with or
// without local disks, so any node can find pieces held elsewhere.
func (c *Cluster) refreshSnapshot(ctx context.Context) error {
	for attempt := 0; ; attempt++ {
		before, err := c.mapVersion(ctx)
		if err != nil {
			return err
		}
		snap, err := c.loadSnapshot(ctx)
		if err != nil {
			return err
		}
		after, err := c.mapVersion(ctx)
		if err != nil {
			return err
		}
		if before == after || attempt >= SNAPSHOT_RETRIES {
			c.snap.Store(snap)
			return nil
		}
	}
}

// syncMap reloads the cluster map: the read snapshot on every node, and the
// local space intervals on nodes with disks.
func (c *Cluster) syncMap(ctx context.Context, force bool) error {
	if len(c.roots) > 0 {
		return c.refresh(ctx, force)
	}
	return c.refreshSnapshot(ctx)
}

// Candidates lists the disks that may hold pc, local ones first, from the
// in-memory map: the range owner, both ends of a move covering its hash, and
// disks with misplaced pieces. It runs no SQL. It returns nothing when this
// space has no ranges.
func (s *ClusterSpace) Candidates(pc cid.Cid) ([]Location, error) {
	digest, err := CIDHash(pc)
	if err != nil {
		return nil, err
	}
	snap := s.c.snap.Load()
	if snap == nil {
		return nil, nil
	}
	ranges, moves := snap.ranges, snap.moves
	if s.kind == DIR_ACL {
		ranges, moves = snap.aclRanges, snap.aclMoves
	}
	if len(ranges) == 0 {
		return nil, nil
	}
	return s.c.placesIn(ranges, moves, snap.misplaced, digest)
}

// StatAt reports whether this space's file for pc exists on storageID: a stat
// on a local disk, otherwise a HEAD to the node that holds it. A file's size
// is not compared with the CID.
func (s *ClusterSpace) StatAt(ctx context.Context, storageID string, pc cid.Cid) (bool, error) {
	if s.c.HasLocal(storageID) {
		_, ok, err := s.StatLocal(storageID, pc)
		return ok, err
	}
	hexHash, _, err := cidHashHex(pc)
	if err != nil {
		return false, err
	}
	return s.c.hasHash(ctx, storageID, s.kind, hexHash)
}

// storageURLs returns a storage path's urls, from the snapshot when it has them.
func (c *Cluster) storageURLs(ctx context.Context, storageID string) (string, error) {
	if snap := c.snap.Load(); snap != nil {
		if u := snap.urls[storageID]; u != "" {
			return u, nil
		}
	}
	var urls string
	if err := c.db.QueryRow(ctx, `SELECT COALESCE(urls, '') FROM storage_path WHERE storage_id = $1`, storageID).Scan(&urls); err != nil {
		return "", xerrors.Errorf("looking up storage %s urls: %w", storageID, err)
	}
	return urls, nil
}
