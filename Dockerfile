ARG GO_VERSION=1.26.0
ARG UBUNTU_VERSION=26.04

FROM ubuntu:${UBUNTU_VERSION} AS ubuntu-base
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

FROM ubuntu-base AS curio-builder
ARG GO_VERSION

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential clang curl git jq make pkg-config libhwloc-dev libssl-dev ocl-icd-opencl-dev \
    && rm -rf /var/lib/apt/lists/* \
    && arch="$(dpkg --print-architecture)" \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" \
       | tar -C /usr/local -xz

ENV PATH="/usr/local/go/bin:${PATH}" \
    GOPATH=/go \
    GOROOT=/usr/local/go \
    XDG_CACHE_HOME=/tmp \
    FFI_USE_OPENCL=1

WORKDIR /opt/curio

COPY go.mod go.sum ./
COPY extern/filecoin-ffi/go.mod extern/filecoin-ffi/go.sum ./extern/filecoin-ffi/

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN git submodule update --init --recursive \
    && touch build/.update-modules

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make deps

# build (mainnet), calibnet, debug, or 2k.
ARG CURIO_MAKE_TARGET=build
ARG CURIO_BUILD_COMMIT=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make "${CURIO_MAKE_TARGET}" CURIO_BUILD_COMMIT="${CURIO_BUILD_COMMIT}"

FROM ubuntu-base AS curio

ARG BUILD_VERSION=dev
ARG CURIO_BUILD_COMMIT=unknown

LABEL org.opencontainers.image.title="curio" \
      org.opencontainers.image.version=$BUILD_VERSION \
      org.opencontainers.image.revision=$CURIO_BUILD_COMMIT \
      org.opencontainers.image.authors="Curio Dev Team" \
      org.opencontainers.image.source="https://github.com/filecoin-project/curio" \
      org.opencontainers.image.description="Curio Filecoin storage provider"

RUN apt-get update && apt-get install -y --no-install-recommends \
    libhwloc15 libnuma1 libssl3t64 libstdc++6 ocl-icd-libopencl1 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -r -u 532 -U fc \
    && mkdir -p /etc/OpenCL/vendors /var/tmp/filecoin-proof-parameters /var/lib/curio \
    && echo "libnvidia-opencl.so.1" > /etc/OpenCL/vendors/nvidia.icd \
    && chown fc: /var/tmp/filecoin-proof-parameters /var/lib/curio

COPY --from=curio-builder /opt/curio/curio /usr/local/bin/curio
COPY --from=curio-builder /opt/curio/sptool /usr/local/bin/sptool

RUN curio --version && sptool --version

ENV FIL_PROOFS_PARAMETER_CACHE=/var/tmp/filecoin-proof-parameters \
    CURIO_REPO_PATH=/var/lib/curio

VOLUME ["/var/tmp/filecoin-proof-parameters", "/var/lib/curio"]
EXPOSE 12300 4701 32100 12310

ENTRYPOINT ["curio"]
CMD ["run"]
