//go:build linux && !nosupraseal

package supraffi

/*
// We need to check if a usable CUDA GPU is available.
// Since we link against cudart_static (via seal.go), we can call cudaGetDeviceCount.
// We declare it manually to avoid dependency on cuda headers location during build.
// cudaError_t is an enum (int), cudaSuccess is 0.

typedef int cudaError_t;
extern cudaError_t cudaGetDeviceCount(int *count);

static int has_usable_cuda_gpu() {
	int count = 0;
	if (cudaGetDeviceCount(&count) != 0) {
		return 0;
	}
	return count > 0 ? 1 : 0;
}
*/
import "C"

import (
	"sync"

	logging "github.com/ipfs/go-log/v2"
)

var logger = logging.Logger("supraffi")

var (
	cudaProbeOnce sync.Once
	cudaProbeOK   bool
)

func HasUsableCUDAGPU() bool {
	cudaProbeOnce.Do(func() {
		// Call the C function which calls cudaGetDeviceCount
		res := C.has_usable_cuda_gpu()
		cudaProbeOK = (res == 1)
		logger.Infof("cuda probe result: %t", cudaProbeOK)
	})

	return cudaProbeOK
}
