//go:build darwin
// +build darwin

package resources

var GpuOverprovisionFactor = 1

func getGPUDevices() float64 {
	return 1.0 // likely-true value intended for non-production use.
}
