# Curio Makefile variables and overrides

This project uses a modular Makefile layout:

- `Makefile`: entrypoint; includes all `mk/*.mk` fragments.
- `mk/00-vars.mk`: shared defaults and derived variables.
- `mk/05-help.mk`: `make help` and quick usage guidance.
- `mk/10-deps.mk`: dependency bootstrap (`ffi`, `blst`, `supraseal`, submodules).
- `mk/20-test.mk`: test and coverage targets.
- `mk/30-build.mk`: binary build/install/cleanup targets.
- `mk/40-gen.mk`: code/doc generation targets.
- `mk/50-docker.mk`: docker/devnet targets.
- `mk/60-abi.mk`: ABI to Go generation pattern rule.

The refactor preserves behavior but makes execution paths easier to reason about.

## How overrides work

You can override variables at invocation time:

```bash
make build FFI_USE_OPENCL=1
make gen GOCC=gotip GOCACHE_CLEAN=1
make docker/devnet build_lotus=1 lotus_version=v1.35.0
```

You can also export them from the environment:

```bash
export GOFLAGS='-mod=mod'
export FFI_USE_OPENCL=1
make build
```

Command-line values take precedence over file defaults.

## Build target matrix

These targets share the same pipeline and differ only in tags:

- `make build`
  - Builds: `curio`, `sptool`
  - Tag behavior: uses `CURIO_TAGS` as-is.
  - Default tags: `CURIO_TAGS_BASE` (`cunative nofvm`) plus conditional extras.

- `make calibnet`
  - Builds: `curio`, `sptool`
  - Tag behavior: appends `calibnet` to `CURIO_TAGS` then runs `build`.

- `make debug`
  - Builds: `curio`, `sptool`
  - Tag behavior: appends `debug` to `CURIO_TAGS` then runs `build`.

- `make 2k`
  - Builds: `curio`, `sptool`
  - Tag behavior: appends `2k` to `CURIO_TAGS` then runs `build`.

Equivalent one-off forms are also available:

- `make calibnet-curio`
- `make calibnet-sptool`
- `make cu2k`

## Detailed `make build` scenarios

This section answers "what exactly is in the binary and what path was taken?" for common cases.

### Quick outcome table

| Scenario | Example command | `CURIO_TAGS` used by `curio`/`sptool` | FFI dependency path | `build/.supraseal-install` step | `build.IsOpencl` ldflag in `curio` |
|---|---|---|---|---|---|
| Linux default (CUDA path) | `make build` | `cunative nofvm` | Linux `curio-libfilecoin` path, `FFI_USE_CUDA=1` | Yes | empty string |
| macOS default | `make build` | `cunative nofvm` | Darwin direct `make -C extern/filecoin-ffi .install-filcrypto` | No (linux-only dep block) | empty string |
| Linux OpenCL path | `make build FFI_USE_OPENCL=1` | `cunative nofvm nosupraseal` | Linux `curio-libfilecoin` path, `FFI_USE_CUDA=0` | No | `1` |
| Linux CUDA but force no supraseal code in binary | `make build FFI_USE_OPENCL= CURIO_TAGS='cunative nofvm nosupraseal'` | `cunative nofvm nosupraseal` | Linux `curio-libfilecoin` path, `FFI_USE_CUDA=1` | Yes (still built) | empty string |

### 1. Linux + `make build` (default CUDA-style path)

Command:

```bash
make build
```

Resulting behavior:

- `build` invokes `curio` and `sptool`.
- `BUILD_DEPS` includes:
  - `setup-cgo-env`
  - `build/.filecoin-install`
  - `ffi-version-check`
  - `build/.blst-install`
  - `build/.supraseal-install` (because linux and `FFI_USE_OPENCL != 1`)
- `build/.filecoin-install` uses `curio-libfilecoin` (linux path), which:
  - requires `nvcc` unless `FFI_USE_OPENCL=1`
  - passes `FFI_USE_CUDA=1` when `FFI_USE_OPENCL` is unset/empty
- tags default to `CURIO_TAGS="cunative nofvm"` (no `nosupraseal`)
- `curio` compile specifics:
  - `GOAMD64=v3` is set in the build command
  - `CGO_LDFLAGS_ALLOW='.*'` is set on Linux for `curio`
  - `-X github.com/filecoin-project/curio/build.IsOpencl=` (empty)
- `sptool` builds with the same tag set (`cunative nofvm`)
  - `sptool` uses the default `CGO_LDFLAGS_ALLOW` pattern (not Linux `curio` override)

### 2. macOS + `make build`

Command:

```bash
make build
```

Resulting behavior:

- `BUILD_DEPS` still runs setup/version/blst checks.
- `build/.filecoin-install` uses Darwin-specific path:
  - `make -C extern/filecoin-ffi .install-filcrypto`
  - it does **not** use `curio-libfilecoin` runtime `nvcc` gate.
- linux-only supraseal dependency block is not added.
- `build/.blst-install` still runs (`bash scripts/build-blst.sh`) because BLST is always in `BUILD_DEPS`.
- tags are still `CURIO_TAGS="cunative nofvm"` by default.
- `curio` ldflag `build.IsOpencl` remains empty unless you set `FFI_USE_OPENCL`.
- even without `nosupraseal` tag, non-linux builds compile non-linux supraffi stubs by file-level build constraints (`!linux`).

### 3. Linux + OpenCL GPU path

Command:

```bash
make build FFI_USE_OPENCL=1
```

Resulting behavior:

- `FFI_USE_OPENCL=1` changes both deps and tags:
  - linux supraseal dependency block is skipped
  - `CURIO_TAGS_EXTRA` adds `nosupraseal`
  - final tags become `CURIO_TAGS="cunative nofvm nosupraseal"`
- `curio-libfilecoin` still runs for filecoin-ffi, but:
  - `nvcc` gate is bypassed (`FFI_USE_OPENCL == 1`)
  - `FFI_USE_CUDA=0` is passed
- `curio` ldflag embeds `build.IsOpencl=1`.

### 4. Linux + CUDA, but you do not want supraseal in the binary

If your goal is "CUDA filecoin-ffi path, but compile binary without supraseal code":

```bash
make build FFI_USE_OPENCL= CURIO_TAGS='cunative nofvm nosupraseal'
```

What this does:

- binary tag set includes `nosupraseal`, so supraffi linux implementations are excluded at compile time.
- because `FFI_USE_OPENCL` is still unset, dependency resolution remains CUDA-style:
  - `curio-libfilecoin` runs with `FFI_USE_CUDA=1`
  - linux supraseal dependency step is still included/built

Important limitation:

- There is currently no single first-class switch for "CUDA path + skip supraseal dependency build entirely".
- `FFI_USE_OPENCL=1` skips supraseal dependency build, but also flips filecoin-ffi path to `FFI_USE_CUDA=0`.

## What `nosupraseal` means

`nosupraseal` is a Go build tag that selects non-supraseal code paths in `lib/supraffi`.

Relevant files:

- supraseal-enabled linux files:
  - `lib/supraffi/seal.go`
  - `lib/supraffi/cuda_linux.go`
  - `lib/supraffi/seal_nvme.go`
  - `lib/supraffi/spdk_setup.go`
- fallback/stub files selected by `!linux || nosupraseal`:
  - `lib/supraffi/seal_nonlinux.go`
  - `lib/supraffi/cuda_nonlinux.go`

Practical effect:

- with `nosupraseal`, linux supraseal integration code is not linked into the binary.
- calls into supraffi functionality will use stub behavior (errors/panics for unavailable operations).
- on non-linux, those stub files are selected even without explicitly adding `nosupraseal`.

## Common `make build` use cases

- Linux host without CUDA toolkit:

```bash
make build FFI_USE_OPENCL=1
```

- Build calibnet binary set with OpenCL path:

```bash
make calibnet FFI_USE_OPENCL=1
```

- Build debug variant with explicit Go flags:

```bash
make debug GOFLAGS='-mod=mod -trimpath'
```

- Build with fully explicit tags (overrides computed tag composition):

```bash
make build CURIO_TAGS='cunative nofvm debug calibnet'
```

- Build host-optimized native curio at a fixed ISA level:

```bash
make curio-native GOAMD64_NATIVE=v2
```

## Variables you are expected to override

### Build and toolchain

- `GOCC` (default: `go`)
  - Purpose: Go command used for `build/run/generate`.
  - Override when: testing with a non-default Go binary (`gotip`, custom wrapper).
  - Example: `make build GOCC=/usr/local/go/bin/go`

- `GOFLAGS` (default: empty)
  - Purpose: standard Go flags propagated into `go build`/`go run`/`go generate`.
  - Override when: changing module behavior, enabling race, changing trimpath, etc.
  - Example: `make build GOFLAGS='-mod=mod -trimpath'`

- `FFI_USE_OPENCL` (default: unset)
  - Purpose: controls GPU backend assumptions for FFI-related dependency builds.
  - Expected values:
    - unset/empty: CUDA path by default.
    - `1`: OpenCL path.
  - Override when:
    - Linux host has no CUDA toolkit (`nvcc`) and you want OpenCL.
    - Running `make gen` is already forced to `FFI_USE_OPENCL=1` by target export.
  - Example: `make deps FFI_USE_OPENCL=1`
  - Important caveat:
    - Some checks use `FFI_USE_OPENCL == 1`.
    - Another check uses "non-empty" (`$(if $(FFI_USE_OPENCL),...)`).
    - Therefore values like `FFI_USE_OPENCL=0` produce mixed behavior and should be avoided.

- `CGO_LDFLAGS_ALLOW` (default: quoted value of `CGO_LDFLAGS_ALLOW_PATTERN`)
  - Purpose: linker flag allowlist for cgo.
  - Override when: integrating custom toolchains requiring different allow patterns.
  - Usually do not override directly; prefer default.

- `CURIO_TAGS_BASE` (default: `cunative nofvm`)
  - Purpose: baseline build tags for binaries.
  - Override when: customizing feature profile globally.
  - Example: `make build CURIO_TAGS_BASE='cunative nofvm debug'`

- `CURIO_TAGS` (default: computed from `CURIO_TAGS_BASE` plus conditional extras)
  - Purpose: final tag string passed to most builds.
  - Override when: you need exact control and do not want computed defaults.
  - Example: `make curio CURIO_TAGS='cunative calibnet debug'`

- `GOAMD64_NATIVE` (default: auto-detected on linux/amd64 via `/proc/cpuinfo`)
  - Purpose: ISA level for `curio-native` target.
  - Override when: reproducibility across hosts or intentionally targeting older CPUs.
  - Example: `make curio-native GOAMD64_NATIVE=v2`

### Generation and caching

- `GOCACHE_CLEAN` (default: unset)
  - Purpose: when set to `1`, `make gensimple` runs `go clean -cache` first.
  - Override when: generation repeatedly fails due Go build cache contention/corruption.
  - Example: `make gen GOCACHE_CLEAN=1`

### Testing and coverage

- `COVERAGE_DIR` (default: `coverage`)
  - Purpose: output directory for coverage artifacts.
  - Override when: CI artifact path conventions differ.
  - Example: `make cov COVERAGE_DIR=build/coverage`

### Docker/devnet

- `build_lotus` (default: `0`)
  - Purpose:
    - `0`: use prebuilt lotus image.
    - `1`: clone/build lotus image locally.
  - Override when: testing against a custom lotus branch/tag implementation.

- `lotus_version` (default: `v1.34.2`)
  - Purpose: lotus version for prebuilt image tag or local clone branch/tag.
  - Override when: validating compatibility with another lotus version.

- `curio_docker_user` (default: `curio`)
  - Purpose: image name prefix for locally built images.
  - Override when: publishing/testing in your own registry namespace.

- `curio_base_image` (default: `$(curio_docker_user)/curio-all-in-one:latest-debug`)
  - Purpose: base image reference passed into docker builds.
  - Override when: pinning to an alternate base image/tag.

- `ffi_from_source` (default: `0`)
  - Purpose: docker build arg controlling FFI source build behavior in image builds.
  - Override when: image tests require source-built FFI.

- `lotus_base_image` (default when `build_lotus=0`: prebuilt GHCR image)
  - Purpose: lotus image consumed by curio docker build flow.
  - Override when: testing against a custom already-built lotus image.

- `docker_args` (default: empty)
  - Purpose: appended raw args to docker build commands.
  - Override when: injecting platform/buildkit/cache args.
  - Example: `make docker/curio docker_args='--platform linux/amd64 --no-cache'`

## Variables that are internal/derived (usually do not override)

- `FFI_PATH`, `BLST_PATH`, `SUPRA_FFI_PATH`
- `BUILD_DEPS`, `MODULES`, `BINS`, `CLEAN`
- `UNAME_S`, `NVCC_PATH`, `CUDA_PATH`, `CUDA_LIB_PATH`
- `CURIO_TAGS_EXTRA`, `CURIO_TAGS_CSV`
- `TEST_ENV_VARS`
- `CGO_LDFLAGS_ALLOW_PATTERN`

If you override internal variables, do so only when actively developing the Makefile itself.

## About `make test`

Current state in this repository:

- `make test` Runs `go test -v -tags="cgo,fvm" -timeout 30m ./itests/...`. It can be used to run integration tests locally.

## Safe override recipes

- Linux without CUDA:
  - `make deps FFI_USE_OPENCL=1`
  - `make build FFI_USE_OPENCL=1`

- Build calibnet binaries with explicit tags:
  - `make calibnet CURIO_TAGS='cunative nofvm calibnet'`

- Recover from unstable generation cache:
  - `make gen GOCACHE_CLEAN=1`

- Build docker devnet with locally built lotus:
  - `make docker/devnet build_lotus=1 lotus_version=v1.34.2`
