# Copyright Envoy Ecosystem
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# Read any local configuration. This is an optional, local git-ignored file that can be used
# to set any value commonly used for development. This helps not having to set the overrides
# in the command line every time
-include .makerc

# Arguments for builds.
OUTPUT_DIR ?= out
BINARY_NAME ?= ee

# Arguments for Docker builds.
OCI_REGISTRY           ?= docker.io/tetratelabs
OCI_REPOSITORY_PREFIX  ?= ${OCI_REGISTRY}/envoy-ecosystem
ENABLE_MULTI_PLATFORMS ?= false
IMAGE_NAME	           ?= cli
TAG                    ?= latest

# Arguments for go test. This can be used, for example, to run specific tests via
# `GO_TEST_ARGS="-run TestName/foo/etc -v -race"`
GO_TEST_ARGS ?=

# Use a specific go tool invocation that uses the tools module for linters and other tools so that
# the main go.mod is not polluted with tool dependencies.
GO_TOOL := go tool -modfile=tools/go.mod

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
build: ## Build a binary for the given OS and architectures
	@mkdir -p $(OUTPUT_DIR)
	@for goos in $(GOOS_LIST); do \
		for goarch in $(GOARCH_LIST); do \
			echo "-> Building for $$goos/$$goarch"; \
			CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch go build \
				-o $(OUTPUT_DIR)/$(BINARY_NAME)-$$goos-$$goarch .; \
			echo "<- Built $(OUTPUT_DIR)/$(BINARY_NAME)-$$goos-$$goarch"; \
		done; \
	done

.PHONY: clean
clean:  ## Clean the build artifacts
	rm -rf $(OUTPUT_DIR)

.PHONY: test
test:  ## Run the unit tests
	@PKGS=$$(go list ./... | grep -v -E "e2e"); \
	  echo "Running unit tests for packages: $$PKGS"; \
	  go test $(GO_TEST_ARGS) $$PKGS

.PHONY: test-e2e
test-e2e:  ## Run the end-to-end tests
	@echo "Run e2e tests"
	@go test $(GO_TEST_ARGS) ./e2e/...

##@ Docker

# Builds the Docker image for the CLI.
#
# By default, this will build for the current GOARCH and linux.
# To build for multiple platforms, set the ENABLE_MULTI_PLATFORMS variable to true.
#
# Example:
# - `make docker ENABLE_MULTI_PLATFORMS=true`
#
# Also, DOCKER_BUILD_ARGS can be set to pass additional arguments to the docker build command.
#
# Example:
# - `make docker ENABLE_MULTI_PLATFORMS=true DOCKER_BUILD_ARGS="--push"` to push the image to the registry.
# - `make docker ENABLE_MULTI_PLATFORMS=true DOCKER_BUILD_ARGS="--load"` to load the image after building.
#
# By default, the image tag is set to `latest`. `TAG` can be set to a different value.
#
# Example:
# - `make docker TAG=v1.2.3`
#
.PHONY: docker
ifeq ($(ENABLE_MULTI_PLATFORMS),true)
docker: GOARCH_LIST = amd64 arm64
docker: PLATFORMS = --platform linux/amd64,linux/arm64
endif
docker:  ## Build the Docker iamge
	@$(MAKE) build GOOS_LIST="linux" GOARCH_LIST="$(GOARCH_LIST)"
	docker buildx build . -t $(OCI_REPOSITORY_PREFIX)-$(IMAGE_NAME):$(TAG) $(PLATFORMS) $(DOCKER_BUILD_ARGS)

##@ Code Quality

.PHONY: lint
lint:  ## Run the linters
	@echo "golangci-lint => ./..."
	@$(GO_TOOL) golangci-lint run ./...

# Some IDEs like Goland place `.go` files in the `.idea` directory when using code templates. Using a
# git command to find the files ensures that only relevant files are formatted and that git-ignored
# files do not get in the way
GO_FILES=$(shell git ls-files --cached --others --exclude-standard | grep '\.go$$')
.PHONY: format
format:  ## Format code.
	@echo "format => *.go"
	@$(GO_TOOL) golangci-lint fmt $(GO_FILES)
	@echo "licenses => **"
	@$(GO_TOOL) license-eye header fix

.PHONY: check
check: format  ## Run all necessary steps to prepare for a commit and check for any differences in the codebase
	@go mod tidy
	@if [ ! -z "`git status -s`" ]; then \
		echo "The following differences will fail CI until committed:"; \
		git status; \
		echo "Please ensure you have run 'make check' and committed the changes."; \
		exit 1; \
	fi

##@ Help

.PHONY: help
help:  ## Show this help info
	@echo "Envoy Ecosystem. Discover, run, and build custom filters with zero friction.\n"
	@echo "Usage:\n  make \033[36m<Target>\033[0m \n\nTargets:"
	@awk 'BEGIN {FS = ":.*##"; printf ""} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
