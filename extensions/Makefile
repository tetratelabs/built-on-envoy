# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

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

EXTENSION_PATH ?=
# Ensure the EXTENSION_PATH always be set
ifeq ($(EXTENSION_PATH),)
  $(error "EXTENSION_PATH is not set. Please set it to the path of the extension.")
endif

# Labels for this lua extension image.
NAME             := $(shell grep "^name:" ./$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
VERSION          := $(shell grep "^version:" ./$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
TYPE             := $(shell grep "^type:" ./$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
DESCRIPTION      := $(shell grep "^description:" ./$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
TIMESTAMP        := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COMMIT_SHA       := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
AUTHOR           := $(shell grep "^author:" ./$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
LICENSE          := Apache-2.0

HUB    := $(OCI_REGISTRY)/built-on-envoy
IMAGE  := $(HUB)/extension-$(NAME):$(VERSION)$(IMAGE_NONCE)
SOURCE := https://$(subst ghcr.io,github.com,$(HUB))

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
	--annotation "io.tetratelabs.built-on-envoy.extension.type=$(TYPE)" \
	--annotation "io.tetratelabs.built-on-envoy.extension.version=$(VERSION)"

BUILDER_NAME := boe-builder
.PHONY: create_builder
create_builder:
	@docker buildx inspect $(BUILDER_NAME) || \
		docker buildx create --name $(BUILDER_NAME) \
			--driver docker-container --driver-opt network=host \
			--buildkitd-flags '--allow-insecure-entitlement network.host' --use

.PHONY: push_lua
push_lua: create_builder
	@echo "Building and pushing Lua extension image: $(IMAGE)"
	@docker buildx build \
		--builder $(BUILDER_NAME) \
		--output type=registry,oci-mediatypes=true$(registry_output) \
		--provenance=false \
		$(BASE_OCI_ANNOTATIONS) \
		--tag $(IMAGE) \
		--file Dockerfile.lua ./$(EXTENSION_PATH)
