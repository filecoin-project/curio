//go:build skiff

package pdpnode

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

type mountDiscoveredLocalStorage struct{}

var _ paths.LocalStorage = &mountDiscoveredLocalStorage{}

func newLocalStorage(_ string) (paths.LocalStorage, error) {
	return &mountDiscoveredLocalStorage{}, nil
}

func (ls *mountDiscoveredLocalStorage) GetStorage() (storiface.StorageConfig, error) {
	mountPoints, err := listMountPoints()
	if err != nil {
		return storiface.StorageConfig{}, xerrors.Errorf("listing mount points: %w", err)
	}

	cfg, err := pdpStorageConfig(mountPoints)
	if err != nil {
		return storiface.StorageConfig{}, err
	}

	if len(cfg.StoragePaths) == 0 {
		return storiface.StorageConfig{}, xerrors.Errorf("no %s directories found within %d levels of mount points", pdpDataDirName, maxPDPDataSearchDepth)
	}

	log.Infof("discovered %d %s storage path(s)", len(cfg.StoragePaths), pdpDataDirName)
	for _, p := range cfg.StoragePaths {
		log.Infof("  %s", p.Path)
	}

	return cfg, nil
}

func (ls *mountDiscoveredLocalStorage) SetStorage(func(*storiface.StorageConfig)) error {
	return xerrors.Errorf("storage paths are auto-discovered from %s directories on mount points", pdpDataDirName)
}

func (ls *mountDiscoveredLocalStorage) Stat(path string) (fsutil.FsStat, error) {
	return fsutil.Statfs(path)
}

func (ls *mountDiscoveredLocalStorage) DiskUsage(path string) (int64, error) {
	si, err := fsutil.FileSize(path)
	if err != nil {
		return 0, err
	}
	return si.OnDisk, nil
}
