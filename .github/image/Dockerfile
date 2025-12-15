## This image is used to github action runner for this repo
# Use the official Golang image as the base image
FROM golang:1.24-trixie AS builder

# Set the working directory inside the container
WORKDIR /app

# Install Git
RUN apt-get update && apt-get install -y git

# Clone the repositories
RUN git clone https://github.com/filecoin-project/boost.git
RUN git clone https://github.com/filecoin-project/curio.git

# Download Go dependencies for Boost
WORKDIR /app/boost
RUN git submodule update --init
RUN go mod download

# Download Go dependencies for Curio
WORKDIR /app/curio
RUN git submodule update --init
RUN go mod download

# Stage 2: Install Lotus binary
FROM ghcr.io/filecoin-shipyard/lotus-containers:lotus-v1.34.0-rc2-devnet AS lotus-test

# Stage 3: Build the final image
FROM myoung34/github-runner:ubuntu-24.04 AS curio-github-runner

# Copy Go dependencies from the builder stage
COPY --from=builder /go/pkg /go/pkg
COPY --from=builder /go/bin /go/bin
COPY --from=lotus-test /usr/local/bin/lotus /usr/local/bin/

RUN apt-get update && apt-get install -y \
      build-essential \
      gcc-12 g++-12 \
      pkg-config \
      autoconf automake libtool \
      clang \
      curl wget git \
      ca-certificates gnupg \
      python3 python3-pip python3-dev python3-venv \
      xxd \
      nasm \
      hwloc libhwloc-dev \
      jq \
      mesa-opencl-icd \
      ocl-icd-opencl-dev \
      libfuse3-dev uuid-dev libssl-dev libnuma-dev libaio-dev libarchive-dev libkeyutils-dev libncurses-dev \
      libgmp-dev libconfig++-dev gcc-12 g++-12 \
      ca-certificates gnupg \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Set up GCC 12 as default
RUN update-alternatives --install /usr/bin/gcc gcc /usr/bin/gcc-12 100 \
    && update-alternatives --install /usr/bin/g++ g++ /usr/bin/g++-12 100 \
    && update-alternatives --set gcc /usr/bin/gcc-12 \
    && update-alternatives --set g++ /usr/bin/g++-12

ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH \
    RUST_VERSION=1.76.0

RUN set -eux; \
    dpkgArch="$(dpkg --print-architecture)"; \
    case "${dpkgArch##*-}" in \
        amd64) rustArch='x86_64-unknown-linux-gnu'; rustupSha256='0b2f6c8f85a3d02fde2efc0ced4657869d73fccfce59defb4e8d29233116e6db' ;; \
        armhf) rustArch='armv7-unknown-linux-gnueabihf'; rustupSha256='f21c44b01678c645d8fbba1e55e4180a01ac5af2d38bcbd14aa665e0d96ed69a' ;; \
        arm64) rustArch='aarch64-unknown-linux-gnu'; rustupSha256='673e336c81c65e6b16dcdede33f4cc9ed0f08bde1dbe7a935f113605292dc800' ;; \
        i386) rustArch='i686-unknown-linux-gnu'; rustupSha256='e7b0f47557c1afcd86939b118cbcf7fb95a5d1d917bdd355157b63ca00fc4333' ;; \
        *) echo >&2 "unsupported architecture: ${dpkgArch}"; exit 1 ;; \
    esac; \
    url="https://static.rust-lang.org/rustup/archive/1.26.0/${rustArch}/rustup-init"; \
    wget "$url"; \
    echo "${rustupSha256} *rustup-init" | sha256sum -c -; \
    chmod +x rustup-init; \
    ./rustup-init -y --no-modify-path --profile minimal --default-toolchain $RUST_VERSION --default-host ${rustArch}; \
    rm rustup-init; \
    chmod -R a+w $RUSTUP_HOME $CARGO_HOME; \
    rustup --version; \
    cargo --version; \
    rustc --version;

# Allow attaching a volume for the specified path
VOLUME /var/tmp/filecoin-proof-parameters
