# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# This IMAGE_NONCE will be appended to the final image tag if set.
# It is intended to be used in CI environments to ensure that each image
# built gets a unique tag.
IMAGE_NONCE ?= ""
OCI_REGISTRY ?= ghcr.io/tetratelabs
ifndef EXTENSION_PATH
$(error The EXTENSION_PATH variable should be set)
endif

# Labels for this plugin image.
NAME := $(shell grep "^name:" ../../${EXTENSION_PATH}/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
VERSION := $(shell grep "^version:" ../../${EXTENSION_PATH}/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
DESCRIPTION := $(shell grep "^description:" ../../${EXTENSION_PATH}/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
TIMESTAMP := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COMMIT_SHA := $(shell git rev-parse HEAD)
AUTHOR := $(shell grep "^author:" ../../${EXTENSION_PATH}/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
COMPOSER_VERSION := $(shell grep "^composerVersion:" ../../${EXTENSION_PATH}/manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')

HUB := ${OCI_REGISTRY}/built-on-envoy
IMAGE := ${HUB}/extension-${NAME}:${VERSION}${IMAGE_NONCE}

OCI_METADATA := \
	"org.opencontainers.image.source=https://$(subst ghcr.io.,github.com,$(HUB))" \
	"org.opencontainers.image.licenses=Apache-2.0" \
	"org.opencontainers.image.title=${NAME}" \
	"org.opencontainers.image.version=${VERSION}" \
	"org.opencontainers.image.description=${DESCRIPTION}" \
	"org.opencontainers.image.created=${TIMESTAMP}" \
	"org.opencontainers.image.revision=${COMMIT_SHA}" \
	"org.opencontainers.image.authors=${AUTHOR}" \
	"io.tetratelabs.built-on-envoy.extension.type=composer" \
	"io.tetratelabs.built-on-envoy.extension.composer_version=${COMPOSER_VERSION}"

BUILD_ANNOTATIONS        := $(subst "org.,--annotation "org.,$(subst "io.,--annotation "io.,$(OCI_METADATA)))
BUILDX_ANNOTATION_PREFIX := index,manifest:
BUILDX_ANNOTATIONS       := $(subst --annotation ",--annotation "$(BUILDX_ANNOTATION_PREFIX),$(BUILD_ANNOTATIONS))

OS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH := $(shell uname -m)
ifeq ($(ARCH),x86_64)
  ARCH := amd64
else ifeq ($(ARCH),aarch64)
  ARCH := arm64
endif

# For single platform build, we will add the OS and ARCH to the image tag to avoid confusion.
.PHONY: build
build:
	docker buildx build --build-context composer=../.. \
		--output type=registry,oci-mediatypes=true \
		--provenance=false \
		$(BUILD_ANNOTATIONS) \
		--progress=plain \
		-t ${IMAGE}-${OS}-${ARCH} \
		--build-arg EXTENSION_PATH=${EXTENSION_PATH} \
		-f ./Dockerfile .

# For buildx, we default it would be multiple architectures image on Linux.
PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: buildx
buildx: ## Build and push docker image for the plugin for cross-platform support
	docker buildx build --build-context composer=../.. --platform=$(PLATFORMS) \
		--output type=registry,oci-mediatypes=true \
		--provenance=false \
		$(BUILDX_ANNOTATIONS) \
		--tag ${IMAGE} \
		--build-arg EXTENSION_PATH=${EXTENSION_PATH} \
		-f ./Dockerfile .

.PHONY: lib_so
lib_so: build
	docker create --name temp-container ${IMAGE}-${OS}-${ARCH} true
	docker cp temp-container:/plugin.so ./plugin.so
	docker rm temp-container
