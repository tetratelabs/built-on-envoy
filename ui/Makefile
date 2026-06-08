# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

ROOT         := $(shell git rev-parse --show-toplevel)
GO_TOOL      := go tool -modfile=$(ROOT)/tools/go.mod
SCHEMA_FILES := $(shell find $(ROOT)/extensions -type f -name config.schema.json)
SCHEMAS_DIR  := schemas

# Arguments for go test. This can be used, for example, to run specific tests via
# `GO_TEST_ARGS="-run TestName/foo/etc -v -race"`
GO_TEST_ARGS ?=

.PHONY: build
build: compiled/bundle.js  ## Build the UI assets.

.PHONY: config-schemas
config-schemas: $(SCHEMA_FILES)
	@mkdir -p $(SCHEMAS_DIR)
	@for f in $(SCHEMA_FILES); do \
		dir=$$(basename "$$(dirname "$$f")"); \
		cp "$$f" "$(SCHEMAS_DIR)/$$dir.json"; \
	done

compiled/bundle.js: src/node_modules config-schemas
	@echo "Compiling UI code..."
	@cd src && npm run build

src/node_modules: src/package.json src/package-lock.json
	@cd src && npm ci

.PHONY: test
test: compiled/bundle.js  ## Run the UI tests.
	@echo "Running UI tests..."
	@cd src && npm test
	@go test $(GO_TEST_ARGS) ./...

.PHONY: test-coverage
test-coverage: compiled/bundle.js  ## Run the UI tests with coverage check.
	@echo "Running UI tests with coverage..."
	@cd src && npm run test:coverage
	@go test -covermode=atomic \
		-count=1 \
		-coverpkg=github.com/tetratelabs/built-on-envoy/ui/... \
		-coverprofile=src/coverage/go-test-coverage.out \
		./
	@$(GO_TOOL) go-test-coverage --config=.testcoverage.yml

.PHONY: lint
lint: compiled/bundle.js  ## Run linters on the UI code.
	@echo "Linting UI code..."
	@cd src && npm run lint
	@$(GO_TOOL) golangci-lint run ./...

.PHONY: format
format:  ## Format code.
	@echo "format => *.go"
	@$(GO_TOOL) golangci-lint fmt .
	@echo "licenses => **"
	@$(GO_TOOL) license-eye -c $(ROOT)/.licenserc.yaml header fix

.PHONY: check
check: build format  ## Run all necessary steps to prepare for a commit and check for any differences in the codebase
	@if [ ! -z "`git status -s`" ]; then \
		echo "The following differences will fail CI until committed:"; \
		git status; \
		echo "Please ensure you have run 'make check' and committed the changes."; \
		exit 1; \
	fi

.PHONY: clean
clean:  ## Clean the UI build artifacts.
	@echo "Cleaning UI build artifacts..."
	@rm -rf $(SCHEMAS_DIR) compiled src/coverage

.PHONY: help
help:  ## Show this help info.
	@echo "Built On Envoy. Discover, run, and build custom filters with zero friction.\n"
	@echo "Usage:\n  make \033[36m<Target>\033[0m \n\nTargets:"
	@awk 'BEGIN {FS = ":.*##"; printf ""} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
