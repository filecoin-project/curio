# Shared variables and defaults.

SHELL := /usr/bin/env bash
GOCC ?= go

# External module locations.
FFI_PATH := extern/filecoin-ffi/
BLST_PATH := extern/supraseal/
SUPRA_FFI_PATH := extern/supraseal/

# Build linker flag allowlist used by cgo-related targets.
CGO_LDFLAGS_ALLOW_PATTERN := (-Wl,--whole-archive|-Wl,--no-as-needed|-Wl,--no-whole-archive|-Wl,--allow-multiple-definition|--whole-archive|--no-as-needed|--no-whole-archive|--allow-multiple-definition)
CGO_LDFLAGS_ALLOW ?= "$(CGO_LDFLAGS_ALLOW_PATTERN)"
export CGO_LDFLAGS_ALLOW
TEST_ENV_VARS := CGO_LDFLAGS_ALLOW=$(CGO_LDFLAGS_ALLOW)

# Aggregate lists populated by included fragments.
BUILD_DEPS := setup-cgo-env
MODULES :=
BINS :=
CLEAN :=

# Host OS identifier for parse-time branching.
UNAME_S := $(shell uname)

# CUDA library path setup for Linux hosts with nvcc present.
ifeq ($(UNAME_S),Linux)
NVCC_PATH := $(shell which nvcc 2>/dev/null)
ifneq ($(NVCC_PATH),)
$(eval CUDA_PATH := $(shell dirname $$(dirname $$(which nvcc))))
$(eval CUDA_LIB_PATH := $(CUDA_PATH)/lib64)
export LIBRARY_PATH := $(LIBRARY_PATH):$(CUDA_LIB_PATH)
endif
endif

# Coverage defaults.
COVERAGE_DIR ?= coverage
COVERAGE_PROFILE = $(COVERAGE_DIR)/coverage.out
COVERAGE_HTML = $(COVERAGE_DIR)/coverage.html

# Toggle to force curio binary build without supraseal code paths.
# 0 = default behavior, 1 = add nosupraseal tag and skip supraseal dependency build.
DISABLE_SUPRASEAL ?= 0

# FFI backend selection.
# 1 = CUDA-style FFI build path, 0 = OpenCL-style FFI build path.
FFI_USE_CUDA ?= $(if $(filter 1,$(FFI_USE_OPENCL)),0,1)

# FFI-only switch for filecoin-ffi's cuda-supraseal feature.
# Enabled by default only when building on Linux with CUDA enabled.
FFI_USE_CUDA_SUPRASEAL ?= $(if $(and $(filter Linux,$(UNAME_S)),$(filter 1,$(FFI_USE_CUDA))),1,0)

# Guardrail: cuda-supraseal cannot apply when CUDA is disabled.
FFI_USE_CUDA_SUPRASEAL_EFFECTIVE = $(if $(filter 1,$(FFI_USE_CUDA)),$(FFI_USE_CUDA_SUPRASEAL),0)

# Build tags.
CURIO_TAGS_BASE ?= cunative
CURIO_NOSUPRASEAL = $(if $(filter 1,$(FFI_USE_OPENCL) $(DISABLE_SUPRASEAL)),1,)
CURIO_TAGS_EXTRA = $(if $(CURIO_NOSUPRASEAL),nosupraseal,)
CURIO_TAGS ?= $(strip $(CURIO_TAGS_BASE) $(CURIO_TAGS_EXTRA))
# Convert space-separated tags to comma-separated for GOFLAGS (whitespace-split).
CURIO_TAGS_CSV = $(shell echo "$(CURIO_TAGS)" | tr ' ' ',')

# Native-curio binary ISA level (linux/amd64 auto-detect).
# Override manually if desired, e.g. make curio-native GOAMD64_NATIVE=v3
GOAMD64_NATIVE ?= $(shell \
	if [ "$$(go env GOARCH)" = "amd64" ] && [ -r /proc/cpuinfo ]; then \
		if grep -qm1 'avx512f' /proc/cpuinfo; then echo v4; \
		elif grep -qm1 'avx2' /proc/cpuinfo; then echo v3; \
		elif grep -qm1 'sse4_2' /proc/cpuinfo; then echo v2; \
		else echo v1; fi; \
	fi)
