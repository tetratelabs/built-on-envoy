# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# This IMAGE_NONCE will be appended to the final image tag if set.
# It is intended to be used in CI environments to ensure that each image
# built gets a unique tag.
IMAGE_NONCE ?= ""
OCI_REGISTRY ?= docker.io/tetratelabs
COMPOSER_LITE ?= false

NAME := composer

# Labels for this composer image.
VERSION := 0.2.1
DESCRIPTION := Extension set of all in-tree golang dynamic module extensions and Go plugin loader.
TIMESTAMP := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COMMIT_SHA := $(shell git rev-parse HEAD)
AUTHOR := Tetrate
COMPOSER_VERSION := ${VERSION}

# Option to build lite version of the loader.
ifeq ($(strip ${COMPOSER_LITE}),true)
	NAME := composer-lite
	DESCRIPTION := Go plugin loader.
	COMPOSER_BUILD_TAG := lite
else
	COMPOSER_BUILD_TAG := ""
endif

HUB := ${OCI_REGISTRY}/built-on-envoy
IMAGE := ${HUB}/${NAME}:${VERSION}${IMAGE_NONCE}

OS := linux
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
	--annotation "org.opencontainers.image.source=https://github.com/tetratelabs/built-on-envoy" \
	--annotation "org.opencontainers.image.licenses=Apache-2.0" \
	--annotation "org.opencontainers.image.title=${NAME}" \
	--annotation "org.opencontainers.image.version=${VERSION}" \
	--annotation "org.opencontainers.image.description=${DESCRIPTION}" \
	--annotation "org.opencontainers.image.created=${TIMESTAMP}" \
	--annotation "org.opencontainers.image.revision=${COMMIT_SHA}" \
	--annotation "org.opencontainers.image.authors=${AUTHOR}" \
	--annotation "io.tetratelabs.built-on-envoy.extension.type=dynamic_module" \
	--annotation "io.tetratelabs.built-on-envoy.extension.composer_version=${COMPOSER_VERSION}" \
	--progress=plain \
	-t ${IMAGE}-${OS}-${ARCH} \
	--build-arg BUILD_TAG=${COMPOSER_BUILD_TAG} \
	 -f ./Dockerfile .

# For buildx, we default it would be multiple architectures image on Linux.
PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: buildx
buildx: ## Build and push docker image for the server for cross-platform support
	docker buildx build --build-context composer=../.. --platform=$(PLATFORMS) \
	  --output type=registry,oci-mediatypes=true \
	  --provenance=false \
	  --annotation "index,manifest:org.opencontainers.image.source=https://github.com/tetratelabs/built-on-envoy" \
	  --annotation "index,manifest:org.opencontainers.image.licenses=Apache-2.0" \
	  --annotation "index,manifest:org.opencontainers.image.title=${NAME}" \
	  --annotation "index,manifest:org.opencontainers.image.version=${VERSION}" \
	  --annotation "index,manifest:org.opencontainers.image.description=${DESCRIPTION}" \
	  --annotation "index,manifest:org.opencontainers.image.created=${TIMESTAMP}" \
	  --annotation "index,manifest:org.opencontainers.image.revision=${COMMIT_SHA}" \
	  --annotation "index,manifest:org.opencontainers.image.authors=${AUTHOR}" \
	  --annotation "index,manifest:io.tetratelabs.built-on-envoy.extension.type=dynamic_module" \
	  --annotation "index,manifest:io.tetratelabs.built-on-envoy.extension.composer_version=${COMPOSER_VERSION}" \
	  --tag ${IMAGE} \
	  --build-arg BUILD_TAG=${COMPOSER_BUILD_TAG} \
	  -f ./Dockerfile .

.PHONY: lib_so
lib_so: build
	docker create --name temp-container ${IMAGE}-${OS}-${ARCH} true
	docker cp temp-container:/libcomposer.so ./libcomposer.so
	docker rm temp-container

BOE_DATA_HOME ?= $(HOME)/.local/share/boe
ALL_GOPLUGIN_PATH := example-go/standalone

.PHONY: build_local_cache
build_local_cache:
	@echo "Installing composer...";
	@mkdir -p $(BOE_DATA_HOME)/extensions/dym/composer/$(VERSION); \
		CGO_ENABLED=1 go build -buildmode=c-shared -o libcomposer.so .; \
		mv libcomposer.so $(BOE_DATA_HOME)/extensions/dym/composer/$(VERSION)/libcomposer.so;
	@for plugin_path in $(ALL_GOPLUGIN_PATH); do \
		plugin_version=$$(grep "^version:" ../../$$plugin_path/manifest.yaml | awk '{print $$2}'); \
		plugin_name=$$(grep "^name:" ../../$$plugin_path/manifest.yaml | awk '{print $$2}'); \
		echo "Installing $$plugin_name: $$plugin_version..."; \
		CGO_ENABLED=1 go build -buildmode=plugin -o $$plugin_name.so ../../$$plugin_path; \
		mkdir -p $(BOE_DATA_HOME)/extensions/goplugin/$$plugin_name/$$plugin_version; \
		mv $$plugin_name.so $(BOE_DATA_HOME)/extensions/goplugin/$$plugin_name/$$plugin_version/plugin.so; \
	done
