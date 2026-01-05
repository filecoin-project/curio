//go:build !linux

package supraffi

func HasUsableCUDAGPU() bool {
	return false
}
