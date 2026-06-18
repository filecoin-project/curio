package pdpnode

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

const defaultPDPStorageWeight = 10

func canonicalStoragePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func sameStoragePath(a, b string) bool {
	if a == b {
		return true
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

func isWritableDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".skiff-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func discoverWritableStoragePaths(dataRoot string) ([]string, error) {
	dataRoot = filepath.Clean(dataRoot)
	info, err := os.Stat(dataRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, xerrors.Errorf("stat data root %s: %w", dataRoot, err)
	}
	if !info.IsDir() {
		return nil, xerrors.Errorf("data root %s is not a directory", dataRoot)
	}

	var pathsOut []string
	seen := make(map[string]struct{})

	addPath := func(p string) {
		canon, err := canonicalStoragePath(p)
		if err != nil {
			canon = filepath.Clean(p)
		}
		if _, ok := seen[canon]; ok {
			return
		}
		for _, existing := range pathsOut {
			if sameStoragePath(canon, existing) {
				return
			}
		}
		seen[canon] = struct{}{}
		pathsOut = append(pathsOut, canon)
	}

	err = filepath.WalkDir(dataRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsPermission(walkErr) {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if isWritableDir(path) {
			addPath(path)
		}
		return nil
	})
	if err != nil {
		return nil, xerrors.Errorf("walking data root %s: %w", dataRoot, err)
	}

	sort.Strings(pathsOut)
	return pathsOut, nil
}

func defaultPDPStorageMeta() storiface.LocalStorageMeta {
	return storiface.LocalStorageMeta{
		ID:       storiface.ID(uuid.New().String()),
		Weight:   defaultPDPStorageWeight,
		CanStore: true,
	}
}

func ensureSectorstoreJSON(storagePath string) error {
	metaPath := filepath.Join(storagePath, paths.MetaFile)
	_, err := os.Stat(metaPath)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return xerrors.Errorf("statting %s: %w", metaPath, err)
	}

	meta := defaultPDPStorageMeta()
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return xerrors.Errorf("marshaling storage metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, b, 0o644); err != nil {
		return xerrors.Errorf("persisting storage metadata (%s): %w", metaPath, err)
	}

	log.Infof("initialized %s at %s", paths.MetaFile, metaPath)
	return nil
}

func pdpStorageConfig(dataRoot string) (storiface.StorageConfig, error) {
	storagePaths, err := discoverWritableStoragePaths(dataRoot)
	if err != nil {
		return storiface.StorageConfig{}, err
	}

	cfg := storiface.StorageConfig{
		StoragePaths: make([]storiface.LocalPath, 0, len(storagePaths)),
	}
	for _, p := range storagePaths {
		if err := ensureSectorstoreJSON(p); err != nil {
			return storiface.StorageConfig{}, err
		}
		cfg.StoragePaths = append(cfg.StoragePaths, storiface.LocalPath{Path: p})
	}

	return cfg, nil
}
