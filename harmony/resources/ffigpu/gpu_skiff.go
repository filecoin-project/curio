//go:build skiff && !darwin

package ffigpu

// gpuCount reports no GPUs in the skiff (FFI-free) build. HARMONY_OVERRIDE_GPUS,
// if set, is applied by resources.System.
func gpuCount() float64 {
	return 0
}
