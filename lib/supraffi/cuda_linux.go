//go:build linux

package supraffi

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	logging "github.com/ipfs/go-log/v2"
)

var logger = logging.Logger("supraffi")

var (
	cudaProbeOnce sync.Once
	cudaProbeOK   bool
)

const cudaProbeSource = `
#include <cuda_runtime_api.h>

int main() {
	int device_count = 0;
	if (cudaGetDeviceCount(&device_count) != cudaSuccess) {
		return 1;
	}
	return device_count > 0 ? 0 : 1;
}
`

func HasUsableCUDAGPU() bool {
	cudaProbeOnce.Do(func() {
		cudaProbeOK = detectCUDAWithNvcc()
		logger.Infof("cuda probe result: %t", cudaProbeOK)
	})

	return cudaProbeOK
}

func detectCUDAWithNvcc() bool {
	nvccPath, err := exec.LookPath("nvcc")
	if err != nil {
		return false
	}

	tmpDir, err := os.MkdirTemp("", "curio-cuda-check-*")
	if err != nil {
		return false
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srcPath := filepath.Join(tmpDir, "cuda_check.cu")
	if err := os.WriteFile(srcPath, []byte(cudaProbeSource), 0o600); err != nil {
		return false
	}

	binPath := filepath.Join(tmpDir, "cuda_check")
	compile := exec.Command(nvccPath, srcPath, "-o", binPath)
	compile.Env = os.Environ()
	if _, err := compile.CombinedOutput(); err != nil {
		return false
	}

	run := exec.Command(binPath)
	run.Env = os.Environ()
	if err := run.Run(); err != nil {
		return false
	}

	return true
}
