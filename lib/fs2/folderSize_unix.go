//go:build unix && !darwin

package fs2

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// sumFileSizesRangeSimple walks directory with the standard library.
// Linux uses it when io_uring cannot be created. Other Unix systems use it
// as their only scanner.
//
// kBufSize is accepted for API compatibility and ignored.
func sumFileSizesRangeSimple(directory, low, high string, kBufSize uint32) (Result, error) {
	if err := checkSumArgs(directory, low, high, kBufSize); err != nil {
		return Result{}, err
	}

	var result Result
	err := filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				result.Vanished++
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hash := hashFromRel(rel)
		if d.IsDir() {
			if !subtreeCanMatch(hash, low, high) {
				return filepath.SkipDir
			}
			return nil
		}
		if !hashInRange(hash, low, high) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				result.Vanished++
				return nil
			}
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		result.Bytes += info.Size()
		result.Files++
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("sum file sizes: %w", err)
	}
	return result, nil
}
