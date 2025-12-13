SHELL=/usr/bin/env bash

GOCC?=go

## FILECOIN-FFI

FFI_PATH:=extern/filecoin-ffi/
FFI_DEPS:=.install-filcrypto
FFI_DEPS:=$(addprefix $(FFI_PATH),$(FFI_DEPS))

$(FFI_DEPS): build/.filecoin-install ;

# When enabled, build size-optimized libfilcrypto by default
CURIO_OPTIMAL_LIBFILCRYPTO ?= 1

build/.filecoin-install: $(FFI_PATH)
	@if [ "$(CURIO_OPTIMAL_LIBFILCRYPTO)" = "1" ]; then \
		FFI_DISABLE_FVM=1 $(MAKE) curio-libfilecoin; \
	else \
		$(MAKE) -C $(FFI_PATH) $(FFI_DEPS:$(FFI_PATH)%=%); \
	fi
	@touch $@

MODULES+=$(FFI_PATH)
BUILD_DEPS+=build/.filecoin-install
CLEAN+=build/.filecoin-install

## Custom libfilcrypto build for Curio (size-optimized, no FVM)
.PHONY: curio-libfilecoin
curio-libfilecoin:
	FFI_BUILD_FROM_SOURCE=1 \
	FFI_USE_GPU=1 \
	FFI_USE_MULTICORE_SDR=1 \
	RUSTFLAGS='-C codegen-units=1 -C opt-level=3 -C strip=symbols' \
	$(MAKE) -C $(FFI_PATH) clean .install-filcrypto
	@echo "Rebuilt libfilcrypto for Curio (OpenCL+multicore, no default features)."

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
	go test -v -tags="cgo,fvm" -timeout 30m ./itests/...
.PHONY: test

## ldflags -s -w strips binary

CURIO_TAGS ?= cunative

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

sptool: $(BUILD_DEPS)
	rm -f sptool
	$(GOCC) build $(GOFLAGS) -tags "$(CURIO_TAGS)" -o sptool ./cmd/sptool
.PHONY: sptool
BINS+=sptool

pdptool: $(BUILD_DEPS)
	rm -f pdptool
	$(GOCC) build $(GOFLAGS) -tags "$(CURIO_TAGS)" -o pdptool ./cmd/pdptool
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
	rm -rf $(CLEAN) $(BINS)
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
	$(GOCC) run ./deps/config/cfgdocgen > ./deps/config/doc_gen.go

fix-imports:
	$(GOCC) run ./scripts/fiximports

docsgen: docsgen-md docsgen-openrpc
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen

docsgen-md: docsgen-md-curio
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-md

api-gen:
	$(GOCC) run ./api/gen/api/proxygen.go
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
	$(GOCC) build $(GOFLAGS) -o docgen-md ./scripts/docgen/cmd
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-md-bin

docsgen-openrpc: docsgen-openrpc-curio
	@echo "FixImports will run only from the 'make gen' target"
.PHONY: docsgen-openrpc

docsgen-openrpc-bin: api-gen 
	$(GOCC) build $(GOFLAGS) -o docgen-openrpc ./api/docgen-openrpc/cmd

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

go-generate:
	$(GOCC) generate ./...
.PHONY: go-generate

gen: gensimple
.PHONY: gen

marketgen:
	swag init -dir market/mk20/http -g http.go  -o market/mk20/http --parseDependencyLevel 3 --parseDependency
.PHONY: marketgen

# Run gen steps sequentially in a single shell to avoid Go build cache race conditions.
# The "unlinkat: directory not empty" error occurs when multiple go processes
# contend for the same build cache simultaneously.
# Set GOCACHE_CLEAN=1 to clear the build cache before running (fixes persistent issues).
gensimple:
ifeq ($(GOCACHE_CLEAN),1)
	$(GOCC) clean -cache
endif
	$(MAKE) deps
	$(MAKE) api-gen
	$(MAKE) go-generate
	$(MAKE) cfgdoc-gen
	$(MAKE) docsgen
	$(MAKE) marketgen
	$(MAKE) docsgen-cli
	$(GOCC) run ./scripts/fiximports
	go mod tidy
.PHONY: gensimple

fiximports:
	$(GOCC) run ./scripts/fiximports
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
