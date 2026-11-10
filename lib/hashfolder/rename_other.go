//go:build !linux

package hashfolder

// renameNoReplace moves oldpath to newpath and leaves an existing newpath
// untouched, reporting os.ErrExist.
func renameNoReplace(oldpath, newpath string) error {
	return renameViaStat(oldpath, newpath)
}
