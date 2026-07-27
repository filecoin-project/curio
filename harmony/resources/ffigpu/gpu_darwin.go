//go:build darwin

package ffigpu

// gpuCount returns a likely-true value intended for non-production use on darwin.
func gpuCount() float64 {
	return 1.0
}
