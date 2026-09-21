package hashspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/xerrors"
)

// Layout is layout.json at the root of one space folder.
type Layout struct {
	Used        int64       `json:"used"`
	CommittedAt time.Time   `json:"committed_at"`
	Split       string      `json:"split"`
	Ranges      []HashRange `json:"ranges"`
}

// HashRange is one half-open interval (start, end] owned by this folder.
// Start and end are lowercase hex of the solver hash bytes.
type HashRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
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
	if layout.Split != SPLIT {
		return Layout{}, xerrors.Errorf("%s split %q, want %s", path, layout.Split, SPLIT)
	}
	if layout.Used < 0 {
		return Layout{}, xerrors.Errorf("%s used is negative", path)
	}
	if layout.Ranges == nil {
		layout.Ranges = []HashRange{}
	}
	return layout, nil
}

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
	return writeAtomic(filepath.Join(dir, layoutFile), buf)
}

func writeAtomic(path string, data []byte) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return xerrors.Errorf("writing %s: %w", tmp, err)
	}
	_, werr := f.Write(data)
	serr := f.Sync()
	cerr := f.Close()
	if werr != nil || serr != nil || cerr != nil {
		_ = os.Remove(tmp)
		if werr != nil {
			return werr
		}
		if serr != nil {
			return serr
		}
		return cerr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return xerrors.Errorf("renaming %s: %w", path, err)
	}
	return nil
}

func removeLayoutTemp(root, kind string) {
	_ = os.Remove(filepath.Join(root, kind, "."+layoutFile+".tmp"))
}
