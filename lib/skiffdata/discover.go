// Package skiffdata provides Curio-PDP storage candidate discovery and path helpers.
package skiffdata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

var log = logging.Logger("skiffdata")

const (
	DefaultDataPath         = "/data"
	defaultPDPStorageWeight = 10
	maxStorageScanDepth     = 3
)

var skipStorageDirNames = func() map[string]struct{} {
	out := map[string]struct{}{
		paths.FetchTempSubdir: {},
		paths.StashDirName:    {},
		"yugabyte":            {},
		"skiff":               {},
		"forest":              {},
		"forest-token":        {},
	}
	for _, ft := range storiface.PathTypes {
		out[ft.String()] = struct{}{}
	}
	return out
}()

// ResolveDataRoot returns the preferred storage candidate root for Curio-PDP.
// Precedence: DATA_STORAGE, SKIFF_DATA, CURIO_DATA, [Subsystems].DataPath, /data.
func ResolveDataRoot(cfg *config.CurioConfig) string {
	if v := os.Getenv("DATA_STORAGE"); v != "" {
		return v
	}
	if v := os.Getenv("SKIFF_DATA"); v != "" {
		return v
	}
	if v := os.Getenv("CURIO_DATA"); v != "" {
		return v
	}
	if cfg != nil && cfg.Subsystems.DataPath != "" {
		return cfg.Subsystems.DataPath
	}
	return DefaultDataPath
}

// DiscoverStorageCandidates lists writable directories under dataRoot for UI selection.
func DiscoverStorageCandidates(dataRoot string) ([]string, error) {
	return discoverWritableStoragePaths(dataRoot)
}

// EnsurePDPSectorstore creates sectorstore.json with PDP defaults when missing.
func EnsurePDPSectorstore(storagePath string) error {
	return ensureSectorstoreJSON(storagePath)
}

// CanonicalLocalPath expands path to an absolute, symlink-resolved directory path.
func CanonicalLocalPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", xerrors.Errorf("path is empty")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", xerrors.Errorf("abs path: %w", err)
	}
	canonPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return filepath.Clean(absPath), nil
	}
	return canonPath, nil
}

// CanonicalPathUnderDataRoot expands path and requires it to be dataRoot or a subdirectory.
func CanonicalPathUnderDataRoot(path, dataRoot string) (string, error) {
	canonPath, err := CanonicalLocalPath(path)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return "", xerrors.Errorf("abs data root: %w", err)
	}
	canonRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		canonRoot = filepath.Clean(absRoot)
	}

	rel, err := filepath.Rel(canonRoot, canonPath)
	if err != nil {
		return "", xerrors.Errorf("path %s is outside data root %s", canonPath, canonRoot)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", xerrors.Errorf("path %s is outside data root %s", canonPath, canonRoot)
	}
	return canonPath, nil
}

func isSkippedStorageDir(name string) bool {
	_, ok := skipStorageDirNames[name]
	return ok
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

	if isWritableDir(dataRoot) {
		addPath(dataRoot)
	}

	type scanEntry struct {
		path  string
		depth int
	}
	queue := []scanEntry{{path: dataRoot, depth: 0}}

	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]

		if entry.depth >= maxStorageScanDepth {
			continue
		}

		children, err := os.ReadDir(entry.path)
		if err != nil {
			if os.IsPermission(err) {
				continue
			}
			return nil, xerrors.Errorf("reading %s: %w", entry.path, err)
		}

		for _, child := range children {
			if !child.IsDir() {
				continue
			}
			name := child.Name()
			if isSkippedStorageDir(name) {
				continue
			}

			childPath := filepath.Join(entry.path, name)
			if isWritableDir(childPath) {
				addPath(childPath)
			}
			queue = append(queue, scanEntry{path: childPath, depth: entry.depth + 1})
		}
	}

	sort.Strings(pathsOut)
	return pathsOut, nil
}

func defaultPDPStorageMeta() storiface.LocalStorageMeta {
	return storiface.LocalStorageMeta{
		ID:       storiface.ID(uuid.New().String()),
		Weight:   defaultPDPStorageWeight,
		CanStore: true,
		CanSeal:  true,
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
