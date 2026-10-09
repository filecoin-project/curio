//go:build unix && !linux && !darwin

package fs2

// SumFileSizesRange sums logical file sizes for regular files under directory
// whose concatenated hash paths compare in the bytewise interval (low, high].
// An empty low or high bound leaves that side of the interval open.
//
// QueueDepth is accepted for API compatibility and ignored. Linux and Darwin
// use their own scanners; this walk covers the other Unix systems.
//
// Performance is poor: see comments in the other implementations.
func SumFileSizesRange(directory, low, high string, queueDepth uint32) (Result, error) {
	return sumFileSizesRangeSimple(directory, low, high, queueDepth)
}
