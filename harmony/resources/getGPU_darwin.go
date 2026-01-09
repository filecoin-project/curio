//go:build darwin

package resources

var GpuOverprovisionFactor = 1

func getGPUDevices() (float64, error) {
	return 1.0, nil // likely-true value intended for non-production use.
}
