# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Build the manager binary
FROM golang:1.25.6 AS builder

WORKDIR /workspace
COPY --from=composer . .

RUN go mod download all
ARG EXTENSION_PATH=""

# Build go plugin
RUN CGO_ENABLED=1 go build -buildmode=plugin -o plugin.so ./${EXTENSION_PATH}

FROM scratch AS final

COPY --from=builder /workspace/plugin.so /
