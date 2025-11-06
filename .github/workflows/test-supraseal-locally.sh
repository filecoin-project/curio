#!/bin/bash

# Local testing script for Supraseal CI workflow
# This mimics what GitHub Actions does

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "============================================"
echo "Testing Supraseal Build Locally"
echo "============================================"
echo ""

# Build the Docker image
echo "Step 1: Building Docker image..."
docker build -t supraseal-test:ubuntu24 -f "$SCRIPT_DIR/Dockerfile.supraseal-test" "$SCRIPT_DIR"

echo ""
echo "Step 2: Running build in Docker container..."
echo ""

# Run the container with:
# - Mount the repo
# - Set environment variables (CUDA is installed in the image from NVIDIA repos)
docker run --rm -it \
    -v "$REPO_ROOT:/workspace" \
    -e CC=gcc-12 \
    -e CXX=g++-12 \
    -e CUDA=/usr/local/cuda \
    -e PATH=/usr/local/cuda/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    -e LD_LIBRARY_PATH=/usr/local/cuda/lib64 \
    -e PIP_BREAK_SYSTEM_PACKAGES=1 \
    -w /workspace \
    supraseal-test:ubuntu24 \
    bash -c '
        set -e
        
        echo "=== Environment ==="
        echo "GCC Version:"
        gcc --version | head -1
        echo ""
        echo "G++ Version:"
        g++ --version | head -1
        echo ""
        echo "NVCC Version:"
        nvcc --version | grep release || echo "CUDA not available"
        echo ""
        
        echo "=== Initializing submodules ==="
        git config --global --add safe.directory /workspace
        git submodule update --init --recursive
        echo ""
        
        echo "=== Building Supraseal ==="
        cd extern/supraseal
        ./build.sh
        echo ""
        
        echo "=== Verifying binaries ==="
        ls -lh bin/
        echo ""
        
        test -f bin/seal && echo "✓ seal binary created" || { echo "✗ seal binary missing"; exit 1; }
        test -f bin/pc2 && echo "✓ pc2 binary created" || { echo "✗ pc2 binary missing"; exit 1; }
        test -f bin/tree_r && echo "✓ tree_r binary created" || { echo "✗ tree_r binary missing"; exit 1; }
        test -f bin/tree_r_cpu && echo "✓ tree_r_cpu binary created" || { echo "✗ tree_r_cpu binary missing"; exit 1; }
        test -f bin/tree_d_cpu && echo "✓ tree_d_cpu binary created" || { echo "✗ tree_d_cpu binary missing"; exit 1; }
        
        echo ""
        echo "=== Binary sizes ==="
        du -h bin/*
        echo ""
        
        echo "✅ All binaries built successfully!"
    '

echo ""
echo "============================================"
echo "✅ Local test completed successfully!"
echo "============================================"

