//go:build maxboom && !darwin

package resources

func getGPUDevices() float64 {
	if n, ok := gpuOverrideCount(); ok {
		return n
	}
	return 0
}
