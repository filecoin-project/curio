package hashspace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"iter"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

	for _, id := range arrived {
		if err := c.raise(ctx, id, EVENT_ARRIVE); err != nil {
			log.Errorw("hash space arrive", "storage", id, "error", err)
		}
	}
	c.noticeSpread(ctx)

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
	var lastOverlapCheck time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		c.dropDeadMoves(ctx)
		c.requeueOrphans(ctx)
		if c.open != nil {
			if err := c.refresh(ctx, false); err != nil {
				log.Warnw("refreshing hash space map", "error", err)
			}
			for id := range c.roots {
				if err := c.MaybeRebalance(ctx, id); err != nil {
					log.Warnw("hash space rebalance", "storage", id, "error", err)
				}
			}
		}
		if err := c.reevaluate(ctx); err != nil {
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

// LocalIDs returns the storage ids of hash-space roots on this node.
func (c *Cluster) LocalIDs() []string {
	ids := make([]string, 0, len(c.roots))
	for id := range c.roots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
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

func (c *Cluster) ownsRanges(ctx context.Context, storageID string) (bool, error) {
	var owns bool
	if err := c.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM hash_space_range WHERE storage_id = $1)`, storageID).Scan(&owns); err != nil {
		return false, xerrors.Errorf("reading ranges of %s: %w", storageID, err)
	}
	return owns, nil
}

func (c *Cluster) localUsed(root string) (int64, error) {
	var n int64
	for _, kind := range spaceKinds {
		sp := c.space(kind)
		if sp == nil {
			continue
		}
		u, err := sp.UsedOn(root)
		if err != nil {
			return 0, err
		}
		n += u
	}
	return n, nil
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
		ok, err := c.hasHash(ctx, p.StorageID, DIR_OPEN, hexHash)
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
		return nil, xerrors.Errorf("hash %x: %w", digest, errNoRange)
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

// Drop removes pc from storageID, locally or through that node's DELETE.
// A missing file is fine.
func (c *Cluster) Drop(ctx context.Context, storageID string, pc cid.Cid) error {
	return c.deleteOn(ctx, storageID, pc)
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

// PutRemote streams r to the node that holds storageID. That node writes the
// file locally, publishes its usage, and maybe starts a rebalance. The body
// is not buffered. A client with no storage auth does not send the request.
func (c *Cluster) PutRemote(ctx context.Context, storageID string, pc cid.Cid, r io.Reader) (int64, bool, error) {
	if len(c.auth) == 0 {
		return 0, false, xerrors.Errorf("putting %s on %s: hash space client has no storage auth", pc, storageID)
	}
	if r == nil {
		return 0, false, xerrors.Errorf("putting %s on %s: empty body", pc, storageID)
	}
	var size int64
	var existed bool
	err := c.remoteBody(ctx, http.MethodPut, storageID, pc, r, func(resp *http.Response) error {
		if resp.StatusCode != http.StatusNoContent {
			return xerrors.Errorf("PUT %s: %s", resp.Request.URL, resp.Status)
		}
		n, err := strconv.ParseInt(resp.Header.Get(headerPutSize), 10, 64)
		if err != nil {
			return xerrors.Errorf("PUT %s: bad size", resp.Request.URL)
		}
		size = n
		existed = resp.Header.Get(headerPutExisted) == "1"
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return size, existed, nil
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
	return c.remoteHash(ctx, method, storageID, DIR_OPEN, hexHash, nil, prep, handle)
}

// remoteBody sends one PUT of body. A seekable body is rewound for the next
// URL; otherwise the first attempt that starts sending is the only one.
func (c *Cluster) remoteBody(ctx context.Context, method, storageID string, pc cid.Cid, body io.Reader, handle func(*http.Response) error) error {
	hexHash, _, err := cidHashHex(pc)
	if err != nil {
		return err
	}
	return c.remoteHash(ctx, method, storageID, DIR_OPEN, hexHash, body, nil, func(r *http.Response) (bool, error) {
		err := handle(r)
		return false, err
	})
}

func (c *Cluster) remoteHash(ctx context.Context, method, storageID, kind, hexHash string, body io.Reader, prep func(*http.Request), handle func(*http.Response) (bool, error)) error {
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
	started := false
	for _, u := range strings.Split(urls, storageURLSeparator) {
		if u == "" {
			continue
		}
		if body != nil {
			if seeker, ok := body.(io.Seeker); ok {
				if _, err := seeker.Seek(0, io.SeekStart); err != nil {
					return err
				}
			} else if started {
				return lastErr
			}
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
		if body != nil {
			req.Body = io.NopCloser(body)
			req.ContentLength = -1
		}
		if prep != nil {
			prep(req)
		}
		resp, err := c.client.Do(req)
		if body != nil {
			started = true
		}
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

// DeleteCID removes pieceCID from the disks that hold its hash: the range
// owner, or both ends of a move that covers it. A missing file is fine.
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
		locs, err := c.places(ctx, digest)
		if errors.Is(err, errNoRange) {
			break
		}
		if err != nil {
			return err
		}
		for _, loc := range locs {
			if err := c.deleteOn(ctx, loc.StorageID, pc); err != nil {
				return xerrors.Errorf("deleting %s from %s: %w", pieceCID, loc.StorageID, err)
			}
			touched[loc.StorageID] = struct{}{}
		}
	}
	for id := range touched {
		if err := c.MaybeRebalance(ctx, id); err != nil {
			log.Warnw("hash space rebalance", "storage", id, "error", err)
		}
	}
	return nil
}

// MaybeRebalance starts a vacate or full rebalance for storageID when that
// disk is leaving or is above FILL_LIMIT_PERCENT, then notices fill spread.
func (c *Cluster) MaybeRebalance(ctx context.Context, storageID string) error {
	defer c.noticeSpread(ctx)
	if root, ok := c.roots[storageID]; ok && c.open != nil {
		c.publishOne(ctx, storageID, root)
		if diskVacate(root) {
			used, err := c.localUsed(root)
			if err != nil {
				return err
			}
			owns, err := c.ownsRanges(ctx, storageID)
			if err != nil {
				return err
			}
			if used > 0 || owns {
				return c.raise(ctx, storageID, EVENT_VACATE)
			}
			return nil
		}
	}
	var used, capacity int64
	err := c.db.QueryRow(ctx, `SELECT used_open + used_acl, capacity FROM hash_space_disk WHERE storage_id = $1`, storageID).Scan(&used, &capacity)
	if err != nil {
		return xerrors.Errorf("reading hash space disk %s: %w", storageID, err)
	}
	if !isOverFull(used, capacity) {
		return nil
	}
	return c.raise(ctx, storageID, EVENT_FULL)
}

func isOverFull(used, capacity int64) bool {
	if capacity <= 0 {
		return used > 0
	}
	limit := capacity/100*hashspacesolver.FILL_LIMIT_PERCENT + capacity%100*hashspacesolver.FILL_LIMIT_PERCENT/100
	return used > limit
}

// errNoRange means the hash is outside every published range.
var errNoRange = errors.New("no hash space range owns this hash")

// errMovesActive means a rebalance is already copying. The trigger is recorded
// and solved when the last of those moves finishes.
var errMovesActive = errors.New("hash space moves are active")

// raise is the event phase. With no move in flight it runs the solver and
// starts the copies. Otherwise it records the trigger, at most once per disk
// and kind. Without a task engine yet the trigger is only recorded; the
// refresh loop solves it once moves can be tasked.
func (c *Cluster) raise(ctx context.Context, storageID, kind string) error {
	c.dropDeadMoves(ctx)
	addMove := harmonytask.AdderFor(tasknames.HashSpaceMove)
	if addMove == nil {
		return c.queueEvent(ctx, storageID, kind)
	}
	busy, err := c.movesActive(ctx)
	if err != nil {
		return err
	}
	if busy {
		return c.queueEvent(ctx, storageID, kind)
	}
	err = c.casTaskTx(ctx, addMove, func(tx *harmonydb.Tx, id harmonytask.TaskID) (bool, error) {
		var active bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM hash_space_move_source)`).Scan(&active); err != nil {
			return false, err
		}
		if active {
			return false, errMovesActive
		}

		st, ids, err := loadClusterState(tx)
		if err != nil {
			return false, err
		}
		disk := indexOf(ids, storageID)
		if disk < 0 {
			return true, consumePending(tx, storageID, kind)
		}
		ev := hashspacesolver.Event{Disk: disk}
		switch kind {
		case EVENT_FULL:
			if !isOverFull(ownedBytes(st, disk), st.Disks[disk]) {
				return true, consumePending(tx, storageID, kind)
			}
			ev.Kind = hashspacesolver.EventFull
		case EVENT_ARRIVE:
			ev.Kind = hashspacesolver.EventArrive
		case EVENT_ABSORB:
			ev.Kind = hashspacesolver.EventAbsorb
		case EVENT_VACATE:
			ev.Kind = hashspacesolver.EventVacate
		case EVENT_BALANCE:
			ev.Kind = hashspacesolver.EventBalance
		default:
			return false, xerrors.Errorf("unknown hash space event %q", kind)
		}

		res, err := hashspacesolver.Solve(st, ev)
		if err != nil {
			log.Warnw("hash space solver", "storage", storageID, "event", kind, "error", err)
			return false, nil
		}
		if res.BytesMoved == 0 {
			if err := consumePending(tx, storageID, kind); err != nil {
				return false, err
			}
			if ev.Kind == hashspacesolver.EventFull {
				return true, nil
			}
			return true, storeState(tx, res.State, ids)
		}

		var moves []hashspacesolver.Transfer
		for _, t := range res.Diff {
			if t.Size <= 0 || t.From < 0 || t.From == t.To {
				continue
			}
			if t.Space < 0 || t.Space >= len(spaceKinds) {
				return false, xerrors.Errorf("move space %d", t.Space)
			}
			moves = append(moves, t)
		}
		if len(moves) == 0 {
			return true, consumePending(tx, storageID, kind)
		}
		if id == 0 {
			return false, harmonytask.ErrNeedTask
		}

		// One task copies onto one destination disk. The node that holds that
		// disk can run every row, so the move task never has to hand work off.
		byDest := map[int][]hashspacesolver.Transfer{}
		var destOrder []int
		for _, t := range moves {
			if _, ok := byDest[t.To]; !ok {
				destOrder = append(destOrder, t.To)
			}
			byDest[t.To] = append(byDest[t.To], t)
		}
		pick := destOrder[0]
		var best int64
		for _, t := range byDest[pick] {
			best += t.Size
		}
		for _, to := range destOrder[1:] {
			var n int64
			for _, t := range byDest[to] {
				n += t.Size
			}
			if n > best || (n == best && ids[to] < ids[pick]) {
				best = n
				pick = to
			}
		}

		var planned int
		for _, t := range byDest[pick] {
			n, err := tx.Exec(`INSERT INTO hash_space_move_source (space, start_hash, end_hash, from_storage, to_storage, size, task_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (space, start_hash, end_hash) DO NOTHING`,
				spaceKinds[t.Space], t.StartHash, t.EndHash, ids[t.From], ids[t.To], t.Size, id)
			if err != nil {
				return false, xerrors.Errorf("inserting hash space move source: %w", err)
			}
			planned += n
		}
		// Every insert conflicted with a move already in flight. Leave the
		// event queued; rolling back also drops the empty task.
		if planned == 0 {
			return false, nil
		}
		if len(byDest) > 1 {
			if _, err := tx.Exec(`INSERT INTO hash_space_pending_event (storage_id, event_kind) VALUES ($1, $2)
				ON CONFLICT (storage_id, event_kind) DO NOTHING`, storageID, kind); err != nil {
				return false, err
			}
		} else if err := consumePending(tx, storageID, kind); err != nil {
			return false, err
		}
		log.Infow("planned hash space rebalance", "storage", storageID, "event", kind, "to", ids[pick], "moves", planned, "bytes", best)
		return true, nil
	})
	if errors.Is(err, errMovesActive) {
		return c.queueEvent(ctx, storageID, kind)
	}
	return err
}

func (c *Cluster) queueEvent(ctx context.Context, storageID, kind string) error {
	_, err := c.db.Exec(ctx, `INSERT INTO hash_space_pending_event (storage_id, event_kind) VALUES ($1, $2)
		ON CONFLICT (storage_id, event_kind) DO NOTHING`, storageID, kind)
	return err
}

func (c *Cluster) movesActive(ctx context.Context) (bool, error) {
	var busy bool
	err := c.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM hash_space_move_source)`).Scan(&busy)
	return busy, err
}

// consumePending removes a queued event inside the transaction that handled it.
func consumePending(tx *harmonydb.Tx, storageID, kind string) error {
	_, err := tx.Exec(`DELETE FROM hash_space_pending_event WHERE storage_id = $1 AND event_kind = $2`, storageID, kind)
	return err
}

// dropDeadMoves removes move rows whose harmony task is gone. A failed task
// otherwise leaves the row behind and every rebalance treats the cluster as busy.
func (c *Cluster) dropDeadMoves(ctx context.Context) {
	if _, err := c.db.Exec(ctx, `DELETE FROM hash_space_move_source
		WHERE task_id NOT IN (SELECT id FROM harmony_task)`); err != nil {
		log.Warnw("dropping hash space moves whose task is gone", "error", err)
	}
}

// requeueOrphans points drop rows at a new task when theirs was deleted.
// The row is what keeps the piece from being queued again.
func (c *Cluster) requeueOrphans(ctx context.Context) {
	c.requeueDrop(ctx)
}

func (c *Cluster) requeueDrop(ctx context.Context) {
	add := harmonytask.AdderFor(tasknames.HashSpaceDrop)
	if add == nil {
		return
	}
	_, err := harmonytask.TxWithTask(ctx, c.db, add, func(tx *harmonydb.Tx, id harmonytask.TaskID) (bool, error) {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM hash_space_delete d WHERE d.task_id NOT IN (SELECT id FROM harmony_task)`).Scan(&n); err != nil {
			return false, err
		}
		if n == 0 {
			return false, nil
		}
		if id == 0 {
			return false, harmonytask.ErrNeedTask
		}
		_, err := tx.Exec(`UPDATE hash_space_delete SET task_id = $1 WHERE task_id NOT IN (SELECT id FROM harmony_task)`, id)
		return err == nil, err
	})
	if err != nil {
		log.Warnw("requeueing hash space work whose task is gone", "task", tasknames.HashSpaceDrop, "error", err)
	}
}

// reevaluate runs after the cluster is idle. Triggers recorded while moves
// were copying are checked against the current fill; ones that no longer
// apply are dropped, and the solver runs for the rest until a plan starts
// new moves or nothing valid is left.
func (c *Cluster) reevaluate(ctx context.Context) error {
	c.dropDeadMoves(ctx)
	tried := map[string]struct{}{}
	for {
		busy, err := c.movesActive(ctx)
		if err != nil {
			return err
		}
		if busy {
			return nil
		}
		var evs []struct {
			StorageID string    `db:"storage_id"`
			Kind      string    `db:"event_kind"`
			CreatedAt time.Time `db:"created_at"`
		}
		if err := c.db.Select(ctx, &evs, `SELECT storage_id, event_kind, created_at FROM hash_space_pending_event ORDER BY created_at, storage_id`); err != nil {
			return err
		}
		if len(evs) == 0 {
			return nil
		}
		fills, err := c.diskFills(ctx)
		if err != nil {
			return err
		}
		sort.SliceStable(evs, func(i, j int) bool {
			pi, pj := eventPriority(evs[i].Kind), eventPriority(evs[j].Kind)
			if pi != pj {
				return pi < pj
			}
			return evs[i].CreatedAt.Before(evs[j].CreatedAt)
		})
		var nextStorage, nextKind string
		found := false
		for _, e := range evs {
			key := e.StorageID + "\x00" + e.Kind
			if _, ok := tried[key]; ok {
				continue
			}
			ok, err := c.triggerStill(ctx, fills, e.StorageID, e.Kind)
			if err != nil {
				return err
			}
			if !ok {
				if _, err := c.db.Exec(ctx, `DELETE FROM hash_space_pending_event WHERE storage_id = $1 AND event_kind = $2`, e.StorageID, e.Kind); err != nil {
					return err
				}
				continue
			}
			nextStorage, nextKind = e.StorageID, e.Kind
			found = true
			break
		}
		if !found {
			return nil
		}
		tried[nextStorage+"\x00"+nextKind] = struct{}{}
		if err := c.raise(ctx, nextStorage, nextKind); err != nil {
			return err
		}
	}
}

func eventPriority(kind string) int {
	switch kind {
	case EVENT_VACATE:
		return 0
	case EVENT_FULL:
		return 1
	case EVENT_ARRIVE:
		return 2
	case EVENT_ABSORB:
		return 3
	case EVENT_BALANCE:
		return 4
	default:
		return 9
	}
}

type diskFill struct {
	StorageID string `db:"storage_id"`
	UsedOpen  int64  `db:"used_open"`
	UsedACL   int64  `db:"used_acl"`
	Capacity  int64  `db:"capacity"`
	Vacating  bool   `db:"vacating"`
}

func (c *Cluster) diskFills(ctx context.Context) ([]diskFill, error) {
	var rows []diskFill
	err := c.db.Select(ctx, &rows, `SELECT storage_id, used_open, used_acl, capacity, vacating FROM hash_space_disk`)
	return rows, err
}

// triggerStill reports whether a queued event still describes the cluster.
// Moves that finished while it waited may have removed the reason to run it.
func (c *Cluster) triggerStill(ctx context.Context, fills []diskFill, storageID, kind string) (bool, error) {
	var disk *diskFill
	for i := range fills {
		if fills[i].StorageID == storageID {
			disk = &fills[i]
			break
		}
	}
	if disk == nil {
		return false, nil
	}
	used := disk.UsedOpen + disk.UsedACL
	switch kind {
	case EVENT_VACATE:
		if !disk.Vacating {
			return false, nil
		}
		if used > 0 {
			return true, nil
		}
		return c.ownsRanges(ctx, storageID)
	case EVENT_FULL:
		return !disk.Vacating && isOverFull(used, disk.Capacity), nil
	case EVENT_ARRIVE:
		return !disk.Vacating, nil
	case EVENT_ABSORB:
		if disk.Vacating || isOverFull(used, disk.Capacity) {
			return false, nil
		}
		for _, o := range fills {
			if isOverFull(o.UsedOpen+o.UsedACL, o.Capacity) {
				return true, nil
			}
		}
		return false, nil
	case EVENT_BALANCE:
		if disk.Vacating || disk.Capacity <= 0 {
			return false, nil
		}
		for _, o := range fills {
			if o.StorageID == storageID || o.Vacating || o.Capacity <= 0 {
				continue
			}
			if hashspacesolver.BalanceBytes(o.UsedOpen+o.UsedACL, o.Capacity, used, disk.Capacity) > 0 {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, nil
	}
}

// MoveSource is an in-flight interval copy.
type MoveSource struct {
	ID          int64
	Space       string
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
		out[i] = &MoveSource{ID: m.ID, Space: m.Space, StartHash: m.StartHash, EndHash: m.EndHash, FromStorage: m.FromStorage, ToStorage: m.ToStorage}
	}
	return out, nil
}

// PendingCopy yields piece hashes still on the source in the move interval,
// one directory page at a time. The caller copies each hash and removes it
// from the source; ranging stops without holding the rest of the interval.
func (c *Cluster) PendingCopy(ctx context.Context, m *MoveSource) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		if !c.HasLocal(m.ToStorage) {
			yield("", xerrors.Errorf("move destination %s is not local", m.ToStorage))
			return
		}
		low, high := hexEncode(m.StartHash), hexEncode(m.EndHash)
		after := ""
		for {
			if err := ctx.Err(); err != nil {
				yield("", err)
				return
			}
			batch, err := c.listPieceHashes(ctx, m.FromStorage, m.Space, low, high, after, LIST_PAGE)
			if err != nil {
				yield("", err)
				return
			}
			if len(batch) == 0 {
				return
			}
			for _, h := range batch {
				after = h
				if !yield(h, nil) {
					return
				}
			}
			if len(batch) < LIST_PAGE {
				return
			}
		}
	}
}

// CopyOne copies one piece hash of the move source onto its local destination,
// then removes the source file. The move row still exists, so a delete during
// the copy still sees this disk. A source file that disappears during the copy
// is a delete: the destination copy is removed and the move continues. A
// destination file already the right size only needs the source removed.
func (c *Cluster) CopyOne(ctx context.Context, m *MoveSource, hexHash string) error {
	size, ok, err := c.hashSize(ctx, m.FromStorage, m.Space, hexHash)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	have, err := c.hasHashLocal(m.ToStorage, m.Space, hexHash)
	if err != nil {
		return err
	}
	if have {
		destSize, destOK, err := c.hashSize(ctx, m.ToStorage, m.Space, hexHash)
		if err != nil {
			return err
		}
		if destOK && destSize == size {
			return c.deleteHash(ctx, m.FromStorage, m.Space, hexHash)
		}
		if err := c.deleteHash(ctx, m.ToStorage, m.Space, hexHash); err != nil {
			return xerrors.Errorf("dropping short copy of %s: %w", hexHash, err)
		}
	}
	src, err := c.openHash(ctx, m.FromStorage, m.Space, hexHash)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return xerrors.Errorf("opening %s on %s: %w", hexHash, m.FromStorage, err)
	}
	n, err := c.writeHashLocal(m.ToStorage, m.Space, hexHash, src)
	_ = src.Close()
	if err != nil {
		return err
	}
	if n != size {
		if derr := c.deleteHash(ctx, m.ToStorage, m.Space, hexHash); derr != nil {
			return xerrors.Errorf("dropping short copy of %s (%d bytes, source %d): %w", hexHash, n, size, derr)
		}
		return xerrors.Errorf("copied %d bytes of %s, source is %d", n, hexHash, size)
	}
	ok, err = c.hasHash(ctx, m.FromStorage, m.Space, hexHash)
	if err != nil {
		return err
	}
	if !ok {
		if err := c.deleteHash(ctx, m.ToStorage, m.Space, hexHash); err != nil {
			return xerrors.Errorf("dropping %s deleted during copy: %w", hexHash, err)
		}
		return nil
	}
	return c.deleteHash(ctx, m.FromStorage, m.Space, hexHash)
}

// CompleteMoveSource hands the move interval to its destination once every
// file still on the source is also on the destination. CopyOne removed each
// source file after the destination had it. A file deleted from the source
// is not required on the destination.
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

	var last bool
	err = c.casTx(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var rs []rangeRow
		kind, err := parseSpaceKind(m.Space)
		if err != nil {
			return false, err
		}
		if err := tx.Select(&rs, `SELECT end_hash, storage_id, size FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, kind); err != nil {
			return false, err
		}
		// A misplacement fix moves pieces into an interval the destination
		// already owns; only the source copies need to go.
		if !intervalOwnedBy(rs, m.StartHash, m.EndHash, m.ToStorage) {
			next, err := transferRanges(rs, m.StartHash, m.EndHash, m.FromStorage, m.ToStorage)
			if err != nil {
				return false, xerrors.Errorf("move source %d: %w", m.ID, err)
			}
			if err := writeSpaceRanges(tx, kind, next); err != nil {
				return false, err
			}
		}
		if _, err := tx.Exec(`DELETE FROM hash_space_move_source WHERE id = $1`, m.ID); err != nil {
			return false, err
		}
		var left int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM hash_space_move_source`).Scan(&left); err != nil {
			return false, err
		}
		last = left == 0
		return true, nil
	})
	if err != nil {
		return err
	}

	// Publish the new sizes before re-evaluating, and hold new triggers until
	// the queued ones have had their turn. Completions of this plan serialize
	// on the map version, so only the transaction that leaves zero moves runs
	// the solver.
	var absorb []string
	for _, id := range []string{m.FromStorage, m.ToStorage} {
		if !c.HasLocal(id) {
			if err := c.accountStorage(ctx, id); err != nil {
				log.Warnw("publishing hash space sizes", "storage", id, "error", err)
			}
			continue
		}
		root, err := c.rootOf(id)
		if err != nil {
			log.Warnw("publishing hash space sizes", "storage", id, "error", err)
			continue
		}
		want, err := c.publishDisk(ctx, id, root)
		if err != nil {
			log.Warnw("publishing hash space sizes", "storage", id, "error", err)
			continue
		}
		if want {
			absorb = append(absorb, id)
		}
	}
	if last {
		if err := c.reevaluate(ctx); err != nil {
			return err
		}
	}
	for _, id := range absorb {
		if err := c.raise(ctx, id, EVENT_ABSORB); err != nil {
			log.Warnw("hash space absorb", "storage", id, "error", err)
		}
	}
	c.noticeSpread(ctx)
	return nil
}

// noticeSpread raises a balance when two disks' fill percentages differ by
// SPREAD_POINTS or more. The emptier disk receives half that gap from the
// fuller one. Vacating disks are left out of the pair.
func (c *Cluster) noticeSpread(ctx context.Context) {
	var rows []struct {
		StorageID string `db:"storage_id"`
		UsedOpen  int64  `db:"used_open"`
		UsedACL   int64  `db:"used_acl"`
		Capacity  int64  `db:"capacity"`
		Vacating  bool   `db:"vacating"`
	}
	if err := c.db.Select(ctx, &rows, `SELECT storage_id, used_open, used_acl, capacity, vacating FROM hash_space_disk`); err != nil {
		log.Warnw("reading hash space fill spread", "error", err)
		return
	}
	var dest string
	var best int64
	for i, a := range rows {
		if a.Vacating || a.Capacity <= 0 {
			continue
		}
		usedA := a.UsedOpen + a.UsedACL
		for _, b := range rows[i+1:] {
			if b.Vacating || b.Capacity <= 0 {
				continue
			}
			usedB := b.UsedOpen + b.UsedACL
			if n := hashspacesolver.BalanceBytes(usedA, a.Capacity, usedB, b.Capacity); n > best {
				best = n
				dest = b.StorageID
			}
			if n := hashspacesolver.BalanceBytes(usedB, b.Capacity, usedA, a.Capacity); n > best {
				best = n
				dest = a.StorageID
			}
		}
	}
	if best <= 0 {
		return
	}
	if err := c.raise(ctx, dest, EVENT_BALANCE); err != nil {
		log.Warnw("hash space balance", "storage", dest, "error", err)
	}
}

// sourceMissing reports whether the source still has a piece hash the
// destination does not.
func (c *Cluster) sourceMissing(ctx context.Context, m *MoveSource) (bool, error) {
	low, high := hexEncode(m.StartHash), hexEncode(m.EndHash)
	after := ""
	for {
		batch, err := c.listPieceHashes(ctx, m.FromStorage, m.Space, low, high, after, LIST_PAGE)
		if err != nil {
			return false, err
		}
		if len(batch) == 0 {
			return false, nil
		}
		for _, h := range batch {
			after = h
			ok, err := c.hasHashLocal(m.ToStorage, m.Space, h)
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

func (c *Cluster) listPieceHashes(ctx context.Context, storageID, kind, low, high, after string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []string
	for len(out) < limit {
		batch, err := c.listHashes(ctx, storageID, kind, low, high, after, limit)
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

func (c *Cluster) listHashes(ctx context.Context, storageID, kind, low, high, after string, limit int) ([]string, error) {
	kind, err := parseSpaceKind(kind)
	if err != nil {
		return nil, err
	}
	if c.HasLocal(storageID) {
		root, err := c.rootOf(storageID)
		if err != nil {
			return nil, err
		}
		hashes, err := fs2.ListHashesInterval(filepath.Join(root, kind), low, high, after, limit)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, xerrors.Errorf("listing %s: %w", storageID, err)
		}
		return hashes, nil
	}
	return c.remoteList(ctx, storageID, kind, low, high, after, limit)
}

func isPieceHash(h string) bool {
	if len(h) != HASH_BYTES*2 {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil
}

func (c *Cluster) hasHash(ctx context.Context, storageID, kind, hexHash string) (bool, error) {
	if c.HasLocal(storageID) {
		return c.hasHashLocal(storageID, kind, hexHash)
	}
	var found bool
	err := c.remoteHash(ctx, http.MethodHead, storageID, kind, hexHash, nil, nil, func(r *http.Response) (bool, error) {
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

// hashSize is the byte length of the open-pieces file, and whether it exists.
func (c *Cluster) hashSize(ctx context.Context, storageID, kind, hexHash string) (int64, bool, error) {
	kind, err := parseSpaceKind(kind)
	if err != nil {
		return 0, false, err
	}
	sp := c.space(kind)
	if sp == nil {
		return 0, false, xerrors.Errorf("hash space %s is not loaded", kind)
	}
	if c.HasLocal(storageID) {
		root, err := c.rootOf(storageID)
		if err != nil {
			return 0, false, err
		}
		path, err := sp.hashPathOn(root, hexHash)
		if err != nil {
			return 0, false, err
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return 0, false, nil
			}
			return 0, false, err
		}
		if !info.Mode().IsRegular() {
			return 0, false, xerrors.Errorf("%s is not a regular file", path)
		}
		return info.Size(), true, nil
	}
	var n int64
	var found bool
	err = c.remoteHash(ctx, http.MethodHead, storageID, kind, hexHash, nil, nil, func(r *http.Response) (bool, error) {
		switch r.StatusCode {
		case http.StatusOK:
			if r.ContentLength < 0 {
				return false, xerrors.Errorf("HEAD %s: missing content length", r.Request.URL)
			}
			n = r.ContentLength
			found = true
			return false, nil
		case http.StatusNotFound:
			return false, nil
		default:
			return false, xerrors.Errorf("HEAD %s: %s", r.Request.URL, r.Status)
		}
	})
	return n, found, err
}

func (c *Cluster) hasHashLocal(storageID, kind, hexHash string) (bool, error) {
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
}

func (c *Cluster) openHash(ctx context.Context, storageID, kind, hexHash string) (io.ReadCloser, error) {
	kind, err := parseSpaceKind(kind)
	if err != nil {
		return nil, err
	}
	sp := c.space(kind)
	if sp == nil {
		return nil, xerrors.Errorf("hash space %s is not loaded", kind)
	}
	if c.HasLocal(storageID) {
		root, err := c.rootOf(storageID)
		if err != nil {
			return nil, err
		}
		return sp.openHashOn(root, hexHash)
	}
	var resp *http.Response
	err = c.remoteHash(ctx, http.MethodGet, storageID, kind, hexHash, nil, nil, func(r *http.Response) (bool, error) {
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

func (c *Cluster) writeHashLocal(storageID, kind, hexHash string, r io.Reader) (int64, error) {
	kind, err := parseSpaceKind(kind)
	if err != nil {
		return 0, err
	}
	sp := c.space(kind)
	if sp == nil {
		return 0, xerrors.Errorf("hash space %s is not loaded", kind)
	}
	root, err := c.rootOf(storageID)
	if err != nil {
		return 0, err
	}
	w, err := sp.WriteHashOn(root, hexHash)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			ok, serr := c.hasHashLocal(storageID, kind, hexHash)
			if serr != nil {
				return 0, serr
			}
			if !ok {
				return 0, os.ErrNotExist
			}
			path, serr := sp.hashPathOn(root, hexHash)
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
		return 0, xerrors.Errorf("copying %s into %s: %w", hexHash, kind, err)
	}
	if err := w.Close(); err != nil {
		if errors.Is(err, os.ErrExist) {
			return n, nil
		}
		return 0, err
	}
	return n, nil
}

func (c *Cluster) deleteHash(ctx context.Context, storageID, kind, hexHash string) error {
	kind, err := parseSpaceKind(kind)
	if err != nil {
		return err
	}
	sp := c.space(kind)
	if sp == nil {
		return xerrors.Errorf("hash space %s is not loaded", kind)
	}
	if c.HasLocal(storageID) {
		root, err := c.rootOf(storageID)
		if err != nil {
			return err
		}
		if err := sp.deleteHashOn(root, hexHash); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return c.remoteHash(ctx, http.MethodDelete, storageID, kind, hexHash, nil, nil, func(r *http.Response) (bool, error) {
		_ = r.Body.Close()
		switch r.StatusCode {
		case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
			return false, nil
		default:
			return false, xerrors.Errorf("DELETE %s: %s", r.Request.URL, r.Status)
		}
	})
}

func (c *Cluster) remoteList(ctx context.Context, storageID, kind, low, high, after string, limit int) ([]string, error) {
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
	err := c.remoteHash(ctx, http.MethodGet, storageID, kind, "list"+q, nil, nil, func(r *http.Response) (bool, error) {
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
			return true, storeState(tx, st, ids)
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
	absorb, err := c.publishDisk(ctx, storageID, root)
	if err != nil {
		log.Warnw("publishing hash space used", "storage", storageID, "error", err)
		return
	}
	if !absorb {
		return
	}
	if err := c.raise(ctx, storageID, EVENT_ABSORB); err != nil {
		log.Warnw("hash space absorb", "storage", storageID, "error", err)
	}
}

// publishDisk writes used and capacity. It reports whether the disk gained
// enough free space to take overflow, without starting that event.
func (c *Cluster) publishDisk(ctx context.Context, storageID, root string) (bool, error) {
	usedOpen, usedACL, err := c.folderUsed(root)
	if err != nil {
		return false, err
	}
	used := usedOpen + usedACL
	for _, kind := range spaceKinds {
		n := usedOpen
		if kind == DIR_ACL {
			n = usedACL
		}
		if err := c.publishRangeSizes(ctx, storageID, root, kind, n); err != nil {
			log.Warnw("publishing hash space range sizes", "storage", storageID, "space", kind, "error", err)
		}
	}
	var prev int64
	if err := c.db.QueryRow(ctx, `SELECT capacity FROM hash_space_disk WHERE storage_id = $1`, storageID).Scan(&prev); err != nil {
		prev = 0
	}
	capacity, err := effectiveCapacity(root, used)
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
	if vacating || capacity <= prev || isOverFull(used, capacity) || capacity-prev < ROOM_RETURN_MIN {
		return false, nil
	}
	over, err := c.anyOverFill(ctx)
	if err != nil {
		log.Warnw("reading hash space fill", "storage", storageID, "error", err)
		return false, nil
	}
	return over, nil
}

// anyOverFill reports whether any disk is above its fill limit.
func (c *Cluster) anyOverFill(ctx context.Context) (bool, error) {
	var rows []struct {
		UsedOpen int64 `db:"used_open"`
		UsedACL  int64 `db:"used_acl"`
		Capacity int64 `db:"capacity"`
	}
	if err := c.db.Select(ctx, &rows, `SELECT used_open, used_acl, capacity FROM hash_space_disk`); err != nil {
		return false, err
	}
	for _, r := range rows {
		if isOverFull(r.UsedOpen+r.UsedACL, r.Capacity) {
			return true, nil
		}
	}
	return false, nil
}

func (c *Cluster) folderUsed(root string) (openUsed, aclUsed int64, err error) {
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
}

func (c *Cluster) publishRangeSizes(ctx context.Context, storageID, root, kind string, used int64) error {
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

// loadClusterState builds the solver state from the cluster map. Disks are
// ordered by storage id. Each space's range sizes are the totals the owning
// node published from that directory.
func loadClusterState(tx *harmonydb.Tx) (hashspacesolver.State, []string, error) {
	var disks []struct {
		StorageID string `db:"storage_id"`
		Capacity  int64  `db:"capacity"`
		Vacating  bool   `db:"vacating"`
	}
	if err := tx.Select(&disks, `SELECT storage_id, capacity, vacating FROM hash_space_disk ORDER BY storage_id`); err != nil {
		return hashspacesolver.State{}, nil, err
	}
	ids := make([]string, len(disks))
	st := hashspacesolver.State{
		Disks:    make([]int64, len(disks)),
		Vacating: make([]bool, len(disks)),
	}
	for i, d := range disks {
		ids[i] = d.StorageID
		st.Disks[i] = d.Capacity
		st.Vacating[i] = d.Vacating
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
			sp.Ranges[i] = hashspacesolver.Range{EndHash: r.EndHash, Size: r.Size}
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
