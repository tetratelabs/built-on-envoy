# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Arguments for builds.
OUTPUT_DIR ?= out
NAME       ?= boe

# Arguments for Docker builds.
OCI_REGISTRY ?= $(or $(BOE_REGISTRY),ghcr.io/tetratelabs/built-on-envoy)
TAG          ?= latest
COMMIT_SHA   := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")

# Arguments for go test. This can be used, for example, to run specific tests via
# `GO_TEST_ARGS="-run TestName/foo/etc -v -race"`
GO_TEST_ARGS ?=

# Use a specific go tool invocation that uses the tools module for linters and other tools so that
# the main go.mod is not polluted with tool dependencies.
ROOT     := $(shell git rev-parse --show-toplevel)
GO_TOOL  := go tool -modfile=$(ROOT)/cli/tools/go.mod

# Base OCI image annotations (shared by all image types)
BASE_OCI_ANNOTATIONS := \
	--annotation "org.opencontainers.image.source=https://$(subst ghcr.io,github.com,$(OCI_REGISTRY))" \
	--annotation "org.opencontainers.image.licenses=Apache-2.0" \
	--annotation "org.opencontainers.image.title=$(NAME)" \
	--annotation "org.opencontainers.image.version=$(TAG)" \
	--annotation "org.opencontainers.image.description=Built On Envoy CLI" \
	--annotation "org.opencontainers.image.created=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")" \
	--annotation "org.opencontainers.image.revision=$(COMMIT_SHA)" \
	--annotation "org.opencontainers.image.authors=Tetrate" \

# Multi-platform OCI annotations (with index,manifest prefix, for binary artifacts)
comma := ,
MULTI_OCI_ANNOTATIONS := \
	$(subst --annotation ",--annotation "index$(comma)manifest:,$(BASE_OCI_ANNOTATIONS)) \

##@ Build

# This builds a binary ffor the given OS and architectures.
# By default, this will build for the current GOOS and GOARCH.
# To build for multiple platforms, set the GOOS_LIST and GOARCH_LIST variables.
#
# Example:
# - `make build GOOS_LIST="linux darwin" GOARCH_LIST="amd64 arm64"`
GOOS_LIST ?= $(shell go env GOOS)
GOARCH_LIST ?= $(shell go env GOARCH)
.PHONY: build
build: gen-code  ## Build a binary for the given OS and architectures
	@mkdir -p $(OUTPUT_DIR)
	@for goos in $(GOOS_LIST); do \
		for goarch in $(GOARCH_LIST); do \
			echo "-> Building for $$goos/$$goarch"; \
			CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch go build \
				-o $(OUTPUT_DIR)/$(NAME)-$$goos-$$goarch .; \
			echo "<- Built $(OUTPUT_DIR)/$(NAME)-$$goos-$$goarch"; \
		done; \
	done

.PHONY: clean
clean:  ## Clean the build artifacts
	rm -rf $(OUTPUT_DIR)

.PHONY: test
test: gen-code  ## Run the unit tests
	@PKGS=$$(go list ./... | grep -v -E "e2e"); \
	  echo "Running unit tests for packages: $$PKGS"; \
	  go test $(GO_TEST_ARGS) $$PKGS

.PHONY: test-integration
test-integration: gen-code  ## Run the unit and integration tests
	@PKGS=$$(go list ./... | grep -v -E "e2e"); \
	  echo "Running unit and integration tests for packages: $$PKGS"; \
	  go test -tags integration $(GO_TEST_ARGS) $$PKGS

.PHONY: test-e2e
test-e2e: GO_TEST_ARGS += -timeout 30m
test-e2e: gen-code  ## Run the end-to-end tests
	@echo "Run e2e tests"
	@go test $(GO_TEST_ARGS) ./e2e/...

# This runs the unit tests for the codebase with coverage check.
.PHONY: test-coverage
test-coverage: gen-code  ## Run the unit tests for the codebase with coverage check.
	@mkdir -p $(OUTPUT_DIR)
	@$(MAKE) test GO_TEST_ARGS="-coverprofile=$(OUTPUT_DIR)/go-test-coverage.out \
		-covermode=atomic \
		-coverpkg=github.com/tetratelabs/built-on-envoy/cli/... \
		-count=1 \
		-tags integration $(GO_TEST_ARGS)"
	@$(GO_TOOL) go-test-coverage --config=.testcoverage.yml

##@ Code Generation

.PHONY: gen
gen: gen-code gen-docs  ## Generate all the code and documentation

.PHONY: gen-code
gen-code:  ## Generate code
	@echo "Generating code..."
	@go generate ./...

.PHONY: gen-docs
gen-docs:  ## Generate documentation
	@echo "Generating documentation..."
	@go run tools/gen-cli-docs/main.go -docs-dir=../website/src/pages/docs
	@go run tools/gen-manifest-reference/main.go \
		-schema=../extensions/manifest.schema.json \
		-output=../website/src/pages/docs/reference/manifest.mdx \
		-extension-index=../website/public/extensions.json

##@ Docker

registry_output :=
ifeq ($(BOE_REGISTRY_INSECURE),true)
  registry_output := ",registry.insecure=true"
endif

BUILDER_NAME := boe-builder
.PHONY: create_builder
create_builder:
	@docker buildx inspect $(BUILDER_NAME) || \
		docker buildx create --name $(BUILDER_NAME) \
			--driver docker-container --driver-opt network=host \
			--buildkitd-flags '--allow-insecure-entitlement network.host' --use

.PHONY: build_image
build_image:  ## Build the Docker image
	@$(MAKE) build GOOS_LIST="linux" GOARCH_LIST="$(GOARCH_LIST)"
	docker buildx build \
		--output type=docker,oci-mediatypes=true \
		--provenance=false \
		$(BASE_OCI_ANNOTATIONS) \
		-t $(OCI_REGISTRY)/$(NAME):$(TAG) \
		-f ./Dockerfile .

.PHONY: push_image
push_image: GOARCH_LIST = amd64 arm64
push_image: PLATFORMS = linux/amd64,linux/arm64
push_image: create_builder  ## Push the Docker image to the registry
	@$(MAKE) build GOOS_LIST="linux" GOARCH_LIST="$(GOARCH_LIST)"
	docker buildx build --platform=$(PLATFORMS) \
		--builder $(BUILDER_NAME) \
		--output type=registry,oci-mediatypes=true$(registry_output) \
		--provenance=false \
		$(MULTI_OCI_ANNOTATIONS) \
		-t $(OCI_REGISTRY)/$(NAME):$(TAG) \
		-f ./Dockerfile .

##@ Code Quality

.PHONY: lint
lint: gen-code  ## Run the linters
	@echo "golangci-lint => ./..."
	@$(GO_TOOL) golangci-lint run --build-tags integration
	@echo "golangci-lint => tools/gen-cli-docs/..."
	@cd tools/gen-cli-docs && $(GO_TOOL) golangci-lint run
	@echo "golangci-lint => tools/gen-manifest-reference/..."
	@cd tools/gen-manifest-reference && $(GO_TOOL) golangci-lint run

# Some IDEs like Goland place `.go` files in the `.idea` directory when using code templates. Using a
# git command to find the files ensures that only relevant files are formatted and that git-ignored
# files do not get in the way
GO_FILES=$(shell git ls-files --cached --others --exclude-standard | grep '\.go$$')
.PHONY: format
format:  ## Format code.
	@echo "format => *.go"
	@$(GO_TOOL) golangci-lint fmt $(GO_FILES)
	@echo "licenses => **"
	@$(GO_TOOL) license-eye -c $(ROOT)/.licenserc.yaml header fix

.PHONY: tidy
tidy:  ## Run go mod tidy.
	@echo "go mod tidy => ./..."
	@find . -name go.mod -execdir go mod tidy \;

.PHONY: check
check: gen tidy format  ## Run all necessary steps to prepare for a commit and check for any differences in the codebase
	@if [ ! -z "`git status -s`" ]; then \
		echo "The following differences will fail CI until committed:"; \
		git status; \
		echo "Please ensure you have run 'make check' and committed the changes."; \
		exit 1; \
	fi

##@ Help

.PHONY: help
help:  ## Show this help info
	@echo "Built On Envoy. Discover, run, and build custom filters with zero friction.\n"
	@echo "Usage:\n  make \033[36m<Target>\033[0m \n\nTargets:"
	@awk 'BEGIN {FS = ":.*##"; printf ""} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
