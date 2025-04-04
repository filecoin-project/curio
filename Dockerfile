#####################################
ARG LOTUS_TEST_IMAGE=curio/lotus-all-in-one:latest
FROM ${LOTUS_TEST_IMAGE} AS lotus-test
FROM golang:1.23-bullseye AS curio-builder
LABEL Maintainer="Curio Development Team"

RUN apt-get update && apt-get install -y ca-certificates build-essential clang ocl-icd-opencl-dev ocl-icd-libopencl1 jq libhwloc-dev

ENV XDG_CACHE_HOME="/tmp"

### taken from https://github.com/rust-lang/docker-rust/blob/master/1.63.0/buster/Dockerfile
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH \
    RUST_VERSION=1.63.0

RUN set -eux; \
    dpkgArch="$(dpkg --print-architecture)"; \
    case "${dpkgArch##*-}" in \
        amd64) rustArch='x86_64-unknown-linux-gnu'; rustupSha256='5cc9ffd1026e82e7fb2eec2121ad71f4b0f044e88bca39207b3f6b769aaa799c' ;; \
        arm64) rustArch='aarch64-unknown-linux-gnu'; rustupSha256='e189948e396d47254103a49c987e7fb0e5dd8e34b200aa4481ecc4b8e41fb929' ;; \
        *) echo >&2 "unsupported architecture: ${dpkgArch}"; exit 1 ;; \
    esac; \
    url="https://static.rust-lang.org/rustup/archive/1.25.1/${rustArch}/rustup-init"; \
    wget "$url"; \
    echo "${rustupSha256} *rustup-init" | sha256sum -c -; \
    chmod +x rustup-init; \
    ./rustup-init -y --no-modify-path --profile minimal --default-toolchain $RUST_VERSION --default-host ${rustArch}; \
    rm rustup-init; \
    chmod -R a+w $RUSTUP_HOME $CARGO_HOME; \
    rustup --version; \
    cargo --version; \
    rustc --version;

COPY ./ /opt/curio
WORKDIR /opt/curio

### make configurable filecoin-ffi build
ARG FFI_BUILD_FROM_SOURCE=0
ENV FFI_BUILD_FROM_SOURCE=${FFI_BUILD_FROM_SOURCE}

RUN make clean deps

ARG RUSTFLAGS=""
ARG GOFLAGS=""
ARG CURIO_TAGS=""

RUN make build

####################################
FROM golang:1.23-bullseye AS piece-server-builder

RUN go install github.com/ipld/go-car/cmd/car@latest \
 && cp $GOPATH/bin/car /usr/local/bin/

RUN go install github.com/LexLuthr/piece-server@latest \
 && cp $GOPATH/bin/piece-server /usr/local/bin/

RUN go install github.com/ipni/storetheindex@v0.8.38 \
 && cp $GOPATH/bin/storetheindex /usr/local/bin/

#####################################
FROM ubuntu:22.04 AS curio-all-in-one

RUN apt-get update && apt-get install -y dnsutils vim curl aria2 jq

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