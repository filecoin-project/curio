//go:build linux

package hashfolder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestRenameNoReplaceLinkMovesAndKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "piece", "s-t00-434401")
	dst := filepath.Join(dir, "open-pieces", "ab", "cdef")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("parked"), 0o644))
	rememberRenameMode(src, dst, hardlinkMove, unix.EINVAL)

	require.NoError(t, renameNoReplace(src, dst))
	_, err := os.Lstat(src)
	require.ErrorIs(t, err, os.ErrNotExist)
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, []byte("parked"), got)
	var st unix.Stat_t
	require.NoError(t, unix.Stat(dst, &st))
	require.Equal(t, uint64(1), uint64(st.Nlink))

	other := filepath.Join(dir, "piece", "s-t00-2")
	require.NoError(t, os.WriteFile(other, []byte("second"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("already"), 0o644))
	require.ErrorIs(t, renameNoReplace(other, dst), os.ErrExist)
	got, err = os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, []byte("already"), got)
	got, err = os.ReadFile(other)
	require.NoError(t, err)
	require.Equal(t, []byte("second"), got)
}

func TestRenameNoReplaceLinkFinishesInterruptedLink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	require.NoError(t, os.WriteFile(src, []byte("same-inode"), 0o644))
	require.NoError(t, os.Link(src, dst))
	rememberRenameMode(src, dst, hardlinkMove, unix.EINVAL)

	require.NoError(t, renameNoReplace(src, dst))
	_, err := os.Lstat(src)
	require.ErrorIs(t, err, os.ErrNotExist)
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, []byte("same-inode"), got)
	var st unix.Stat_t
	require.NoError(t, unix.Stat(dst, &st))
	require.Equal(t, uint64(1), uint64(st.Nlink))
}

func TestRenameModeRememberedPerFolder(t *testing.T) {
	nfs := t.TempDir()
	local := t.TempDir()
	src := filepath.Join(nfs, "piece", "s-t00-1")
	dst := filepath.Join(nfs, "open-pieces", "ab", "cdef")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("parked"), 0o644))
	rememberRenameMode(src, dst, hardlinkMove, unix.EINVAL)

	folderRenameMu.Lock()
	stored := folderRenameMode[filepath.Dir(src)]
	other := folderRenameMode[filepath.Dir(filepath.Join(local, "piece", "s-t00-2"))]
	folderRenameMu.Unlock()
	require.Equal(t, hardlinkMove, stored)
	require.Equal(t, exclusiveMove, other)

	again := filepath.Join(nfs, "piece", "s-t00-3")
	againDst := filepath.Join(nfs, "open-pieces", "cd", "ef")
	require.NoError(t, os.MkdirAll(filepath.Dir(againDst), 0o755))
	require.NoError(t, os.WriteFile(again, []byte("next"), 0o644))
	require.NoError(t, renameNoReplace(again, againDst))
	got, err := os.ReadFile(againDst)
	require.NoError(t, err)
	require.Equal(t, []byte("next"), got)
	_, err = os.Lstat(again)
	require.ErrorIs(t, err, os.ErrNotExist)

	folderRenameMu.Lock()
	shard := folderRenameMode[filepath.Dir(againDst)]
	folderRenameMu.Unlock()
	require.Equal(t, exclusiveMove, shard)
}

func TestRenameNoReplaceStatModeMovesDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "moveme")
	dst := filepath.Join(dir, "moved")
	require.NoError(t, os.Mkdir(src, 0o755))
	rememberRenameMode(src, dst, statMove, unix.EPERM)

	require.NoError(t, renameNoReplace(src, dst))
	info, err := os.Lstat(dst)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	file := filepath.Join(dir, "file")
	moved := filepath.Join(dir, "file-moved")
	require.NoError(t, os.WriteFile(file, []byte("f"), 0o644))
	require.NoError(t, renameNoReplace(file, moved))
	got, err := os.ReadFile(moved)
	require.NoError(t, err)
	require.Equal(t, []byte("f"), got)
}

func TestRenameNoReplaceLinkModeDoesNotMoveDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "moveme")
	dst := filepath.Join(dir, "moved")
	require.NoError(t, os.Mkdir(src, 0o755))
	rememberRenameMode(src, dst, hardlinkMove, unix.EINVAL)

	require.Error(t, renameNoReplace(src, dst))
	info, err := os.Lstat(src)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	_, err = os.Lstat(dst)
	require.ErrorIs(t, err, os.ErrNotExist)
}
