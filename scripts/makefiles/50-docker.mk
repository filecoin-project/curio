# Devnet image build targets.

##################### Curio devnet images ##################
build_lotus ?= 0
curio_docker_user ?= curio
curio_base_image = $(curio_docker_user)/curio-all-in-one:latest-debug
ffi_from_source ?= 0
lotus_version ?= v1.34.2

ifeq ($(build_lotus),1)
# v1: building lotus image with provided lotus version
lotus_info_msg = !!! building lotus base image from github: branch/tag $(lotus_version) !!!
override lotus_src_dir = /tmp/lotus-$(lotus_version)
lotus_build_cmd = update/lotus docker/lotus-all-in-one
lotus_base_image = $(curio_docker_user)/lotus-all-in-one:$(lotus_version)-debug
else
# v2 (default): using prebuilt lotus image
lotus_base_image ?= ghcr.io/filecoin-shipyard/lotus-containers:lotus-$(lotus_version)-devnet
lotus_info_msg = using lotus image from github: $(lotus_base_image)
lotus_build_cmd = info/lotus-all-in-one
endif

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

curio_docker_build_cmd = docker build --build-arg CURIO_TEST_IMAGE=$(curio_base_image) \
	--build-arg FFI_BUILD_FROM_SOURCE=$(ffi_from_source) --build-arg LOTUS_TEST_IMAGE=$(lotus_base_image) $(docker_args)

docker/curio-all-in-one:
	$(curio_docker_build_cmd) -f Dockerfile --target curio-all-in-one \
		-t $(curio_base_image) --build-arg CURIO_TAGS="cunative debug nosupraseal" .
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
