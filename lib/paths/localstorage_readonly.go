package paths

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

// readonlyLocalStorage returns no local paths so NewLocal skips StorageAttach
// and declare writes. Used with CURIO_DB_READONLY / --db-readonly.
type readonlyLocalStorage struct{}

var _ LocalStorage = &readonlyLocalStorage{}

// NewReadonlyLocalStorage returns a LocalStorage that discovers no paths.
func NewReadonlyLocalStorage() LocalStorage {
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
