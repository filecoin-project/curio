SHELL=/usr/bin/env bash

GOCC?=go

## FILECOIN-FFI

FFI_PATH:=extern/filecoin-ffi/
FFI_DEPS:=.install-filcrypto
FFI_DEPS:=$(addprefix $(FFI_PATH),$(FFI_DEPS))

$(FFI_DEPS): build/.filecoin-install ;

# When enabled, build size-optimized libfilcrypto by default
CURIO_OPTIMAL_LIBFILCRYPTO ?= 1
CGO_LDFLAGS_ALLOW_PATTERN := (-Wl,--whole-archive|-Wl,--no-as-needed|-Wl,--no-whole-archive|-Wl,--allow-multiple-definition|--whole-archive|--no-as-needed|--no-whole-archive|--allow-multiple-definition)
CGO_LDFLAGS_ALLOW ?= "$(CGO_LDFLAGS_ALLOW_PATTERN)"
export CGO_LDFLAGS_ALLOW

TEST_ENV_VARS := CGO_LDFLAGS_ALLOW=$(CGO_LDFLAGS_ALLOW)
BUILD_DEPS := setup-cgo-env

.PHONY: setup-cgo-env
setup-cgo-env:
	@current=$$(go env CGO_LDFLAGS_ALLOW); \
	if [ "$$current" != "$(CGO_LDFLAGS_ALLOW_PATTERN)" ]; then \
		echo "Configuring go to allow $(CGO_LDFLAGS_ALLOW_PATTERN)"; \
		go env -w CGO_LDFLAGS_ALLOW='$(CGO_LDFLAGS_ALLOW_PATTERN)'; \
	fi

build/.filecoin-install: $(FFI_PATH)
	@if [ "$(CURIO_OPTIMAL_LIBFILCRYPTO)" = "1" ]; then \
		$(MAKE) curio-libfilecoin; \
	else \
		$(MAKE) -C $(FFI_PATH) $(FFI_DEPS:$(FFI_PATH)%=%); \
	fi
	@touch $@

MODULES+=$(FFI_PATH)
BUILD_DEPS+=build/.filecoin-install
CLEAN+=build/.filecoin-install

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
	FFI_DISABLE_FVM=1 \
	RUSTFLAGS='-C codegen-units=1 -C opt-level=3 -C strip=symbols' \
	$(MAKE) -C $(FFI_PATH) clean .install-filcrypto

ffi-version-check:
	@[[ "$$(awk '/const Version/{print $$5}' extern/filecoin-ffi/version.go)" -eq 3 ]] || (echo "FFI version mismatch, update submodules"; exit 1)
BUILD_DEPS+=ffi-version-check

.PHONY: ffi-version-check

## BLST (from supraseal, but needed in curio)

BLST_PATH:=extern/supraseal/
BLST_DEPS:=.install-blst
BLST_DEPS:=$(addprefix $(BLST_PATH),$(BLST_DEPS))

$(BLST_DEPS): build/.blst-install ;

build/.blst-install: $(BLST_PATH)
	bash scripts/build-blst.sh
	@touch $@

BUILD_DEPS+=build/.blst-install
CLEAN+=build/.blst-install

## SUPRA-FFI

ifeq ($(shell uname),Linux)
ifneq ($(FFI_USE_OPENCL),1)

SUPRA_FFI_PATH:=extern/supraseal/
SUPRA_FFI_DEPS:=.install-supraseal
SUPRA_FFI_DEPS:=$(addprefix $(SUPRA_FFI_PATH),$(SUPRA_FFI_DEPS))

$(SUPRA_FFI_DEPS): build/.supraseal-install ;

build/.supraseal-install: $(SUPRA_FFI_PATH)
	cd $(SUPRA_FFI_PATH) && ./build.sh
	@touch $@

BUILD_DEPS+=build/.supraseal-install
CLEAN+=build/.supraseal-install

endif
endif

$(MODULES): build/.update-modules ;
# dummy file that marks the last time modules were updated
build/.update-modules:
	git submodule update --init --recursive
	touch $@

# end git modules

# CUDA Library Path
# Conditional execution block for Linux
OS := $(shell uname)
ifeq ($(OS), Linux)
    NVCC_PATH := $(shell which nvcc 2>/dev/null)
    ifneq ($(NVCC_PATH),)
        $(eval CUDA_PATH := $(shell dirname $$(dirname $$(which nvcc))))
        $(eval CUDA_LIB_PATH := $(CUDA_PATH)/lib64)
        export LIBRARY_PATH := $(LIBRARY_PATH):$(CUDA_LIB_PATH)
    endif
endif

## MAIN BINARIES

CLEAN+=build/.update-modules

deps: $(BUILD_DEPS)
.PHONY: deps

## Test targets

test-deps: CURIO_OPTIMAL_LIBFILCRYPTO=0
test-deps: $(BUILD_DEPS)
	@echo "Built dependencies with FVM support for testing"
.PHONY: test-deps

test: test-deps
	$(TEST_ENV_VARS) go test -v -tags="cgo,fvm" -timeout 30m ./itests/...
.PHONY: test

## Coverage targets

COVERAGE_DIR ?= coverage
COVERAGE_PROFILE = $(COVERAGE_DIR)/coverage.out
COVERAGE_HTML = $(COVERAGE_DIR)/coverage.html

coverage: cov
.PHONY: coverage

cov:
	@mkdir -p $(COVERAGE_DIR)
	go test -coverprofile=$(COVERAGE_PROFILE) -covermode=atomic ./...
	go tool cover -html=$(COVERAGE_PROFILE) -o $(COVERAGE_HTML)
	@echo ""
	@echo "Coverage report generated:"
	@echo "  Profile: $(COVERAGE_PROFILE)"
	@echo "  HTML:    $(COVERAGE_HTML)"
	@echo ""
	@echo "Opening coverage report..."
	@which xdg-open > /dev/null 2>&1 && xdg-open $(COVERAGE_HTML) || open $(COVERAGE_HTML) || echo "Open $(COVERAGE_HTML) in your browser"
.PHONY: cov

## ldflags -s -w strips binary

CURIO_TAGS_BASE ?= cunative nofvm
CURIO_TAGS_EXTRA = $(if $(filter 1,$(FFI_USE_OPENCL)),nosupraseal,)
CURIO_TAGS = $(strip $(CURIO_TAGS_BASE) $(CURIO_TAGS_EXTRA))

# Convert space-separated tags to comma-separated for GOFLAGS (which is whitespace-split)
CURIO_TAGS_CSV = $(shell echo "$(CURIO_TAGS)" | tr ' ' ',')

ifeq ($(shell uname),Linux)
curio: CGO_LDFLAGS_ALLOW='.*'
endif

curio: $(BUILD_DEPS)
	rm -f curio
	GOAMD64=v3 CGO_LDFLAGS_ALLOW=$(CGO_LDFLAGS_ALLOW) $(GOCC) build $(GOFLAGS) \
	-tags "$(CURIO_TAGS)" \
	-o curio -ldflags " -s -w \
	-X github.com/filecoin-project/curio/build.IsOpencl=$(FFI_USE_OPENCL) \
	-X github.com/filecoin-project/curio/build.CurrentCommit=+git_`git log -1 --format=%h_%cI`" \
	./cmd/curio
.PHONY: curio
BINS+=curio

## Native-curio binary (host-optimized ISA; may not run on older CPUs)
#
# For amd64, automatically selects the highest GOAMD64 level supported by the
# build host CPU:
# - v4: AVX-512 (avx512f)
# - v3: AVX2 (avx2)
# - v2: SSE4.2 (sse4_2)
# - v1: baseline amd64
#
# Override manually if desired:
#   make curio-native GOAMD64_NATIVE=v3
GOAMD64_NATIVE ?= $(shell \
	if [ "$$(go env GOARCH)" = "amd64" ] && [ -r /proc/cpuinfo ]; then \
		if grep -qm1 'avx512f' /proc/cpuinfo; then echo v4; \
		elif grep -qm1 'avx2' /proc/cpuinfo; then echo v3; \
		elif grep -qm1 'sse4_2' /proc/cpuinfo; then echo v2; \
		else echo v1; fi; \
	fi)

ifeq ($(shell uname),Linux)
curio-native: CGO_LDFLAGS_ALLOW='.*'
endif

curio-native: $(BUILD_DEPS)
	rm -f curio
	if [ -n "$(GOAMD64_NATIVE)" ]; then \
		echo "Building curio-native with GOAMD64=$(GOAMD64_NATIVE)"; \
		GOAMD64="$(GOAMD64_NATIVE)" CGO_LDFLAGS_ALLOW=$(CGO_LDFLAGS_ALLOW) $(GOCC) build $(GOFLAGS) \
			-tags "$(CURIO_TAGS)" \
			-o curio -ldflags " -s -w \
			-X github.com/filecoin-project/curio/build.IsOpencl=$(FFI_USE_OPENCL) \
			-X github.com/filecoin-project/curio/build.CurrentCommit=+git_`git log -1 --format=%h_%cI`" \
			./cmd/curio ; \
	else \
		echo "Building curio-native (non-amd64; GOAMD64 not applicable)"; \
		CGO_LDFLAGS_ALLOW=$(CGO_LDFLAGS_ALLOW) $(GOCC) build $(GOFLAGS) \
			-tags "$(CURIO_TAGS)" \
			-o curio -ldflags " -s -w \
			-X github.com/filecoin-project/curio/build.IsOpencl=$(FFI_USE_OPENCL) \
			-X github.com/filecoin-project/curio/build.CurrentCommit=+git_`git log -1 --format=%h_%cI`" \
			./cmd/curio ; \
	fi
.PHONY: curio-native

sptool: $(BUILD_DEPS)
	rm -f sptool
	CGO_LDFLAGS_ALLOW=$(CGO_LDFLAGS_ALLOW) $(GOCC) build $(GOFLAGS) -tags "$(CURIO_TAGS)" -o sptool ./cmd/sptool
.PHONY: sptool
BINS+=sptool

pdptool: $(BUILD_DEPS)
	rm -f pdptool
	CGO_LDFLAGS_ALLOW=$(CGO_LDFLAGS_ALLOW) $(GOCC) build $(GOFLAGS) -tags "$(CURIO_TAGS)" -o pdptool ./cmd/pdptool
.PHONY: pdptool
BINS+=pdptool


calibnet: CURIO_TAGS+= calibnet
calibnet: build

debug: CURIO_TAGS+= debug
debug: build

2k: CURIO_TAGS+= 2k
2k: build

all: build 
.PHONY: all

build: curio sptool
	@[[ $$(type -P "curio") ]] && echo "Caution: you have \
an existing curio binary in your PATH. This may cause problems if you don't run 'sudo make install'" || true

.PHONY: build


calibnet-sptool: CURIO_TAGS+= calibnet
calibnet-sptool: sptool

calibnet-curio: CURIO_TAGS+= calibnet
calibnet-curio: curio

install: install-curio install-sptool
.PHONY: install

install-curio:
	install -C ./curio /usr/local/bin/curio

install-sptool:
	install -C ./sptool /usr/local/bin/sptool

uninstall: uninstall-curio uninstall-sptool
.PHONY: uninstall

uninstall-curio:
	rm -f /usr/local/bin/curio

uninstall-sptool:
	rm -f /usr/local/bin/sptool

# TODO move systemd?

buildall: $(BINS)

clean:
	rm -rf $(CLEAN) $(BINS) $(COVERAGE_DIR)
	-$(MAKE) -C $(FFI_PATH) clean
.PHONY: clean

dist-clean:
	git clean -xdff
	git submodule deinit --all -f
.PHONY: dist-clean

install-completions:
	mkdir -p /usr/share/bash-completion/completions /usr/local/share/zsh/site-functions/
	install -C ./scripts/completion/bash_autocomplete /usr/share/bash-completion/completions/curio
	install -C ./scripts/completion/zsh_autocomplete /usr/local/share/zsh/site-functions/_curio

cu2k: CURIO_TAGS+= 2k
cu2k: curio

cfgdoc-gen:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./deps/config/cfgdocgen > ./deps/config/doc_gen.go

fix-imports:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./scripts/fiximports

docsgen: docsgen-md docsgen-openrpc
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen

docsgen-md: docsgen-md-curio
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-md

api-gen:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./api/gen/api/proxygen.go
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: api-gen

docsgen-md-curio: docsgen-md-bin
	echo '---' > documentation/en/api.md
	echo 'description: Curio API references' >> documentation/en/api.md
	echo '---' >> documentation/en/api.md
	echo '' >> documentation/en/api.md
	echo '# API' >> documentation/en/api.md
	echo '' >> documentation/en/api.md
	./docgen-md "api/api_curio.go" "Curio" "api" "./api" >> documentation/en/api.md
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: api-gen

docsgen-md-bin: api-gen
	$(GOCC) build $(GOFLAGS) -tags="$(CURIO_TAGS)" -o docgen-md ./scripts/docgen/cmd
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-md-bin

docsgen-openrpc: docsgen-openrpc-curio
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-openrpc

docsgen-openrpc-bin: api-gen 
	$(GOCC) build $(GOFLAGS) -tags="$(CURIO_TAGS)" -o docgen-openrpc ./api/docgen-openrpc/cmd

docsgen-openrpc-curio: docsgen-openrpc-bin
	./docgen-openrpc "api/api_curio.go" "Curio" "api" "./api" > build/openrpc/curio.json

docsgen-cli: curio sptool
	python3 ./scripts/generate-cli.py
	echo '---' > documentation/en/configuration/default-curio-configuration.md
	echo 'description: The default curio configuration' >> documentation/en/configuration/default-curio-configuration.md
	echo '---' >> documentation/en/configuration/default-curio-configuration.md
	echo '' >> documentation/en/configuration/default-curio-configuration.md
	echo '# Default Curio Configuration' >> documentation/en/configuration/default-curio-configuration.md
	echo '' >> documentation/en/configuration/default-curio-configuration.md
	echo '```toml' >> documentation/en/configuration/default-curio-configuration.md
	LANG=en-US ./curio config default >> documentation/en/configuration/default-curio-configuration.md
	echo '```' >> documentation/en/configuration/default-curio-configuration.md
.PHONY: docsgen-cli

docsgen-metrics:
	$(GOCC) run $(GOFLAGS) ./scripts/metricsdocgen > documentation/en/configuration/metrics-reference.md
.PHONY: docsgen-metrics

translation-gen:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./scripts/translationcheck
.PHONY: translation-gen

go-generate:
	@bash -lc 'set -euo pipefail; \
	  CGO_ALLOW="$(subst ",,$(CGO_LDFLAGS_ALLOW))"; \
	  GO_FLAGS="$(GOFLAGS) -tags=$(CURIO_TAGS_CSV)"; \
	  for p in $$(go list ./...); do \
	    tf="$$(mktemp -t go-gen-time.XXXXXX)"; \
	    cmd=(env CGO_LDFLAGS_ALLOW="$$CGO_ALLOW" GOFLAGS="$$GO_FLAGS" $(GOCC) generate "$$p"); \
	    printf "CMD: "; printf "%q " "$${cmd[@]}"; echo ""; \
	    if /usr/bin/time -p -o "$$tf" "$${cmd[@]}"; then \
	      : ; \
	    else \
	      rc="$$?"; \
	      echo "FAILED: $$p (exit $$rc)"; \
	      grep "^real " "$$tf" || true; \
	      rm -f "$$tf" || true; \
	      exit "$$rc"; \
	    fi; \
	    echo "### timing for $$p ###"; \
	    grep "^real " "$$tf"; \
	    rm -f "$$tf"; \
	  done'
.PHONY: go-generate

gen: gensimple
.PHONY: gen

marketgen:
	swag init -dir market/mk20/http -g http.go  -o market/mk20/http --parseDependencyLevel 3 --parseDependency
.PHONY: marketgen

gen-deps: CURIO_OPTIMAL_LIBFILCRYPTO=0
gen-deps: $(BUILD_DEPS)
	@echo "Built dependencies with FVM support for testing"
.PHONY: gen-deps

# Run gen steps sequentially in a single shell to avoid Go build cache race conditions.
# The "unlinkat: directory not empty" error occurs when multiple go processes
# contend for the same build cache simultaneously.
# Set GOCACHE_CLEAN=1 to clear the build cache before running (fixes persistent issues).
gensimple: export FFI_USE_OPENCL=1
gensimple:
ifeq ($(GOCACHE_CLEAN),1)
	$(GOCC) clean -cache
endif
	@bash -lc '\
		set -euo pipefail; \
		t() { name="$$1"; shift; \
			start=$$(date +%s); \
			"$$@"; \
			end=$$(date +%s); \
			echo "TIMING $$name: $$((end-start))s"; \
		}; \
		t gen-deps    $(MAKE) gen-deps; \
		t api-gen     $(MAKE) api-gen; \
		t go-generate $(MAKE) go-generate; \
		t translation-gen $(MAKE) translation-gen; \
		t cfgdoc-gen  $(MAKE) cfgdoc-gen; \
		t docsgen $(MAKE) docsgen; \
		t marketgen   $(MAKE) marketgen; \
		t docsgen-cli $(MAKE) docsgen-cli; \
		t docsgen-metrics $(MAKE) docsgen-metrics; \
	'
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./scripts/fiximports
.PHONY: gensimple

fiximports:
	$(GOCC) run $(GOFLAGS) -tags="$(CURIO_TAGS)" ./scripts/fiximports
.PHONY: fiximports

forest-test: GOFLAGS+=-tags=forest
forest-test: buildall

##################### Curio devnet images ##################
build_lotus?=0
curio_docker_user?=curio
curio_base_image=$(curio_docker_user)/curio-all-in-one:latest-debug
ffi_from_source?=0
lotus_version?=v1.34.0-rc2

ifeq ($(build_lotus),1)
# v1: building lotus image with provided lotus version
	lotus_info_msg=!!! building lotus base image from github: branch/tag $(lotus_version) !!!
	override lotus_src_dir=/tmp/lotus-$(lotus_version)
	lotus_build_cmd=update/lotus docker/lotus-all-in-one
	lotus_base_image=$(curio_docker_user)/lotus-all-in-one:$(lotus_version)-debug
else
# v2 (default): using prebuilt lotus image
	lotus_base_image?=ghcr.io/filecoin-shipyard/lotus-containers:lotus-$(lotus_version)-devnet
	lotus_info_msg=using lotus image from github: $(lotus_base_image)
	lotus_build_cmd=info/lotus-all-in-one
endif
#docker_build_cmd=docker build --build-arg LOTUS_TEST_IMAGE=$(lotus_base_image) \
#	--build-arg FFI_BUILD_FROM_SOURCE=$(ffi_from_source) $(docker_args)
### lotus-all-in-one docker image build
info/lotus-all-in-one:
	@echo Docker build info: $(lotus_info_msg)
.PHONY: info/lotus-all-in-one
### checkout/update lotus if needed
$(lotus_src_dir):
	git clone --depth 1 --branch $(lotus_version) https://github.com/filecoin-project/lotus $@
update/lotus: $(lotus_src_dir)
	cd $(lotus_src_dir) && git pull
.PHONY: update/lotus

docker/lotus-all-in-one: info/lotus-all-in-one | $(lotus_src_dir)
	cd $(lotus_src_dir) && $(curio_docker_build_cmd) -f Dockerfile --target lotus-all-in-one \
		-t $(lotus_base_image) --build-arg GOFLAGS=-tags=debug .
.PHONY: docker/lotus-all-in-one

curio_docker_build_cmd=docker build --build-arg CURIO_TEST_IMAGE=$(curio_base_image) \
	--build-arg FFI_BUILD_FROM_SOURCE=$(ffi_from_source) --build-arg LOTUS_TEST_IMAGE=$(lotus_base_image) $(docker_args)

docker/curio-all-in-one:
	$(curio_docker_build_cmd) -f Dockerfile --target curio-all-in-one \
		-t $(curio_base_image) --build-arg CURIO_TAGS="cunative debug" .
.PHONY: docker/curio-all-in-one

docker/lotus:
	cd docker/lotus && DOCKER_BUILDKIT=1 $(curio_docker_build_cmd) -t $(curio_docker_user)/lotus-dev:dev \
		--build-arg BUILD_VERSION=dev .
.PHONY: docker/lotus

docker/lotus-miner:
	cd docker/lotus-miner && DOCKER_BUILDKIT=1 $(curio_docker_build_cmd) -t $(curio_docker_user)/lotus-miner-dev:dev \
		--build-arg BUILD_VERSION=dev .
.PHONY: docker/lotus-miner

docker/curio:
	cd docker/curio && DOCKER_BUILDKIT=1 $(curio_docker_build_cmd) -t $(curio_docker_user)/curio-dev:dev \
		--build-arg BUILD_VERSION=dev .
.PHONY: docker/curio

docker/piece-server:
	cd docker/piece-server && DOCKER_BUILDKIT=1 $(curio_docker_build_cmd) -t $(curio_docker_user)/piece-server-dev:dev \
		--build-arg BUILD_VERSION=dev .
.PHONY: docker/piece-server

docker/indexer:
	cd docker/indexer && DOCKER_BUILDKIT=1 $(curio_docker_build_cmd) -t $(curio_docker_user)/indexer-dev:dev \
		--build-arg BUILD_VERSION=dev .
.PHONY: docker/indexer

docker/devnet: $(lotus_build_cmd) docker/curio-all-in-one docker/lotus docker/lotus-miner docker/curio docker/piece-server docker/indexer
.PHONY: docker/devnet

devnet/up:
	rm -rf ./docker/data && docker compose -f ./docker/docker-compose.yaml up -d

devnet/down:
	docker compose -f ./docker/docker-compose.yaml down --rmi=local && sleep 2 && rm -rf ./docker/data
