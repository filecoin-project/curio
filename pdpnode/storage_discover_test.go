package pdpnode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/lib/storiface"
)

func TestFindCurioPDPDataOnMount(t *testing.T) {
	root := t.TempDir()

	shallow := filepath.Join(root, "filecoin-hot-data")
	deep := filepath.Join(root, "a", "b", "filecoin-hot-data")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	require.NoError(t, os.MkdirAll(shallow, 0o755))

	got, err := findCurioPDPDataOnMount(root, maxPDPDataSearchDepth)
	require.NoError(t, err)
	require.Equal(t, shallow, got)
}

func TestFindCurioPDPDataOnMountIgnoresUnreadableDirs(t *testing.T) {
	root := t.TempDir()

	blocked := filepath.Join(root, "blocked")
	require.NoError(t, os.MkdirAll(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	target := filepath.Join(root, "other", "filecoin-hot-data")
	require.NoError(t, os.MkdirAll(target, 0o755))

	got, err := findCurioPDPDataOnMount(root, maxPDPDataSearchDepth)
	require.NoError(t, err)
	require.Equal(t, target, got)
}

func TestFindCurioPDPDataOnMountRespectsDepth(t *testing.T) {
	root := t.TempDir()
	tooDeep := filepath.Join(root, "a", "b", "c", "d", "filecoin-hot-data")
	require.NoError(t, os.MkdirAll(tooDeep, 0o755))

	got, err := findCurioPDPDataOnMount(root, maxPDPDataSearchDepth)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestDiscoverPDPStoragePathsDedupesSymlinks(t *testing.T) {
	root := t.TempDir()
	mountA := filepath.Join(root, "a")
	require.NoError(t, os.MkdirAll(filepath.Join(mountA, "filecoin-hot-data"), 0o755))
	mountB := filepath.Join(root, "b")
	require.NoError(t, os.Symlink(mountA, mountB))

	paths, err := discoverPDPStoragePaths([]string{mountA, mountB})
	require.NoError(t, err)
	require.Len(t, paths, 1)
}

func TestDiscoverPDPStoragePathsOnePerMount(t *testing.T) {
	mountA := t.TempDir()
	mountB := t.TempDir()

	pathA := filepath.Join(mountA, "filecoin-hot-data")
	pathB := filepath.Join(mountB, "nested", "filecoin-hot-data")
	require.NoError(t, os.MkdirAll(pathA, 0o755))
	require.NoError(t, os.MkdirAll(pathB, 0o755))

	paths, err := discoverPDPStoragePaths([]string{mountA, mountB, mountA})
	require.NoError(t, err)
	require.Equal(t, []string{mustCanon(t, pathA), mustCanon(t, pathB)}, paths)
}

func mustCanon(t *testing.T, p string) string {
	t.Helper()
	canon, err := canonicalStoragePath(p)
	require.NoError(t, err)
	return canon
}

func TestPDPStorageConfig(t *testing.T) {
	mount := t.TempDir()
	path := filepath.Join(mount, "filecoin-hot-data")
	require.NoError(t, os.MkdirAll(path, 0o755))

	cfg, err := pdpStorageConfig([]string{mount})
	require.NoError(t, err)
	require.Len(t, cfg.StoragePaths, 1)
	require.Equal(t, mustCanon(t, path), cfg.StoragePaths[0].Path)

	metaPath := filepath.Join(path, "sectorstore.json")
	require.FileExists(t, metaPath)
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
