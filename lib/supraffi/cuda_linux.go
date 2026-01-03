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
		logger.Warnf("nvcc not found in PATH: %v", err)
		return false
	}

	tmpDir, err := os.MkdirTemp("", "curio-cuda-check-*")
	if err != nil {
		logger.Warnf("failed to create temp directory for CUDA check: %v", err)
		return false
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srcPath := filepath.Join(tmpDir, "cuda_check.cu")
	if err := os.WriteFile(srcPath, []byte(cudaProbeSource), 0o600); err != nil {
		logger.Warnf("failed to write CUDA test source file: %v", err)
		return false
	}

	binPath := filepath.Join(tmpDir, "cuda_check")
	compile := exec.Command(nvccPath, srcPath, "-o", binPath)
	compile.Env = os.Environ()
	if output, err := compile.CombinedOutput(); err != nil {
		logger.Warnf("failed to compile CUDA test program: %v, output: %s", err, string(output))
		return false
	}

	run := exec.Command(binPath)
	run.Env = os.Environ()
	if err := run.Run(); err != nil {
		logger.Warnf("CUDA test program execution failed (no usable CUDA GPU detected): %v", err)
		return false
	}

	return true
}
