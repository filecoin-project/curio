//go:build !((linux || darwin) && cgo)

package fs2

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SumFileSizesRange sums logical file sizes for regular files under directory
// whose concatenated hash paths compare in the bytewise interval (low, high].
// An empty low or high bound leaves that side of the interval open.
//
// QueueDepth is accepted for API compatibility and ignored.
func SumFileSizesRange(directory, low, high string, queueDepth uint32) (Result, error) {
	if err := checkSumArgs(directory, low, high, queueDepth); err != nil {
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

		mode := d.Type()
		if mode != 0 && !mode.IsRegular() {
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
		if info.Size() < 0 {
			return fmt.Errorf("stat %s: negative size", rel)
		}
		size := uint64(info.Size())
		if result.Bytes > ^uint64(0)-size {
			return fmt.Errorf("sum overflow at %s", rel)
		}
		result.Bytes += size
		result.Files++
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("sum file sizes: %w", err)
	}
	return result, nil
}
