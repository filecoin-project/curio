# Testing Supraseal Build Locally

This directory contains tools to test the Supraseal GitHub Actions build workflow locally before pushing to CI.

## Quick Start

```bash
# From the repository root
./.github/workflows/test-supraseal-locally.sh
```

## What It Does

The test script:
1. Builds a Docker image based on Ubuntu 24.04 (same as GitHub Actions)
2. Installs all dependencies including CUDA from NVIDIA's official repository
3. Installs GCC 12, nasm, SPDK dependencies, etc.
4. Runs the exact same build steps as the CI workflow
5. Verifies all binaries are created successfully

## Requirements

- Docker installed and running
- At least 25GB of free disk space (for Docker image + CUDA + build)
- Internet connection (to download CUDA from NVIDIA repository)

## Troubleshooting

### CUDA Installation Issues

CUDA is installed automatically from NVIDIA's official repository during Docker image build.
If you encounter CUDA-related errors:

```bash
# Verify CUDA is installed in the Docker image
docker run --rm supraseal-test:ubuntu24 nvcc --version

# Rebuild the Docker image to get latest CUDA
docker build --no-cache -t supraseal-test:ubuntu24 \
    -f .github/workflows/Dockerfile.supraseal-test \
    .github/workflows/
```

### Build Fails

If the build fails in Docker but works locally:

```bash
# Run the container interactively for debugging
docker run --rm -it \
    -v "$PWD:/workspace" \
    -v /usr/local/cuda-13.0:/usr/local/cuda-13.0:ro \
    supraseal-test:ubuntu24 \
    bash

# Inside the container, manually run build steps:
cd /workspace/extern/supraseal
./build.sh
```

### Clean Build

To force a clean build (remove cached dependencies):

```bash
cd extern/supraseal
rm -rf deps obj bin
./.github/workflows/test-supraseal-locally.sh
```

## Manual Testing Without Docker

If you prefer to test without Docker on Ubuntu 24.04:

```bash
# Install dependencies (requires sudo)
sudo apt-get update && sudo apt-get install -y \
    build-essential gcc-12 g++-12 nasm pkg-config \
    autoconf automake libtool libssl-dev libnuma-dev \
    uuid-dev libaio-dev libfuse3-dev libarchive-dev \
    libkeyutils-dev libncurses-dev python3 python3-pip \
    python3-dev curl wget git xxd libconfig++-dev libgmp-dev

# Install Python tools
pip3 install --break-system-packages meson ninja pyelftools

# Set GCC 12 as default
sudo update-alternatives --install /usr/bin/gcc gcc /usr/bin/gcc-12 100
sudo update-alternatives --install /usr/bin/g++ g++ /usr/bin/g++-12 100

# Build
cd extern/supraseal
./build.sh
```

## CI Workflow Details

The actual CI workflow (`.github/workflows/ci.yml`) includes:

- **Job Name**: `build-supraseal-ubuntu24`
- **Runner**: `ubuntu-24.04`
- **Caching**: CUDA installation and SPDK build are cached
- **Artifacts**: Binaries and library are uploaded for 30 days
- **Triggers**: Runs on all pushes and PRs

## Differences Between Local and CI

1. **CUDA Version**: Both CI and local Docker use the latest CUDA from NVIDIA's official repository
   - The build script supports CUDA 12.x and newer
   - Your host machine may have a different CUDA version (this is fine)
   
2. **Caching**: CI caches CUDA and SPDK builds across runs
   - Local Docker tests rebuild from scratch each time (unless you cache the image)
   
3. **Build Time**: 
   - First CI run: ~45-60 minutes (includes CUDA installation + build)
   - Subsequent CI runs: ~20-30 minutes (with cache)
   - Local Docker: ~60 minutes first time (includes image build + CUDA)
   - Local host: depends on your hardware and existing dependencies

## Support

If you encounter issues not covered here, check:
- Build script: `extern/supraseal/build.sh`
- CI workflow: `.github/workflows/ci.yml`
- Supraseal documentation: `extern/supraseal/README.md`

