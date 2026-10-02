package hashspace

import (
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
	"github.com/filecoin-project/curio/lib/hashspacesolver"
)

var log = logging.Logger("hashspace")

const (
	// EVENT_FULL, EVENT_ARRIVE, EVENT_ABSORB, EVENT_VACATE, and EVENT_BALANCE
	// are hash_space_pending_event.event_kind values.
	EVENT_FULL    = "full"
	EVENT_ARRIVE  = "arrive"
	EVENT_ABSORB  = "absorb"
	EVENT_VACATE  = "vacate"
	EVENT_BALANCE = "balance"

	// ROOM_RETURN_MIN is the capacity increase that asks a disk to take
	// bytes back after other filesystem activity frees space.
	ROOM_RETURN_MIN = 1 << 30

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
	acl   *Space
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

	if len(drives) > 0 {
		if _, err := c.join(ctx, drives); err != nil {
			return nil, xerrors.Errorf("joining hash space: %w", err)
		}
		if err := c.writeLocalLayouts(ctx); err != nil {
			return nil, err
		}
		roots := make([]string, 0, len(drives))
		for _, d := range drives {
			roots = append(roots, d.Root)
		}
		var err error
		c.open, err = Load(DIR_OPEN, roots)
		if err != nil {
			return nil, err
		}
		c.acl, err = Load(DIR_ACL, roots)
		if err != nil {
			_ = c.open.Close()
			return nil, err
		}
		if err := c.refresh(ctx, true); err != nil {
			return nil, err
		}
		c.publishUsed(ctx)
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
	var err error
	if c.open != nil {
		err = c.open.Close()
	}
	if c.acl != nil {
		if aerr := c.acl.Close(); err == nil {
			err = aerr
		}
	}
	return err
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
		if c.open == nil {
			continue
		}
		if err := c.refresh(ctx, false); err != nil {
			log.Warnw("refreshing hash space map", "error", err)
		}
	}
}

// HasLocal reports whether storageID is a hash-space root on this node.
func (c *Cluster) HasLocal(storageID string) bool {
	_, ok := c.roots[storageID]
	return ok
}

func (c *Cluster) space(kind string) *Space {
	switch kind {
	case DIR_OPEN:
		return c.open
	case DIR_ACL:
		return c.acl
	default:
		return nil
	}
}

func parseSpaceKind(s string) (string, error) {
	switch s {
	case "", DIR_OPEN:
		return DIR_OPEN, nil
	case DIR_ACL:
		return DIR_ACL, nil
	default:
		return "", xerrors.Errorf("unknown hash space %q", s)
	}
}

// diskVacate reports whether this storage path denies piece park. Those
// disks are emptied and are not given ranges on first setup or arrival.
func diskVacate(root string) bool {
	deny, err := deniesPiecePark(root)
	if err != nil {
		log.Warnw("reading piece park allowance", "root", root, "error", err)
		return false
	}
	return deny
}

func (c *Cluster) rootOf(storageID string) (string, error) {
	root, ok := c.roots[storageID]
	if !ok || (c.open == nil && c.acl == nil) {
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

// ReturnLocal moves an open-pieces file back to dest and subtracts its size.
// dest must not already exist. Used to undo an adopt when placement loses
// the race with a delete.
func (c *Cluster) ReturnLocal(storageID string, pc cid.Cid, dest string) error {
	root, err := c.rootOf(storageID)
	if err != nil {
		return err
	}
	return c.open.ReturnFileOn(root, pc, dest)
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
		ok, err := /* Cluster.hasHash */ func(ctx context.Context, storageID, kind, hexHash string) (bool, error) {
			if c.HasLocal(storageID) {
				return /* Cluster.hasHashLocal */ func(storageID, kind, hexHash string) (bool, error) {
					kind, err := parseSpaceKind(kind)
					if err != nil {
						return false, err
					}
					sp := c.space(kind)
					if sp == nil {
						return false, xerrors.Errorf("hash space %s is not loaded", kind)
					}
					root, err := c.rootOf(storageID)
					if err != nil {
						return false, err
					}
					path, err := sp.hashPathOn(root, hexHash)
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
				}(storageID, kind, hexHash)
			}
			var found bool
			err := c.remoteHash(ctx, http.MethodHead, storageID, kind, hexHash, nil, func(r *http.Response) (bool, error) {
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
		}(ctx, p.StorageID, DIR_OPEN, hexHash)
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
	return /* localFirst */ func(c *Cluster, ids []string) []Location {
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
	}(c, ids), nil
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

// remote sends one request to each URL of storageID until one answers.
// handle reports whether it kept the response body open.
// notifyPeers asks every other node holding a hash-space disk to reload the
// map now, once per node. Nodes that miss it still pick the change up from
// hash_space_meta.version on their next refresh.

func (c *Cluster) remote(ctx context.Context, method, storageID string, pc cid.Cid, prep func(*http.Request), handle func(*http.Response) (bool, error)) error {
	hexHash, _, err := cidHashHex(pc)
	if err != nil {
		return err
	}
	return c.remoteHash(ctx, method, storageID, DIR_OPEN, hexHash, prep, handle)
}

func (c *Cluster) remoteHash(ctx context.Context, method, storageID, kind, hexHash string, prep func(*http.Request), handle func(*http.Response) (bool, error)) error {
	kind, err := parseSpaceKind(kind)
	if err != nil {
		return err
	}
	if strings.Contains(hexHash, "?") {
		hexHash += "&space=" + kind
	} else {
		hexHash += "?space=" + kind
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

// DeleteCID removes pieceCID from every hash-space disk, not only the range
// owner. A move or a failed cleanup can leave a copy on another disk, and
// overlap repair would copy that stray back onto the owner. A missing file
// is fine. The scan runs twice so a copy that starts during the first pass
// is still cleared. A copy that lands after both passes drops its destination
// when the source file is already gone.
func (c *Cluster) DeleteCID(ctx context.Context, pieceCID string) error {
	pc, err := cid.Parse(pieceCID)
	if err != nil {
		return xerrors.Errorf("parsing piece cid %s: %w", pieceCID, err)
	}
	if _, err := CIDHash(pc); err != nil {
		return err
	}
	var disks []struct {
		StorageID string `db:"storage_id"`
	}
	if err := c.db.Select(ctx, &disks, `SELECT storage_id FROM hash_space_disk`); err != nil {
		return xerrors.Errorf("listing hash space disks: %w", err)
	}
	for pass := 0; pass < 2; pass++ {
		for _, d := range disks {
			if err := /* Cluster.deleteOn */ func(ctx context.Context, storageID string, pc cid.Cid) error {
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
			}(ctx, d.StorageID, pc); err != nil {
				return xerrors.Errorf("deleting %s from %s: %w", pieceCID, d.StorageID, err)
			}
		}
	}
	return nil
}

func overFill(used, capacity int64) bool {
	if capacity <= 0 {
		return used > 0
	}
	limit := capacity/100*hashspacesolver.FILL_LIMIT_PERCENT + capacity%100*hashspacesolver.FILL_LIMIT_PERCENT/100
	return used > limit
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

// casTaskTx is casTx for map edits that start a task: fn returns
// harmonytask.ErrNeedTask when run with id 0 to be rerun inside addTask's
// transaction with the new task's id.

func (c *Cluster) join(ctx context.Context, drives []LocalDrive) ([]string, error) {
	var arrived []string
	err := /* Cluster.casTx */ func(ctx context.Context, fn func(tx *harmonydb.Tx) (bool, error)) error {
		return /* Cluster.casTaskTx */ func(ctx context.Context, addTask harmonytask.AddTaskFunc, fn func(tx *harmonydb.Tx, id harmonytask.TaskID) (bool, error)) error {
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
						/* Cluster.notifyPeers */ func() {
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
										if lastErr = /* Cluster.notifyOne */ func(ctx context.Context, target string) error {
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
										}(ctx, b+notifyPath); lastErr == nil {
											notified[b] = true
											break
										}
									}
									if lastErr != nil {
										log.Debugw("notifying hash space node", "storage", p.StorageID, "error", lastErr)
									}
								}
							}()
						}()
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
		}(ctx, nil, func(tx *harmonydb.Tx, _ harmonytask.TaskID) (bool, error) {
			return fn(tx)
		})
	}(ctx, func(tx *harmonydb.Tx) (bool, error) {
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
			if errors.Is(err, errNoPieceDrive) {
				for _, d := range drives {
					capacity, err := capacityOf(Drive{Root: d.Root})
					if err != nil {
						return false, err
					}
					if _, err := tx.Exec(`INSERT INTO hash_space_disk (storage_id, capacity, vacating) VALUES ($1, $2, TRUE)
						ON CONFLICT (storage_id) DO UPDATE SET capacity = EXCLUDED.capacity, vacating = TRUE, updated_at = NOW()`,
						d.StorageID, capacity); err != nil {
						return false, err
					}
				}
				return true, nil
			}
			if err != nil {
				return false, err
			}
			ids := make([]string, len(drives))
			for i, d := range drives {
				ids[i] = d.StorageID
				deny, err := deniesPiecePark(d.Root)
				if err != nil {
					return false, err
				}
				if _, err := tx.Exec(`INSERT INTO hash_space_disk (storage_id, capacity, vacating) VALUES ($1, $2, $3)
					ON CONFLICT (storage_id) DO UPDATE SET capacity = EXCLUDED.capacity, vacating = EXCLUDED.vacating, updated_at = NOW()`,
					d.StorageID, st.Disks[i], deny); err != nil {
					return false, err
				}
			}
			return true /* storeState */, func(tx *harmonydb.Tx, st hashspacesolver.State, ids []string) error {
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
			}(tx, st, ids)
		}

		var fresh []string
		for _, d := range drives {
			capacity, err := capacityOf(Drive{Root: d.Root})
			if err != nil {
				return false, err
			}
			deny, err := deniesPiecePark(d.Root)
			if err != nil {
				return false, err
			}
			if indexOf(known, d.StorageID) >= 0 {
				if _, err := tx.Exec(`UPDATE hash_space_disk SET capacity = $1, vacating = $2, updated_at = NOW() WHERE storage_id = $3`, capacity, deny, d.StorageID); err != nil {
					return false, err
				}
				continue
			}
			if _, err := tx.Exec(`INSERT INTO hash_space_disk (storage_id, capacity, vacating) VALUES ($1, $2, $3)
				ON CONFLICT (storage_id) DO NOTHING`, d.StorageID, capacity, deny); err != nil {
				return false, err
			}
			if !deny {
				fresh = append(fresh, d.StorageID)
			}
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
				_, folderUsed, err := accountedLayout(root, kind)
				if err != nil {
					return err
				}
				used = folderUsed
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
	for _, sp := range []*Space{c.open, c.acl} {
		if sp == nil {
			continue
		}
		for id, root := range c.roots {
			owned, moveSources, err := c.localIntervals(ctx, sp.kind, id)
			if err != nil {
				return err
			}
			if err := sp.SetIntervalsOn(root, ver, owned, moveSources); err != nil {
				return err
			}
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

// publishOne publishes both spaces' bytes and the disk's effective capacity:
// those bytes plus the filesystem's free space, capped by the configured
// capacity. Anything else on the filesystem (piece-park, sealing) shows up
// only as less free space.
func (c *Cluster) publishOne(ctx context.Context, storageID, root string) {
	if _, err := /* Cluster.publishDisk */ func(ctx context.Context, storageID, root string) (bool, error) {
		usedOpen, usedACL, err := /* Cluster.folderUsed */ func(root string) (openUsed, aclUsed int64, err error) {
			if c.open == nil {
				return 0, 0, xerrors.Errorf("open-pieces is not loaded")
			}
			openUsed, err = c.open.UsedOn(root)
			if err != nil {
				return 0, 0, err
			}
			if c.acl == nil {
				return openUsed, 0, nil
			}
			aclUsed, err = c.acl.UsedOn(root)
			return openUsed, aclUsed, err
		}(root)
		if err != nil {
			return false, err
		}
		used := usedOpen + usedACL
		for _, kind := range spaceKinds {
			n := usedOpen
			if kind == DIR_ACL {
				n = usedACL
			}
			if err := /* Cluster.publishRangeSizes */ func(ctx context.Context, storageID, root, kind string, used int64) error {
				owned, _, err := c.localIntervals(ctx, kind, storageID)
				if err != nil {
					return err
				}
				sizes, err := sizesForRanges(filepath.Join(root, kind), owned, used)
				if err != nil {
					return err
				}
				for i, r := range owned {
					end, err := decodeHash(r.End)
					if err != nil {
						return err
					}
					if _, err := c.db.Exec(ctx, `UPDATE hash_space_range SET size = $1
			WHERE space = $2 AND end_hash = $3 AND storage_id = $4`, sizes[i], kind, end, storageID); err != nil {
						return err
					}
				}
				return nil
			}(ctx, storageID, root, kind, n); err != nil {
				log.Warnw("publishing hash space range sizes", "storage", storageID, "space", kind, "error", err)
			}
		}
		var prev int64
		if err := c.db.QueryRow(ctx, `SELECT capacity FROM hash_space_disk WHERE storage_id = $1`, storageID).Scan(&prev); err != nil {
			prev = 0
		}
		capacity, err := /* effectiveCapacity */ func(root string, used int64) (int64, error) {
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
		}(root, used)
		if err != nil {
			log.Warnw("reading hash space capacity", "storage", storageID, "error", err)
			if _, err := c.db.Exec(ctx, `UPDATE hash_space_disk SET used_open = $1, used_acl = $2, vacating = $3, updated_at = NOW() WHERE storage_id = $4`, usedOpen, usedACL, diskVacate(root), storageID); err != nil {
				log.Warnw("publishing hash space used", "storage", storageID, "error", err)
			}
			return false, nil
		}
		vacating := diskVacate(root)
		if _, err := c.db.Exec(ctx, `UPDATE hash_space_disk SET used_open = $1, used_acl = $2, capacity = $3, vacating = $4, updated_at = NOW() WHERE storage_id = $5`,
			usedOpen, usedACL, capacity, vacating, storageID); err != nil {
			return false, err
		}
		// Other filesystem use shrinks the capacity the solver sees. When a large
		// chunk of that space comes back and some disk is over the limit, this
		// disk can take that overflow. Disks under the limit are not evened out.
		if vacating || capacity <= prev || overFill(used, capacity) || capacity-prev < ROOM_RETURN_MIN {
			return false, nil
		}
		over, err := /* Cluster.anyOverFill */ func(ctx context.Context) (bool, error) {
			var rows []struct {
				UsedOpen int64 `db:"used_open"`
				UsedACL  int64 `db:"used_acl"`
				Capacity int64 `db:"capacity"`
			}
			if err := c.db.Select(ctx, &rows, `SELECT used_open, used_acl, capacity FROM hash_space_disk`); err != nil {
				return false, err
			}
			for _, r := range rows {
				if overFill(r.UsedOpen+r.UsedACL, r.Capacity) {
					return true, nil
				}
			}
			return false, nil
		}(ctx)
		if err != nil {
			log.Warnw("reading hash space fill", "storage", storageID, "error", err)
			return false, nil
		}
		return over, nil
	}(ctx, storageID, root); err != nil {
		log.Warnw("publishing hash space used", "storage", storageID, "error", err)
	}
}

// publishDisk writes used and capacity. It reports whether the disk gained
// enough free space to take overflow, without starting that event.

// anyOverFill reports whether any disk is above its fill limit.

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
