package paths

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	gopath "path"

	"github.com/mitchellh/go-homedir"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

type BasicLocalStorage struct {
	PathToJSON string
}

var _ LocalStorage = &BasicLocalStorage{}

func (ls *BasicLocalStorage) GetStorage() (storiface.StorageConfig, error) {
	var def storiface.StorageConfig
	c, err := StorageFromFile(ls.PathToJSON, &def)
	if err != nil {
		return storiface.StorageConfig{}, err
	}
	return *c, nil
}

func (ls *BasicLocalStorage) SetStorage(f func(*storiface.StorageConfig)) error {
	var def storiface.StorageConfig
	c, err := StorageFromFile(ls.PathToJSON, &def)
	if err != nil {
		return err
	}
	f(c)
	return WriteStorageFile(ls.PathToJSON, *c)
}

func (ls *BasicLocalStorage) Stat(path string) (fsutil.FsStat, error) {
	return fsutil.Statfs(path)
}

func (ls *BasicLocalStorage) DiskUsage(path string) (int64, error) {
	si, err := fsutil.FileSize(path)
	if err != nil {
		return 0, err
	}
	return si.OnDisk, nil
}

func StorageFromFile(path string, def *storiface.StorageConfig) (*storiface.StorageConfig, error) {
	path, err := homedir.Expand(path)
	if err != nil {
		return nil, xerrors.Errorf("expanding storage config path: %w", err)
	}

	file, err := os.Open(path)
	switch {
	case os.IsNotExist(err):
		if def == nil {
			return nil, xerrors.Errorf("couldn't load storage config: %w", err)
		}
		return def, nil
	case err != nil:
		return nil, err
	}

	defer file.Close() //nolint:errcheck // The file is RO
	return StorageFromReader(file)
}

func StorageFromReader(reader io.Reader) (*storiface.StorageConfig, error) {
	var cfg storiface.StorageConfig
	err := json.NewDecoder(reader).Decode(&cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func WriteStorageFile(filePath string, config storiface.StorageConfig) error {
	filePath, err := homedir.Expand(filePath)
	if err != nil {
		return xerrors.Errorf("expanding storage config path: %w", err)
	}

	b, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return xerrors.Errorf("marshaling storage config: %w", err)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return xerrors.Errorf("statting storage config (%s): %w", filePath, err)
		}
		if gopath.Base(filePath) == "." {
			filePath = gopath.Join(filePath, "storage.json")
		}
	} else {
		if info.IsDir() || gopath.Base(filePath) == "." {
			filePath = gopath.Join(filePath, "storage.json")
		}
	}

	if err := os.MkdirAll(gopath.Dir(filePath), 0755); err != nil {
		return xerrors.Errorf("making storage config parent directory: %w", err)
	}
	if err := os.WriteFile(filePath, b, 0644); err != nil {
		return xerrors.Errorf("persisting storage config (%s): %w", filePath, err)
	}

	return nil
}
