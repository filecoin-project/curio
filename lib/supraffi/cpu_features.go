package supraffi

import (
	"runtime"
	"sync"

	"github.com/klauspost/cpuid/v2"
)

var (
	hasAMD64v4Once sync.Once
	hasAMD64v4     bool

	hasSHAExtOnce sync.Once
	hasSHAExt     bool

	cpuFeaturesOnce sync.Once
	cpuFeatures     CPUFeatures
)

// CPUFeatures contains the detected CPU feature flags relevant for supraseal.
type CPUFeatures struct {
	// AMD64v4 microarchitecture level (AVX512F, AVX512DQ, AVX512BW, AVX512VL)
	HasAMD64v4 bool

	// Intel SHA Extensions (SHA-NI) - required for sha_ext_mbx2
	// Supported on: AMD Zen1+, Intel Ice Lake+
	HasSHAExt bool

	// Basic SIMD features required by sha_ext_mbx2
	HasSSE2  bool
	HasSSSE3 bool
	HasSSE4  bool

	// AVX features (useful for other optimizations)
	HasAVX  bool
	HasAVX2 bool
}

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

// HasSHAExtensions checks if the CPU supports Intel SHA Extensions (SHA-NI).
// This is required for the sha_ext_mbx2 function used in supraseal PC1.
// SHA-NI provides hardware acceleration for SHA-256 operations.
//
// Supported CPUs:
//   - AMD: Zen1 (Ryzen 1000, EPYC 7001) and all later generations
//   - Intel: Ice Lake (10th Gen) and later, some Goldmont Plus Atoms
func HasSHAExtensions() bool {
	hasSHAExtOnce.Do(detectSHAExt)
	return hasSHAExt
}

func detectSHAExt() {
	// Only check on amd64 architecture
	if runtime.GOARCH != "amd64" {
		hasSHAExt = false
		return
	}

	hasSHAExt = cpuid.CPU.Supports(cpuid.SHA)
}

// GetCPUFeatures returns all detected CPU features relevant for supraseal.
// Results are cached after the first call.
func GetCPUFeatures() CPUFeatures {
	cpuFeaturesOnce.Do(detectAllFeatures)
	return cpuFeatures
}

func detectAllFeatures() {
	// Only check on amd64 architecture
	if runtime.GOARCH != "amd64" {
		cpuFeatures = CPUFeatures{}
		return
	}

	cpuFeatures = CPUFeatures{
		// AMD64v4 level (AVX512)
		HasAMD64v4: cpuid.CPU.Supports(cpuid.AVX512F) &&
			cpuid.CPU.Supports(cpuid.AVX512DQ) &&
			cpuid.CPU.Supports(cpuid.AVX512BW) &&
			cpuid.CPU.Supports(cpuid.AVX512VL),

		// SHA Extensions (SHA-NI) - required for sha_ext_mbx2
		HasSHAExt: cpuid.CPU.Supports(cpuid.SHA),

		// Basic SIMD features used by sha_ext_mbx2
		HasSSE2:  cpuid.CPU.Supports(cpuid.SSE2),
		HasSSSE3: cpuid.CPU.Supports(cpuid.SSSE3),
		HasSSE4:  cpuid.CPU.Supports(cpuid.SSE4),

		// AVX features
		HasAVX:  cpuid.CPU.Supports(cpuid.AVX),
		HasAVX2: cpuid.CPU.Supports(cpuid.AVX2),
	}
}

// CanRunSupraSealPC1 checks if the CPU has all features required to run
// supraseal's PC1 implementation (sha_ext_mbx2).
//
// Required features:
//   - Intel SHA Extensions (SHA-NI) for sha256rnds2, sha256msg1, sha256msg2
//   - SSE2 for basic 128-bit SIMD operations
//   - SSSE3 for palignr instruction
//   - SSE4.1 for additional SIMD operations
//
// If this returns false, callers should fall back to filecoin-ffi's
// GenerateSDR implementation.
func CanRunSupraSealPC1() bool {
	f := GetCPUFeatures()
	return f.HasSHAExt && f.HasSSE2 && f.HasSSSE3 && f.HasSSE4
}

// CanRunSupraSeal checks if the CPU has all features required to run
// supraseal operations. This includes PC1 requirements plus AVX512
// for other supraseal optimizations.
//
// If this returns false, callers should fall back to filecoin-ffi.
func CanRunSupraSeal() bool {
	f := GetCPUFeatures()
	// Need SHA extensions for PC1 (sha_ext_mbx2) plus AVX512 for other operations
	return f.HasSHAExt && f.HasSSE2 && f.HasSSSE3 && f.HasSSE4 && f.HasAMD64v4
}

// CPUFeaturesSummary returns a human-readable summary of CPU features
// for logging/debugging purposes.
func CPUFeaturesSummary() string {
	f := GetCPUFeatures()
	return cpuid.CPU.BrandName + " | " +
		"SHA-NI:" + boolStr(f.HasSHAExt) + " " +
		"SSE2:" + boolStr(f.HasSSE2) + " " +
		"SSSE3:" + boolStr(f.HasSSSE3) + " " +
		"SSE4.1:" + boolStr(f.HasSSE4) + " " +
		"AVX:" + boolStr(f.HasAVX) + " " +
		"AVX2:" + boolStr(f.HasAVX2) + " " +
		"AVX512:" + boolStr(f.HasAMD64v4)
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
