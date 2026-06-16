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

	shallow := filepath.Join(root, "curio-pdp-data")
	deep := filepath.Join(root, "a", "b", "curio-pdp-data")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	require.NoError(t, os.MkdirAll(shallow, 0o755))

	got, err := findCurioPDPDataOnMount(root, maxPDPDataSearchDepth)
	require.NoError(t, err)
	require.Equal(t, shallow, got)
}

func TestFindCurioPDPDataOnMountRespectsDepth(t *testing.T) {
	root := t.TempDir()
	tooDeep := filepath.Join(root, "a", "b", "c", "d", "curio-pdp-data")
	require.NoError(t, os.MkdirAll(tooDeep, 0o755))

	got, err := findCurioPDPDataOnMount(root, maxPDPDataSearchDepth)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestDiscoverPDPStoragePathsOnePerMount(t *testing.T) {
	mountA := t.TempDir()
	mountB := t.TempDir()

	pathA := filepath.Join(mountA, "curio-pdp-data")
	pathB := filepath.Join(mountB, "nested", "curio-pdp-data")
	require.NoError(t, os.MkdirAll(pathA, 0o755))
	require.NoError(t, os.MkdirAll(pathB, 0o755))

	paths, err := discoverPDPStoragePaths([]string{mountA, mountB, mountA})
	require.NoError(t, err)
	require.Equal(t, []string{pathA, pathB}, paths)
}

func TestPDPStorageConfig(t *testing.T) {
	mount := t.TempDir()
	path := filepath.Join(mount, "curio-pdp-data")
	require.NoError(t, os.MkdirAll(path, 0o755))

	cfg, err := pdpStorageConfig([]string{mount})
	require.NoError(t, err)
	require.Len(t, cfg.StoragePaths, 1)
	require.Equal(t, path, cfg.StoragePaths[0].Path)

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
