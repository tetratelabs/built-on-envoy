# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Build the runtime plugin with the tools module used by the repository's golangci binary.
ROOT := $(abspath ../..)
GO_TOOL := go tool -modfile="$(ROOT)/tools/go.mod"
GO_FILES := $(wildcard scanner/*.go runtime-plugin/*.go testdata/fixture/probe/*.go testdata/fixture/probe/*/*.go)
PLUGIN_SOURCES := $(filter-out %_test.go,$(wildcard scanner/*.go runtime-plugin/*.go))
GO_TEST_ARGS ?=

.PHONY: check format lint test runtime-plugin runtime-plugin-check
check: lint test

format:
	$(GO_TOOL) golangci-lint fmt $(GO_FILES)

lint: runtime-plugin
	$(GO_TOOL) golangci-lint run ./...

test:
	# Subprocess builds of the runtime plugin are outside Go's test-cache dependency tracking.
	go test -count=1 $(GO_TEST_ARGS) ./...

runtime-plugin: out/ffideadcode.so

# Directory timestamps also invalidate the plugin when source files are added or removed.
out/ffideadcode.so: $(PLUGIN_SOURCES) scanner/ runtime-plugin/ ../go.mod ../go.sum Makefile
	mkdir -p "$(@D)"
	go build -buildmode=plugin -o "$@" ./runtime-plugin

runtime-plugin-check: runtime-plugin
	cd ../../extensions/composer && $(GO_TOOL) golangci-lint run --config=../../tools/deadcode-lint-plugin/runtime-plugin/.golangci.yml ./example/...
