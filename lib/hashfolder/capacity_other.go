//go:build !linux && !darwin

package hashfolder

import "golang.org/x/xerrors"

func filesystemCapacity(root string) (int64, error) {
	return 0, xerrors.Errorf("statfs is unsupported for %s", root)
}

func filesystemFree(root string) (int64, error) {
	return 0, xerrors.Errorf("statfs is unsupported for %s", root)
}
