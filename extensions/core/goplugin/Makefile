# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

BUF      := github.com/bufbuild/buf/cmd/buf@v1.65.0
SHELL    := /bin/bash
repo_dir := $(shell git rev-parse --show-toplevel)

.PHONY: all
all: lint gen check

.PHONY: lint
lint:
	@echo "Linting protos..."
	@go run $(BUF) lint

.PHONY: gen
gen:
	@echo "Building protos..."
	@go run $(BUF) build

	@echo "Generating Go code..."
	@go run $(BUF) generate --template buf.gen.go.yaml

.PHONY: test
test:
	@echo "Running tests..."
	@go test ./...

.PHONY: clean
clean:
	@echo "Cleaning generated code..."
	@rm -f *.pb.*

.PHONY: check
check:  ## Run all necessary steps to prepare for a commit and check for any differences in the codebase
	@if [ ! -z "`git status -s`" ]; then \
		echo "The following differences will fail CI until committed:"; \
		git status; \
		echo "Please ensure you have run 'make check' and committed the changes."; \
		exit 1; \
	fi
