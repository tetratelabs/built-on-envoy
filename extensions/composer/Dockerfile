# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

ARG GO_VERSION

FROM golang:${GO_VERSION} AS builder

WORKDIR /workspace
COPY . .

# Install necessary tools for cgo
RUN apt-get update
RUN apt-get install -y --no-install-recommends \
    ca-certificates wget git ssh netcat-openbsd gcc build-essential binutils binutils-gold

RUN go mod download all
ARG BUILD_TAG=""
# Build go shared library
RUN CGO_ENABLED=1 go build -trimpath -buildmode=c-shared ${BUILD_TAG} -o libcomposer.so ./main

# Collect all sub-extension manifest.yaml files preserving their directory structure.
RUN mkdir -p /manifests; find . -mindepth 2 -name 'manifest.yaml' \
    -exec sh -c 'install -D "$1" /manifests/"$1"' _ {} \;

FROM scratch AS final

COPY --from=builder /workspace/libcomposer* /
COPY --from=builder /workspace/manifest.yaml /
COPY --from=builder /manifests/ /manifests/
