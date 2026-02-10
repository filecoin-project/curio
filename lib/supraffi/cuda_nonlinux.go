//go:build !linux || nosupraseal

package supraffi

func HasUsableCUDAGPU() bool {
	return false
}
