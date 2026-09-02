package skiffdata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/lib/storiface"
)

func TestIsWritableDir(t *testing.T) {
	root := t.TempDir()
	require.True(t, isWritableDir(root))

	blocked := filepath.Join(root, "blocked")
	require.NoError(t, os.MkdirAll(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })
	require.False(t, isWritableDir(blocked))
}

func TestDiscoverWritableStoragePaths(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "hot"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested", "deep"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "other"), 0o755))

	got, err := discoverWritableStoragePaths(root)
	require.NoError(t, err)
	require.Equal(t, []string{
		mustCanon(t, root),
		mustCanon(t, filepath.Join(root, "hot")),
		mustCanon(t, filepath.Join(root, "nested")),
		mustCanon(t, filepath.Join(root, "nested", "deep")),
		mustCanon(t, filepath.Join(root, "other")),
	}, got)
}

func TestDiscoverWritableStoragePathsSkipsSectorLayoutDirs(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "sealed", "s-t01234-1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "unsealed", "s-t01234-1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cache", "s-t01234-1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "fetching", "tmp"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "stash"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "forest"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "yugabyte"), 0o755))

	got, err := discoverWritableStoragePaths(root)
	require.NoError(t, err)
	require.Equal(t, []string{mustCanon(t, root)}, got)
}

func TestDiscoverWritableStoragePathsRespectsMaxDepth(t *testing.T) {
	root := t.TempDir()

	deep := filepath.Join(root, "l1", "l2", "l3", "l4")
	require.NoError(t, os.MkdirAll(deep, 0o755))

	got, err := discoverWritableStoragePaths(root)
	require.NoError(t, err)
	require.Equal(t, []string{
		mustCanon(t, root),
		mustCanon(t, filepath.Join(root, "l1")),
		mustCanon(t, filepath.Join(root, "l1", "l2")),
		mustCanon(t, filepath.Join(root, "l1", "l2", "l3")),
	}, got)
}

func TestDiscoverWritableStoragePathsIgnoresUnreadableSubtree(t *testing.T) {
	root := t.TempDir()

	blocked := filepath.Join(root, "blocked")
	require.NoError(t, os.MkdirAll(blocked, 0o755))
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	got, err := discoverWritableStoragePaths(root)
	require.NoError(t, err)
	require.Equal(t, []string{mustCanon(t, root)}, got)
}

func TestDiscoverWritableStoragePathsDedupesSymlinks(t *testing.T) {
	root := t.TempDir()
	hot := filepath.Join(root, "hot")
	require.NoError(t, os.MkdirAll(hot, 0o755))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(hot, link))

	got, err := discoverWritableStoragePaths(root)
	require.NoError(t, err)
	require.Equal(t, []string{mustCanon(t, root), mustCanon(t, hot)}, got)
}

func TestDiscoverWritableStoragePathsMissingRoot(t *testing.T) {
	got, err := discoverWritableStoragePaths(t.TempDir() + "/missing")
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestEnsureSectorstoreJSONPreservesExisting(t *testing.T) {
	storagePath := t.TempDir()
	metaPath := filepath.Join(storagePath, "sectorstore.json")
	existingID := storiface.ID("existing-id")
	existing, err := json.MarshalIndent(storiface.LocalStorageMeta{
		ID:       existingID,
		Weight:   1,
		CanStore: true,
	}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(metaPath, existing, 0o644))

	require.NoError(t, ensureSectorstoreJSON(storagePath))

	got, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	require.Equal(t, existing, got)
}

func TestCanonicalLocalPath(t *testing.T) {
	root := t.TempDir()
	got, err := CanonicalLocalPath(root)
	require.NoError(t, err)
	require.Equal(t, mustCanon(t, root), got)

	_, err = CanonicalLocalPath("  ")
	require.Error(t, err)
}

func TestCanonicalPathUnderDataRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "disk1")
	require.NoError(t, os.MkdirAll(child, 0o755))

	got, err := CanonicalPathUnderDataRoot(child, root)
	require.NoError(t, err)
	require.Equal(t, mustCanon(t, child), mustCanon(t, got))

	_, err = CanonicalPathUnderDataRoot(t.TempDir(), root)
	require.Error(t, err)
}

func TestResolveDataRootEnv(t *testing.T) {
	t.Setenv("DATA_STORAGE", "")
	t.Setenv("SKIFF_DATA", "")
	t.Setenv("CURIO_DATA", "")
	require.Equal(t, DefaultDataPath, ResolveDataRoot(nil))

	t.Setenv("SKIFF_DATA", "/from-skiff")
	require.Equal(t, "/from-skiff", ResolveDataRoot(nil))

	t.Setenv("DATA_STORAGE", "/from-data")
	require.Equal(t, "/from-data", ResolveDataRoot(nil))
}

func mustCanon(t *testing.T, p string) string {
	t.Helper()
	canon, err := canonicalStoragePath(p)
	require.NoError(t, err)
	return canon
}
