package hashfolder

import "os"

// renameViaStat moves oldpath to newpath when newpath is absent. A file
// created after that check is replaced. Callers use this when the filesystem
// provides no exclusive rename and no hard link.
func renameViaStat(oldpath, newpath string) error {
	if _, err := os.Lstat(newpath); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(oldpath, newpath)
}
