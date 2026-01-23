# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Pre-download Envoy for using func-e. This reduces latency and avoids
# nboeding to declare a volume for the Envoy binary, which is tricky in Docker
# Compose v2 because volumes end up owned by root.
FROM golang:1.25 AS envoy-downloader
ARG TARGETOS
ARG TARGETARCH

# Download Envoy binary to boe_DATA_HOME for the nonroot user
WORKDIR /build
RUN go install github.com/tetratelabs/func-e/cmd/func-e@latest && \
    FUNC_E_DATA_HOME=/home/nonroot/.local/share/boe func-e --platform ${TARGETOS}/${TARGETARCH} run --version \
    # Create directories for the nonroot user
    && mkdir -p /home/nonroot \
    && chown -R 65532:65532 /home/nonroot \
    && chmod -R 755 /home/nonroot

FROM gcr.io/distroless/base-nossl-debian12:nonroot
ARG TARGETOS
ARG TARGETARCH

# Copy pre-downloaded Envoy binary and EG certs directory
COPY --from=envoy-downloader /home/nonroot /home/nonroot
COPY ./out/boe-${TARGETOS}-${TARGETARCH} /app

USER nonroot:nonroot

# Set BOE_RUN_ID=0 for predictable file paths in containers.
# This creates the following directory structure:
#   ~/.config/boe/                     - XDG config (e.g., envoy-version preference)
#   ~/.local/share/boe/                - XDG data (downloaded Envoy binaries via func-e)
#   ~/.local/state/boe/runs/0/         - XDG state (boe.log)
#   ~/.local/state/boe/envoy-runs/0/   - XDG state (func-e stdout.log, stderr.log)
#   /tmp/boe-0/                        - XDG runtime (uds.sock, admin-address.txt)
ENV BOE_RUN_ID=0

ENTRYPOINT ["/app"]

# Default CMD for boe - uses boe_RUN_ID from environment
CMD ["run"]
