//go:build linux

package hashfolder

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	// exclusiveMove tries RENAME_NOREPLACE. A directory stays on this until
	// that flag is refused.
	exclusiveMove = iota
	// hardlinkMove moves by hard link. The directory's filesystem rejected
	// RENAME_NOREPLACE.
	hardlinkMove
	// statMove moves by a check then a plain rename. The directory's
	// filesystem also rejected hard links.
	statMove
)

var (
	folderRenameMu   sync.Mutex
	folderRenameMode = map[string]int{}
)

// renameNoReplace moves oldpath to newpath and leaves an existing newpath
// untouched, reporting os.ErrExist. A directory with a stored move uses that
// move and does not try the others.
func renameNoReplace(oldpath, newpath string) error {
	mode := exclusiveMove
	folderRenameMu.Lock()
	if stored, ok := folderRenameMode[filepath.Dir(oldpath)]; ok {
		mode = stored
	} else if stored, ok := folderRenameMode[filepath.Dir(newpath)]; ok {
		mode = stored
	}
	folderRenameMu.Unlock()

	switch mode {
	case hardlinkMove:
		return renameViaHardlink(oldpath, newpath)
	case statMove:
		return renameViaStat(oldpath, newpath)
	default:
		err := renameViaExclusive(oldpath, newpath)
		// NFS returns EINVAL for any rename flag. Other network filesystems
		// report EOPNOTSUPP for the same refusal.
		if err == nil || (!errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EOPNOTSUPP)) {
			return err
		}
		rememberRenameMode(oldpath, newpath, hardlinkMove, err)
		err = renameViaHardlink(oldpath, newpath)
		if !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EOPNOTSUPP) {
			return err
		}
		// The link call created newpath and then failed to drop oldpath. Hard
		// links work on this filesystem; keep the stored move.
		if same, serr := sameFile(oldpath, newpath); serr == nil && same {
			return err
		}
		rememberRenameMode(oldpath, newpath, statMove, err)
		return renameViaStat(oldpath, newpath)
	}
}

// renameViaExclusive moves oldpath to newpath with RENAME_NOREPLACE. An
// existing newpath is os.ErrExist. A filesystem that rejects the flag returns
// that error to the caller.
func renameViaExclusive(oldpath, newpath string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EEXIST) || errors.Is(err, os.ErrExist) {
		return os.ErrExist
	}
	return err
}

// renameViaHardlink moves oldpath to newpath by adding a second name and
// removing the old one. An existing different newpath is left in place. When
// the two paths are already one file, only the old name is removed.
func renameViaHardlink(oldpath, newpath string) error {
	if err := os.Link(oldpath, newpath); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		same, serr := sameFile(oldpath, newpath)
		if serr != nil {
			if !errors.Is(serr, os.ErrNotExist) {
				return serr
			}
			if _, derr := os.Lstat(newpath); derr != nil {
				return derr
			}
			return os.ErrExist
		}
		if !same {
			return os.ErrExist
		}
	}
	if err := os.Remove(oldpath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// rememberRenameMode stores mode for the directories of oldpath and newpath,
// replacing a mode already stored. Each directory is logged when its mode
// changes. The caller has already chosen mode.
func rememberRenameMode(oldpath, newpath string, mode int, err error) {
	msg := "filesystem rejects RENAME_NOREPLACE; linking instead"
	if mode == statMove {
		msg = "filesystem rejects hard links; renaming without replace is not atomic"
	}
	var fresh []string
	folderRenameMu.Lock()
	for _, dir := range []string{filepath.Dir(oldpath), filepath.Dir(newpath)} {
		if folderRenameMode[dir] == mode {
			continue
		}
		folderRenameMode[dir] = mode
		fresh = append(fresh, dir)
	}
	folderRenameMu.Unlock()
	for _, dir := range fresh {
		log.Warnw(msg, "folder", dir, "error", err)
	}
}

// sameFile reports whether a and b name one file. It returns an error when
// either path cannot be stated.
func sameFile(a, b string) (bool, error) {
	ai, err := os.Lstat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Lstat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}
