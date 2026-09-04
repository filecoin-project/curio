//go:build (linux || darwin) && cgo

package fs2

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSumFileSizesRange(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "a"), 100)
	writeFile(t, filepath.Join(dir, "b"), 200)
	writeFile(t, filepath.Join(dir, "c"), 50)
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "subdir", "nested"), 999)
	if err := os.Symlink(filepath.Join(dir, "a"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	result, err := SumFileSizesRange(dir, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 350 || result.Files != 3 {
		t.Fatalf("full range: got %+v, want 350 bytes / 3 files", result)
	}

	result, err = SumFileSizesRange(dir, "a", "c", 8)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 300 || result.Files != 2 {
		t.Fatalf("[a, c): got %+v, want 300 bytes / 2 files", result)
	}

	result, err = SumFileSizesRange(dir, "c", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 50 || result.Files != 1 {
		t.Fatalf("[c, +inf): got %+v, want 50 bytes / 1 file", result)
	}

	empty := t.TempDir()
	result, err = SumFileSizesRange(empty, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 0 || result.Files != 0 || result.Vanished != 0 {
		t.Fatalf("empty dir: got %+v, want zero", result)
	}
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}
