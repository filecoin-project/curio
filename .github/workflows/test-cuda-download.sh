#!/bin/bash

# Test script to verify CUDA download URLs work before pushing to CI
# This helps iterate on the GitHub Actions workflow locally

set -e

echo "============================================"
echo "Testing CUDA Download for CI"
echo "============================================"
echo ""

# Test the URL from the CI workflow
CUDA_URL="https://developer.download.nvidia.com/compute/cuda/13.0.0/local_installers/cuda_13.0.0_530.30.02_linux.run"

echo "Testing URL: $CUDA_URL"
echo ""

# Check if URL is accessible
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$CUDA_URL")

if [ "$HTTP_CODE" = "200" ]; then
    echo "✅ URL is accessible (HTTP $HTTP_CODE)"
    echo ""
    
    # Get file size
    SIZE=$(curl -sI "$CUDA_URL" | grep -i content-length | awk '{print $2}' | tr -d '\r')
    SIZE_GB=$(echo "scale=2; $SIZE / 1024 / 1024 / 1024" | bc)
    echo "File size: ${SIZE_GB}GB"
    echo ""
    
    echo "To download manually for testing:"
    echo "  wget $CUDA_URL"
    echo ""
    
elif [ "$HTTP_CODE" = "404" ]; then
    echo "❌ URL not found (HTTP $HTTP_CODE)"
    echo ""
    echo "CUDA 13.0 may not be publicly available yet."
    echo ""
    echo "Options:"
    echo ""
    echo "1. Use your local CUDA 13 installation:"
    echo "   - Package it: tar -czf cuda-13.0-local.tar.gz -C /usr/local cuda-13.0"
    echo "   - Upload to GitHub release or artifact storage"
    echo "   - Download in CI workflow"
    echo ""
    echo "2. Use CUDA 12.6 (latest public version):"
    echo "   Test URL: https://developer.download.nvidia.com/compute/cuda/12.6.0/local_installers/cuda_12.6.0_560.28.03_linux.run"
    echo ""
    echo "3. Use NVIDIA's runfile for CUDA 12.6:"
    echo "   wget https://developer.download.nvidia.com/compute/cuda/12.6.0/local_installers/cuda_12.6.0_560.28.03_linux.run"
    echo ""
    echo "Testing CUDA 12.6 URL..."
    CUDA_12_URL="https://developer.download.nvidia.com/compute/cuda/12.6.0/local_installers/cuda_12.6.0_560.28.03_linux.run"
    HTTP_CODE_12=$(curl -s -o /dev/null -w "%{http_code}" "$CUDA_12_URL")
    
    if [ "$HTTP_CODE_12" = "200" ]; then
        echo "✅ CUDA 12.6 URL works (HTTP $HTTP_CODE_12)"
        SIZE=$(curl -sI "$CUDA_12_URL" | grep -i content-length | awk '{print $2}' | tr -d '\r')
        SIZE_GB=$(echo "scale=2; $SIZE / 1024 / 1024 / 1024" | bc)
        echo "File size: ${SIZE_GB}GB"
        echo ""
        echo "Recommendation: Update ci.yml to use CUDA 12.6 temporarily"
        echo "Your build script already supports CUDA 12+, so it will work fine."
    else
        echo "❌ CUDA 12.6 URL also failed (HTTP $HTTP_CODE_12)"
    fi
    
else
    echo "❌ Unexpected HTTP response: $HTTP_CODE"
fi

echo ""
echo "============================================"
echo "Local Iteration Workflow"
echo "============================================"
echo ""
echo "To test CI changes locally before pushing:"
echo ""
echo "1. Edit the Dockerfile to test CUDA installation:"
echo "   .github/workflows/Dockerfile.supraseal-test"
echo ""
echo "2. Run local test:"
echo "   ./.github/workflows/test-supraseal-locally.sh"


