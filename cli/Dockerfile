# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

FROM debian:12.13-slim

ENV DEBIAN_FRONTEND=noninteractive

# Install minimal required build deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    clang \
    libclang-dev \
    llvm \
    curl \
    make \
    git \
    gcc \
    g++ \
    libc6-dev \
    pkg-config \
    libssl-dev \
    gosu \
    && rm -rf /var/lib/apt/lists/*

# Create a non-root user and install Go and Rust with that user, so that
# later on the user can build extensions without permission issues.
# Wer don't switch to the boe user here, we'll do that in the entrypoint so that
# the permissions for the /var/boe mounted named volume can be properly set.
RUN groupadd -g 10001 boe && useradd -u 10001 -g boe -m -s /usr/sbin/nologin boe

# Configure the build cache directories to persist data across runs.
# The /var/boe directory is mounted as a volume when running the CLI in Docker mode.
ENV GOMODCACHE=/var/boe/go/pkg/mod \
    GOCACHE=/var/boe/go-build \
    CARGO_TARGET_DIR=/var/boe/cargo-target \
    RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/go/bin:/usr/local/cargo/bin:$PATH


ARG TARGETOS
ARG TARGETARCH
ARG GO_VERSION=1.26.1

# Install Go
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.${TARGETOS}-${TARGETARCH}.tar.gz \
    | tar -C /usr/local -xz

# Install Rust (minimal profile)
RUN curl https://sh.rustup.rs -sSf \
    | sh -s -- -y --profile minimal --default-toolchain stable
RUN chown -R boe:boe $RUSTUP_HOME $CARGO_HOME

# Install the CLI
COPY --chown=boe:boe ./out/boe-${TARGETOS}-${TARGETARCH} /boe
COPY --chown=boe:boe ./entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
