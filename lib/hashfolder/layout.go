package hashfolder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/xerrors"
)

// Layout is layout-{kind}.json at the storage root, beside the space folder.
type Layout struct {
	// Version is the hash_space_meta.version the ranges were read at. A
	// layout newer than the database is written back to it on startup.
	Version     int64       `json:"version,omitempty"`
	Used        int64       `json:"used"`
	CommittedAt time.Time   `json:"committed_at"`
	Split       string      `json:"split"`
	Ranges      []HashRange `json:"ranges"`
	// MoveSources are intervals owned by another folder that are being moved
	// here. A piece in one may be on either folder until the move ends.
	MoveSources []HashRange `json:"move_sources,omitempty"`
}

// HashRange is one half-open interval (start, end] owned by this folder.
// Start and end are lowercase hex of the solver hash bytes.
type HashRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// layoutPath is kind's layout file at root. It sits outside the space folder
// so the folder holds only hash-named files.
func layoutPath(root, kind string) string {
	return filepath.Join(root, "layout-"+kind+".json")
}

// legacyLayoutPath is the layout.json inside the space folder that older
// builds wrote.
func legacyLayoutPath(root, kind string) string {
	return filepath.Join(root, kind, legacyLayoutFile)
}

// layoutExists reports whether kind has a layout on root, at either path.
func layoutExists(root, kind string) (bool, error) {
	ok, err := fileExists(layoutPath(root, kind))
	if err != nil || ok {
		return ok, err
	}
	return fileExists(legacyLayoutPath(root, kind))
}

// openLayout reads kind's layout on root and the file's mtime. A missing
// root layout falls back to the legacy layout.json; that path also removes
// temp files older builds left in the space folder. The next writeLayout
// moves the layout to the root path.
func openLayout(root, kind string) (Layout, time.Time, error) {
	path := layoutPath(root, kind)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		legacy := legacyLayoutPath(root, kind)
		if _, lerr := os.Stat(legacy); lerr == nil {
			if err := removeLegacyTemps(filepath.Join(root, kind)); err != nil {
				return Layout{}, time.Time{}, err
			}
			path = legacy
			info, err = os.Stat(path)
		}
	}
	if err != nil {
		return Layout{}, time.Time{}, xerrors.Errorf("reading %s: %w", path, err)
	}
	layout, err := readLayout(path)
	if err != nil {
		return Layout{}, time.Time{}, err
	}
	return layout, info.ModTime(), nil
}

// removeLegacyTemps deletes dot-prefixed .tmp files at the top of dir and in
// its shard directories. Older builds wrote pieces and layout.json through
// such siblings, and a crash left them behind. A missing dir is fine.
func removeLegacyTemps(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return xerrors.Errorf("reading %s: %w", dir, err)
	}
	isTemp := func(name string) bool {
		return strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".tmp")
	}
	for _, e := range entries {
		if !e.IsDir() {
			if isTemp(e.Name()) {
				if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			continue
		}
		shard := filepath.Join(dir, e.Name())
		files, err := os.ReadDir(shard)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return xerrors.Errorf("reading %s: %w", shard, err)
		}
		for _, f := range files {
			if f.IsDir() || !isTemp(f.Name()) {
				continue
			}
			if err := os.Remove(filepath.Join(shard, f.Name())); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func readLayout(path string) (Layout, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Layout{}, xerrors.Errorf("reading %s: %w", path, err)
	}
	var layout Layout
	if err := json.Unmarshal(b, &layout); err != nil {
		return Layout{}, xerrors.Errorf("decoding %s: %w", path, err)
	}
	if layout.Used < 0 {
		return Layout{}, xerrors.Errorf("%s used is negative", path)
	}
	if layout.Ranges == nil {
		layout.Ranges = []HashRange{}
	}
	return layout, nil
}

// writeLayout writes kind's layout to the root path and syncs it, then
// removes a legacy layout.json for kind. The space folder is created for
// pieces.
func writeLayout(root, kind string, layout Layout) error {
	if layout.Ranges == nil {
		layout.Ranges = []HashRange{}
	}
	dir := filepath.Join(root, kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return xerrors.Errorf("creating %s: %w", dir, err)
	}
	buf, err := json.MarshalIndent(layout, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	path := layoutPath(root, kind)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return xerrors.Errorf("writing %s: %w", path, err)
	}
	_, werr := f.Write(buf)
	serr := f.Sync()
	cerr := f.Close()
	for _, err := range []error{werr, serr, cerr} {
		if err != nil {
			return xerrors.Errorf("writing %s: %w", path, err)
		}
	}
	for _, old := range []string{legacyLayoutPath(root, kind), filepath.Join(dir, "."+legacyLayoutFile+".tmp")} {
		if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
