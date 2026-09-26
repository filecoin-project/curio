package hashspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/hashspacesolver"
)

var log = logging.Logger("hashspace")

const (
	// EVENT_FULL and EVENT_ARRIVE are hash_space_pending_event.event_kind values.
	EVENT_FULL   = "full"
	EVENT_ARRIVE = "arrive"

	// REFRESH_INTERVAL is how often a node reloads the cluster map for its
	// local roots, republishes used counters, and runs a queued event.
	REFRESH_INTERVAL = FLUSH_INTERVAL

	CAS_RETRIES = 20

	storageURLSeparator = ","
	remoteSuffix        = "/remote"
	httpPrefix          = "/hashspace/"
)

var spaceKinds = []string{DIR_OPEN, DIR_ACL}

// LocalDrive is one storage path on this node that holds hash-space folders.
type LocalDrive struct {
	StorageID string
	Root      string
}

// Location is one disk holding a piece file.
type Location struct {
	StorageID string
	Local     bool
}

// Cluster shares the hash-space map of every node through the database.
// hash_space_range is the single-owner tiling the solver plans on;
// hash_space_move_source adds a second place for intervals being moved.
type Cluster struct {
	db     *harmonydb.DB
	auth   http.Header
	client *http.Client

	open  *Space
	roots map[string]string

	lastVersion atomic.Int64
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

type rangeRow struct {
	EndHash   []byte `db:"end_hash"`
	StorageID string `db:"storage_id"`
}

type moveSourceRow struct {
	ID          int64  `db:"id"`
	Space       string `db:"space"`
	StartHash   []byte `db:"start_hash"`
	EndHash     []byte `db:"end_hash"`
	FromStorage string `db:"from_storage"`
	ToStorage   string `db:"to_storage"`
	Size        int64  `db:"size"`
}

// NewCluster joins drives into the cluster map, writes their layout.json
// from it, and loads the local open-pieces space. auth is sent on requests
// to other nodes' /hashspace routes.
func NewCluster(ctx context.Context, db *harmonydb.DB, drives []LocalDrive, auth http.Header) (*Cluster, error) {
	c := &Cluster{
		db:     db,
		auth:   auth,
		client: &http.Client{},
		roots:  make(map[string]string, len(drives)),
	}
	for _, d := range drives {
		if d.StorageID == "" || d.Root == "" {
			return nil, xerrors.Errorf("hash space drive needs a storage id and root")
		}
		c.roots[d.StorageID] = d.Root
	}

	var arrived []string
	if len(drives) > 0 {
		var err error
		arrived, err = c.join(ctx, drives)
		if err != nil {
			return nil, xerrors.Errorf("joining hash space: %w", err)
		}
		if err := c.writeLocalLayouts(ctx); err != nil {
			return nil, err
		}
		roots := make([]string, 0, len(drives))
		for _, d := range drives {
			roots = append(roots, d.Root)
		}
		c.open, err = Load(DIR_OPEN, roots)
		if err != nil {
			return nil, err
		}
		if err := c.refresh(ctx, true); err != nil {
			return nil, err
		}
		c.publishUsed(ctx)
	}

	for _, id := range arrived {
		if err := c.raise(ctx, id, EVENT_ARRIVE); err != nil {
			log.Errorw("hash space arrive", "storage", id, "error", err)
		}
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.wg.Add(1)
	go c.loop(loopCtx)
	return c, nil
}

// Close stops the refresh loop and flushes the local space.
func (c *Cluster) Close() error {
	if c.cancel != nil {
		c.cancel()
		c.wg.Wait()
	}
	if c.open != nil {
		return c.open.Close()
	}
	return nil
}

func (c *Cluster) loop(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(REFRESH_INTERVAL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if c.open != nil {
			if err := c.refresh(ctx, false); err != nil {
				log.Warnw("refreshing hash space map", "error", err)
			}
			for id := range c.roots {
				if err := c.CheckCapacity(ctx, id); err != nil {
					log.Warnw("hash space capacity check", "storage", id, "error", err)
				}
			}
		}
		if err := c.processPending(ctx); err != nil {
			log.Warnw("processing pending hash space event", "error", err)
		}
	}
}

// HasLocal reports whether storageID is a hash-space root on this node.
func (c *Cluster) HasLocal(storageID string) bool {
	_, ok := c.roots[storageID]
	return ok
}

func (c *Cluster) rootOf(storageID string) (string, error) {
	root, ok := c.roots[storageID]
	if !ok || c.open == nil {
		return "", xerrors.Errorf("storage %s is not a local hash space root", storageID)
	}
	return root, nil
}

// Target returns the disk a new open-pieces file for digest belongs on: the
// destination of an in-flight move covering it, otherwise the range owner.
func (c *Cluster) Target(ctx context.Context, digest []byte) (string, error) {
	resolve, err := c.Targets(ctx)
	if err != nil {
		return "", err
	}
	return resolve(digest)
}

// Targets snapshots the open-pieces map and returns a resolver with the
// same rules as Target.
func (c *Cluster) Targets(ctx context.Context) (func(digest []byte) (string, error), error) {
	var moveSources []moveSourceRow
	err := c.db.Select(ctx, &moveSources, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE space = $1`, DIR_OPEN)
	if err != nil {
		return nil, xerrors.Errorf("reading hash space moveSources: %w", err)
	}
	var rs []rangeRow
	err = c.db.Select(ctx, &rs, `SELECT end_hash, storage_id FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, DIR_OPEN)
	if err != nil {
		return nil, xerrors.Errorf("reading hash space ranges: %w", err)
	}
	return func(digest []byte) (string, error) {
		for _, m := range moveSources {
			if hashspacesolver.Contains(m.StartHash, m.EndHash, digest) {
				return m.ToStorage, nil
			}
		}
		for i, r := range rs {
			if hashspacesolver.Contains(rs[(i-1+len(rs))%len(rs)].EndHash, r.EndHash, digest) {
				return r.StorageID, nil
			}
		}
		return "", xerrors.Errorf("no hash space range owns %x", digest)
	}, nil
}

// StatLocal returns the size of the open-pieces file on a local disk.
func (c *Cluster) StatLocal(storageID string, pc cid.Cid) (int64, bool, error) {
	root, err := c.rootOf(storageID)
	if err != nil {
		return 0, false, err
	}
	return c.open.StatCIDOn(root, pc)
}

// OpenLocal opens the open-pieces file on a local disk.
func (c *Cluster) OpenLocal(storageID string, pc cid.Cid) (io.ReadCloser, error) {
	root, err := c.rootOf(storageID)
	if err != nil {
		return nil, err
	}
	return c.open.OpenCIDOn(root, pc)
}

// OpenLocalAt opens bytes [offset, offset+size) of the open-pieces file
// on a local disk.
func (c *Cluster) OpenLocalAt(storageID string, pc cid.Cid, offset, size int64) (io.ReadCloser, error) {
	root, err := c.rootOf(storageID)
	if err != nil {
		return nil, err
	}
	return c.open.OpenCIDAt(root, pc, offset, size)
}

// AdoptLocal renames src into open-pieces on a local disk. See AdoptFileOn.
func (c *Cluster) AdoptLocal(storageID string, pc cid.Cid, src string) (int64, error) {
	root, err := c.rootOf(storageID)
	if err != nil {
		return 0, err
	}
	return c.open.AdoptFileOn(root, pc, src)
}

// WriteLocal copies r into open-pieces on a local disk. An existing file is
// kept and its size returned.
func (c *Cluster) WriteLocal(storageID string, pc cid.Cid, r io.Reader) (int64, error) {
	root, err := c.rootOf(storageID)
	if err != nil {
		return 0, err
	}
	w, err := c.open.WriteCIDOn(root, pc)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			size, _, err := c.StatLocal(storageID, pc)
			return size, err
		}
		return 0, err
	}
	n, err := io.CopyBuffer(w, r, make([]byte, 8<<20))
	if err != nil {
		if a, ok := w.(interface{ Abort() error }); ok {
			_ = a.Abort()
		}
		return 0, xerrors.Errorf("copying %s into open-pieces: %w", pc, err)
	}
	if err := w.Close(); err != nil {
		if errors.Is(err, os.ErrExist) {
			size, _, err := c.StatLocal(storageID, pc)
			return size, err
		}
		return 0, err
	}
	return n, nil
}

// DropLocal removes the open-pieces file from a local disk. Missing is fine.
func (c *Cluster) DropLocal(storageID string, pc cid.Cid) error {
	root, err := c.rootOf(storageID)
	if err != nil {
		return err
	}
	if err := c.open.DeleteCIDOn(root, pc); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Locations lists every disk recorded as holding pieceCID, local disks first.
func (c *Cluster) Locations(ctx context.Context, pieceCID string) ([]Location, error) {
	var ids []string
	if err := c.db.Select(ctx, &ids, `SELECT storage_id FROM open_piece WHERE piece_cid = $1`, pieceCID); err != nil {
		return nil, xerrors.Errorf("reading open_piece: %w", err)
	}
	out := make([]Location, 0, len(ids))
	for _, id := range ids {
		if c.HasLocal(id) {
			out = append(out, Location{StorageID: id, Local: true})
		}
	}
	for _, id := range ids {
		if !c.HasLocal(id) {
			out = append(out, Location{StorageID: id})
		}
	}
	return out, nil
}

// Open reads the whole open-pieces file from storageID, locally or through
// that node's /hashspace route.
func (c *Cluster) Open(ctx context.Context, storageID string, pc cid.Cid) (io.ReadCloser, error) {
	if c.HasLocal(storageID) {
		return c.OpenLocal(storageID, pc)
	}
	return c.remoteGet(ctx, storageID, pc, "")
}

// RemoteAt reads bytes [start, end) of the file on another node.
func (c *Cluster) RemoteAt(ctx context.Context, storageID string, pc cid.Cid, start, end int64) (io.ReadCloser, error) {
	if end <= start {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	return c.remoteGet(ctx, storageID, pc, "bytes="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end-1, 10))
}

func (c *Cluster) remoteGet(ctx context.Context, storageID string, pc cid.Cid, byteRange string) (io.ReadCloser, error) {
	var resp *http.Response
	err := c.remote(ctx, http.MethodGet, storageID, pc, func(req *http.Request) {
		if byteRange != "" {
			req.Header.Set("Range", byteRange)
		}
	}, func(r *http.Response) (bool, error) {
		switch {
		case byteRange == "" && r.StatusCode == http.StatusOK,
			byteRange != "" && r.StatusCode == http.StatusPartialContent:
			resp = r
			return true, nil
		case r.StatusCode == http.StatusNotFound:
			return false, os.ErrNotExist
		default:
			return false, xerrors.Errorf("GET %s: %s", r.Request.URL, r.Status)
		}
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Cluster) deleteOn(ctx context.Context, storageID string, pc cid.Cid) error {
	if c.HasLocal(storageID) {
		return c.DropLocal(storageID, pc)
	}
	return c.remote(ctx, http.MethodDelete, storageID, pc, nil, func(r *http.Response) (bool, error) {
		_ = r.Body.Close()
		switch r.StatusCode {
		case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
			return false, nil
		default:
			return false, xerrors.Errorf("DELETE %s: %s", r.Request.URL, r.Status)
		}
	})
}

// remote sends one request to each URL of storageID until one answers.
// handle reports whether it kept the response body open.
func (c *Cluster) remote(ctx context.Context, method, storageID string, pc cid.Cid, prep func(*http.Request), handle func(*http.Response) (bool, error)) error {
	hexHash, _, err := cidHashHex(pc)
	if err != nil {
		return err
	}
	var urls string
	if err := c.db.QueryRow(ctx, `SELECT COALESCE(urls, '') FROM storage_path WHERE storage_id = $1`, storageID).Scan(&urls); err != nil {
		return xerrors.Errorf("looking up storage %s urls: %w", storageID, err)
	}
	lastErr := xerrors.Errorf("storage %s has no urls", storageID)
	for _, u := range strings.Split(urls, storageURLSeparator) {
		if u == "" {
			continue
		}
		target := strings.TrimSuffix(strings.TrimSuffix(u, "/"), remoteSuffix) + httpPrefix + storageID + "/" + hexHash
		req, err := http.NewRequestWithContext(ctx, method, target, nil)
		if err != nil {
			lastErr = err
			continue
		}
		for k, v := range c.auth {
			req.Header[k] = v
		}
		if prep != nil {
			prep(req)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		kept, err := handle(resp)
		if !kept {
			_ = resp.Body.Close()
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			lastErr = err
			continue
		}
		return err
	}
	return lastErr
}

type PlaceResult int

const (
	Placed PlaceResult = iota
	PlaceRefGone
	PlaceDeletePending
)

// RecordPlaced records the open-pieces file for pc on storageID, provided
// the PDP reference pdpRef still exists and no delete of pc is queued.
// A row on a disk whose range moved meanwhile is kept: reads and deletes go
// by open_piece rows, not by range ownership.
func (c *Cluster) RecordPlaced(ctx context.Context, pc cid.Cid, storageID string, size, pdpRef int64) (PlaceResult, error) {
	digest, err := CIDHash(pc)
	if err != nil {
		return 0, err
	}
	var res PlaceResult
	_, err = c.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var refOK, deletePending bool
		if err := tx.QueryRow(`SELECT
				EXISTS (SELECT 1 FROM pdp_piecerefs WHERE id = $1),
				EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $2)`,
			pdpRef, pc.String()).Scan(&refOK, &deletePending); err != nil {
			return false, err
		}
		switch {
		case !refOK:
			res = PlaceRefGone
			return false, nil
		case deletePending:
			res = PlaceDeletePending
			return false, nil
		}
		if _, err := tx.Exec(`INSERT INTO open_piece (piece_cid, storage_id, space, piece_hash, size)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT (piece_cid, storage_id) DO NOTHING`,
			pc.String(), storageID, DIR_OPEN, digest, size); err != nil {
			return false, err
		}
		res = Placed
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return 0, xerrors.Errorf("recording open piece %s: %w", pc, err)
	}
	return res, nil
}

// HasRow reports whether open_piece records pc on storageID.
func (c *Cluster) HasRow(ctx context.Context, pc cid.Cid, storageID string) (bool, error) {
	var ok bool
	err := c.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM open_piece WHERE piece_cid = $1 AND storage_id = $2)`,
		pc.String(), storageID).Scan(&ok)
	return ok, err
}

// DeleteCID removes pieceCID from every disk recorded in open_piece,
// including an in-flight move destination, then drops those rows. Rows added by a
// concurrent move are picked up by the next pass.
func (c *Cluster) DeleteCID(ctx context.Context, pieceCID string) error {
	pc, err := cid.Parse(pieceCID)
	if err != nil {
		return xerrors.Errorf("parsing piece cid %s: %w", pieceCID, err)
	}
	touched := map[string]struct{}{}
	for pass := 0; pass < 8; pass++ {
		var ids []string
		if err := c.db.Select(ctx, &ids, `SELECT storage_id FROM open_piece WHERE piece_cid = $1`, pieceCID); err != nil {
			return xerrors.Errorf("reading open_piece: %w", err)
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if err := c.deleteOn(ctx, id, pc); err != nil {
				return xerrors.Errorf("deleting %s from %s: %w", pieceCID, id, err)
			}
			touched[id] = struct{}{}
		}
		if _, err := c.db.Exec(ctx, `DELETE FROM open_piece WHERE piece_cid = $1 AND storage_id = ANY($2)`, pieceCID, ids); err != nil {
			return xerrors.Errorf("deleting open_piece rows: %w", err)
		}
	}
	for id := range touched {
		if err := c.CheckCapacity(ctx, id); err != nil {
			log.Warnw("hash space capacity check", "storage", id, "error", err)
		}
	}
	return nil
}

// CheckCapacity starts an EventFull rebalance for storageID when it is above
// FILL_LIMIT_PERCENT of its capacity.
func (c *Cluster) CheckCapacity(ctx context.Context, storageID string) error {
	if root, ok := c.roots[storageID]; ok && c.open != nil {
		c.publishOne(ctx, storageID, root)
	}
	var used, capacity int64
	err := c.db.QueryRow(ctx, `SELECT used_open + used_acl, capacity FROM hash_space_disk WHERE storage_id = $1`, storageID).Scan(&used, &capacity)
	if err != nil {
		return xerrors.Errorf("reading hash space disk %s: %w", storageID, err)
	}
	if !overFill(used, capacity) {
		return nil
	}
	return c.raise(ctx, storageID, EVENT_FULL)
}

func overFill(used, capacity int64) bool {
	if capacity <= 0 {
		return used > 0
	}
	limit := capacity/100*hashspacesolver.FILL_LIMIT_PERCENT + capacity%100*hashspacesolver.FILL_LIMIT_PERCENT/100
	return used > limit
}

// raise runs one disk event. While any move source is unfinished it only records
// the event, at most once per disk and kind.
func (c *Cluster) raise(ctx context.Context, storageID, kind string) error {
	return c.casTx(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var busy bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM hash_space_move_source)`).Scan(&busy); err != nil {
			return false, err
		}
		if busy {
			n, err := tx.Exec(`INSERT INTO hash_space_pending_event (storage_id, event_kind) VALUES ($1, $2)
				ON CONFLICT (storage_id, event_kind) DO NOTHING`, storageID, kind)
			return n > 0, err
		}

		st, ids, err := loadClusterState(tx)
		if err != nil {
			return false, err
		}
		disk := indexOf(ids, storageID)
		if disk < 0 {
			return false, nil
		}
		ev := hashspacesolver.Event{Disk: disk}
		switch kind {
		case EVENT_FULL:
			if !overFill(ownedBytes(st, disk), st.Disks[disk]) {
				return false, nil
			}
			ev.Kind = hashspacesolver.EventFull
		case EVENT_ARRIVE:
			ev.Kind = hashspacesolver.EventArrive
		default:
			return false, xerrors.Errorf("unknown hash space event %q", kind)
		}

		res, err := hashspacesolver.Solve(st, ev)
		if err != nil {
			log.Warnw("hash space solver", "storage", storageID, "event", kind, "error", err)
			return false, nil
		}
		if res.BytesMoved == 0 {
			if ev.Kind == hashspacesolver.EventFull {
				return false, nil
			}
			return true, storeState(tx, res.State, ids)
		}

		var planned int
		for _, t := range res.Diff {
			if t.Size <= 0 || t.From < 0 || t.From == t.To {
				continue
			}
			if t.Space != 0 {
				log.Warnw("skipping acl-pieces transfer", "from", ids[t.From], "to", ids[t.To], "size", t.Size)
				continue
			}
			n, err := tx.Exec(`INSERT INTO hash_space_move_source (space, start_hash, end_hash, from_storage, to_storage, size)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (space, start_hash, end_hash) DO NOTHING`,
				DIR_OPEN, t.StartHash, t.EndHash, ids[t.From], ids[t.To], t.Size)
			if err != nil {
				return false, xerrors.Errorf("inserting hash space move source: %w", err)
			}
			planned += n
		}
		log.Infow("planned hash space rebalance", "storage", storageID, "event", kind, "moves", planned, "bytes", res.BytesMoved)
		return planned > 0, nil
	})
}

// processPending runs the oldest queued event once no move source is unfinished.
func (c *Cluster) processPending(ctx context.Context) error {
	var busy bool
	if err := c.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM hash_space_move_source)`).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return nil
	}
	var evs []struct {
		StorageID string `db:"storage_id"`
		Kind      string `db:"event_kind"`
	}
	if err := c.db.Select(ctx, &evs, `SELECT storage_id, event_kind FROM hash_space_pending_event ORDER BY created_at LIMIT 1`); err != nil {
		return err
	}
	if len(evs) == 0 {
		return nil
	}
	n, err := c.db.Exec(ctx, `DELETE FROM hash_space_pending_event WHERE storage_id = $1 AND event_kind = $2`, evs[0].StorageID, evs[0].Kind)
	if err != nil || n == 0 {
		return err
	}
	return c.raise(ctx, evs[0].StorageID, evs[0].Kind)
}

// MoveSource is an in-flight interval copy.
type MoveSource struct {
	ID          int64
	StartHash   []byte
	EndHash     []byte
	FromStorage string
	ToStorage   string
}

// MoveSourceByTask returns the move source owned by a HashSpaceMove task.
func (c *Cluster) MoveSourceByTask(ctx context.Context, taskID int64) (*MoveSource, error) {
	var ms []moveSourceRow
	err := c.db.Select(ctx, &ms, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE task_id = $1`, taskID)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, nil
	}
	m := ms[0]
	return &MoveSource{ID: m.ID, StartHash: m.StartHash, EndHash: m.EndHash, FromStorage: m.FromStorage, ToStorage: m.ToStorage}, nil
}

// PendingCopy lists source pieces in the move source not yet on the destination.
func (c *Cluster) PendingCopy(ctx context.Context, m *MoveSource, limit int) ([]string, error) {
	var cids []string
	err := c.db.Select(ctx, &cids, `SELECT s.piece_cid FROM open_piece s
		WHERE s.storage_id = $1 AND s.space = $4
		  AND (($2::bytea = $3::bytea)
		    OR ($2::bytea < $3::bytea AND s.piece_hash > $2::bytea AND s.piece_hash <= $3::bytea)
		    OR ($2::bytea > $3::bytea AND (s.piece_hash > $2::bytea OR s.piece_hash <= $3::bytea)))
		  AND NOT EXISTS (SELECT 1 FROM open_piece d WHERE d.piece_cid = s.piece_cid AND d.storage_id = $5)
		ORDER BY s.piece_hash LIMIT $6`, m.FromStorage, m.StartHash, m.EndHash, DIR_OPEN, m.ToStorage, limit)
	return cids, err
}

// CopyOne copies one piece of the move source onto its local destination. The
// destination row is recorded only while the source row still exists, so a
// piece deleted during the copy does not come back.
func (c *Cluster) CopyOne(ctx context.Context, m *MoveSource, pieceCID string) error {
	pc, err := cid.Parse(pieceCID)
	if err != nil {
		return err
	}
	digest, err := CIDHash(pc)
	if err != nil {
		return err
	}
	src, err := c.Open(ctx, m.FromStorage, pc)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if has, herr := c.HasRow(ctx, pc, m.FromStorage); herr == nil && !has {
				return nil
			}
		}
		return xerrors.Errorf("opening %s on %s: %w", pieceCID, m.FromStorage, err)
	}
	size, err := c.WriteLocal(m.ToStorage, pc, src)
	_ = src.Close()
	if err != nil {
		return err
	}
	n, err := c.db.Exec(ctx, `INSERT INTO open_piece (piece_cid, storage_id, space, piece_hash, size)
		SELECT $1, $2, $3, $4, $5
		WHERE EXISTS (SELECT 1 FROM open_piece WHERE piece_cid = $1 AND storage_id = $6)
		ON CONFLICT (piece_cid, storage_id) DO NOTHING`, pieceCID, m.ToStorage, DIR_OPEN, digest, size, m.FromStorage)
	if err != nil {
		return xerrors.Errorf("recording %s on %s: %w", pieceCID, m.ToStorage, err)
	}
	if n == 0 {
		var have bool
		if err := c.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM open_piece WHERE piece_cid = $1 AND storage_id = $2)`, pieceCID, m.ToStorage).Scan(&have); err != nil {
			return err
		}
		if !have {
			return c.DropLocal(m.ToStorage, pc)
		}
	}
	return nil
}

// CompleteMoveSource hands the move source interval to its destination, so a piece
// there has one place again, then removes the source copies.
func (c *Cluster) CompleteMoveSource(ctx context.Context, m *MoveSource) error {
	var sourceCIDs []string
	err := c.casTx(ctx, func(tx *harmonydb.Tx) (bool, error) {
		sourceCIDs = nil
		var missing int
		err := tx.QueryRow(`SELECT COUNT(*) FROM open_piece s
			WHERE s.storage_id = $1 AND s.space = $4
			  AND (($2::bytea = $3::bytea)
			    OR ($2::bytea < $3::bytea AND s.piece_hash > $2::bytea AND s.piece_hash <= $3::bytea)
			    OR ($2::bytea > $3::bytea AND (s.piece_hash > $2::bytea OR s.piece_hash <= $3::bytea)))
			  AND NOT EXISTS (SELECT 1 FROM open_piece d WHERE d.piece_cid = s.piece_cid AND d.storage_id = $5)`,
			m.FromStorage, m.StartHash, m.EndHash, DIR_OPEN, m.ToStorage).Scan(&missing)
		if err != nil {
			return false, err
		}
		if missing > 0 {
			return false, xerrors.Errorf("%d pieces in move source %d are not on %s yet", missing, m.ID, m.ToStorage)
		}

		var rs []rangeRow
		if err := tx.Select(&rs, `SELECT end_hash, storage_id FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, DIR_OPEN); err != nil {
			return false, err
		}
		next, err := transferRanges(rs, m.StartHash, m.EndHash, m.FromStorage, m.ToStorage)
		if err != nil {
			return false, xerrors.Errorf("move source %d: %w", m.ID, err)
		}
		if err := writeSpaceRanges(tx, DIR_OPEN, next); err != nil {
			return false, err
		}

		if err := tx.Select(&sourceCIDs, `SELECT piece_cid FROM open_piece
			WHERE storage_id = $1 AND space = $4
			  AND (($2::bytea = $3::bytea)
			    OR ($2::bytea < $3::bytea AND piece_hash > $2::bytea AND piece_hash <= $3::bytea)
			    OR ($2::bytea > $3::bytea AND (piece_hash > $2::bytea OR piece_hash <= $3::bytea)))`,
			m.FromStorage, m.StartHash, m.EndHash, DIR_OPEN); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`DELETE FROM open_piece
			WHERE storage_id = $1 AND space = $4
			  AND (($2::bytea = $3::bytea)
			    OR ($2::bytea < $3::bytea AND piece_hash > $2::bytea AND piece_hash <= $3::bytea)
			    OR ($2::bytea > $3::bytea AND (piece_hash > $2::bytea OR piece_hash <= $3::bytea)))`,
			m.FromStorage, m.StartHash, m.EndHash, DIR_OPEN); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`DELETE FROM hash_space_move_source WHERE id = $1`, m.ID); err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	for _, s := range sourceCIDs {
		pc, err := cid.Parse(s)
		if err != nil {
			log.Warnw("bad piece cid in open_piece", "cid", s, "error", err)
			continue
		}
		if err := c.deleteOn(ctx, m.FromStorage, pc); err != nil {
			log.Warnw("removing moved piece from source", "cid", s, "storage", m.FromStorage, "error", err)
		}
	}
	if c.open != nil {
		if err := c.refresh(ctx, true); err != nil {
			log.Warnw("refreshing hash space map", "error", err)
		}
	}
	for _, id := range []string{m.FromStorage, m.ToStorage} {
		if root, ok := c.roots[id]; ok && c.open != nil {
			c.publishOne(ctx, id, root)
		}
	}
	return c.processPending(ctx)
}

// Refresh reloads local ranges and move sources from the cluster map now.
func (c *Cluster) Refresh(ctx context.Context) error {
	if c.open == nil {
		return nil
	}
	return c.refresh(ctx, true)
}

// casTx runs fn with hash_space_meta.version compare-and-set so map edits
// from different nodes serialize. fn returning false rolls back.
func (c *Cluster) casTx(ctx context.Context, fn func(tx *harmonydb.Tx) (bool, error)) error {
	for attempt := 0; attempt < CAS_RETRIES; attempt++ {
		var ver int64
		if err := c.db.QueryRow(ctx, `SELECT version FROM hash_space_meta WHERE id = 1`).Scan(&ver); err != nil {
			return xerrors.Errorf("reading hash space version: %w", err)
		}
		var stale bool
		_, err := c.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			stale = false
			n, err := tx.Exec(`UPDATE hash_space_meta SET version = version + 1 WHERE id = 1 AND version = $1`, ver)
			if err != nil {
				return false, err
			}
			if n != 1 {
				stale = true
				return false, nil
			}
			return fn(tx)
		}, harmonydb.OptionRetry())
		if err != nil {
			return err
		}
		if !stale {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}
	return xerrors.Errorf("hash space map changed %d times during update", CAS_RETRIES)
}

func (c *Cluster) join(ctx context.Context, drives []LocalDrive) ([]string, error) {
	var arrived []string
	err := c.casTx(ctx, func(tx *harmonydb.Tx) (bool, error) {
		arrived = nil
		var known []string
		if err := tx.Select(&known, `SELECT storage_id FROM hash_space_disk`); err != nil {
			return false, err
		}
		var nRanges int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM hash_space_range`).Scan(&nRanges); err != nil {
			return false, err
		}

		if nRanges == 0 {
			hd := make([]Drive, len(drives))
			for i, d := range drives {
				hd[i] = Drive{Root: d.Root}
			}
			st, err := FirstSetup(hd)
			if err != nil {
				return false, err
			}
			ids := make([]string, len(drives))
			for i, d := range drives {
				ids[i] = d.StorageID
				if _, err := tx.Exec(`INSERT INTO hash_space_disk (storage_id, capacity) VALUES ($1, $2)
					ON CONFLICT (storage_id) DO UPDATE SET capacity = EXCLUDED.capacity, updated_at = NOW()`,
					d.StorageID, st.Disks[i]); err != nil {
					return false, err
				}
			}
			return true, storeState(tx, st, ids)
		}

		for _, d := range drives {
			capacity, err := capacityOf(Drive{Root: d.Root})
			if err != nil {
				return false, err
			}
			if indexOf(known, d.StorageID) >= 0 {
				if _, err := tx.Exec(`UPDATE hash_space_disk SET capacity = $1, updated_at = NOW() WHERE storage_id = $2`, capacity, d.StorageID); err != nil {
					return false, err
				}
				continue
			}
			if _, err := tx.Exec(`INSERT INTO hash_space_disk (storage_id, capacity) VALUES ($1, $2)
				ON CONFLICT (storage_id) DO NOTHING`, d.StorageID, capacity); err != nil {
				return false, err
			}
			arrived = append(arrived, d.StorageID)
		}
		return true, nil
	})
	return arrived, err
}

// writeLocalLayouts writes layout.json for both spaces on every local root
// from the cluster map, keeping each folder's used counter.
func (c *Cluster) writeLocalLayouts(ctx context.Context) error {
	now := time.Now().UTC().Truncate(time.Second)
	for id, root := range c.roots {
		for _, kind := range spaceKinds {
			owned, moveSources, err := c.localIntervals(ctx, kind, id)
			if err != nil {
				return err
			}
			var used int64
			ok, err := fileExists(filepath.Join(root, kind, layoutFile))
			if err != nil {
				return err
			}
			if ok {
				if _, used, err = accountedLayout(root, kind); err != nil {
					return err
				}
			}
			if err := writeLayout(root, kind, Layout{
				Used:        used,
				CommittedAt: now,
				Split:       SPLIT,
				Ranges:      owned,
				MoveSources: moveSources,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Cluster) refresh(ctx context.Context, force bool) error {
	var ver int64
	if err := c.db.QueryRow(ctx, `SELECT version FROM hash_space_meta WHERE id = 1`).Scan(&ver); err != nil {
		return err
	}
	if !force && ver == c.lastVersion.Load() {
		return nil
	}
	for id, root := range c.roots {
		owned, moveSources, err := c.localIntervals(ctx, DIR_OPEN, id)
		if err != nil {
			return err
		}
		if err := c.open.SetIntervalsOn(root, owned, moveSources); err != nil {
			return err
		}
	}
	c.lastVersion.Store(ver)
	return nil
}

func (c *Cluster) localIntervals(ctx context.Context, kind, storageID string) ([]HashRange, []HashRange, error) {
	var rs []rangeRow
	if err := c.db.Select(ctx, &rs, `SELECT end_hash, storage_id FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, kind); err != nil {
		return nil, nil, err
	}
	owned := []HashRange{}
	for i, r := range rs {
		if r.StorageID != storageID {
			continue
		}
		owned = append(owned, HashRange{
			Start: hexEncode(rs[(i-1+len(rs))%len(rs)].EndHash),
			End:   hexEncode(r.EndHash),
		})
	}
	var ms []moveSourceRow
	if err := c.db.Select(ctx, &ms, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE space = $1 AND to_storage = $2`, kind, storageID); err != nil {
		return nil, nil, err
	}
	var moveSources []HashRange
	for _, m := range ms {
		moveSources = append(moveSources, HashRange{Start: hexEncode(m.StartHash), End: hexEncode(m.EndHash)})
	}
	return owned, moveSources, nil
}

func (c *Cluster) publishUsed(ctx context.Context) {
	for id, root := range c.roots {
		c.publishOne(ctx, id, root)
	}
}

// publishOne publishes the disk's open-pieces bytes and its effective
// capacity: those bytes plus the filesystem's free space, capped by the
// configured capacity. Anything else on the filesystem (piece-park, sealing)
// shows up only as less free space.
func (c *Cluster) publishOne(ctx context.Context, storageID, root string) {
	used, err := c.open.UsedOn(root)
	if err != nil {
		return
	}
	capacity, err := effectiveCapacity(root, used)
	if err != nil {
		log.Warnw("reading hash space capacity", "storage", storageID, "error", err)
		if _, err := c.db.Exec(ctx, `UPDATE hash_space_disk SET used_open = $1, updated_at = NOW() WHERE storage_id = $2`, used, storageID); err != nil {
			log.Warnw("publishing hash space used", "storage", storageID, "error", err)
		}
		return
	}
	if _, err := c.db.Exec(ctx, `UPDATE hash_space_disk SET used_open = $1, capacity = $2, updated_at = NOW() WHERE storage_id = $3`,
		used, capacity, storageID); err != nil {
		log.Warnw("publishing hash space used", "storage", storageID, "error", err)
	}
}

func effectiveCapacity(root string, used int64) (int64, error) {
	limit, err := capacityOf(Drive{Root: root})
	if err != nil {
		return 0, err
	}
	free, err := filesystemFree(root)
	if err != nil {
		return 0, err
	}
	if used+free < limit {
		return used + free, nil
	}
	return limit, nil
}

// loadState builds the solver state from the cluster map. Disks are ordered
// by storage id; open-pieces range sizes come from open_piece.
func loadClusterState(tx *harmonydb.Tx) (hashspacesolver.State, []string, error) {
	var disks []struct {
		StorageID string `db:"storage_id"`
		Capacity  int64  `db:"capacity"`
	}
	if err := tx.Select(&disks, `SELECT storage_id, capacity FROM hash_space_disk ORDER BY storage_id`); err != nil {
		return hashspacesolver.State{}, nil, err
	}
	ids := make([]string, len(disks))
	st := hashspacesolver.State{Disks: make([]int64, len(disks))}
	for i, d := range disks {
		ids[i] = d.StorageID
		st.Disks[i] = d.Capacity
	}
	for _, kind := range spaceKinds {
		var rs []rangeRow
		if err := tx.Select(&rs, `SELECT end_hash, storage_id FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, kind); err != nil {
			return hashspacesolver.State{}, nil, err
		}
		if len(rs) == 0 {
			return hashspacesolver.State{}, nil, xerrors.Errorf("hash space %s has no ranges", kind)
		}
		sp := hashspacesolver.Space{Ranges: make([]hashspacesolver.Range, len(rs)), Owner: make([]int, len(rs))}
		for i, r := range rs {
			owner := indexOf(ids, r.StorageID)
			if owner < 0 {
				return hashspacesolver.State{}, nil, xerrors.Errorf("range owner %s is not a hash space disk", r.StorageID)
			}
			var size int64
			if kind == DIR_OPEN {
				start := rs[(i-1+len(rs))%len(rs)].EndHash
				if err := tx.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM (
						SELECT piece_cid, MAX(size) AS size FROM open_piece
						WHERE space = $1
						  AND (($2::bytea = $3::bytea)
						    OR ($2::bytea < $3::bytea AND piece_hash > $2::bytea AND piece_hash <= $3::bytea)
						    OR ($2::bytea > $3::bytea AND (piece_hash > $2::bytea OR piece_hash <= $3::bytea)))
						GROUP BY piece_cid) s`, kind, start, r.EndHash).Scan(&size); err != nil {
					return hashspacesolver.State{}, nil, err
				}
			}
			sp.Ranges[i] = hashspacesolver.Range{EndHash: r.EndHash, Size: size}
			sp.Owner[i] = owner
		}
		st.Spaces = append(st.Spaces, sp)
	}
	return st, ids, nil
}

func storeState(tx *harmonydb.Tx, st hashspacesolver.State, ids []string) error {
	if len(st.Spaces) != len(spaceKinds) {
		return xerrors.Errorf("expected %d hash spaces, got %d", len(spaceKinds), len(st.Spaces))
	}
	for s, kind := range spaceKinds {
		sp := st.Spaces[s]
		rs := make([]rangeRow, len(sp.Ranges))
		for i, r := range sp.Ranges {
			rs[i] = rangeRow{EndHash: r.EndHash, StorageID: ids[sp.Owner[i]]}
		}
		if err := writeSpaceRanges(tx, kind, rs); err != nil {
			return err
		}
	}
	return nil
}

func writeSpaceRanges(tx *harmonydb.Tx, kind string, rs []rangeRow) error {
	if _, err := tx.Exec(`DELETE FROM hash_space_range WHERE space = $1`, kind); err != nil {
		return err
	}
	for _, r := range rs {
		if _, err := tx.Exec(`INSERT INTO hash_space_range (space, end_hash, storage_id) VALUES ($1, $2, $3)`, kind, r.EndHash, r.StorageID); err != nil {
			return err
		}
	}
	return nil
}

// transferRanges gives (start, end] to "to". The interval must lie within
// one range owned by "from". Neighbours with the same owner are merged.
func transferRanges(rs []rangeRow, start, end []byte, from, to string) ([]rangeRow, error) {
	n := len(rs)
	if n == 0 {
		return nil, xerrors.Errorf("no ranges")
	}
	if bytes.Equal(start, end) {
		if n != 1 || rs[0].StorageID != from {
			return nil, xerrors.Errorf("full-circle transfer needs one range owned by %s", from)
		}
		return []rangeRow{{EndHash: rs[0].EndHash, StorageID: to}}, nil
	}
	idx := -1
	for i, r := range rs {
		rStart := rs[(i-1+n)%n].EndHash
		startOK := bytes.Equal(start, rStart) ||
			(hashspacesolver.Contains(rStart, r.EndHash, start) && !bytes.Equal(start, r.EndHash))
		if startOK && hashspacesolver.Contains(start, r.EndHash, end) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, xerrors.Errorf("no range covers (%x, %x]", start, end)
	}
	if rs[idx].StorageID != from {
		return nil, xerrors.Errorf("interval owned by %s, want %s", rs[idx].StorageID, from)
	}

	rStart := rs[(idx-1+n)%n].EndHash
	out := make([]rangeRow, 0, n+2)
	for i, r := range rs {
		if i != idx {
			out = append(out, r)
			continue
		}
		if !bytes.Equal(start, rStart) {
			out = append(out, rangeRow{EndHash: cloneBytes(start), StorageID: from})
		}
		if bytes.Equal(end, r.EndHash) {
			out = append(out, rangeRow{EndHash: r.EndHash, StorageID: to})
		} else {
			out = append(out, rangeRow{EndHash: cloneBytes(end), StorageID: to}, r)
		}
	}
	sortRanges(out)
	return mergeRanges(out), nil
}

func mergeRanges(rs []rangeRow) []rangeRow {
	for len(rs) > 1 {
		merged := false
		for i := 0; i < len(rs); i++ {
			next := (i + 1) % len(rs)
			if rs[i].StorageID == rs[next].StorageID {
				rs = append(rs[:i], rs[i+1:]...)
				merged = true
				break
			}
		}
		if !merged {
			break
		}
	}
	return rs
}

func sortRanges(rs []rangeRow) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && bytes.Compare(rs[j].EndHash, rs[j-1].EndHash) < 0; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

func ownedBytes(st hashspacesolver.State, disk int) int64 {
	var n int64
	for _, sp := range st.Spaces {
		for i, r := range sp.Ranges {
			if sp.Owner[i] == disk {
				n += r.Size
			}
		}
	}
	return n
}

func indexOf(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

func cloneBytes(b []byte) []byte {
	return append([]byte(nil), b...)
}
