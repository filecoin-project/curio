package hashfolder

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/hashspacesolver"
)

// ClusterSpace is one circle of the cluster map. Cluster.Public is open-pieces
// and Cluster.ACL is acl-pieces.
type ClusterSpace struct {
	c    *Cluster
	kind string
}

// Name is the folder for this space, open-pieces or acl-pieces.
func (s *ClusterSpace) Name() string {
	return s.kind
}

// HasLocal reports whether storageID is a hash-space root on this node.
func (s *ClusterSpace) HasLocal(storageID string) bool {
	return s.c.HasLocal(storageID)
}

// folder is the on-disk space. The caller has a Cluster whose spaces are loaded.
func (s *ClusterSpace) folder() (*Space, error) {
	sp := s.c.space(s.kind)
	if sp == nil {
		return nil, xerrors.Errorf("hash space %s is not loaded", s.kind)
	}
	return sp, nil
}

// HasFile reports whether this space holds pc on any place that can hold it.
func (s *ClusterSpace) HasFile(ctx context.Context, pc cid.Cid) (bool, error) {
	digest, err := CIDHash(pc)
	if err != nil {
		return false, err
	}
	places, err := s.places(ctx, digest)
	if err != nil {
		return false, err
	}
	hexHash := hex.EncodeToString(digest)
	for _, p := range places {
		ok, err := s.c.hasHash(ctx, p.StorageID, s.kind, hexHash)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// Target returns the disk a new file for digest belongs on: the destination
// of an in-flight move covering it, otherwise an owner of the range.
func (s *ClusterSpace) Target(ctx context.Context, digest []byte) (string, error) {
	resolve, err := s.Targets(ctx)
	if err != nil {
		return "", err
	}
	return resolve(digest)
}

// Targets snapshots this space and returns a resolver with the same rules as Target.
func (s *ClusterSpace) Targets(ctx context.Context) (func(digest []byte) (string, error), error) {
	var moveSources []moveSourceRow
	err := s.c.db.Select(ctx, &moveSources, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE space = $1`, s.kind)
	if err != nil {
		return nil, xerrors.Errorf("reading hash space moveSources: %w", err)
	}
	var rs []rangeRow
	err = s.c.db.Select(ctx, &rs, `SELECT end_hash, storage_id, problem FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, s.kind)
	if err != nil {
		return nil, xerrors.Errorf("reading hash space ranges: %w", err)
	}
	return func(digest []byte) (string, error) {
		for _, m := range moveSources {
			if hashspacesolver.Contains(m.StartHash, m.EndHash, digest) {
				return m.ToStorage, nil
			}
		}
		ends := onePerEnd(rs)
		for i, r := range ends {
			if !hashspacesolver.Contains(ends[(i-1+len(ends))%len(ends)].EndHash, r.EndHash, digest) {
				continue
			}
			return ownerForWrite(rowsAt(rs, r.EndHash)), nil
		}
		return "", xerrors.Errorf("no hash space range owns %x", digest)
	}, nil
}

// StatLocal returns the size of this space's file on a local disk.
func (s *ClusterSpace) StatLocal(storageID string, pc cid.Cid) (int64, bool, error) {
	sp, err := s.folder()
	if err != nil {
		return 0, false, err
	}
	root, err := s.c.rootOf(storageID)
	if err != nil {
		return 0, false, err
	}
	return sp.StatCIDOn(root, pc)
}

// OpenLocal opens this space's file on a local disk.
func (s *ClusterSpace) OpenLocal(storageID string, pc cid.Cid) (io.ReadCloser, error) {
	sp, err := s.folder()
	if err != nil {
		return nil, err
	}
	root, err := s.c.rootOf(storageID)
	if err != nil {
		return nil, err
	}
	return sp.OpenCIDOn(root, pc)
}

// OpenLocalAt opens bytes [offset, offset+size) of this space's file on a local disk.
func (s *ClusterSpace) OpenLocalAt(storageID string, pc cid.Cid, offset, size int64) (io.ReadCloser, error) {
	sp, err := s.folder()
	if err != nil {
		return nil, err
	}
	root, err := s.c.rootOf(storageID)
	if err != nil {
		return nil, err
	}
	return sp.OpenCIDAt(root, pc, offset, size)
}

// ReturnLocal moves this space's file back to dest. dest must not already exist.
func (s *ClusterSpace) ReturnLocal(storageID string, pc cid.Cid, dest string) error {
	sp, err := s.folder()
	if err != nil {
		return err
	}
	root, err := s.c.rootOf(storageID)
	if err != nil {
		return err
	}
	return sp.ReturnFileOn(root, pc, dest)
}

// AdoptLocal renames src into this space on a local disk.
func (s *ClusterSpace) AdoptLocal(storageID string, pc cid.Cid, src string) (int64, error) {
	sp, err := s.folder()
	if err != nil {
		return 0, err
	}
	root, err := s.c.rootOf(storageID)
	if err != nil {
		return 0, err
	}
	return sp.AdoptFileOn(root, pc, src)
}

// WriteLocal copies r into this space on a local disk. When r's length is known,
// an existing file shorter than it is replaced. Otherwise an existing file is kept.
func (s *ClusterSpace) WriteLocal(storageID string, pc cid.Cid, r io.Reader) (int64, error) {
	sp, err := s.folder()
	if err != nil {
		return 0, err
	}
	root, err := s.c.rootOf(storageID)
	if err != nil {
		return 0, err
	}
	var w io.WriteCloser
	if size, ok := readerSize(r); ok {
		hexHash, _, err := cidHashHex(pc)
		if err != nil {
			return 0, err
		}
		var existing int64
		w, existing, err = sp.ReplaceShorterHashOn(root, hexHash, size)
		if err != nil {
			return 0, err
		}
		if w == nil {
			return existing, nil
		}
	} else {
		w, err = sp.WriteCIDOn(root, pc)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				size, _, err := s.StatLocal(storageID, pc)
				return size, err
			}
			return 0, err
		}
	}
	n, err := io.CopyBuffer(w, r, make([]byte, 8<<20))
	if err != nil {
		if a, ok := w.(interface{ Abort() error }); ok {
			_ = a.Abort()
		}
		return 0, xerrors.Errorf("copying %s into %s: %w", pc, s.kind, err)
	}
	if err := w.Close(); err != nil {
		if errors.Is(err, os.ErrExist) {
			size, _, err := s.StatLocal(storageID, pc)
			return size, err
		}
		return 0, err
	}
	return n, nil
}

// DropLocal removes this space's file from a local disk. Missing is fine.
func (s *ClusterSpace) DropLocal(storageID string, pc cid.Cid) error {
	sp, err := s.folder()
	if err != nil {
		return err
	}
	root, err := s.c.rootOf(storageID)
	if err != nil {
		return err
	}
	if err := sp.DeleteCIDOn(root, pc); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// PutRemote streams r into this space on the node that holds storageID.
func (s *ClusterSpace) PutRemote(ctx context.Context, storageID string, pc cid.Cid, r io.Reader) (int64, bool, error) {
	if len(s.c.auth) == 0 {
		return 0, false, xerrors.Errorf("putting %s on %s: hash space client has no storage auth", pc, storageID)
	}
	if r == nil {
		return 0, false, xerrors.Errorf("putting %s on %s: empty body", pc, storageID)
	}
	hexHash, _, err := cidHashHex(pc)
	if err != nil {
		return 0, false, err
	}
	var prep func(*http.Request)
	if n, ok := readerSize(r); ok {
		prep = func(req *http.Request) { req.ContentLength = n }
	}
	var size int64
	var existed bool
	err = s.c.remoteHash(ctx, http.MethodPut, storageID, s.kind, hexHash, r, prep, func(resp *http.Response) (bool, error) {
		if resp.StatusCode != http.StatusNoContent {
			return false, xerrors.Errorf("PUT %s: %s", resp.Request.URL, resp.Status)
		}
		n, err := strconv.ParseInt(resp.Header.Get(headerPutSize), 10, 64)
		if err != nil {
			return false, xerrors.Errorf("PUT %s: bad size", resp.Request.URL)
		}
		size = n
		existed = resp.Header.Get(headerPutExisted) == "1"
		return false, nil
	})
	if err != nil {
		return 0, false, err
	}
	return size, existed, nil
}

// Drop removes pc from this space on storageID. A missing file is fine.
func (s *ClusterSpace) Drop(ctx context.Context, storageID string, pc cid.Cid) error {
	return s.deleteOn(ctx, storageID, pc)
}

// deleteOn removes pc from this space on storageID, locally or through that node's DELETE.
func (s *ClusterSpace) deleteOn(ctx context.Context, storageID string, pc cid.Cid) error {
	if s.c.HasLocal(storageID) {
		return s.DropLocal(storageID, pc)
	}
	return s.remote(ctx, http.MethodDelete, storageID, pc, nil, func(r *http.Response) (bool, error) {
		_ = r.Body.Close()
		switch r.StatusCode {
		case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
			return false, nil
		default:
			return false, xerrors.Errorf("DELETE %s: %s", r.Request.URL, r.Status)
		}
	})
}

// places returns the disks that may hold digest, local ones first.
func (s *ClusterSpace) places(ctx context.Context, digest []byte) ([]Location, error) {
	var moves []moveSourceRow
	if err := s.c.db.Select(ctx, &moves, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE space = $1`, s.kind); err != nil {
		return nil, xerrors.Errorf("reading hash space moveSources: %w", err)
	}
	var rs []rangeRow
	if err := s.c.db.Select(ctx, &rs, `SELECT end_hash, storage_id, size FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, s.kind); err != nil {
		return nil, xerrors.Errorf("reading hash space ranges: %w", err)
	}
	var misplaced []string
	if err := s.c.db.Select(ctx, &misplaced, `SELECT storage_id FROM hash_space_disk WHERE has_misplaced ORDER BY storage_id`); err != nil {
		return nil, xerrors.Errorf("reading hash space disks: %w", err)
	}
	return s.c.placesIn(rs, moves, misplaced, digest)
}

// DeleteCID removes pieceCID from this space on the disks that hold its hash.
// A missing file is fine. The lookup runs twice so a move that starts during
// the first pass is still cleared.
func (s *ClusterSpace) DeleteCID(ctx context.Context, pieceCID string) error {
	pc, err := cid.Parse(pieceCID)
	if err != nil {
		return xerrors.Errorf("parsing piece cid %s: %w", pieceCID, err)
	}
	digest, err := CIDHash(pc)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for pass := 0; pass < 2; pass++ {
		locs, err := s.places(ctx, digest)
		if errors.Is(err, errNoRange) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, loc := range locs {
			if _, ok := seen[loc.StorageID]; ok {
				continue
			}
			if err := s.deleteOn(ctx, loc.StorageID, pc); err != nil {
				return xerrors.Errorf("deleting %s from %s: %w", pieceCID, loc.StorageID, err)
			}
			seen[loc.StorageID] = struct{}{}
		}
	}
	for id := range seen {
		if err := s.c.MaybeRebalance(ctx, id); err != nil {
			log.Warnw("hash space rebalance", "storage", id, "error", err)
		}
	}
	return nil
}

// Locations lists where pieceCID can be in this space.
func (s *ClusterSpace) Locations(ctx context.Context, pieceCID string) ([]Location, error) {
	pc, err := cid.Parse(pieceCID)
	if err != nil {
		return nil, xerrors.Errorf("parsing piece cid %s: %w", pieceCID, err)
	}
	digest, err := CIDHash(pc)
	if err != nil {
		return nil, err
	}
	return s.places(ctx, digest)
}

// Open reads the whole file for pc from this space on storageID.
func (s *ClusterSpace) Open(ctx context.Context, storageID string, pc cid.Cid) (io.ReadCloser, error) {
	if s.c.HasLocal(storageID) {
		return s.OpenLocal(storageID, pc)
	}
	return s.remoteGet(ctx, storageID, pc, "")
}

// RemoteAt reads bytes [start, end) of this space's file on another node.
func (s *ClusterSpace) RemoteAt(ctx context.Context, storageID string, pc cid.Cid, start, end int64) (io.ReadCloser, error) {
	if end <= start {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	return s.remoteGet(ctx, storageID, pc, "bytes="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end-1, 10))
}

// remoteGet reads pc from this space on another node. byteRange is a Range header value, or empty.
func (s *ClusterSpace) remoteGet(ctx context.Context, storageID string, pc cid.Cid, byteRange string) (io.ReadCloser, error) {
	var resp *http.Response
	err := s.remote(ctx, http.MethodGet, storageID, pc, func(req *http.Request) {
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

// remote sends one request for this space to each URL of storageID until one answers.
// handle reports whether it kept the response body open.
func (s *ClusterSpace) remote(ctx context.Context, method, storageID string, pc cid.Cid, prep func(*http.Request), handle func(*http.Response) (bool, error)) error {
	hexHash, _, err := cidHashHex(pc)
	if err != nil {
		return err
	}
	return s.c.remoteHash(ctx, method, storageID, s.kind, hexHash, nil, prep, handle)
}
