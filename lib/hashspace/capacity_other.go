//go:build !linux && !darwin

package hashspace

import "golang.org/x/xerrors"

func filesystemCapacity(root string) (int64, error) {
	return 0, xerrors.Errorf("statfs is unsupported for %s", root)
}
