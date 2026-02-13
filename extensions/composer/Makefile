# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

ROOT    := $(shell git rev-parse --show-toplevel)
GO_TOOL := go tool -modfile=$(ROOT)/cli/tools/go.mod

GO_TEST_ARGS ?=

# Some IDEs like Goland place `.go` files in the `.idea` directory when using code templates. Using a
# git command to find the files ensures that only relevant files are formatted and that git-ignored
# files do not get in the way
GO_FILES=$(shell git ls-files --cached --others --exclude-standard | grep '\.go$$')
.PHONY: format
format: 
	@echo "format => *.go"
	@$(GO_TOOL) golangci-lint fmt $(GO_FILES)
	@echo "licenses => **"
	@$(GO_TOOL) license-eye -c $(ROOT)/.licenserc.yaml header fix

.PHONY: lint
lint:
	@echo "golangci-lint => ./..."
	@$(GO_TOOL) golangci-lint run

.PHONY: test
test:
	@echo "Running tests..."
	@go test $(GO_TEST_ARGS) ./...

# This IMAGE_NONCE will be appended to the final image tag if set.
# It is intended to be used in CI environments to ensure that each image
# built gets a unique tag.
IMAGE_NONCE  ?=
OCI_REGISTRY ?= ghcr.io/tetratelabs
INSECURE_REGISTRY ?= false

registry_output :=
ifeq ($(INSECURE_REGISTRY),true)
  registry_output := ",registry.insecure=true"
endif

# Default data home layout for boe
BOE_DATA_HOME ?= $(HOME)/.local/share/boe

# Labels for this plugin image.
NAME             := $(shell grep "^name:" manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
VERSION          := $(shell grep "^version:" manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
DESCRIPTION      := $(shell grep "^description:" manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
TIMESTAMP        := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COMMIT_SHA       := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
AUTHOR           := $(shell grep "^author:" manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
COMPOSER_VERSION := $(VERSION)
LICENSE          := Apache-2.0

HUB    := $(OCI_REGISTRY)/built-on-envoy
IMAGE  := $(HUB)/$(NAME):$(VERSION)$(IMAGE_NONCE)
SOURCE := https://$(subst ghcr.io,github.com,$(HUB))

# Option to build lite version of the loader.
ifeq ($(strip $(COMPOSER_LITE)),true)
	DESCRIPTION := Go plugin loader.
	COMPOSER_BUILD_TAG := lite
	IMAGE := $(HUB)/$(NAME)-lite:$(VERSION)$(IMAGE_NONCE)
else
	COMPOSER_BUILD_TAG := ""
endif

OS := linux
ARCH := $(shell uname -m)
ifeq ($(ARCH),x86_64)
  ARCH := amd64
else ifeq ($(ARCH),aarch64)
  ARCH := arm64
endif

# Base OCI image annotations (shared by all image types)
BASE_OCI_ANNOTATIONS := \
	--annotation "org.opencontainers.image.source=$(SOURCE)" \
	--annotation "org.opencontainers.image.licenses=$(LICENSE)" \
	--annotation "org.opencontainers.image.title=$(NAME)" \
	--annotation "org.opencontainers.image.version=$(VERSION)" \
	--annotation "org.opencontainers.image.description=$(DESCRIPTION)" \
	--annotation "org.opencontainers.image.created=$(TIMESTAMP)" \
	--annotation "org.opencontainers.image.revision=$(COMMIT_SHA)" \
	--annotation "org.opencontainers.image.authors=$(AUTHOR)" \
	--annotation "io.tetratelabs.built-on-envoy.extension.type=composer" \
	--annotation "io.tetratelabs.built-on-envoy.extension.composer_version=$(COMPOSER_VERSION)"

# Artifact type annotations
BINARY_ARTIFACT := --annotation "io.tetratelabs.built-on-envoy.extension.artifact=binary"
SOURCE_ARTIFACT := --annotation "io.tetratelabs.built-on-envoy.extension.artifact=source"

# Multi-platform OCI annotations (with index,manifest prefix, for binary artifacts)
comma := ,
MULTI_OCI_ANNOTATIONS := \
	$(subst --annotation ",--annotation "index$(comma)manifest:,$(BASE_OCI_ANNOTATIONS)) \
	--annotation "index,manifest:io.tetratelabs.built-on-envoy.extension.artifact=binary"

BUILDER_NAME := boe-builder
.PHONY: create_builder
create_builder:
	@docker buildx inspect $(BUILDER_NAME) || \
		docker buildx create --name $(BUILDER_NAME) \
			--driver docker-container --driver-opt network=host \
			--buildkitd-flags '--allow-insecure-entitlement network.host' --use

.PHONY: build
build:
	CGO_ENABLED=1 go build -buildmode=c-shared -tags $(COMPOSER_BUILD_TAG) -o lib$(NAME).so ./main

.PHONY: test
test:
	go test $(GO_TEST_ARGS) ./...

.PHONY: install
install: build
	@echo "Installing $(NAME)..."
	mkdir -p $(BOE_DATA_HOME)/extensions/dym/$(NAME)/build/$(VERSION); \
	mv lib$(NAME).so $(BOE_DATA_HOME)/extensions/dym/$(NAME)/build/$(VERSION)/lib$(NAME).so;
	@echo "Installed to $(BOE_DATA_HOME)/extensions/dym/$(NAME)/build"

# For single local platform build, we will add the OS and ARCH to the image tag to avoid confusion.
# And we will not push it to the registry by default.
.PHONY: build_image
build_image:
	docker buildx build \
		--output type=docker,oci-mediatypes=true \
		--provenance=false \
		$(BASE_OCI_ANNOTATIONS) \
		$(BINARY_ARTIFACT) \
		-t $(IMAGE)-linux-$(ARCH) \
		-f ./Dockerfile .

# For default push operation, we will also prefer to build a multi-platform image to support
# both amd64 and arm64 architectures, and push it to the registry.
PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: push_image
push_image: create_builder  ## Build and push docker image for the plugin for cross-platform support
	@echo "Building and pushing image..."
	docker buildx build --platform=$(PLATFORMS) \
		--builder $(BUILDER_NAME) \
		--output type=registry,oci-mediatypes=true$(registry_output) \
		--provenance=false \
		$(MULTI_OCI_ANNOTATIONS) \
		--tag $(IMAGE) \
		-f ./Dockerfile .

# Package source code into an image and push it to the registry.
CODE_IMAGE := $(HUB)/$(NAME)-src:$(VERSION)$(IMAGE_NONCE)
.PHONY: push_code
push_code: create_builder  ## Build and push source code image
	@echo "Building and pushing source code image..."
	docker buildx build \
		--builder $(BUILDER_NAME) \
		--output type=registry,oci-mediatypes=true$(registry_output) \
		--provenance=false \
		$(BASE_OCI_ANNOTATIONS) \
		$(SOURCE_ARTIFACT) \
		--tag $(CODE_IMAGE) \
		-f ./Dockerfile.code .

# Find all sub-directories containing a manifest.yaml file and get their names.
# Ignore current directory.
PLUGIN_DIRS := $(shell find . -mindepth 2 -maxdepth 2 -type f -name 'manifest.yaml' -exec dirname {} \; | sort)
PLUGIN_CLEAN_DIRS := $(notdir $(PLUGIN_DIRS))

push_plugins: ## Push all plugin images
	@for dir in $(PLUGIN_CLEAN_DIRS); do \
		$(MAKE) -f Makefile.plugin push_image EXTENSION_PATH=$$dir; \
	done

build_plugins: ## Build all plugins locally
	@for dir in $(PLUGIN_CLEAN_DIRS); do \
		$(MAKE) -f Makefile.plugin build EXTENSION_PATH=$$dir; \
	done

install_plugins: ## Install all plugins locally
	@for dir in $(PLUGIN_CLEAN_DIRS); do \
		$(MAKE) -f Makefile.plugin install EXTENSION_PATH=$$dir; \
	done
