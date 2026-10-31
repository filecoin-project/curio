ARG UBUNTU_VERSION=26.04
ARG GO_VERSION=1.26.0
ARG CURIO_BASE_IMAGE=filecoin/curio:debug
ARG LOTUS_TEST_IMAGE=ghcr.io/filecoin-shipyard/lotus-containers:lotus-v1.36.0-devnet

FROM ${LOTUS_TEST_IMAGE} AS lotus-test

FROM ubuntu:${UBUNTU_VERSION} AS piece-server-builder

ARG GO_VERSION
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git build-essential pkg-config \
    && rm -rf /var/lib/apt/lists/* \
    && arch="$(dpkg --print-architecture)" \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" \
        | tar -C /usr/local -xz

ENV GOPATH=/go \
    PATH=/usr/local/go/bin:/go/bin:${PATH}

RUN go install github.com/ipld/go-car/cmd/car@latest \
    && go install github.com/LexLuthr/piece-server@latest \
    && go install github.com/ipni/storetheindex@v0.8.41 \
    && go install github.com/ethereum/go-ethereum/cmd/geth@latest

FROM ${CURIO_BASE_IMAGE} AS curio-all-in-one

USER root

RUN apt-get update && apt-get install -y --no-install-recommends \
    dnsutils vim xxd curl aria2 jq git wget ca-certificates make \
    && curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://foundry.paradigm.xyz | bash \
    && /root/.foundry/bin/foundryup

ENV PATH=/root/.foundry/bin:${PATH}

RUN forge --version && cast --version && anvil --version

ENV LOTUS_MINER_PATH=/var/lib/lotus-miner \
    LOTUS_PATH=/var/lib/lotus \
    CURIO_MK12_CLIENT_REPO=/var/lib/curio-client \
    STORETHEINDEX_PATH=/var/lib/indexer

COPY --from=lotus-test /usr/local/bin/lotus /usr/local/bin/
COPY --from=lotus-test /usr/local/bin/lotus-seed /usr/local/bin/
COPY --from=lotus-test /usr/local/bin/lotus-shed /usr/local/bin/
COPY --from=lotus-test /usr/local/bin/lotus-miner /usr/local/bin/
COPY --from=piece-server-builder /go/bin/piece-server /usr/local/bin/
COPY --from=piece-server-builder /go/bin/car /usr/local/bin/
COPY --from=piece-server-builder /go/bin/storetheindex /usr/local/bin/
COPY --from=piece-server-builder /go/bin/geth /usr/local/bin/

RUN mkdir -p /var/lib/lotus /var/lib/lotus-miner /var/lib/curio-client /var/lib/indexer \
    && chown fc: /var/lib/lotus /var/lib/lotus-miner /var/lib/curio-client /var/lib/indexer

VOLUME ["/var/lib/lotus", "/var/lib/lotus-miner", "/var/lib/curio-client", "/var/lib/indexer"]

EXPOSE 1234 2345 12320 3000 3001 3002 3003

ENTRYPOINT []
CMD ["/bin/bash"]
