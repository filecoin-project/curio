//go:build skiff

package pdpnode

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

type dataRootLocalStorage struct {
	dataRoot string
}

var _ paths.LocalStorage = &dataRootLocalStorage{}

func newLocalStorage(dataRoot string) (paths.LocalStorage, error) {
	return &dataRootLocalStorage{dataRoot: dataRoot}, nil
}

func (ls *dataRootLocalStorage) GetStorage() (storiface.StorageConfig, error) {
	cfg, err := pdpStorageConfig(ls.dataRoot)
	if err != nil {
		return storiface.StorageConfig{}, err
	}

	if len(cfg.StoragePaths) == 0 {
		return storiface.StorageConfig{}, xerrors.Errorf("no writable storage directories found under %s", ls.dataRoot)
	}

	log.Infof("discovered %d writable storage path(s) under %s", len(cfg.StoragePaths), ls.dataRoot)
	for _, p := range cfg.StoragePaths {
		log.Infof("  %s", p.Path)
	}

	return cfg, nil
}

func (ls *dataRootLocalStorage) SetStorage(func(*storiface.StorageConfig)) error {
	return xerrors.Errorf("storage paths are auto-discovered under %s", ls.dataRoot)
}

func (ls *dataRootLocalStorage) Stat(path string) (fsutil.FsStat, error) {
	return fsutil.Statfs(path)
}

func (ls *dataRootLocalStorage) DiskUsage(path string) (int64, error) {
	si, err := fsutil.FileSize(path)
	if err != nil {
		return 0, err
	}
	return si.OnDisk, nil
}

func pdpStorageConfig(dataRoot string) (storiface.StorageConfig, error) {
	storagePaths, err := discoverWritableStoragePaths(dataRoot)
	if err != nil {
		return storiface.StorageConfig{}, err
	}

	cfg := storiface.StorageConfig{
		StoragePaths: make([]storiface.LocalPath, 0, len(storagePaths)),
	}
	for _, p := range storagePaths {
		if err := ensureSectorstoreJSON(p); err != nil {
			return storiface.StorageConfig{}, err
		}
		cfg.StoragePaths = append(cfg.StoragePaths, storiface.LocalPath{Path: p})
	}

	return cfg, nil
}
