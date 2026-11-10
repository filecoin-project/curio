package hashfolder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenameNoReplaceMovesAndKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "piece", "s-t00-1")
	dst := filepath.Join(dir, "open-pieces", "ab", "cdef")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("piece-bytes"), 0o644))

	require.NoError(t, renameNoReplace(src, dst))
	_, err := os.Lstat(src)
	require.ErrorIs(t, err, os.ErrNotExist)
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, []byte("piece-bytes"), got)

	again := filepath.Join(dir, "piece", "s-t00-2")
	require.NoError(t, os.WriteFile(again, []byte("other"), 0o644))
	require.ErrorIs(t, renameNoReplace(again, dst), os.ErrExist)
	got, err = os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, []byte("piece-bytes"), got)
	left, err := os.ReadFile(again)
	require.NoError(t, err)
	require.Equal(t, []byte("other"), left)
}

func TestRenameNoReplaceStatLeavesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	require.NoError(t, os.WriteFile(src, []byte("src"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("dst"), 0o644))

	require.ErrorIs(t, renameViaStat(src, dst), os.ErrExist)
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, []byte("dst"), got)
	got, err = os.ReadFile(src)
	require.NoError(t, err)
	require.Equal(t, []byte("src"), got)
}
