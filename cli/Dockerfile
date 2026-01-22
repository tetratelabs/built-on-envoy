# Copyright Envoy Ecosystem
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Pre-download Envoy for using func-e. This reduces latency and avoids
# needing to declare a volume for the Envoy binary, which is tricky in Docker
# Compose v2 because volumes end up owned by root.
FROM golang:1.25 AS envoy-downloader
ARG TARGETOS
ARG TARGETARCH

# Download Envoy binary to EE_DATA_HOME for the nonroot user
WORKDIR /build
RUN go install github.com/tetratelabs/func-e/cmd/func-e@latest && \
    FUNC_E_DATA_HOME=/home/nonroot/.local/share/ee func-e --platform ${TARGETOS}/${TARGETARCH} run --version \
    # Create directories for the nonroot user
    && mkdir -p /home/nonroot \
    && chown -R 65532:65532 /home/nonroot \
    && chmod -R 755 /home/nonroot

FROM gcr.io/distroless/base-nossl-debian12:nonroot
ARG TARGETOS
ARG TARGETARCH

# Copy pre-downloaded Envoy binary and EG certs directory
COPY --from=envoy-downloader /home/nonroot /home/nonroot
COPY ./out/ee-${TARGETOS}-${TARGETARCH} /app

USER nonroot:nonroot

# Set EE_RUN_ID=0 for predictable file paths in containers.
# This creates the following directory structure:
#   ~/.config/ee/                     - XDG config (e.g., envoy-version preference)
#   ~/.local/share/ee/                - XDG data (downloaded Envoy binaries via func-e)
#   ~/.local/state/ee/runs/0/         - XDG state (ee.log)
#   ~/.local/state/ee/envoy-runs/0/   - XDG state (func-e stdout.log, stderr.log)
#   /tmp/ee-0/                        - XDG runtime (uds.sock, admin-address.txt)
ENV EE_RUN_ID=0

ENTRYPOINT ["/app"]

# Default CMD for ee - uses EE_RUN_ID from environment
CMD ["run"]
