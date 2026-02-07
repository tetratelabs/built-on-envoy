# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# This IMAGE_NONCE will be appended to the final image tag if set.
# It is intended to be used in CI environments to ensure that each image
# built gets a unique tag.
IMAGE_NONCE  ?= ""
OCI_REGISTRY ?= ghcr.io/tetratelabs
ifndef EXTENSION_PATH
$(error The EXTENSION_PATH variable should be set)
endif

# Labels for this plugin image.
NAME             := $(shell grep "^name:" ../../$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
VERSION          := $(shell grep "^version:" ../../$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
DESCRIPTION      := $(shell grep "^description:" ../../$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
TIMESTAMP        := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COMMIT_SHA       := $(shell git rev-parse HEAD)
AUTHOR           := $(shell grep "^author:" ../../$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
COMPOSER_VERSION := $(shell grep "^composerVersion:" ../../$(EXTENSION_PATH)/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
LICENSE          := Apache-2.0

HUB    := $(OCI_REGISTRY)/built-on-envoy
IMAGE  := $(HUB)/extension-$(NAME):$(VERSION)$(IMAGE_NONCE)
SOURCE := https://$(subst ghcr.io.,github.com,$(HUB))

OS   ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH ?= $(shell uname -m)
ifeq ($(ARCH),x86_64)
  ARCH := amd64
else ifeq ($(ARCH),aarch64)
  ARCH := arm64
endif

# For single local platform build, we will add the OS and ARCH to the image tag to avoid confusion.
.PHONY: build
build:
	docker buildx build --build-context composer=../.. \
		--output type=image,oci-mediatypes=true \
		--provenance=false \
		--annotation "org.opencontainers.image.source=$(SOURCE)" \
		--annotation "org.opencontainers.image.licenses=$(LICENSE)" \
		--annotation "org.opencontainers.image.title=$(NAME)" \
		--annotation "org.opencontainers.image.version=$(VERSION)" \
		--annotation "org.opencontainers.image.description=$(DESCRIPTION)" \
		--annotation "org.opencontainers.image.created=$(TIMESTAMP)" \
		--annotation "org.opencontainers.image.revision=$(COMMIT_SHA)" \
		--annotation "org.opencontainers.image.authors=$(AUTHOR)" \
		--annotation "io.tetratelabs.built-on-envoy.extension.type=composer" \
		--annotation "io.tetratelabs.built-on-envoy.extension.composer_version=$(COMPOSER_VERSION)" \
		-t $(IMAGE)-$(OS)-$(ARCH) \
		--build-arg EXTENSION_PATH=$(EXTENSION_PATH) \
		-f ./Dockerfile .

# For push, we default it would be multiple architectures image on Linux.
PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: push
push: ## Build and push docker image for the plugin for cross-platform support
	docker buildx build --build-context composer=../.. --platform=$(PLATFORMS) \
		--output type=registry,oci-mediatypes=true \
		--provenance=false \
		--annotation "index,manifest:org.opencontainers.image.source=$(SOURCE)" \
		--annotation "index,manifest:org.opencontainers.image.licenses=$(LICENSE)" \
		--annotation "index,manifest:org.opencontainers.image.title=$(NAME)" \
		--annotation "index,manifest:org.opencontainers.image.version=$(VERSION)" \
		--annotation "index,manifest:org.opencontainers.image.description=$(DESCRIPTION)" \
		--annotation "index,manifest:org.opencontainers.image.created=$(TIMESTAMP)" \
		--annotation "index,manifest:org.opencontainers.image.revision=$(COMMIT_SHA)" \
		--annotation "index,manifest:org.opencontainers.image.authors=$(AUTHOR)" \
		--annotation "index,manifest:io.tetratelabs.built-on-envoy.extension.type=composer" \
		--annotation "index,manifest:io.tetratelabs.built-on-envoy.extension.composer_version=$(COMPOSER_VERSION)" \
		--tag $(IMAGE) \
		--build-arg EXTENSION_PATH=$(EXTENSION_PATH) \
		-f ./Dockerfile .

.PHONY: lib_so
lib_so: build
	docker create --name temp-container $(IMAGE)-$(OS)-$(ARCH) true
	docker cp temp-container:/plugin.so ./plugin.so
	docker rm temp-container
