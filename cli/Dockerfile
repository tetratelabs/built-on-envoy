# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

FROM cgr.dev/chainguard/gcc-glibc:latest-dev

# Install minimal required build deps
RUN apk add --no-cache \
    ca-certificates \
    curl \
    clang \
    su-exec

# Create a non-root user and install Go and Rust with that user, so that
# later on the user can build extensions without permission issues.
# We don't switch to the boe user here, we'll do that in the entrypoint so that
# the permissions for the /var/boe mounted named volume can be properly set.
RUN addgroup -g 10001 boe && adduser -D -u 10001 -G boe boe

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
ARG GO_VERSION=1.26.3

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

# Set BOE_RUN_ID=0 for predictable file paths in containers.
# This creates the following directory structure:
#   ~/.config/boe/                     - XDG config (e.g., envoy-version preference)
#   ~/.local/share/boe/                - XDG data (downloaded Envoy binaries)
#   ~/.local/state/boe/runs/0/         - XDG state (boe.log, envoy config)
#   ~/.local/state/boe/envoy-runs/0/   - XDG state (Envoy stdout.log, stderr.log)
#   /tmp/boe-0/                        - XDG runtime (uds.sock, admin-address.txt)
ENV BOE_RUN_ID=0
HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/boe", "healthcheck"]

ENTRYPOINT ["/entrypoint.sh"]
