package pdpnode

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

type readonlyLocalStorage struct{}

var _ paths.LocalStorage = &readonlyLocalStorage{}

func newReadonlyLocalStorage() paths.LocalStorage {
	return &readonlyLocalStorage{}
}

func (ls *readonlyLocalStorage) GetStorage() (storiface.StorageConfig, error) {
	log.Info("readonly database mode: skipping local storage discovery")
	return storiface.StorageConfig{}, nil
}

func (ls *readonlyLocalStorage) SetStorage(func(*storiface.StorageConfig)) error {
	return xerrors.Errorf("storage paths are unavailable in readonly database mode")
}

func (ls *readonlyLocalStorage) Stat(path string) (fsutil.FsStat, error) {
	return fsutil.FsStat{}, xerrors.Errorf("storage paths are unavailable in readonly database mode")
}

func (ls *readonlyLocalStorage) DiskUsage(path string) (int64, error) {
	return 0, xerrors.Errorf("storage paths are unavailable in readonly database mode")
}
