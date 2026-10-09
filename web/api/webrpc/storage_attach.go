package webrpc

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/skiffdata"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

// StorageCandidate is a writable folder under the data root that may be attached.
type StorageCandidate struct {
	Path      string `json:"Path"`
	Attached  bool   `json:"Attached"`
	Available int64  `json:"Available"`
	Capacity  int64  `json:"Capacity"`
	Writable  bool   `json:"Writable"`
}

// StorageCandidates lists writable folders under the configured data root (/data by default).
func (a *Handler) StorageCandidates(ctx context.Context) ([]StorageCandidate, error) {
	_ = ctx
	if a.Deps == nil || a.Deps.Cfg == nil {
		return nil, xerrors.Errorf("deps not available")
	}

	dataRoot := skiffdata.ResolveDataRoot(a.Deps.Cfg)
	paths, err := skiffdata.DiscoverStorageCandidates(dataRoot)
	if err != nil {
		return nil, err
	}

	attached := map[string]struct{}{}
	if a.Deps.LocalStore != nil {
		if locals, lerr := a.Deps.LocalStore.Local(ctx); lerr == nil {
			for _, lp := range locals {
				if lp.LocalPath == "" {
					continue
				}
				canon, cerr := filepath.Abs(lp.LocalPath)
				if cerr != nil {
					canon = lp.LocalPath
				}
				attached[filepath.Clean(canon)] = struct{}{}
			}
		}
	}

	out := make([]StorageCandidate, 0, len(paths))
	for _, p := range paths {
		c := StorageCandidate{Path: p, Writable: true}
		if _, ok := attached[filepath.Clean(p)]; ok {
			c.Attached = true
		}
		if st, serr := fsutil.Statfs(p); serr == nil {
			c.Available = st.Available
			c.Capacity = st.Capacity
		}
		out = append(out, c)
	}
	return out, nil
}

// StorageAttachLocal initializes (if needed) and attaches a local directory.
// Any existing absolute path is allowed (not limited to the /data candidate root).
func (a *Handler) StorageAttachLocal(ctx context.Context, path string) error {
	if a.Deps == nil || a.Deps.LocalStore == nil || a.Deps.LocalPaths == nil {
		return xerrors.Errorf("local storage is not available")
	}
	if a.Deps.DB != nil && a.Deps.DB.ReadOnly() {
		return xerrors.Errorf("database is read-only")
	}

	path, err := homedir.Expand(path)
	if err != nil {
		return xerrors.Errorf("expanding path: %w", err)
	}

	path, err = skiffdata.CanonicalLocalPath(path)
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return xerrors.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return xerrors.Errorf("path is not a directory: %s", path)
	}

	if err := skiffdata.EnsurePDPSectorstore(path); err != nil {
		return err
	}

	// Avoid duplicate entries in storage.json.
	cfg, err := a.Deps.LocalPaths.GetStorage()
	if err != nil {
		return xerrors.Errorf("get storage config: %w", err)
	}
	for _, existing := range cfg.StoragePaths {
		if sameLocalPath(existing.Path, path) {
			// Already persisted; ensure it is open.
			return a.Deps.LocalStore.OpenPath(ctx, path)
		}
	}

	if err := a.Deps.LocalStore.OpenPath(ctx, path); err != nil {
		return xerrors.Errorf("opening local path: %w", err)
	}

	if err := a.Deps.LocalPaths.SetStorage(func(sc *storiface.StorageConfig) {
		sc.StoragePaths = append(sc.StoragePaths, storiface.LocalPath{Path: path})
	}); err != nil {
		return xerrors.Errorf("persist storage config: %w", err)
	}
	return nil
}

// StorageDetachLocal removes a local storage path from this node.
func (a *Handler) StorageDetachLocal(ctx context.Context, path string) error {
	if a.Deps == nil || a.Deps.LocalStore == nil || a.Deps.LocalPaths == nil {
		return xerrors.Errorf("local storage is not available")
	}
	if a.Deps.DB != nil && a.Deps.DB.ReadOnly() {
		return xerrors.Errorf("database is read-only")
	}

	path, err := homedir.Expand(path)
	if err != nil {
		return xerrors.Errorf("expanding path: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}

	lps, err := a.Deps.LocalStore.Local(ctx)
	if err != nil {
		return xerrors.Errorf("getting local path list: %w", err)
	}

	var localPath *storiface.StoragePath
	for _, lp := range lps {
		if sameLocalPath(lp.LocalPath, path) {
			lp := lp
			localPath = &lp
			break
		}
	}
	if localPath == nil {
		return xerrors.Errorf("no local paths match %q", path)
	}

	var found bool
	if err := a.Deps.LocalPaths.SetStorage(func(sc *storiface.StorageConfig) {
		out := make([]storiface.LocalPath, 0, len(sc.StoragePaths))
		for _, storagePath := range sc.StoragePaths {
			if sameLocalPath(storagePath.Path, path) || sameLocalPath(storagePath.Path, localPath.LocalPath) {
				found = true
				continue
			}
			out = append(out, storagePath)
		}
		sc.StoragePaths = out
	}); err != nil {
		return xerrors.Errorf("set storage config: %w", err)
	}
	if !found {
		return xerrors.Errorf("path not found in storage.json")
	}

	return a.Deps.LocalStore.ClosePath(ctx, localPath.ID)
}

func sameLocalPath(a, b string) bool {
	if a == b {
		return true
	}
	aa, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	if filepath.Clean(aa) == filepath.Clean(bb) {
		return true
	}
	ai, err := os.Stat(aa)
	if err != nil {
		return false
	}
	bi, err := os.Stat(bb)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}
