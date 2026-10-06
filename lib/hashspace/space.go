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
	// moveDests are intervals another disk still owns that are moving here.
	moveDests []hashInterval
	// misplaced mirrors hash_space_disk.has_misplaced.
	misplaced bool
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
		// loadDisk
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
		if d.moveDests, err = decodeIntervals(layout.MoveDests); err != nil {
			return nil, xerrors.Errorf("%s move destinations: %w", path, err)
		}
		if err := d.catchUp(kind, info.ModTime()); err != nil {
			return nil, err
		}
		s.disks = append(s.disks, d)
	}
	// startFlush
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
	return s, nil
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
			MoveDests:   encodeIntervals(d.moveDests),
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

// WriteCID opens a new file for a piece CID that is not already stored.
// The file is written to the root a move is bringing the hash to, else the
// root whose ranges contain it. Close adds the written byte count. A second Close does not add again.
// Abort drops the temp sibling and leaves the counter unchanged.
// If the CID file already exists, WriteCID returns os.ErrExist and does
// not change the counter.
func (s *Space) WriteCID(c cid.Cid) (io.WriteCloser, error) {
	hexHash, digest, err := cidHashHex(c)
	if err != nil {
		return nil, err
	}
	d := s.writeTarget(digest)
	if d == nil {
		return nil, xerrors.Errorf("cid hash is not owned by any local range")
	}
	return s.writeOn(d, hexHash)
}

// writeTarget picks the local disk a new file for digest goes to: a move
// destination covering it, else the range owner. Never the move's source, as
// the mover may already have passed the hash. Misplaced-only disks are skipped.
func (s *Space) writeTarget(digest []byte) *disk {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.disks {
		if covers(d.moveDests, digest) {
			return d
		}
	}
	for _, d := range s.disks {
		if covers(d.intervals, digest) {
			return d
		}
	}
	return nil
}

func covers(ivs []hashInterval, digest []byte) bool {
	for _, iv := range ivs {
		if hashspacesolver.Contains(iv.start, iv.end, digest) {
			return true
		}
	}
	return false
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

// DeleteCID removes the CID file from every local disk that holds its hash:
// the range owner and, while a move covers it, the destination. A missing
// file on all of them returns os.ErrNotExist and does not change the counter.
func (s *Space) DeleteCID(c cid.Cid) error {
	hexHash, digest, err := cidHashHex(c)
	if err != nil {
		return err
	}
	disks := s.locate(digest)
	if len(disks) == 0 {
		return xerrors.Errorf("cid hash is not owned by any local range")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := false
	for _, d := range disks {
		err = removePiece(d, s.kind, hexHash)
		if err == nil {
			removed = true
			continue
		}
		if os.IsNotExist(err) {
			continue
		}
		return err
	}
	if !removed {
		return os.ErrNotExist
	}
	return nil
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

// ReadCIDFileFrom opens the CID file. Disks that hold the hash come first
// (the range owner, then a disk the bytes are moving to). Any other local
// root is tried after those, so a stray copy is still readable.
func (s *Space) ReadCIDFileFrom(c cid.Cid) (ReadSeekFile, error) {
	hexHash, digest, err := cidHashHex(c)
	if err != nil {
		return nil, err
	}
	holders := s.locate(digest)
	if len(holders) == 0 {
		return nil, xerrors.Errorf("cid hash is not owned by any local range")
	}
	order := append([]*disk(nil), holders...)
	held := map[*disk]struct{}{}
	for _, d := range holders {
		held[d] = struct{}{}
	}
	for _, d := range s.disks {
		if _, ok := held[d]; !ok {
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

// locate returns the local disks that may hold digest. The range owner comes
// first. A disk the bytes are moving to is included as well, so a hash in
// flight is found on both ends when both disks are local. Every disk with
// misplaced pieces comes last, since any hash may be there too.
func (s *Space) locate(digest []byte) []*disk {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*disk
	seen := map[*disk]struct{}{}
	add := func(d *disk) {
		if _, ok := seen[d]; ok {
			return
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	for _, d := range s.disks {
		if covers(d.intervals, digest) {
			add(d)
		}
	}
	for _, d := range s.disks {
		if covers(d.moveDests, digest) {
			add(d)
		}
	}
	for _, d := range s.disks {
		if d.misplaced {
			add(d)
		}
	}
	return out
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

// ReturnFileOn moves the CID file on root back to dest and subtracts its size
// from the counter. dest must not already exist. The source file is left in
// place when the rename fails.
func (s *Space) ReturnFileOn(root string, c cid.Cid, dest string) error {
	hexHash, _, err := cidHashHex(c)
	if err != nil {
		return err
	}
	d, err := s.diskOn(root)
	if err != nil {
		return err
	}
	final, err := piecePath(d.root, s.kind, hexHash)
	if err != nil {
		return err
	}
	info, err := os.Stat(final)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return xerrors.Errorf("%s is not a regular file", final)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := renameNoReplace(final, dest); err != nil {
		if isCrossDevice(err) {
			return ErrCrossDevice
		}
		return err
	}
	d.tracker.Sub(info.Size())
	return nil
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

// SetIntervalsOn replaces the owned ranges and move destinations of root, read
// at map version, and rewrites its layout.json.
func (s *Space) SetIntervalsOn(root string, version int64, owned, moveDests []HashRange) error {
	d, err := s.diskOn(root)
	if err != nil {
		return err
	}
	ivs, err := decodeIntervals(owned)
	if err != nil {
		return err
	}
	mivs, err := decodeIntervals(moveDests)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d.version = version
	d.intervals = ivs
	d.moveDests = mivs
	return s.flushLocked()
}

// SetMisplacedOn records whether root holds pieces outside its own ranges
// (hash_space_disk.has_misplaced). locate then includes root for every hash.
func (s *Space) SetMisplacedOn(root string, misplaced bool) {
	d, err := s.diskOn(root)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d.misplaced = misplaced
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
		// addNewFiles
		sub := filepath.Join(dir, name)
		subEntries, err := os.ReadDir(sub)
		if err != nil {
			return xerrors.Errorf("reading %s: %w", sub, err)
		}
		for _, sube := range subEntries {
			if sube.IsDir() || strings.HasPrefix(sube.Name(), ".") {
				continue
			}
			subInfo, err := sube.Info()
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			if !subInfo.Mode().IsRegular() || !subInfo.ModTime().After(cutoff) {
				continue
			}
			if subInfo.Size() < 0 {
				return xerrors.Errorf("negative size for %s", sube.Name())
			}
			d.tracker.Add(subInfo.Size())
		}
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
