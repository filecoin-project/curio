package hashspace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
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
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/fs2"
	"github.com/filecoin-project/curio/lib/hashspacesolver"
	"github.com/filecoin-project/curio/tasks/tasknames"
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

	// NOTIFY_TIMEOUT bounds each map-change notification to another node.
	NOTIFY_TIMEOUT = 5 * time.Second

	storageURLSeparator = ","
	remoteSuffix        = "/remote"
	httpPrefix          = "/hashspace/"
	notifyPath          = httpPrefix + "notify"
	accountPath         = httpPrefix + "account"

	// LIST_PAGE is how many directory entries a rebalance scan reads at once.
	LIST_PAGE = 256
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
// hash_space_range is the single-owner tiling the solver plans on.
// A piece file is on that range's owner, or on both ends of a move that
// covers its hash.
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
	Size      int64  `db:"size"`
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
	var lastOverlapCheck time.Time
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
		if c.open != nil && time.Since(lastOverlapCheck) >= OVERLAP_CHECK_INTERVAL {
			lastOverlapCheck = time.Now()
			if _, err := c.FixOverlaps(ctx); err != nil {
				log.Warnw("checking for misplaced open pieces", "error", err)
			}
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

// Locations lists where pieceCID can be: the range owner, or both ends of a
// move that covers its hash. Local disks come first.
func (c *Cluster) Locations(ctx context.Context, pieceCID string) ([]Location, error) {
	pc, err := cid.Parse(pieceCID)
	if err != nil {
		return nil, xerrors.Errorf("parsing piece cid %s: %w", pieceCID, err)
	}
	digest, err := CIDHash(pc)
	if err != nil {
		return nil, err
	}
	return c.places(ctx, digest)
}

// HasFile reports whether the open-pieces file is on any place that can hold it.
func (c *Cluster) HasFile(ctx context.Context, pc cid.Cid) (bool, error) {
	digest, err := CIDHash(pc)
	if err != nil {
		return false, err
	}
	places, err := c.places(ctx, digest)
	if err != nil {
		return false, err
	}
	hexHash := hex.EncodeToString(digest)
	for _, p := range places {
		ok, err := c.hasHash(ctx, p.StorageID, hexHash)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// places returns the disks that may hold digest, local ones first.
// A move that covers the hash yields its source and destination; otherwise
// the range owner is the only place.
func (c *Cluster) places(ctx context.Context, digest []byte) ([]Location, error) {
	var moves []moveSourceRow
	err := c.db.Select(ctx, &moves, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE space = $1`, DIR_OPEN)
	if err != nil {
		return nil, xerrors.Errorf("reading hash space moveSources: %w", err)
	}
	var ids []string
	for _, m := range moves {
		if hashspacesolver.Contains(m.StartHash, m.EndHash, digest) {
			ids = []string{m.FromStorage, m.ToStorage}
			break
		}
	}
	if ids == nil {
		var rs []rangeRow
		err = c.db.Select(ctx, &rs, `SELECT end_hash, storage_id FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, DIR_OPEN)
		if err != nil {
			return nil, xerrors.Errorf("reading hash space ranges: %w", err)
		}
		for i, r := range rs {
			if hashspacesolver.Contains(rs[(i-1+len(rs))%len(rs)].EndHash, r.EndHash, digest) {
				ids = []string{r.StorageID}
				break
			}
		}
	}
	if ids == nil {
		return nil, xerrors.Errorf("no hash space range owns %x", digest)
	}
	return localFirst(c, ids), nil
}

func localFirst(c *Cluster, ids []string) []Location {
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
	return out
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
// notifyPeers asks every other node holding a hash-space disk to reload the
// map now, once per node. Nodes that miss it still pick the change up from
// hash_space_meta.version on their next refresh.
func (c *Cluster) notifyPeers() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), REFRESH_INTERVAL)
		defer cancel()
		var peers []struct {
			StorageID string `db:"storage_id"`
			URLs      string `db:"urls"`
		}
		if err := c.db.Select(ctx, &peers, `SELECT d.storage_id, COALESCE(sp.urls, '') AS urls
			FROM hash_space_disk d JOIN storage_path sp ON sp.storage_id = d.storage_id`); err != nil {
			log.Warnw("listing hash space nodes to notify", "error", err)
			return
		}
		notified := map[string]bool{}
		for _, p := range peers {
			if c.HasLocal(p.StorageID) {
				continue
			}
			var bases []string
			for _, u := range strings.Split(p.URLs, storageURLSeparator) {
				if u != "" {
					bases = append(bases, strings.TrimSuffix(strings.TrimSuffix(u, "/"), remoteSuffix))
				}
			}
			done := false
			for _, b := range bases {
				done = done || notified[b]
			}
			if done {
				continue
			}
			var lastErr error
			for _, b := range bases {
				if lastErr = c.notifyOne(ctx, b+notifyPath); lastErr == nil {
					notified[b] = true
					break
				}
			}
			if lastErr != nil {
				log.Debugw("notifying hash space node", "storage", p.StorageID, "error", lastErr)
			}
		}
	}()
}

func (c *Cluster) notifyOne(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, NOTIFY_TIMEOUT)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if err != nil {
		return err
	}
	for k, v := range c.auth {
		req.Header[k] = v
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return xerrors.Errorf("POST %s: %s", target, resp.Status)
	}
	return nil
}

func (c *Cluster) remote(ctx context.Context, method, storageID string, pc cid.Cid, prep func(*http.Request), handle func(*http.Response) (bool, error)) error {
	hexHash, _, err := cidHashHex(pc)
	if err != nil {
		return err
	}
	return c.remoteHash(ctx, method, storageID, hexHash, prep, handle)
}

func (c *Cluster) remoteHash(ctx context.Context, method, storageID, hexHash string, prep func(*http.Request), handle func(*http.Response) (bool, error)) error {
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

// DeleteCID removes pieceCID from every disk that can hold it: the range
// owner, and both ends when a move covers its hash. A missing file is fine.
// The lookup runs twice so a move that starts during the first pass is
// still cleared. A copy that lands after both passes drops its destination
// when the source file is already gone.
func (c *Cluster) DeleteCID(ctx context.Context, pieceCID string) error {
	pc, err := cid.Parse(pieceCID)
	if err != nil {
		return xerrors.Errorf("parsing piece cid %s: %w", pieceCID, err)
	}
	digest, err := CIDHash(pc)
	if err != nil {
		return err
	}
	touched := map[string]struct{}{}
	for pass := 0; pass < 2; pass++ {
		places, err := c.places(ctx, digest)
		if err != nil {
			return err
		}
		for _, p := range places {
			if err := c.deleteOn(ctx, p.StorageID, pc); err != nil {
				return xerrors.Errorf("deleting %s from %s: %w", pieceCID, p.StorageID, err)
			}
			touched[p.StorageID] = struct{}{}
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
// Without a task engine yet (during NewCluster) the event is queued for the
// refresh loop, since planned moves need a HashSpaceMove task.
func (c *Cluster) raise(ctx context.Context, storageID, kind string) error {
	addMove := harmonytask.AdderFor(tasknames.HashSpaceMove)
	if addMove == nil {
		_, err := c.db.Exec(ctx, `INSERT INTO hash_space_pending_event (storage_id, event_kind) VALUES ($1, $2)
			ON CONFLICT (storage_id, event_kind) DO NOTHING`, storageID, kind)
		return err
	}
	return c.casTaskTx(ctx, addMove, func(tx *harmonydb.Tx, id harmonytask.TaskID) (bool, error) {
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

		var moves []hashspacesolver.Transfer
		for _, t := range res.Diff {
			if t.Size <= 0 || t.From < 0 || t.From == t.To {
				continue
			}
			if t.Space != 0 {
				log.Warnw("skipping acl-pieces transfer", "from", ids[t.From], "to", ids[t.To], "size", t.Size)
				continue
			}
			moves = append(moves, t)
		}
		if len(moves) == 0 {
			return false, nil
		}
		if id == 0 {
			return false, harmonytask.ErrNeedTask
		}

		var planned int
		for _, t := range moves {
			n, err := tx.Exec(`INSERT INTO hash_space_move_source (space, start_hash, end_hash, from_storage, to_storage, size, task_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (space, start_hash, end_hash) DO NOTHING`,
				DIR_OPEN, t.StartHash, t.EndHash, ids[t.From], ids[t.To], t.Size, id)
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

// MoveSourcesByTask returns the move sources owned by a HashSpaceMove task.
func (c *Cluster) MoveSourcesByTask(ctx context.Context, taskID int64) ([]*MoveSource, error) {
	var ms []moveSourceRow
	err := c.db.Select(ctx, &ms, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE task_id = $1 ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]*MoveSource, len(ms))
	for i, m := range ms {
		out[i] = &MoveSource{ID: m.ID, StartHash: m.StartHash, EndHash: m.EndHash, FromStorage: m.FromStorage, ToStorage: m.ToStorage}
	}
	return out, nil
}

// PendingCopy lists piece hashes in the move interval that are still on the
// source and not yet on the local destination. Hashes are the file names fs2
// reads back out of the directory.
func (c *Cluster) PendingCopy(ctx context.Context, m *MoveSource, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	if !c.HasLocal(m.ToStorage) {
		return nil, xerrors.Errorf("move destination %s is not local", m.ToStorage)
	}
	low, high := hexEncode(m.StartHash), hexEncode(m.EndHash)
	after := ""
	var out []string
	for len(out) < limit {
		batch, err := c.listPieceHashes(ctx, m.FromStorage, low, high, after, LIST_PAGE)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for _, h := range batch {
			after = h
			ok, err := c.hasHashLocal(m.ToStorage, h)
			if err != nil {
				return nil, err
			}
			if ok {
				continue
			}
			out = append(out, h)
			if len(out) == limit {
				return out, nil
			}
		}
		if len(batch) < LIST_PAGE {
			break
		}
	}
	return out, nil
}

// CopyOne copies one piece hash of the move source onto its local destination.
// A source file that disappears during the copy is a delete: the destination
// copy is removed and the move continues.
func (c *Cluster) CopyOne(ctx context.Context, m *MoveSource, hexHash string) error {
	src, err := c.openHash(ctx, m.FromStorage, hexHash)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return xerrors.Errorf("opening %s on %s: %w", hexHash, m.FromStorage, err)
	}
	_, err = c.writeHashLocal(m.ToStorage, hexHash, src)
	_ = src.Close()
	if err != nil {
		return err
	}
	ok, err := c.hasHash(ctx, m.FromStorage, hexHash)
	if err != nil {
		return err
	}
	if !ok {
		if err := c.deleteHash(ctx, m.ToStorage, hexHash); err != nil {
			return xerrors.Errorf("dropping %s deleted during copy: %w", hexHash, err)
		}
	}
	return nil
}

// CompleteMoveSource hands the move interval to its destination once every
// file still on the source is also on the destination, then removes the
// source copies. A file deleted from the source is not required on the
// destination.
func (c *Cluster) CompleteMoveSource(ctx context.Context, m *MoveSource) error {
	if !c.HasLocal(m.ToStorage) {
		return xerrors.Errorf("move destination %s is not local", m.ToStorage)
	}
	missing, err := c.sourceMissing(ctx, m)
	if err != nil {
		return err
	}
	if missing {
		return xerrors.Errorf("move source %d still has files missing on %s", m.ID, m.ToStorage)
	}

	err = c.casTx(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var rs []rangeRow
		if err := tx.Select(&rs, `SELECT end_hash, storage_id, size FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, DIR_OPEN); err != nil {
			return false, err
		}
		// A misplacement fix moves pieces into an interval the destination
		// already owns; only the source copies need to go.
		if !intervalOwnedBy(rs, m.StartHash, m.EndHash, m.ToStorage) {
			next, err := transferRanges(rs, m.StartHash, m.EndHash, m.FromStorage, m.ToStorage)
			if err != nil {
				return false, xerrors.Errorf("move source %d: %w", m.ID, err)
			}
			if err := writeSpaceRanges(tx, DIR_OPEN, next); err != nil {
				return false, err
			}
		}
		if _, err := tx.Exec(`DELETE FROM hash_space_move_source WHERE id = $1`, m.ID); err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	low, high := hexEncode(m.StartHash), hexEncode(m.EndHash)
	after := ""
	for {
		batch, err := c.listPieceHashes(ctx, m.FromStorage, low, high, after, LIST_PAGE)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		for _, h := range batch {
			after = h
			if err := c.deleteHash(ctx, m.FromStorage, h); err != nil {
				log.Warnw("removing moved piece from source", "hash", h, "storage", m.FromStorage, "error", err)
			}
		}
		if len(batch) < LIST_PAGE {
			break
		}
	}
	for _, id := range []string{m.FromStorage, m.ToStorage} {
		if err := c.accountStorage(ctx, id); err != nil {
			log.Warnw("publishing hash space sizes", "storage", id, "error", err)
		}
	}
	return c.processPending(ctx)
}

// sourceMissing reports whether the source still has a piece hash the
// destination does not.
func (c *Cluster) sourceMissing(ctx context.Context, m *MoveSource) (bool, error) {
	low, high := hexEncode(m.StartHash), hexEncode(m.EndHash)
	after := ""
	for {
		batch, err := c.listPieceHashes(ctx, m.FromStorage, low, high, after, LIST_PAGE)
		if err != nil {
			return false, err
		}
		if len(batch) == 0 {
			return false, nil
		}
		for _, h := range batch {
			after = h
			ok, err := c.hasHashLocal(m.ToStorage, h)
			if err != nil {
				return false, err
			}
			if !ok {
				return true, nil
			}
		}
		if len(batch) < LIST_PAGE {
			return false, nil
		}
	}
}

func (c *Cluster) listPieceHashes(ctx context.Context, storageID, low, high, after string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []string
	for len(out) < limit {
		batch, err := c.listHashes(ctx, storageID, low, high, after, limit)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for _, h := range batch {
			after = h
			if !isPieceHash(h) {
				continue
			}
			out = append(out, h)
			if len(out) == limit {
				return out, nil
			}
		}
		if len(batch) < limit {
			break
		}
	}
	return out, nil
}

func (c *Cluster) listHashes(ctx context.Context, storageID, low, high, after string, limit int) ([]string, error) {
	if c.HasLocal(storageID) {
		root, err := c.rootOf(storageID)
		if err != nil {
			return nil, err
		}
		hashes, err := fs2.ListHashesInterval(filepath.Join(root, DIR_OPEN), low, high, after, limit)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, xerrors.Errorf("listing %s: %w", storageID, err)
		}
		return hashes, nil
	}
	return c.remoteList(ctx, storageID, low, high, after, limit)
}

func isPieceHash(h string) bool {
	if len(h) != HASH_BYTES*2 {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil
}

func (c *Cluster) hasHash(ctx context.Context, storageID, hexHash string) (bool, error) {
	if c.HasLocal(storageID) {
		return c.hasHashLocal(storageID, hexHash)
	}
	var found bool
	err := c.remoteHash(ctx, http.MethodHead, storageID, hexHash, nil, func(r *http.Response) (bool, error) {
		_ = r.Body.Close()
		switch r.StatusCode {
		case http.StatusOK:
			found = true
			return false, nil
		case http.StatusNotFound:
			return false, nil
		default:
			return false, xerrors.Errorf("HEAD %s: %s", r.Request.URL, r.Status)
		}
	})
	return found, err
}

func (c *Cluster) hasHashLocal(storageID, hexHash string) (bool, error) {
	root, err := c.rootOf(storageID)
	if err != nil {
		return false, err
	}
	path, err := c.open.hashPathOn(root, hexHash)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, xerrors.Errorf("%s is not a regular file", path)
	}
	return true, nil
}

func (c *Cluster) openHash(ctx context.Context, storageID, hexHash string) (io.ReadCloser, error) {
	if c.HasLocal(storageID) {
		root, err := c.rootOf(storageID)
		if err != nil {
			return nil, err
		}
		return c.open.openHashOn(root, hexHash)
	}
	var resp *http.Response
	err := c.remoteHash(ctx, http.MethodGet, storageID, hexHash, nil, func(r *http.Response) (bool, error) {
		switch r.StatusCode {
		case http.StatusOK:
			resp = r
			return true, nil
		case http.StatusNotFound:
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

func (c *Cluster) writeHashLocal(storageID, hexHash string, r io.Reader) (int64, error) {
	root, err := c.rootOf(storageID)
	if err != nil {
		return 0, err
	}
	w, err := c.open.WriteHashOn(root, hexHash)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			ok, serr := c.hasHashLocal(storageID, hexHash)
			if serr != nil {
				return 0, serr
			}
			if !ok {
				return 0, os.ErrNotExist
			}
			path, serr := c.open.hashPathOn(root, hexHash)
			if serr != nil {
				return 0, serr
			}
			info, serr := os.Stat(path)
			if serr != nil {
				return 0, serr
			}
			return info.Size(), nil
		}
		return 0, err
	}
	n, err := io.CopyBuffer(w, r, make([]byte, 8<<20))
	if err != nil {
		if a, ok := w.(interface{ Abort() error }); ok {
			_ = a.Abort()
		}
		return 0, xerrors.Errorf("copying %s into open-pieces: %w", hexHash, err)
	}
	if err := w.Close(); err != nil {
		if errors.Is(err, os.ErrExist) {
			return n, nil
		}
		return 0, err
	}
	return n, nil
}

func (c *Cluster) deleteHash(ctx context.Context, storageID, hexHash string) error {
	if c.HasLocal(storageID) {
		root, err := c.rootOf(storageID)
		if err != nil {
			return err
		}
		if err := c.open.deleteHashOn(root, hexHash); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return c.remoteHash(ctx, http.MethodDelete, storageID, hexHash, nil, func(r *http.Response) (bool, error) {
		_ = r.Body.Close()
		switch r.StatusCode {
		case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
			return false, nil
		default:
			return false, xerrors.Errorf("DELETE %s: %s", r.Request.URL, r.Status)
		}
	})
}

func (c *Cluster) remoteList(ctx context.Context, storageID, low, high, after string, limit int) ([]string, error) {
	q := "?limit=" + strconv.Itoa(limit)
	if low != "" {
		q += "&low=" + low
	}
	if high != "" {
		q += "&high=" + high
	}
	if after != "" {
		q += "&after=" + after
	}
	var body []byte
	err := c.remoteHash(ctx, http.MethodGet, storageID, "list"+q, nil, func(r *http.Response) (bool, error) {
		defer func() { _ = r.Body.Close() }()
		if r.StatusCode != http.StatusOK {
			return false, xerrors.Errorf("GET list %s: %s", storageID, r.Status)
		}
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		return false, err
	})
	if err != nil {
		return nil, err
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			out = append(out, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// accountStorage republishes one disk's open-pieces range sizes. A remote
// disk is asked over /hashspace/account.
func (c *Cluster) accountStorage(ctx context.Context, storageID string) error {
	if c.HasLocal(storageID) {
		root, err := c.rootOf(storageID)
		if err != nil {
			return err
		}
		c.publishOne(ctx, storageID, root)
		return nil
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
		base := strings.TrimSuffix(strings.TrimSuffix(u, "/"), remoteSuffix)
		if err := c.notifyOne(ctx, base+accountPath); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// Refresh reloads local ranges and move sources from the cluster map now.
func (c *Cluster) Refresh(ctx context.Context) error {
	if c.open == nil {
		return nil
	}
	return c.refresh(ctx, true)
}

// casTx runs fn with hash_space_meta.version compare-and-set so map edits
// from different nodes serialize. fn returning false rolls back. A commit
// reloads the local map and notifies the other hash-space nodes.
func (c *Cluster) casTx(ctx context.Context, fn func(tx *harmonydb.Tx) (bool, error)) error {
	return c.casTaskTx(ctx, nil, func(tx *harmonydb.Tx, _ harmonytask.TaskID) (bool, error) {
		return fn(tx)
	})
}

// casTaskTx is casTx for map edits that start a task: fn returns
// harmonytask.ErrNeedTask when run with id 0 to be rerun inside addTask's
// transaction with the new task's id.
func (c *Cluster) casTaskTx(ctx context.Context, addTask harmonytask.AddTaskFunc, fn func(tx *harmonydb.Tx, id harmonytask.TaskID) (bool, error)) error {
	for attempt := 0; attempt < CAS_RETRIES; attempt++ {
		ver, err := c.mapVersion(ctx)
		if err != nil {
			return err
		}
		var stale bool
		committed, err := harmonytask.TxWithTask(ctx, c.db, addTask, func(tx *harmonydb.Tx, id harmonytask.TaskID) (bool, error) {
			stale = false
			n, err := tx.Exec(`UPDATE hash_space_meta SET version = version + 1 WHERE id = 1 AND version = $1`, ver)
			if err != nil {
				return false, err
			}
			if n != 1 {
				stale = true
				return false, nil
			}
			return fn(tx, id)
		})
		if err != nil {
			return err
		}
		if !stale {
			if committed {
				if c.open != nil {
					if err := c.refresh(ctx, true); err != nil {
						log.Warnw("refreshing hash space map", "error", err)
					}
				}
				c.notifyPeers()
			}
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
			// FirstSetup takes existing layouts' ranges as-is and may rewrite
			// them, so only their versions are carried over.
			if _, err := importNewerLayouts(tx, drives, false); err != nil {
				return false, err
			}
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

		var fresh []string
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
			fresh = append(fresh, d.StorageID)
		}
		imported, err := importNewerLayouts(tx, drives, true)
		if err != nil {
			return false, err
		}
		for _, id := range fresh {
			if indexOf(imported, id) < 0 {
				arrived = append(arrived, id)
			}
		}
		return true, nil
	})
	return arrived, err
}

// importNewerLayouts writes the ranges of every local layout.json whose
// version is at or past the version this transaction set, meaning the
// database is behind the disk (restored or rolled back). The version is then
// raised past every local layout so later startups trust the database. Move
// sources are not restored; their source disk is not recorded locally.
// apply=false only raises the version. It returns the storage ids imported.
func importNewerLayouts(tx *harmonydb.Tx, drives []LocalDrive, apply bool) ([]string, error) {
	var current int64
	if err := tx.QueryRow(`SELECT version FROM hash_space_meta WHERE id = 1`).Scan(&current); err != nil {
		return nil, err
	}
	var imported []string
	maxVersion := int64(-1)
	for _, d := range drives {
		for _, kind := range spaceKinds {
			path := filepath.Join(d.Root, kind, layoutFile)
			ok, err := fileExists(path)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			layout, err := readLayout(path)
			if err != nil {
				return nil, err
			}
			if layout.Version > maxVersion {
				maxVersion = layout.Version
			}
			if !apply || layout.Version < current || len(layout.Ranges) == 0 {
				continue
			}
			var rs []rangeRow
			if err := tx.Select(&rs, `SELECT end_hash, storage_id FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, kind); err != nil {
				return nil, err
			}
			for i, r := range layout.Ranges {
				start, err := decodeHash(r.Start)
				if err != nil {
					return nil, xerrors.Errorf("%s range %d: %w", path, i, err)
				}
				end, err := decodeHash(r.End)
				if err != nil {
					return nil, xerrors.Errorf("%s range %d: %w", path, i, err)
				}
				if rs, err = assignRange(rs, start, end, d.StorageID); err != nil {
					return nil, xerrors.Errorf("%s range %d: %w", path, i, err)
				}
			}
			if err := writeSpaceRanges(tx, kind, rs); err != nil {
				return nil, err
			}
			if len(layout.MoveSources) > 0 {
				log.Warnw("hash space layout move sources not restored", "storage", d.StorageID, "space", kind, "count", len(layout.MoveSources))
			}
			log.Warnw("hash space database is behind layout.json; restored its ranges",
				"storage", d.StorageID, "space", kind, "layout_version", layout.Version, "db_version", current-1)
			if indexOf(imported, d.StorageID) < 0 {
				imported = append(imported, d.StorageID)
			}
		}
	}
	if maxVersion >= current {
		if _, err := tx.Exec(`UPDATE hash_space_meta SET version = $1 WHERE id = 1`, maxVersion+1); err != nil {
			return nil, err
		}
	}
	return imported, nil
}

// writeLocalLayouts writes layout.json for both spaces on every local root
// from the cluster map, keeping each folder's used counter.
func (c *Cluster) writeLocalLayouts(ctx context.Context) error {
	ver, err := c.mapVersion(ctx)
	if err != nil {
		return err
	}
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
				Version:     ver,
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

// mapVersion is read before the intervals it labels, so a layout never
// claims a newer version than its ranges.
func (c *Cluster) mapVersion(ctx context.Context) (int64, error) {
	var ver int64
	if err := c.db.QueryRow(ctx, `SELECT version FROM hash_space_meta WHERE id = 1`).Scan(&ver); err != nil {
		return 0, xerrors.Errorf("reading hash space version: %w", err)
	}
	return ver, nil
}

func (c *Cluster) refresh(ctx context.Context, force bool) error {
	ver, err := c.mapVersion(ctx)
	if err != nil {
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
		if err := c.open.SetIntervalsOn(root, ver, owned, moveSources); err != nil {
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
	if err := c.publishRangeSizes(ctx, storageID, root, used); err != nil {
		log.Warnw("publishing hash space range sizes", "storage", storageID, "error", err)
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

func (c *Cluster) publishRangeSizes(ctx context.Context, storageID, root string, used int64) error {
	owned, _, err := c.localIntervals(ctx, DIR_OPEN, storageID)
	if err != nil {
		return err
	}
	sizes, err := sizesForRanges(filepath.Join(root, DIR_OPEN), owned, used)
	if err != nil {
		return err
	}
	for i, r := range owned {
		end, err := decodeHash(r.End)
		if err != nil {
			return err
		}
		if _, err := c.db.Exec(ctx, `UPDATE hash_space_range SET size = $1
			WHERE space = $2 AND end_hash = $3 AND storage_id = $4`, sizes[i], DIR_OPEN, end, storageID); err != nil {
			return err
		}
	}
	return nil
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
// by storage id. Open-pieces range sizes are the totals the owning node
// published from its directory.
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
		if err := tx.Select(&rs, `SELECT end_hash, storage_id, size FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, kind); err != nil {
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
			size := r.Size
			if kind != DIR_OPEN {
				size = 0
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
			rs[i] = rangeRow{EndHash: r.EndHash, StorageID: ids[sp.Owner[i]], Size: r.Size}
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
		if _, err := tx.Exec(`INSERT INTO hash_space_range (space, end_hash, storage_id, size) VALUES ($1, $2, $3, $4)`, kind, r.EndHash, r.StorageID, r.Size); err != nil {
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
		return []rangeRow{{EndHash: rs[0].EndHash, StorageID: to, Size: rs[0].Size}}, nil
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
		full := hashspacesolver.Range{EndHash: r.EndHash, Size: r.Size}
		var prefix int64
		if !bytes.Equal(start, rStart) {
			prefix = hashspacesolver.SliceSize(full, rStart, start)
			out = append(out, rangeRow{EndHash: cloneBytes(start), StorageID: from, Size: prefix})
		}
		moved := hashspacesolver.SliceSize(full, start, end)
		if bytes.Equal(end, r.EndHash) {
			out = append(out, rangeRow{EndHash: r.EndHash, StorageID: to, Size: moved})
			continue
		}
		suffix := r.Size - prefix - moved
		if suffix < 0 {
			suffix = 0
		}
		out = append(out,
			rangeRow{EndHash: cloneBytes(end), StorageID: to, Size: moved},
			rangeRow{EndHash: r.EndHash, StorageID: from, Size: suffix},
		)
	}
	sortRanges(out)
	return mergeRanges(out), nil
}

// assignRange gives (start, end] to owner whatever it covers, splitting the
// ranges at its bounds. Neighbours with the same owner are merged.
func assignRange(rs []rangeRow, start, end []byte, owner string) ([]rangeRow, error) {
	if len(rs) == 0 {
		return nil, xerrors.Errorf("no ranges")
	}
	if bytes.Equal(start, end) {
		return []rangeRow{{EndHash: cloneBytes(end), StorageID: owner}}, nil
	}
	out := splitRangesAt(splitRangesAt(append([]rangeRow(nil), rs...), start), end)
	for i := range out {
		if hashspacesolver.Contains(start, end, out[i].EndHash) {
			out[i].StorageID = owner
		}
	}
	return mergeRanges(out), nil
}

// intervalOwnedBy reports whether owner holds all of (start, end].
func intervalOwnedBy(rs []rangeRow, start, end []byte, owner string) bool {
	if len(rs) == 0 {
		return false
	}
	for _, r := range splitRangesAt(splitRangesAt(append([]rangeRow(nil), rs...), start), end) {
		if hashspacesolver.Contains(start, end, r.EndHash) && r.StorageID != owner {
			return false
		}
	}
	return true
}

// splitRangesAt adds a boundary at p, keeping the owner of the range it cut.
func splitRangesAt(rs []rangeRow, p []byte) []rangeRow {
	n := len(rs)
	for _, r := range rs {
		if bytes.Equal(r.EndHash, p) {
			return rs
		}
	}
	for i, r := range rs {
		if hashspacesolver.Contains(rs[(i-1+n)%n].EndHash, r.EndHash, p) {
			rs = append(rs, rangeRow{EndHash: cloneBytes(p), StorageID: r.StorageID})
			sortRanges(rs)
			return rs
		}
	}
	return rs
}

func mergeRanges(rs []rangeRow) []rangeRow {
	for len(rs) > 1 {
		merged := false
		for i := 0; i < len(rs); i++ {
			next := (i + 1) % len(rs)
			if rs[i].StorageID == rs[next].StorageID {
				rs[next].Size += rs[i].Size
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
