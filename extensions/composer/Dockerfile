# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Build the manager binary
FROM golang:1.25.7 AS builder

WORKDIR /workspace
COPY . .

# Install necessary tools for cgo
RUN apt-get update
RUN apt-get install -y --no-install-recommends \
    ca-certificates wget git ssh netcat-openbsd gcc build-essential binutils binutils-gold

RUN go mod download all
ARG BUILD_TAG=""
# Build go shared library
RUN CGO_ENABLED=1 go build -tags ${BUILD_TAG} -buildmode=c-shared -o libcomposer.so \
    ./main

FROM scratch AS final

COPY --from=builder /workspace/libcomposer* /
