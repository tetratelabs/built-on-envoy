# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

ROOT       := $(shell git rev-parse --show-toplevel)
GO_VERSION := $(shell sed -ne 's/^go //gp' $(ROOT)/go.mod)
GO_TOOL    := go tool -modfile=$(ROOT)/tools/go.mod

# Arguments for go test. This can be used, for example, to run specific tests via
# `GO_TEST_ARGS="-run TestName/foo/etc -v -race"`
GO_TEST_ARGS ?=

.PHONY: test
test:
	@echo "Run extension e2e tests"
	@go test $(GO_TEST_ARGS) ./...

.PHONY: format
format:
	@echo "format => *.go"
	@$(GO_TOOL) golangci-lint fmt
	@echo "licenses => **"
	@$(GO_TOOL) license-eye -c $(ROOT)/.licenserc.yaml header fix

.PHONY: lint
lint: 
	@echo "golangci-lint => ./..."
	@$(GO_TOOL) golangci-lint run

.PHONY: check
check: format lint
	@if [ ! -z "`git status -s`" ]; then \
		echo "The following differences will fail CI until committed:"; \
		git status; \
		echo "Please ensure you have run 'make check' and committed the changes."; \
		exit 1; \
	fi
