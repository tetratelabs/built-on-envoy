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

registry_flag :=
ifeq ($(INSECURE_REGISTRY),true)
  registry_flag := "--plain-http"
endif

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

.PHONY: push_lua
push_lua:
	@echo "Building and pushing Lua extension image: $(IMAGE)"
	@tar -czf $(EXTENSION_PATH).tar.gz -C ./$(EXTENSION_PATH) .
	@oras push $(registry_flag) $(IMAGE) \
		./$(EXTENSION_PATH).tar.gz:application/vnd.tetratelabs.lua.v1+tar+gzip \
		$(BASE_OCI_ANNOTATIONS) \
		--annotation "io.tetratelabs.built-on-envoy.extension.artifact=source"
	@rm -f $(EXTENSION_PATH).tar.gz
