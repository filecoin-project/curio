# Build environment for Curio CI
# Contains: Go, system dependencies, and common tools
# Rebuild when: Go version changes, system deps change

ARG GO_VERSION=1.24.7

FROM golang:${GO_VERSION}-bookworm

# Install system dependencies (same as install-deps action)
RUN apt-get update && apt-get install -y \
    curl \
    ca-certificates \
    gnupg \
    ocl-icd-opencl-dev \
    libhwloc-dev \
    git \
    && rm -rf /var/lib/apt/lists/*

# Pre-warm Go module cache directory
RUN mkdir -p /go/pkg/mod

# Set up workspace
WORKDIR /workspace

