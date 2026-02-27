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

cu2k: CURIO_TAGS += 2k
cu2k: curio

forest-test: GOFLAGS += -tags=forest
forest-test: buildall
