package paths

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

const pathSize = 16 << 20

type TestingLocalStorage struct {
	root string
	c    storiface.StorageConfig
}

func (t *TestingLocalStorage) DiskUsage(path string) (int64, error) {
	return 1, nil
}

func (t *TestingLocalStorage) GetStorage() (storiface.StorageConfig, error) {
	return t.c, nil
}

func (t *TestingLocalStorage) SetStorage(f func(*storiface.StorageConfig)) error {
	f(&t.c)
	return nil
}

func (t *TestingLocalStorage) Stat(path string) (fsutil.FsStat, error) {
	return fsutil.FsStat{
		Capacity:    pathSize,
		Available:   pathSize,
		FSAvailable: pathSize,
	}, nil
}

func (t *TestingLocalStorage) init(subpath string) error {
	path := filepath.Join(t.root, subpath)
	if err := os.Mkdir(path, 0755); err != nil {
		return err
	}

	metaFile := filepath.Join(path, MetaFile)

	meta := &storiface.LocalStorageMeta{
		ID:       storiface.ID(uuid.New().String()),
		Weight:   1,
		CanSeal:  true,
		CanStore: true,
	}

	mb, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(metaFile, mb, 0644); err != nil {
		return err
	}

	return nil
}

var _ LocalStorage = &TestingLocalStorage{}

func TestLocalStorage(t *testing.T) {
	ctx := context.TODO()

	root := t.TempDir()

	tstor := &TestingLocalStorage{
		root: root,
	}

	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	index := NewDBIndex(nil, db)

	st, err := NewLocal(ctx, tstor, index, "")
	require.NoError(t, err)

	p1 := "1"
	require.NoError(t, tstor.init("1"))

	err = st.OpenPath(ctx, filepath.Join(tstor.root, p1))
	require.NoError(t, err)

	// TODO: put more things here
}

func TestExistingLocalFile(t *testing.T) {
	root := t.TempDir()
	sid := abi.SectorID{Miner: 0, Number: 42}

	pieceDir := filepath.Join(root, storiface.FTPiece.String())
	require.NoError(t, os.MkdirAll(pieceDir, 0755))
	fpath := filepath.Join(pieceDir, storiface.SectorName(sid))
	require.NoError(t, os.WriteFile(fpath, []byte("piece-bytes"), 0644))

	emptyRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(emptyRoot, storiface.FTPiece.String()), 0755))

	st := &Local{
		paths: map[storiface.ID]*path{
			"local-1": {Local: root},
			"local-2": {Local: emptyRoot},
		},
	}

	got, ok := st.ExistingLocalFile(sid, storiface.FTPiece)
	require.True(t, ok)
	require.Equal(t, fpath, got)

	_, ok = st.ExistingLocalFile(abi.SectorID{Miner: 0, Number: 99}, storiface.FTPiece)
	require.False(t, ok)

	// Path that denies FTPiece should be skipped even if the file exists.
	st.paths["local-1"].DenyTypes = []string{storiface.FTPiece.String()}
	_, ok = st.ExistingLocalFile(sid, storiface.FTPiece)
	require.False(t, ok)
}
