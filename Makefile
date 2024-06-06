SHELL=/usr/bin/env bash

GOCC?=go

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

$(MODULES): build/.update-modules ;
# dummy file that marks the last time modules were updated
build/.update-modules:
	git submodule update --init --recursive
	touch $@

# end git modules

## MAIN BINARIES

CLEAN+=build/.update-modules

deps: $(BUILD_DEPS)
.PHONY: deps

curio: $(BUILD_DEPS)
	rm -f curio
	$(GOCC) build $(GOFLAGS) -o curio -ldflags " \
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


calibnet: GOFLAGS+=-tags=calibnet
calibnet: build

debug: GOFLAGS+=-tags=debug
debug: build

2k: GOFLAGS+=-tags=2k
2k: build

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

docsgen: curio sptool
	python3 ./scripts/generate-cli.py
.PHONY: docsgen

# TODO API GEN
# TODO DOCS GEN

# TODO DEVNET IMAGES
##################### Curio devnet images ##################
build_lotus?=0
curio_docker_user?=curio
lotus_base_image=$(curio_docker_user)/lotus-all-in-one:latest-debug
curio_base_image=$(curio_docker_user)/curio-all-in-one:latest-debug
ffi_from_source?=0
lotus_version?=v1.27.0

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

docker/%:
	cd curiosrc/docker/$* && DOCKER_BUILDKIT=1 $(curio_docker_build_cmd) -t $(curio_docker_user)/$*-dev:dev \
		--build-arg BUILD_VERSION=dev .

docker/curio-devnet: $(lotus_build_cmd) \
	docker/curio-all-in-one docker/lotus docker/lotus-miner docker/curio docker/yugabyte
.PHONY: docker/curio-devnet

curio-devnet/up:
	rm -rf ./curiosrc/docker/data && docker compose -f ./curiosrc/docker/docker-compose.yaml up -d

curio-devnet/down:
	docker compose -f ./curiosrc/docker/docker-compose.yaml down --rmi=local && sleep 2 && rm -rf ./curiosrc/docker/data
