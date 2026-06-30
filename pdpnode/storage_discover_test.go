package pdpnode

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

	require.NoError(t, os.MkdirAll(filepath.Join(root, skiffHotDataDirName), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested", skiffHotDataDirName), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "other"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested", "other"), 0o755))

	got, err := discoverWritableStoragePaths(root)
	require.NoError(t, err)
	require.Equal(t, []string{
		mustCanon(t, root),
		mustCanon(t, filepath.Join(root, skiffHotDataDirName)),
		mustCanon(t, filepath.Join(root, "nested", skiffHotDataDirName)),
	}, got)
}

func TestDiscoverWritableStoragePathsSkipsSectorLayoutDirs(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "sealed", "s-t01234-1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "unsealed", "s-t01234-1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cache", "s-t01234-1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "fetching", "tmp"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "stash"), 0o755))

	got, err := discoverWritableStoragePaths(root)
	require.NoError(t, err)
	require.Equal(t, []string{mustCanon(t, root)}, got)
}

func TestDiscoverWritableStoragePathsRespectsMaxDepth(t *testing.T) {
	root := t.TempDir()

	deep := filepath.Join(root, "l1", "l2", "l3", skiffHotDataDirName)
	require.NoError(t, os.MkdirAll(deep, 0o755))

	got, err := discoverWritableStoragePaths(root)
	require.NoError(t, err)
	require.Equal(t, []string{mustCanon(t, root)}, got)
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
	hot := filepath.Join(root, skiffHotDataDirName)
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

func mustCanon(t *testing.T, p string) string {
	t.Helper()
	canon, err := canonicalStoragePath(p)
	require.NoError(t, err)
	return canon
}
