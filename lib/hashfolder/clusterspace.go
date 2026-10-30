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

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/hashspacesolver"
)

// rangeLayout is one circle's ranges and moves, plus disks that may still
// hold misplaced files of either space.
type rangeLayout struct {
	ranges    []rangeRow
	moves     []moveSourceRow
	misplaced []string
}

// peerFiles reaches a hash on a disk this node does not mount.
type peerFiles struct {
	do func(ctx context.Context, method, storageID, hexHash string, body io.Reader, prep func(*http.Request), handle func(*http.Response) (bool, error)) error
}

// ClusterSpace is one circle of the cluster map. Cluster.Public is open-pieces
// and Cluster.ACL is acl-pieces.
type ClusterSpace struct {
	kind           string
	space          *Space
	db             *harmonydb.DB
	roots          map[string]string
	layout         func() rangeLayout
	peer           peerFiles
	maybeRebalance func(context.Context, string) error
}

// Name is the folder for this space, open-pieces or acl-pieces.
func (s *ClusterSpace) Name() string {
	return s.kind
}

// HasLocal reports whether storageID is a hash-space root on this node.
func (s *ClusterSpace) HasLocal(storageID string) bool {
	_, ok := s.roots[storageID]
	return ok
}

// rootOf is the local path for storageID. It returns an error when storageID
// is not in roots.
func (s *ClusterSpace) rootOf(storageID string) (string, error) {
	root, ok := s.roots[storageID]
	if !ok {
		return "", xerrors.Errorf("storage %s is not a local hash space root", storageID)
	}
	return root, nil
}

// folder is the on-disk space. The caller has loaded this ClusterSpace's space.
func (s *ClusterSpace) folder() (*Space, error) {
	if s.space == nil {
		return nil, xerrors.Errorf("hash space %s is not loaded", s.kind)
	}
	return s.space, nil
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
		ok, err := s.hasHash(ctx, p.StorageID, hexHash)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// hasHash reports whether storageID holds this space's file named hexHash,
// locally or through that node's HEAD.
func (s *ClusterSpace) hasHash(ctx context.Context, storageID, hexHash string) (bool, error) {
	if s.HasLocal(storageID) {
		return s.hasHashLocal(storageID, hexHash)
	}
	var found bool
	err := s.peer.do(ctx, http.MethodHead, storageID, hexHash, nil, nil, func(r *http.Response) (bool, error) {
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

// hasHashLocal reports whether local storageID holds this space's file named
// hexHash. The caller has established that storageID is local.
func (s *ClusterSpace) hasHashLocal(storageID, hexHash string) (bool, error) {
	space, err := s.folder()
	if err != nil {
		return false, err
	}
	root, err := s.rootOf(storageID)
	if err != nil {
		return false, err
	}
	path, err := space.hashPathOn(root, hexHash)
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
	err := s.db.Select(ctx, &moveSources, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE space = $1`, s.kind)
	if err != nil {
		return nil, xerrors.Errorf("reading hash space moveSources: %w", err)
	}
	var rs []rangeRow
	err = s.db.Select(ctx, &rs, `SELECT end_hash, storage_id, problem FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, s.kind)
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
	space, err := s.folder()
	if err != nil {
		return 0, false, err
	}
	root, err := s.rootOf(storageID)
	if err != nil {
		return 0, false, err
	}
	return space.StatCIDOn(root, pc)
}

// OpenLocal opens this space's file on a local disk.
func (s *ClusterSpace) OpenLocal(storageID string, pc cid.Cid) (io.ReadCloser, error) {
	space, err := s.folder()
	if err != nil {
		return nil, err
	}
	root, err := s.rootOf(storageID)
	if err != nil {
		return nil, err
	}
	return space.OpenCIDOn(root, pc)
}

// OpenLocalAt opens bytes [offset, offset+size) of this space's file on a local disk.
func (s *ClusterSpace) OpenLocalAt(storageID string, pc cid.Cid, offset, size int64) (io.ReadCloser, error) {
	space, err := s.folder()
	if err != nil {
		return nil, err
	}
	root, err := s.rootOf(storageID)
	if err != nil {
		return nil, err
	}
	return space.OpenCIDAt(root, pc, offset, size)
}

// ReturnLocal moves this space's file back to dest. dest must not already exist.
func (s *ClusterSpace) ReturnLocal(storageID string, pc cid.Cid, dest string) error {
	space, err := s.folder()
	if err != nil {
		return err
	}
	root, err := s.rootOf(storageID)
	if err != nil {
		return err
	}
	return space.ReturnFileOn(root, pc, dest)
}

// AdoptLocal renames src into this space on a local disk.
func (s *ClusterSpace) AdoptLocal(storageID string, pc cid.Cid, src string) (int64, error) {
	space, err := s.folder()
	if err != nil {
		return 0, err
	}
	root, err := s.rootOf(storageID)
	if err != nil {
		return 0, err
	}
	return space.AdoptFileOn(root, pc, src)
}

// WriteLocal copies r into this space on a local disk. When r's length is known,
// an existing file shorter than it is replaced. Otherwise an existing file is kept.
func (s *ClusterSpace) WriteLocal(storageID string, pc cid.Cid, r io.Reader) (int64, error) {
	space, err := s.folder()
	if err != nil {
		return 0, err
	}
	root, err := s.rootOf(storageID)
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
		w, existing, err = space.ReplaceShorterHashOn(root, hexHash, size)
		if err != nil {
			return 0, err
		}
		if w == nil {
			return existing, nil
		}
	} else {
		w, err = space.WriteCIDOn(root, pc)
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
	space, err := s.folder()
	if err != nil {
		return err
	}
	root, err := s.rootOf(storageID)
	if err != nil {
		return err
	}
	if err := space.DeleteCIDOn(root, pc); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// PutRemote streams r into this space on the node that holds storageID.
func (s *ClusterSpace) PutRemote(ctx context.Context, storageID string, pc cid.Cid, r io.Reader) (int64, bool, error) {
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
	err = s.peer.do(ctx, http.MethodPut, storageID, hexHash, r, prep, func(resp *http.Response) (bool, error) {
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
	if s.HasLocal(storageID) {
		return s.DropLocal(storageID, pc)
	}
	hexHash, _, err := cidHashHex(pc)
	if err != nil {
		return err
	}
	return s.peer.do(ctx, http.MethodDelete, storageID, hexHash, nil, nil, func(r *http.Response) (bool, error) {
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
	if err := s.db.Select(ctx, &moves, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
		FROM hash_space_move_source WHERE space = $1`, s.kind); err != nil {
		return nil, xerrors.Errorf("reading hash space moveSources: %w", err)
	}
	var rs []rangeRow
	if err := s.db.Select(ctx, &rs, `SELECT end_hash, storage_id, size FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, s.kind); err != nil {
		return nil, xerrors.Errorf("reading hash space ranges: %w", err)
	}
	var misplaced []string
	if err := s.db.Select(ctx, &misplaced, `SELECT storage_id FROM hash_space_disk WHERE has_misplaced ORDER BY storage_id`); err != nil {
		return nil, xerrors.Errorf("reading hash space disks: %w", err)
	}
	return placesIn(s.HasLocal, rs, moves, misplaced, digest)
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
		if s.maybeRebalance == nil {
			continue
		}
		if err := s.maybeRebalance(ctx, id); err != nil {
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
	if s.HasLocal(storageID) {
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
	hexHash, _, err := cidHashHex(pc)
	if err != nil {
		return nil, err
	}
	var resp *http.Response
	err = s.peer.do(ctx, http.MethodGet, storageID, hexHash, nil, func(req *http.Request) {
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
