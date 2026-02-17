# Curio Makefile variables and overrides

This project uses a modular Makefile layout:

- `Makefile`: entrypoint; includes all `scripts/makefiles/*.mk` fragments.
- `scripts/makefiles/00-vars.mk`: shared defaults and derived variables.
- `scripts/makefiles/05-help.mk`: `make help` and quick usage guidance.
- `scripts/makefiles/10-deps.mk`: dependency bootstrap (`ffi`, `blst`, `supraseal`, submodules).
- `scripts/makefiles/20-test.mk`: test and coverage targets.
- `scripts/makefiles/30-build.mk`: binary build/install/cleanup targets.
- `scripts/makefiles/40-gen.mk`: code/doc generation targets.
- `scripts/makefiles/50-docker.mk`: docker/devnet targets.
- `scripts/makefiles/60-abi.mk`: ABI to Go generation pattern rule.

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
  - Default tags: `CURIO_TAGS_BASE` (`cunative`) plus conditional extras.

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

## Important default context

- On Linux, the default `make build` path does **not** add the `nosupraseal` tag. That means `lib/supraffi` Linux implementations (`//go:build linux && !nosupraseal`) are compiled in.
- On CUDA-capable Linux hosts, Curio can use the SupraSeal-backed fast TreeR path for snap encode. If prerequisites are not met, Curio falls back to filecoin-ffi TreeR logic at runtime.
  - See `lib/ffiselect/ffidirect/ffi-direct.go` (`TreeRFile`) for the fast-path + fallback decision.
- Even if you do not plan to run SupraSeal batch sealing, keeping default (non-`nosupraseal`) builds is still useful for snap encode performance paths.
- On Linux CUDA builds, filecoin-ffi now defaults `FFI_USE_CUDA_SUPRASEAL=1` (FFI-internal feature selection only).

## `FFI_USE_OPENCL` and `FFI_USE_CUDA_SUPRASEAL` behavior by OS and CUDA availability

`FFI_USE_OPENCL` controls Curio-side build routing:

- filecoin-ffi backend selection inputs (`FFI_USE_CUDA`/OpenCL direction),
- whether linux supraseal dependency build is included,
- whether `nosupraseal` is auto-added to `CURIO_TAGS`.

`FFI_USE_CUDA_SUPRASEAL` controls only filecoin-ffi's internal GPU feature choice:

- it does not affect Curio `nosupraseal` tagging,
- it does not affect whether linux `build/.supraseal-install` is included.

Defaults and guardrails:

- `FFI_USE_CUDA` is derived as `1` unless `FFI_USE_OPENCL=1`.
- `FFI_USE_CUDA_SUPRASEAL` defaults to `1` only when `UNAME_S=Linux` and `FFI_USE_CUDA=1`; otherwise default `0`.
- effective value is forced to `0` whenever `FFI_USE_CUDA=0`.

### Behavior matrix

| OS | `nvcc` in `PATH` | `FFI_USE_OPENCL` value | default `FFI_USE_CUDA_SUPRASEAL` | Result |
|---|---|---|---|---|
| Linux | Yes | unset/empty | `1` | Build succeeds. `FFI_USE_CUDA=1`, FFI defaults to `cuda-supraseal`, linux supraseal dep included unless `DISABLE_SUPRASEAL=1`, tags default to `cunative` (or `cunative nosupraseal` when `DISABLE_SUPRASEAL=1`). |
| Linux | No | unset/empty | `1` | Build fails early in `curio-libfilecoin` with CUDA-required error. |
| Linux | Yes/No | `1` | `0` | Build succeeds. `FFI_USE_CUDA=0`, effective `FFI_USE_CUDA_SUPRASEAL=0`, FFI OpenCL path, supraseal dep skipped, `nosupraseal` auto-added to tags, `build.IsOpencl=1`. |
| Linux | Yes | `0` | `1` | Build succeeds. `FFI_USE_CUDA=1` (unambiguous CUDA), FFI defaults to `cuda-supraseal`, supraseal dep included unless `DISABLE_SUPRASEAL=1`. |
| Linux | No | `0` | `1` | Build fails (same CUDA-required guard as default). |
| macOS | Yes/No | unset/empty | `0` | Build succeeds. Darwin filecoin-ffi install path is used, linux supraseal dep block not applicable, tags default to `cunative` (or `cunative nosupraseal` when `DISABLE_SUPRASEAL=1`). |
| macOS | Yes/No | `1` | `0` | Build succeeds. Mainly affects Curio tags/ldflags (`nosupraseal`, `build.IsOpencl=1`). Darwin path does not have Linux CUDA guard. |

Notes:

- On macOS, presence/absence of CUDA libraries does not drive Curio's Makefile decision flow.
- In filecoin-ffi source builds, Darwin already chooses OpenCL feature path by default.
- If either `FFI_USE_OPENCL=1` or `DISABLE_SUPRASEAL=1`, linux `build/.supraseal-install` is not added.
- `FFI_USE_CUDA_SUPRASEAL` can be overridden (for example `FFI_USE_CUDA_SUPRASEAL=0`) without changing Curio tags/dependency gating.

## Detailed `make build` scenarios

This section answers "what exactly is in the binary and what path was taken?" for common cases.

### Quick outcome table

| Scenario | Example command | `CURIO_TAGS` used by `curio`/`sptool` | FFI dependency path | FFI GPU feature | `build/.supraseal-install` step | `build.IsOpencl` ldflag in `curio` |
|---|---|---|---|---|---|---|
| Linux default (CUDA path) | `make build` | `cunative` | Linux `curio-libfilecoin` path, `FFI_USE_CUDA=1` | `cuda-supraseal` (default) | Yes | empty string |
| macOS default | `make build` | `cunative` | Darwin direct `make -C extern/filecoin-ffi .install-filcrypto` | Darwin OpenCL-style default in filecoin-ffi | No (linux-only dep block) | empty string |
| Linux OpenCL path | `make build FFI_USE_OPENCL=1` | `cunative nosupraseal` | Linux `curio-libfilecoin` path, `FFI_USE_CUDA=0` | `opencl` | No | `1` |
| Linux CUDA but disable supraseal in Curio binary | `make build FFI_USE_OPENCL=0 DISABLE_SUPRASEAL=1` | `cunative nosupraseal` | Linux `curio-libfilecoin` path, `FFI_USE_CUDA=1` | `cuda-supraseal` (default unless overridden) | No | `0` |

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
  - defaults `FFI_USE_CUDA_SUPRASEAL=1` on Linux CUDA path
- tags default to `CURIO_TAGS="cunative"` (no `nosupraseal`)
- `curio` compile specifics:
  - `GOAMD64=v3` is set in the build command
  - `CGO_LDFLAGS_ALLOW='.*'` is set on Linux for `curio`
  - `-X github.com/filecoin-project/curio/build.IsOpencl=` (empty)
- `sptool` builds with the same tag set (`cunative`)
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
- tags are still `CURIO_TAGS="cunative"` by default.
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
  - final tags become `CURIO_TAGS="cunative nosupraseal"`
- `curio-libfilecoin` still runs for filecoin-ffi, but:
  - `nvcc` gate is bypassed (`FFI_USE_OPENCL == 1`)
  - `FFI_USE_CUDA=0` is passed
  - effective `FFI_USE_CUDA_SUPRASEAL=0`
- `curio` ldflag embeds `build.IsOpencl=1`.

### 4. Linux + CUDA, but you do not want supraseal in the Curio binary

If your goal is "CUDA filecoin-ffi path, but compile binary without supraseal code", use:

```bash
make build FFI_USE_OPENCL=0 DISABLE_SUPRASEAL=1
```

What this does:

- `FFI_USE_OPENCL=0` keeps CUDA mode explicit (`FFI_USE_CUDA=1`).
- `DISABLE_SUPRASEAL=1` adds `nosupraseal`, so supraffi linux implementations are excluded at compile time.
- linux supraseal dependency build step is skipped.
- FFI still defaults to `FFI_USE_CUDA_SUPRASEAL=1` unless you override it.

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

- Linux CUDA path, but disable filecoin-ffi `cuda-supraseal` feature only:

```bash
make build FFI_USE_OPENCL=0 FFI_USE_CUDA_SUPRASEAL=0
```

- Build with fully explicit tags (overrides computed tag composition):

```bash
make build CURIO_TAGS='cunative debug calibnet'
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
    - unset/empty or any value other than `1`: CUDA path (`FFI_USE_CUDA=1`).
    - `1`: OpenCL path (`FFI_USE_CUDA=0`).
  - Override when:
    - Linux host has no CUDA toolkit (`nvcc`) and you want OpenCL.
    - Running `make gen` is already forced to `FFI_USE_OPENCL=1` by target export.
  - Examples:
    - `make deps FFI_USE_OPENCL=1`
    - `make build FFI_USE_OPENCL=0`

- `FFI_USE_CUDA_SUPRASEAL` (default: computed)
  - Purpose: controls whether filecoin-ffi uses `cuda-supraseal` in CUDA builds.
  - Default behavior:
    - Linux with `FFI_USE_CUDA=1`: defaults to `1`.
    - all other cases: defaults to `0`.
    - if `FFI_USE_CUDA=0`, effective value is forced to `0`.
  - Override when: you want CUDA FFI build path but want the non-`cuda-supraseal` FFI feature set.
  - Example: `make build FFI_USE_OPENCL=0 FFI_USE_CUDA_SUPRASEAL=0`
  - Important: this variable does not affect Curio `nosupraseal` tags or linux supraseal dependency gating.

- `DISABLE_SUPRASEAL` (default: `0`)
  - Purpose: force `nosupraseal` tag and skip Linux supraseal dependency build.
  - Expected values:
    - `0`: default behavior.
    - `1`: disable supraseal in Curio build/tag flow.
  - Override when: Linux host has CUDA and you want CUDA-backed FFI build but a Curio binary compiled without supraseal paths.
  - Example: `make build FFI_USE_OPENCL=0 DISABLE_SUPRASEAL=1`

- `CGO_LDFLAGS_ALLOW` (default: quoted value of `CGO_LDFLAGS_ALLOW_PATTERN`)
  - Purpose: linker flag allowlist for cgo.
  - Override when: integrating custom toolchains requiring different allow patterns.
  - Usually do not override directly; prefer default.

- `CURIO_TAGS_BASE` (default: `cunative`)
  - Purpose: baseline build tags for binaries.
  - Override when: customizing feature profile globally.
  - Example: `make build CURIO_TAGS_BASE='cunative debug'`

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
- `FFI_USE_CUDA`, `FFI_USE_CUDA_SUPRASEAL_EFFECTIVE`
- `CURIO_TAGS_EXTRA`, `CURIO_TAGS_CSV`
- `TEST_ENV_VARS`
- `CGO_LDFLAGS_ALLOW_PATTERN`

If you override internal variables, do so only when actively developing the Makefile itself. If you need these overridden as a regular pattern, consider opening a Github issue or talking with support.

## About `make test`

Current state in this repository:

- `make test` Runs `go test -v -tags="cgo,fvm" -timeout 30m ./itests/...`. It can be used to run integration tests locally.

## Safe override recipes

- Linux without CUDA:
  - `make deps FFI_USE_OPENCL=1`
  - `make build FFI_USE_OPENCL=1`

- Build calibnet binaries with explicit tags:
  - `make calibnet CURIO_TAGS='cunative calibnet'`

- Linux CUDA path but compile Curio without supraseal:
  - `make build FFI_USE_OPENCL=0 DISABLE_SUPRASEAL=1`

- Linux CUDA path but disable only filecoin-ffi `cuda-supraseal`:
  - `make build FFI_USE_OPENCL=0 FFI_USE_CUDA_SUPRASEAL=0`

- Recover from unstable generation cache:
  - `make gen GOCACHE_CLEAN=1`

- Build docker devnet with locally built lotus:
  - `make docker/devnet build_lotus=1 lotus_version=v1.34.2`
