SHELL=/usr/bin/env bash

GOCC?=go

## FILECOIN-FFI

FFI_PATH:=extern/filecoin-ffi/
FFI_DEPS:=.install-filcrypto
FFI_DEPS:=$(addprefix $(FFI_PATH),$(FFI_DEPS))

$(FFI_DEPS): build/.filecoin-install ;

build/.filecoin-install: $(FFI_PATH)
	$(MAKE) -C $(FFI_PATH) $(FFI_DEPS:$(FFI_PATH)%=%)
	@touch $@

MODULES+=$(FFI_PATH)
BUILD_DEPS+=build/.filecoin-install
CLEAN+=build/.filecoin-install

ffi-version-check:
	@[[ "$$(awk '/const Version/{print $$5}' extern/filecoin-ffi/version.go)" -eq 3 ]] || (echo "FFI version mismatch, update submodules"; exit 1)
BUILD_DEPS+=ffi-version-check

.PHONY: ffi-version-check

## BLST (from supraseal, but needed in curio)

BLST_PATH:=extern/supra_seal/
BLST_DEPS:=.install-blst
BLST_DEPS:=$(addprefix $(BLST_PATH),$(BLST_DEPS))

$(BLST_DEPS): build/.blst-install ;

build/.blst-install: $(BLST_PATH)
	bash scripts/build-blst.sh
	@touch $@

MODULES+=$(BLST_PATH)
BUILD_DEPS+=build/.blst-install
CLEAN+=build/.blst-install

## SUPRA-FFI

ifeq ($(shell uname),Linux)
SUPRA_FFI_PATH:=extern/supra_seal/
SUPRA_FFI_DEPS:=.install-supraseal
SUPRA_FFI_DEPS:=$(addprefix $(SUPRA_FFI_PATH),$(SUPRA_FFI_DEPS))

$(SUPRA_FFI_DEPS): build/.supraseal-install ;

build/.supraseal-install: $(SUPRA_FFI_PATH)
	cd $(SUPRA_FFI_PATH) && ./build.sh
	@touch $@

# MODULES+=$(SUPRA_FFI_PATH) -- already included in BLST_PATH
CLEAN+=build/.supraseal-install
endif

$(MODULES): build/.update-modules ;
# dummy file that marks the last time modules were updated
build/.update-modules:
	git submodule update --init --recursive
	touch $@

# end git modules

## CUDA Library Path
$(eval CUDA_PATH := $(shell dirname $$(dirname $$(which nvcc))))
$(eval CUDA_LIB_PATH := $(CUDA_PATH)/lib64)
export LIBRARY_PATH=$(CUDA_LIB_PATH)

## MAIN BINARIES

CLEAN+=build/.update-modules

deps: $(BUILD_DEPS)
.PHONY: deps

## ldflags -s -w strips binary

curio: $(BUILD_DEPS)
	rm -f curio
	GOAMD64=v3 CGO_LDFLAGS_ALLOW=$(CGO_LDFLAGS_ALLOW) $(GOCC) build $(GOFLAGS) -o curio -ldflags " -s -w \
	-X github.com/filecoin-project/curio/build.IsOpencl=$(FFI_USE_OPENCL) \
	-X github.com/filecoin-project/curio/build.CurrentCommit=+git_`git log -1 --format=%h_%cI`" \
	./cmd/curio
.PHONY: curio
BINS+=curio

sptool: $(BUILD_DEPS)
	rm -f sptool
	$(GOCC) build $(GOFLAGS) -o sptool ./cmd/sptool
.PHONY: sptool
BINS+=sptool

ifeq ($(shell uname),Linux)

batchdep: build/.supraseal-install
batchdep: $(BUILD_DEPS)
.PHONY: batchdep

batch: GOFLAGS+=-tags=supraseal
batch: CGO_LDFLAGS_ALLOW='.*'
batch: batchdep build
.PHONY: batch

batch-calibnet: GOFLAGS+=-tags=calibnet,supraseal
batch-calibnet: CGO_LDFLAGS_ALLOW='.*'
batch-calibnet: batchdep build
.PHONY: batch-calibnet

else
batch:
	@echo "Batch target is only available on Linux systems"
	@exit 1

batch-calibnet:
	@echo "Batch-calibnet target is only available on Linux systems"
	@exit 1
endif

calibnet: GOFLAGS+=-tags=calibnet
calibnet: build

debug: GOFLAGS+=-tags=debug
debug: build

2k: GOFLAGS+=-tags=2k
2k: build

all: build 
.PHONY: all

build: curio sptool
	@[[ $$(type -P "curio") ]] && echo "Caution: you have \
an existing curio binary in your PATH. This may cause problems if you don't run 'sudo make install'" || true

.PHONY: build

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

cu2k: GOFLAGS+=-tags=2k
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
	./curio config default >> documentation/en/configuration/default-curio-configuration.md
	echo '```' >> documentation/en/configuration/default-curio-configuration.md
.PHONY: docsgen-cli

go-generate:
	$(GOCC) generate ./...
.PHONY: go-generate

gen: gensimple
.PHONY: gen

gensimple: go-generate cfgdoc-gen api-gen docsgen docsgen-cli
	$(GOCC) run ./scripts/fiximports
	go mod tidy
.PHONY: gen

forest-test: GOFLAGS+=-tags=forest
forest-test: buildall

##################### Curio devnet images ##################
build_lotus?=0
curio_docker_user?=curio
curio_base_image=$(curio_docker_user)/curio-all-in-one:latest-debug
ffi_from_source?=0
lotus_version?=v1.28.1

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
		-t $(curio_base_image) --build-arg GOFLAGS=-tags=debug .
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

docker/devnet: $(lotus_build_cmd) docker/curio-all-in-one docker/lotus docker/lotus-miner docker/curio docker/yugabyte
.PHONY: docker/devnet

devnet/up:
	rm -rf ./docker/data && docker compose -f ./docker/docker-compose.yaml up -d

devnet/down:
	docker compose -f ./docker/docker-compose.yaml down --rmi=local && sleep 2 && rm -rf ./docker/data
