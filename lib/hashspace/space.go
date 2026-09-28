package hashspace

import (
	"context"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/hashspacesolver"
)

// Space is one hash namespace across local storage roots.
type Space struct {
	kind   string
	disks  []*disk
	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type disk struct {
	root        string
	tracker     *sizeTracker
	version     int64
	intervals   []hashInterval
	moveSources []hashInterval
}

type hashInterval struct {
	start []byte
	end   []byte
}

// Load reads layout.json for kind on each root, restores used, then adds
// only files whose mtime is newer than layout.json. kind is DIR_OPEN or DIR_ACL.
func Load(kind string, roots []string) (*Space, error) {
	if kind != DIR_OPEN && kind != DIR_ACL {
		return nil, xerrors.Errorf("unknown hash space %q", kind)
	}
	if len(roots) == 0 {
		return nil, xerrors.Errorf("no storage roots")
	}
	s := &Space{kind: kind, disks: make([]*disk, 0, len(roots))}
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if root == "" {
			return nil, xerrors.Errorf("empty storage root")
		}
		if _, ok := seen[root]; ok {
			return nil, xerrors.Errorf("duplicate storage root %s", root)
		}
		seen[root] = struct{}{}
		d, err := loadDisk(kind, root)
		if err != nil {
			return nil, err
		}
		s.disks = append(s.disks, d)
	}
	s.startFlush()
	return s, nil
}

func loadDisk(kind, root string) (*disk, error) {
	removeLayoutTemp(root, kind)
	path := filepath.Join(root, kind, layoutFile)
	layout, err := readLayout(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	d := &disk{
		root:    root,
		tracker: &sizeTracker{},
		version: layout.Version,
	}
	d.tracker.Set(layout.Used)
	if d.intervals, err = decodeIntervals(layout.Ranges); err != nil {
		return nil, xerrors.Errorf("%s ranges: %w", path, err)
	}
	if d.moveSources, err = decodeIntervals(layout.MoveSources); err != nil {
		return nil, xerrors.Errorf("%s move sources: %w", path, err)
	}
	if err := d.catchUp(kind, info.ModTime()); err != nil {
		return nil, err
	}
	return d, nil
}

// Used is the in-memory byte counter for this space, summed across roots.
func (s *Space) Used() int64 {
	var n int64
	for _, d := range s.disks {
		n += d.tracker.Used()
	}
	return n
}

// Flush writes each folder's used counter into layout.json.
func (s *Space) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

func (s *Space) flushLocked() error {
	now := time.Now().UTC().Truncate(time.Second)
	var first error
	for _, d := range s.disks {
		err := writeLayout(d.root, s.kind, Layout{
			Version:     d.version,
			Used:        d.tracker.Used(),
			CommittedAt: now,
			Split:       SPLIT,
			Ranges:      encodeIntervals(d.intervals),
			MoveSources: encodeIntervals(d.moveSources),
		})
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Close stops the periodic flush and writes layout.json once more.
func (s *Space) Close() error {
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
	}
	return s.Flush()
}

func (s *Space) startFlush() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(FLUSH_INTERVAL)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.Flush()
			}
		}
	}()
}

// WriteCID opens a new file for a piece CID that is not already stored.
// The file is written to the root whose ranges contain the CID hash.
// Close adds the written byte count. A second Close does not add again.
// Abort drops the temp sibling and leaves the counter unchanged.
// If the CID file already exists, WriteCID returns os.ErrExist and does
// not change the counter.
func (s *Space) WriteCID(c cid.Cid) (io.WriteCloser, error) {
	hexHash, digest, err := cidHashHex(c)
	if err != nil {
		return nil, err
	}
	disk, ok := s.locate(digest)
	if !ok {
		return nil, xerrors.Errorf("cid hash is not owned by any local range")
	}
	return s.writeOn(disk, hexHash)
}

// WriteCIDOn is WriteCID on the named root, whether or not that root owns
// the hash. Cluster placement and rebalance pick the root.
func (s *Space) WriteCIDOn(root string, c cid.Cid) (io.WriteCloser, error) {
	hexHash, _, err := cidHashHex(c)
	if err != nil {
		return nil, err
	}
	return s.WriteHashOn(root, hexHash)
}

// WriteHashOn opens a new file named by the piece hash on root.
func (s *Space) WriteHashOn(root, hexHash string) (io.WriteCloser, error) {
	d, err := s.diskOn(root)
	if err != nil {
		return nil, err
	}
	return s.writeOn(d, hexHash)
}

func (s *Space) writeOn(disk *disk, hexHash string) (io.WriteCloser, error) {
	final, err := piecePath(disk.root, s.kind, hexHash)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(final); err == nil {
		return nil, os.ErrExist
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return nil, xerrors.Errorf("creating shard for %s: %w", hexHash, err)
	}
	tmp := filepath.Join(filepath.Dir(final), "."+filepath.Base(final)+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, os.ErrExist
		}
		return nil, err
	}
	return &cidWriter{space: s, disk: disk, f: f, tmp: tmp, final: final}, nil
}

// DeleteCID removes the CID file and subtracts its size. A missing file
// returns os.ErrNotExist and does not change the counter.
func (s *Space) DeleteCID(c cid.Cid) error {
	hexHash, digest, err := cidHashHex(c)
	if err != nil {
		return err
	}
	owner, ok := s.locate(digest)
	if !ok {
		return xerrors.Errorf("cid hash is not owned by any local range")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err = removePiece(owner, s.kind, hexHash)
	if err == nil || !os.IsNotExist(err) {
		return err
	}
	for _, d := range s.disks {
		if d == owner {
			continue
		}
		err = removePiece(d, s.kind, hexHash)
		if os.IsNotExist(err) {
			continue
		}
		return err
	}
	return os.ErrNotExist
}

func removePiece(d *disk, kind, hexHash string) error {
	path, err := piecePath(d.root, kind, hexHash)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return xerrors.Errorf("%s is not a regular file", path)
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return os.ErrNotExist
		}
		return err
	}
	d.tracker.Sub(info.Size())
	return nil
}

// ReadCIDFileFrom opens the CID file on the root that owns its hash.
// Other local roots are probed when the file is not on that root.
func (s *Space) ReadCIDFileFrom(c cid.Cid) (ReadSeekFile, error) {
	hexHash, digest, err := cidHashHex(c)
	if err != nil {
		return nil, err
	}
	owner, ok := s.locate(digest)
	if !ok {
		return nil, xerrors.Errorf("cid hash is not owned by any local range")
	}
	order := make([]*disk, 0, len(s.disks))
	order = append(order, owner)
	for _, d := range s.disks {
		if d != owner {
			order = append(order, d)
		}
	}
	for _, d := range order {
		path, err := piecePath(d.root, s.kind, hexHash)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if os.IsNotExist(err) {
			continue
		}
		return nil, err
	}
	return nil, os.ErrNotExist
}

// locate returns the root that owns digest, or a root it is being moved to when no
// local root owns it.
func (s *Space) locate(digest []byte) (*disk, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.disks {
		for _, iv := range d.intervals {
			if hashspacesolver.Contains(iv.start, iv.end, digest) {
				return d, true
			}
		}
	}
	for _, d := range s.disks {
		for _, iv := range d.moveSources {
			if hashspacesolver.Contains(iv.start, iv.end, digest) {
				return d, true
			}
		}
	}
	return nil, false
}

func (s *Space) diskOn(root string) (*disk, error) {
	for _, d := range s.disks {
		if d.root == root {
			return d, nil
		}
	}
	return nil, xerrors.Errorf("%s is not a root of this space", root)
}

// AdoptFileOn renames src onto the CID path on root without copying bytes and
// adds its size to that root's counter. It never replaces an existing CID
// file. A cross-filesystem rename returns ErrCrossDevice with src untouched.
func (s *Space) AdoptFileOn(root string, c cid.Cid, src string) (int64, error) {
	hexHash, _, err := cidHashHex(c)
	if err != nil {
		return 0, err
	}
	d, err := s.diskOn(root)
	if err != nil {
		return 0, err
	}
	final, err := piecePath(d.root, s.kind, hexHash)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(src)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, xerrors.Errorf("%s is not a regular file", src)
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return 0, xerrors.Errorf("creating shard for %s: %w", hexHash, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := renameNoReplace(src, final); err != nil {
		if isCrossDevice(err) {
			return 0, ErrCrossDevice
		}
		return 0, err
	}
	d.tracker.Add(info.Size())
	return info.Size(), nil
}

// DeleteCIDOn removes the CID file from root only. A missing file returns
// os.ErrNotExist and does not change the counter.
func (s *Space) DeleteCIDOn(root string, c cid.Cid) error {
	hexHash, _, err := cidHashHex(c)
	if err != nil {
		return err
	}
	return s.deleteHashOn(root, hexHash)
}

func (s *Space) deleteHashOn(root, hexHash string) error {
	d, err := s.diskOn(root)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return removePiece(d, s.kind, hexHash)
}

// OpenCIDOn opens the CID file on root only.
func (s *Space) OpenCIDOn(root string, c cid.Cid) (io.ReadCloser, error) {
	return s.openCIDFileOn(root, c)
}

// OpenCIDAt opens bytes [offset, offset+size) of the CID file on root only.
func (s *Space) OpenCIDAt(root string, c cid.Cid, offset, size int64) (io.ReadCloser, error) {
	f, err := s.openCIDFileOn(root, c)
	if err != nil {
		return nil, err
	}
	return &sectionCloser{Reader: io.NewSectionReader(f, offset, size), c: f}, nil
}

// StatCIDOn returns the size of the CID file on root, and whether it exists.
func (s *Space) StatCIDOn(root string, c cid.Cid) (int64, bool, error) {
	path, err := s.cidPathOn(root, c)
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
	return info.Size(), true, nil
}

func (s *Space) openCIDFileOn(root string, c cid.Cid) (*os.File, error) {
	path, err := s.cidPathOn(root, c)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *Space) cidPathOn(root string, c cid.Cid) (string, error) {
	hexHash, _, err := cidHashHex(c)
	if err != nil {
		return "", err
	}
	return s.hashPathOn(root, hexHash)
}

func (s *Space) openHashOn(root, hexHash string) (*os.File, error) {
	path, err := s.hashPathOn(root, hexHash)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *Space) hashPathOn(root, hexHash string) (string, error) {
	d, err := s.diskOn(root)
	if err != nil {
		return "", err
	}
	return piecePath(d.root, s.kind, hexHash)
}

type sectionCloser struct {
	io.Reader
	c io.Closer
}

func (s *sectionCloser) Close() error {
	return s.c.Close()
}

// UsedOn is the used counter of one root.
func (s *Space) UsedOn(root string) (int64, error) {
	d, err := s.diskOn(root)
	if err != nil {
		return 0, err
	}
	return d.tracker.Used(), nil
}

// SetIntervalsOn replaces the owned ranges and move sources of root, read at
// map version, and rewrites its layout.json.
func (s *Space) SetIntervalsOn(root string, version int64, owned, moveSources []HashRange) error {
	d, err := s.diskOn(root)
	if err != nil {
		return err
	}
	ivs, err := decodeIntervals(owned)
	if err != nil {
		return err
	}
	mivs, err := decodeIntervals(moveSources)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d.version = version
	d.intervals = ivs
	d.moveSources = mivs
	return s.flushLocked()
}

func decodeIntervals(rs []HashRange) ([]hashInterval, error) {
	out := make([]hashInterval, len(rs))
	for i, r := range rs {
		start, err := decodeHash(r.Start)
		if err != nil {
			return nil, xerrors.Errorf("range %d start: %w", i, err)
		}
		end, err := decodeHash(r.End)
		if err != nil {
			return nil, xerrors.Errorf("range %d end: %w", i, err)
		}
		out[i] = hashInterval{start: start, end: end}
	}
	return out, nil
}

func encodeIntervals(ivs []hashInterval) []HashRange {
	if len(ivs) == 0 {
		return nil
	}
	out := make([]HashRange, len(ivs))
	for i, iv := range ivs {
		out[i] = HashRange{
			Start: hex.EncodeToString(iv.start),
			End:   hex.EncodeToString(iv.end),
		}
	}
	return out
}

func (d *disk) catchUp(kind string, cutoff time.Time) error {
	dir := filepath.Join(d.root, kind)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return xerrors.Errorf("reading %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == layoutFile || strings.HasPrefix(name, ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !info.IsDir() || !info.ModTime().After(cutoff) {
			continue
		}
		if err := d.addNewFiles(filepath.Join(dir, name), cutoff); err != nil {
			return err
		}
	}
	return nil
}

func (d *disk) addNewFiles(dir string, cutoff time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return xerrors.Errorf("reading %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !info.Mode().IsRegular() || !info.ModTime().After(cutoff) {
			continue
		}
		if info.Size() < 0 {
			return xerrors.Errorf("negative size for %s", e.Name())
		}
		d.tracker.Add(info.Size())
	}
	return nil
}

type cidWriter struct {
	mu      sync.Mutex
	space   *Space
	disk    *disk
	f       *os.File
	tmp     string
	final   string
	written int64
	done    bool
	err     error
}

func (w *cidWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return 0, os.ErrClosed
	}
	n, err := w.f.Write(p)
	w.written += int64(n)
	return n, err
}

func (w *cidWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return w.err
	}
	w.done = true
	if err := w.f.Sync(); err != nil {
		_ = w.f.Close()
		_ = os.Remove(w.tmp)
		w.err = err
		return err
	}
	if err := w.f.Close(); err != nil {
		_ = os.Remove(w.tmp)
		w.err = err
		return err
	}
	w.space.mu.Lock()
	defer w.space.mu.Unlock()
	if err := renameNoReplace(w.tmp, w.final); err != nil {
		_ = os.Remove(w.tmp)
		w.err = err
		return err
	}
	w.disk.tracker.Add(w.written)
	return nil
}

// Abort deletes the temp sibling and does not change the used counter.
func (w *cidWriter) Abort() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return w.err
	}
	w.done = true
	err := w.f.Close()
	if rmErr := os.Remove(w.tmp); rmErr != nil && !os.IsNotExist(rmErr) && err == nil {
		err = rmErr
	}
	w.err = err
	return err
}

var _ HashSpace = (*Space)(nil)
