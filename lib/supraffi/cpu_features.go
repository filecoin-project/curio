package supraffi

import (
	"runtime"
	"sync"

	"github.com/klauspost/cpuid/v2"
)

var (
	hasAMD64v4Once sync.Once
	hasAMD64v4     bool
)

// HasAMD64v4 checks if the CPU supports AMD64v4 microarchitecture level.
// AMD64v4 requires AVX512F, AVX512DQ, AVX512BW, and AVX512VL.
// This is required for supraseal operations.
func HasAMD64v4() bool {
	hasAMD64v4Once.Do(detectAMD64v4)
	return hasAMD64v4
}

func detectAMD64v4() {
	// Only check on amd64 architecture
	if runtime.GOARCH != "amd64" {
		hasAMD64v4 = false
		return
	}

	// Use klauspost/cpuid to detect AVX512 features
	// AMD64v4 requires AVX512F, AVX512DQ, AVX512BW, and AVX512VL
	hasAMD64v4 = cpuid.CPU.Supports(cpuid.AVX512F) &&
		cpuid.CPU.Supports(cpuid.AVX512DQ) &&
		cpuid.CPU.Supports(cpuid.AVX512BW) &&
		cpuid.CPU.Supports(cpuid.AVX512VL)
}
