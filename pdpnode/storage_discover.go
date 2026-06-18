package pdpnode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

const defaultPDPStorageWeight = 10

const pdpDataDirName = "filecoin-hot-data"

const maxPDPDataSearchDepth = 3

// findCurioPDPDataOnMount searches up to maxDepth directory levels below mountRoot
// for a directory named filecoin-hot-data. When multiple matches exist, the
// shallowest match wins.
func findCurioPDPDataOnMount(mountRoot string, maxDepth int) (string, error) {
	if maxDepth < 0 {
		return "", nil
	}

	if filepath.Base(mountRoot) == pdpDataDirName {
		info, err := os.Stat(mountRoot)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return mountRoot, nil
		}
	}

	type queueEntry struct {
		path  string
		depth int
	}

	queue := []queueEntry{{path: mountRoot, depth: 0}}
	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]

		entries, err := os.ReadDir(entry.path)
		if err != nil {
			// Unreadable subtrees (e.g. macOS SIP-protected paths) are treated as
			// fully searched so discovery can continue elsewhere on the mount.
			continue
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}

			child := filepath.Join(entry.path, ent.Name())
			if ent.Name() == pdpDataDirName {
				return child, nil
			}

			if entry.depth+1 < maxDepth {
				queue = append(queue, queueEntry{path: child, depth: entry.depth + 1})
			}
		}
	}

	return "", nil
}

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

func discoverPDPStoragePaths(mountPoints []string) ([]string, error) {
	seenMounts := make(map[string]struct{}, len(mountPoints))
	var paths []string

	for _, mountRoot := range mountPoints {
		mountRoot = filepath.Clean(mountRoot)
		if mountRoot == "" {
			continue
		}
		if _, ok := seenMounts[mountRoot]; ok {
			continue
		}
		seenMounts[mountRoot] = struct{}{}

		p, err := findCurioPDPDataOnMount(mountRoot, maxPDPDataSearchDepth)
		if err != nil {
			return nil, err
		}
		if p == "" {
			continue
		}

		canon, err := canonicalStoragePath(p)
		if err != nil {
			canon = filepath.Clean(p)
		}

		duplicate := false
		for _, existing := range paths {
			if sameStoragePath(canon, existing) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}

		paths = append(paths, canon)
	}

	sort.Strings(paths)
	return paths, nil
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

func pdpStorageConfig(mountPoints []string) (storiface.StorageConfig, error) {
	paths, err := discoverPDPStoragePaths(mountPoints)
	if err != nil {
		return storiface.StorageConfig{}, err
	}

	cfg := storiface.StorageConfig{
		StoragePaths: make([]storiface.LocalPath, 0, len(paths)),
	}
	for _, p := range paths {
		if err := ensureSectorstoreJSON(p); err != nil {
			return storiface.StorageConfig{}, err
		}
		cfg.StoragePaths = append(cfg.StoragePaths, storiface.LocalPath{Path: p})
	}

	return cfg, nil
}
