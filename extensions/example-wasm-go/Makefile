# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Simple build for the example Wasm extension. The Dockerfile invokes `make build`,
# so the build command lives in a single place and the same Dockerfile pattern can
# be reused for other languages (e.g. Rust) by swapping only this build target.

default: build

OUT ?= ./out

.PHONY: build
build:  ## Compile the extension into a WebAssembly module (out/plugin.wasm)
	@GOOS=wasip1 GOARCH=wasm CGO_ENABLED=0 go build -buildmode=c-shared -o $(OUT)/plugin.wasm .

.PHONY: test
test:  ## Run the tests
	@go test ./...

.PHONY: clean
clean:  ## Remove build artifacts
	@rm -rf $(OUT)
