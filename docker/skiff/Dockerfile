ARG GO_VERSION=1.26.0
ARG UBUNTU_VERSION=26.04

# Shared Ubuntu base — reused by builder and runtime so apt layers stay cached.
FROM ubuntu:${UBUNTU_VERSION} AS ubuntu-base
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

#####################################
# Builder: compile the FFI-free skiff (PDP-only curio) binary.
FROM ubuntu-base AS skiff-builder
ARG GO_VERSION
LABEL Maintainer="Curio Dev Team"

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential clang curl git make \
    && rm -rf /var/lib/apt/lists/* \
    && arch="$(dpkg --print-architecture)" \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" \
       | tar -C /usr/local -xz

ENV PATH="/usr/local/go/bin:${PATH}" \
    GOPATH=/go \
    GOROOT=/usr/local/go \
    XDG_CACHE_HOME="/tmp"

WORKDIR /opt/curio

# Pre-fetch modules. filecoin-ffi is a local `replace`; skiff does not link the
# native libraries. Sparse-clone default branch tip (Go module metadata only, not rust/, ~9GB).
COPY go.mod go.sum .gitmodules ./

RUN git clone --filter=blob:none --sparse --depth 1 \
      https://github.com/filecoin-project/filecoin-ffi.git extern/filecoin-ffi \
    && cd extern/filecoin-ffi \
    && git sparse-checkout set go.mod go.sum version.go

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

# Network build target. Skiff embeds the Lantern chain backend for mainnet and
# calibration builds. For 2k/debug builds set ChainApiInfo / FULLNODE_API_INFO.
#   skiff (mainnet, default) | calibnet-skiff | 2k-skiff
ARG SKIFF_MAKE_TARGET=skiff
ARG CURIO_BUILD_COMMIT=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    make ${SKIFF_MAKE_TARGET} CURIO_BUILD_COMMIT=${CURIO_BUILD_COMMIT} \
    && mv curio skiff

#####################################
# Runtime image.
FROM ubuntu-base AS skiff

ARG BUILD_VERSION=0.1

LABEL org.opencontainers.image.version=$BUILD_VERSION \
      org.opencontainers.image.authors="Curio Dev Team" \
      name="skiff" \
      maintainer="Curio Dev Team" \
      vendor="Curio Dev Team" \
      version=$BUILD_VERSION \
      release=$BUILD_VERSION \
      summary="Skiff PDP storage service" \
      description="Lightweight PDP-only Curio (skiff) node" \
      io.curio.skiff.ports='[{"port":4701,"protocol":"tcp","purpose":"Admin GUI (webrpc, config editor)"},{"port":80,"protocol":"tcp","purpose":"Public PDP HTTP API when HTTP.DelegateTLS=true"},{"port":443,"protocol":"tcp","purpose":"Public PDP HTTPS API when HTTP.DelegateTLS=false (autocert)"}]'

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Repo holds Lantern chain state + node identity; /data is scanned for storage.
ENV CURIO_REPO_PATH=/var/lib/skiff \
    SKIFF_DATA=/data \
    SKIFF_DOCKER=1

RUN mkdir -p /var/lib/skiff /data /app

COPY --from=skiff-builder /opt/curio/skiff /usr/local/bin/skiff
COPY docker/skiff/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 4701/tcp
EXPOSE 80/tcp
EXPOSE 443/tcp

VOLUME ["/var/lib/skiff", "/data"]

WORKDIR /app
ENTRYPOINT ["./entrypoint.sh"]
