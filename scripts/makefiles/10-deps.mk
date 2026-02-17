# Dependency/bootstrap targets: submodules, ffi/blst/supraseal.

## FILECOIN-FFI

FFI_DEPS := .install-filcrypto
FFI_DEPS := $(addprefix $(FFI_PATH),$(FFI_DEPS))

$(FFI_DEPS): build/.filecoin-install ;

.PHONY: setup-cgo-env
setup-cgo-env:
	@current=$$(go env CGO_LDFLAGS_ALLOW); \
	if [ "$$current" != "$(CGO_LDFLAGS_ALLOW_PATTERN)" ]; then \
		echo "Configuring go to allow $(CGO_LDFLAGS_ALLOW_PATTERN)"; \
		go env -w CGO_LDFLAGS_ALLOW='$(CGO_LDFLAGS_ALLOW_PATTERN)'; \
	fi

build/.filecoin-install: $(FFI_PATH)
ifeq ($(UNAME_S),Darwin)
	$(MAKE) -C $(FFI_PATH) $(FFI_DEPS:$(FFI_PATH)%=%)
else
	$(MAKE) curio-libfilecoin
endif
	@touch $@

MODULES += $(FFI_PATH)
BUILD_DEPS += build/.filecoin-install
CLEAN += build/.filecoin-install

## Custom libfilcrypto build for Curio (size-optimized, no FVM)
## By default, requires CUDA on Linux. Set FFI_USE_OPENCL=1 to build with OpenCL instead.
.PHONY: curio-libfilecoin
curio-libfilecoin:
	@if [ "$$(uname)" = "Linux" ] && [ "$(FFI_USE_OPENCL)" != "1" ] && ! command -v nvcc >/dev/null 2>&1; then \
		echo ""; \
		echo "ERROR: nvcc not found but CUDA build is required for Curio on Linux."; \
		echo ""; \
		echo "Please either:"; \
		echo "  1. Install the CUDA toolkit (nvcc must be in PATH), or"; \
		echo "  2. Build with OpenCL instead: make FFI_USE_OPENCL=1 build"; \
		echo ""; \
		exit 1; \
	fi
	FFI_BUILD_FROM_SOURCE=1 \
	FFI_USE_GPU=1 \
	FFI_USE_CUDA=$(if $(FFI_USE_OPENCL),0,1) \
	FFI_USE_MULTICORE_SDR=1 \
	RUSTFLAGS='-C codegen-units=1 -C opt-level=3 -C strip=symbols' \
	$(MAKE) -C $(FFI_PATH) .install-filcrypto

ffi-version-check:
	@[[ "$$(awk '/const Version/{print $$5}' extern/filecoin-ffi/version.go)" -eq 3 ]] || (echo "FFI version mismatch, update submodules"; exit 1)
BUILD_DEPS += ffi-version-check
.PHONY: ffi-version-check

## BLST (from supraseal, but needed in curio)

BLST_DEPS := .install-blst
BLST_DEPS := $(addprefix $(BLST_PATH),$(BLST_DEPS))

$(BLST_DEPS): build/.blst-install ;

build/.blst-install: $(BLST_PATH)
	bash scripts/build-blst.sh
	@touch $@

BUILD_DEPS += build/.blst-install
CLEAN += build/.blst-install

## SUPRA-FFI

ifeq ($(UNAME_S),Linux)
ifneq ($(FFI_USE_OPENCL),1)
SUPRA_FFI_DEPS := .install-supraseal
SUPRA_FFI_DEPS := $(addprefix $(SUPRA_FFI_PATH),$(SUPRA_FFI_DEPS))

$(SUPRA_FFI_DEPS): build/.supraseal-install ;

build/.supraseal-install: $(SUPRA_FFI_PATH)
	cd $(SUPRA_FFI_PATH) && ./build.sh
	@touch $@

BUILD_DEPS += build/.supraseal-install
CLEAN += build/.supraseal-install
endif
endif

# Submodule synchronization marker.
$(MODULES): build/.update-modules ;
build/.update-modules:
	git submodule update --init --recursive
	touch $@

CLEAN += build/.update-modules

deps: $(BUILD_DEPS)
.PHONY: deps
