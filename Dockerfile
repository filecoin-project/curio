#####################################
ARG LOTUS_TEST_IMAGE=curio/lotus-all-in-one:latest
FROM ${LOTUS_TEST_IMAGE} AS lotus-test

#####################################
FROM rust:1.86.0-slim-bookworm AS rust-toolchain

####################################
FROM golang:1.24-trixie AS curio-builder
LABEL Maintainer="Curio Development Team"

RUN apt-get update && apt-get install -y ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev

WORKDIR /opt/curio

ENV XDG_CACHE_HOME="/tmp"

# Copy prebuilt Rust from the rust image (arch-matched by buildx)
COPY --from=rust-toolchain /usr/local/cargo /usr/local/cargo
COPY --from=rust-toolchain /usr/local/rustup /usr/local/rustup

ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH

RUN rustc --version && cargo --version

COPY go.mod go.sum ./
COPY extern/filecoin-ffi/go.mod extern/filecoin-ffi/go.sum ./extern/filecoin-ffi/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN git submodule update --init

### make configurable filecoin-ffi build
ARG FFI_BUILD_FROM_SOURCE=0
ENV FFI_BUILD_FROM_SOURCE=${FFI_BUILD_FROM_SOURCE}

ARG CURIO_OPTIMAL_LIBFILCRYPTO=0
ENV CURIO_OPTIMAL_LIBFILCRYPTO=${CURIO_OPTIMAL_LIBFILCRYPTO}

ARG FFI_USE_OPENCL=1
ENV FFI_USE_OPENCL=${FFI_USE_OPENCL}

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make clean deps

ARG RUSTFLAGS=""
ARG GOFLAGS=""
ARG CURIO_TAGS=""

ENV RUSTFLAGS="${RUSTFLAGS}" \
        GOFLAGS="${GOFLAGS}" \
        CURIO_TAGS="${CURIO_TAGS}"

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make build

####################################
FROM golang:1.24-trixie AS piece-server-builder

RUN go install github.com/ipld/go-car/cmd/car@latest \
 && cp $GOPATH/bin/car /usr/local/bin/

RUN go install github.com/LexLuthr/piece-server@latest \
 && cp $GOPATH/bin/piece-server /usr/local/bin/

RUN go install github.com/ipni/storetheindex@v0.8.41\
 && cp $GOPATH/bin/storetheindex /usr/local/bin/

RUN go install github.com/ethereum/go-ethereum/cmd/geth@latest \
 && cp $GOPATH/bin/geth /usr/local/bin/

#####################################
FROM ubuntu:24.04 AS curio-all-in-one

RUN apt-get update && apt-get install -y dnsutils vim curl aria2 jq git wget nodejs npm

# Install Foundry
RUN curl -L https://foundry.paradigm.xyz | bash \
    && bash -c ". ~/.foundry/bin/foundryup"

# Make sure foundry binaries are available in PATH
ENV PATH="/root/.foundry/bin:${PATH}"

# Verify installation
RUN forge --version && cast --version && anvil --version

# Copy libraries and binaries from curio-builder
COPY --from=curio-builder /etc/ssl/certs /etc/ssl/certs
COPY --from=curio-builder /lib/*/libdl.so.2 /lib/
COPY --from=curio-builder /lib/*/librt.so.1 /lib/
COPY --from=curio-builder /lib/*/libgcc_s.so.1 /lib/
COPY --from=curio-builder /lib/*/libutil.so.1 /lib/
COPY --from=curio-builder /usr/lib/*/libltdl.so.7 /lib/
COPY --from=curio-builder /usr/lib/*/libnuma.so.1 /lib/
COPY --from=curio-builder /usr/lib/*/libhwloc.so.* /lib/
COPY --from=curio-builder /usr/lib/*/libOpenCL.so.1 /lib/

# Setup user and OpenCL configuration
RUN useradd -r -u 532 -U fc && \
    mkdir -p /etc/OpenCL/vendors && \
    echo "libnvidia-opencl.so.1" > /etc/OpenCL/vendors/nvidia.icd

# Environment setup
ENV FIL_PROOFS_PARAMETER_CACHE=/var/tmp/filecoin-proof-parameters \
    LOTUS_MINER_PATH=/var/lib/lotus-miner \
    LOTUS_PATH=/var/lib/lotus \
    CURIO_REPO_PATH=/var/lib/curio \
    CURIO_MK12_CLIENT_REPO=/var/lib/curio-client \
    STORETHEINDEX_PATH=/var/lib/indexer

# Copy binaries and scripts
COPY --from=lotus-test /usr/local/bin/lotus /usr/local/bin/
COPY --from=lotus-test /usr/local/bin/lotus-seed /usr/local/bin/
COPY --from=lotus-test /usr/local/bin/lotus-shed /usr/local/bin/
COPY --from=lotus-test /usr/local/bin/lotus-miner /usr/local/bin/
COPY --from=curio-builder /opt/curio/curio /usr/local/bin/
COPY --from=curio-builder /opt/curio/sptool /usr/local/bin/
COPY --from=piece-server-builder /usr/local/bin/piece-server /usr/local/bin/
COPY --from=piece-server-builder /usr/local/bin/car /usr/local/bin/
COPY --from=piece-server-builder /usr/local/bin/storetheindex /usr/local/bin/
COPY --from=piece-server-builder /usr/local/bin/geth /usr/local/bin/

# Set up directories and permissions
RUN mkdir /var/tmp/filecoin-proof-parameters \
           /var/lib/lotus \
           /var/lib/lotus-miner \
           /var/lib/curio \
           /var/lib/curio-client \
           /var/lib/indexer && \
    chown fc: /var/tmp/filecoin-proof-parameters /var/lib/lotus /var/lib/lotus-miner /var/lib/curio /var/lib/curio-client /var/lib/indexer

# Define volumes
VOLUME ["/var/tmp/filecoin-proof-parameters", "/var/lib/lotus", "/var/lib/lotus-miner", "/var/lib/curio", "/var/lib/curio-client", "/var/lib/indexer"]

# Expose necessary ports
EXPOSE 1234 2345 12300 4701 32100 12310 12320 3000 3001 3002 3003

CMD ["/bin/bash"]