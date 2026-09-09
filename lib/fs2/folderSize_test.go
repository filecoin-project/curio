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
	if result.Bytes != 1349 || result.Files != 4 {
		t.Fatalf("full range: got %+v, want 1349 bytes / 4 files", result)
	}

	result, err = SumFileSizesRange(dir, "a", "c", 8)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 250 || result.Files != 2 {
		t.Fatalf("(a, c]: got %+v, want 250 bytes / 2 files", result)
	}

	result, err = SumFileSizesRange(dir, "b", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 1049 || result.Files != 2 {
		t.Fatalf("(b, +inf]: got %+v, want 1049 bytes / 2 files", result)
	}

	empty := t.TempDir()
	result, err = SumFileSizesRange(empty, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 0 || result.Files != 0 || result.Vanished != 0 {
		t.Fatalf("empty dir: got %+v, want zero", result)
	}

	if _, err := SumFileSizesRange("dir\x00", "", "", 0); err == nil {
		t.Fatal("expected error for NUL in path")
	}
	if _, err := SumFileSizesRange(".", "", "", 4097); err == nil {
		t.Fatal("expected error for queue depth over 4096")
	}
}

func TestConcatenatedHashPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ab", "cdefg"), 42)
	if err := os.MkdirAll(filepath.Join(dir, "xy", "zt"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "xy", "zt", "uvw"), 9)

	result, err := SumFileSizesRange(dir, "abcdefe", "abcdefg", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 42 || result.Files != 1 {
		t.Fatalf("(abcdefe, abcdefg]: got %+v, want 42 bytes / 1 file", result)
	}

	result, err = SumFileSizesRange(dir, "abcdefg", "abcdefh", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 0 || result.Files != 0 {
		t.Fatalf("(abcdefg, abcdefh]: got %+v, want empty (exclusive low)", result)
	}

	result, err = SumFileSizesRange(dir, "ab", "ac", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 42 || result.Files != 1 {
		t.Fatalf("(ab, ac]: got %+v, want 42 bytes / 1 file", result)
	}

	result, err = SumFileSizesRange(dir, "xyztuvv", "xyztuvw", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 9 || result.Files != 1 {
		t.Fatalf("xy/zt/uvw hash: got %+v, want 9 bytes / 1 file", result)
	}
}

func TestSubtreePrune(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "zz"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "zz", "file"), 100)
	writeFile(t, filepath.Join(dir, "a"), 7)

	result, err := SumFileSizesRange(dir, "", "a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 7 || result.Files != 1 {
		t.Fatalf("('', a]: got %+v, want only root file a", result)
	}
}

func TestHashHelpers(t *testing.T) {
	if got := hashFromRel("ab/cdefg"); got != "abcdefg" {
		t.Fatalf("hashFromRel(ab/cdefg)=%q", got)
	}
	if got := hashFromRel("ab/cd/efg"); got != "abcdefg" {
		t.Fatalf("hashFromRel(ab/cd/efg)=%q", got)
	}

	if !hashInRange("abcdefg", "abcdefe", "abcdefg") {
		t.Fatal("abcdefg should be in (abcdefe, abcdefg]")
	}
	if hashInRange("abcdefg", "abcdefg", "abcdefh") {
		t.Fatal("abcdefg should not be in (abcdefg, abcdefh]")
	}

	if !subtreeCanMatch("ab", "abcdefe", "abcdefg") {
		t.Fatal("prefix ab can match (abcdefe, abcdefg]")
	}
	if subtreeCanMatch("zz", "", "a") {
		t.Fatal("prefix zz cannot match ('', a]")
	}
	if subtreeCanMatch("aa", "b", "") {
		t.Fatal("prefix aa cannot match (b, +inf]")
	}
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}
