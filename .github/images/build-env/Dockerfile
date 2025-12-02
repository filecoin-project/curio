# Build environment for Curio CI
# Contains: Go, system dependencies, and common tools
# Rebuild when: Go version changes, system deps change, go.mod/go.sum change

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

# Install Docker CLI (for test-itest to communicate with host Docker daemon)
RUN install -m 0755 -d /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc && \
    chmod a+r /etc/apt/keyrings/docker.asc && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list && \
    apt-get update && \
    apt-get install -y docker-ce-cli && \
    rm -rf /var/lib/apt/lists/*

# Set up workspace
WORKDIR /workspace

# Copy go.mod and go.sum to pre-download modules
COPY go.mod go.sum ./

# Pre-download Go modules (will be cached in /go/pkg/mod)
RUN go mod download

# Install common dev/CI tools (from tools/tools.go)
RUN go install golang.org/x/tools/cmd/goimports@latest && \
    go install github.com/hannahhoward/cbor-gen-for@latest && \
    go install github.com/swaggo/swag/cmd/swag@latest

