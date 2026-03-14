# Main build/install/cleanup targets.

## ldflags -s -w strips binary

ifeq ($(UNAME_S),Linux)
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
BINS += curio

ifeq ($(UNAME_S),Linux)
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
BINS += sptool

pdptool: $(BUILD_DEPS)
	rm -f pdptool
	CGO_LDFLAGS_ALLOW=$(CGO_LDFLAGS_ALLOW) $(GOCC) build $(GOFLAGS) -tags "$(CURIO_TAGS)" -o pdptool ./cmd/pdptool
.PHONY: pdptool
BINS += pdptool

## CUZK PROVING DAEMON (Rust, requires CUDA)
## cuzk is a persistent GPU-resident SNARK proving daemon. It is built separately
## from the Go binary because it requires CUDA (nvcc) and the Rust toolchain.
## When CUDA is not available (e.g., CI with OpenCL), this target is skipped.

CUZK_PATH := extern/cuzk
CUZK_BIN := $(CUZK_PATH)/target/release/cuzk-daemon

.PHONY: cuzk
cuzk: build/.update-modules
	@if ! command -v cargo >/dev/null 2>&1; then \
		echo ""; \
		echo "ERROR: cargo (Rust) not found. cuzk requires the Rust toolchain."; \
		echo "Install from https://rustup.rs/"; \
		echo ""; \
		exit 1; \
	fi
	@if ! command -v nvcc >/dev/null 2>&1; then \
		echo ""; \
		echo "ERROR: nvcc not found. cuzk requires the CUDA toolkit."; \
		echo "Install the CUDA toolkit and ensure nvcc is in PATH."; \
		echo ""; \
		exit 1; \
	fi
	cd $(CUZK_PATH) && cargo build --release --bin cuzk-daemon
	cp $(CUZK_BIN) ./cuzk

install-cuzk:
	install -C ./cuzk /usr/local/bin/cuzk

uninstall-cuzk:
	rm -f /usr/local/bin/cuzk

calibnet: CURIO_TAGS += calibnet
calibnet: build

debug: CURIO_TAGS += debug
debug: build

2k: CURIO_TAGS += 2k
2k: build

all: build
.PHONY: all

build: curio sptool
	@[[ $$(type -P "curio") ]] && echo "Caution: you have \
an existing curio binary in your PATH. This may cause problems if you don't run 'sudo make install'" || true
.PHONY: build

calibnet-sptool: CURIO_TAGS += calibnet
calibnet-sptool: sptool

calibnet-curio: CURIO_TAGS += calibnet
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
	rm -rf $(CLEAN) $(BINS) cuzk $(COVERAGE_DIR)
	-$(MAKE) -C $(FFI_PATH) clean
	-cd $(CUZK_PATH) && cargo clean 2>/dev/null
.PHONY: clean

dist-clean:
	git clean -xdff
	git submodule deinit --all -f
.PHONY: dist-clean

install-completions:
	mkdir -p /usr/share/bash-completion/completions /usr/local/share/zsh/site-functions/
	install -C ./scripts/completion/bash_autocomplete /usr/share/bash-completion/completions/curio
	install -C ./scripts/completion/zsh_autocomplete /usr/local/share/zsh/site-functions/_curio

cu2k: CURIO_TAGS += 2k
cu2k: curio

forest-test: GOFLAGS += -tags=forest
forest-test: buildall
