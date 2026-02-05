# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# This IMAGE_NONCE will be appended to the final image tag if set.
# It is intended to be used in CI environments to ensure that each image
# built gets a unique tag.
IMAGE_NONCE ?= ""
VERSION ?= 0.2.1

OCI_REGISTRY ?= docker.io/tetratelabs
HUB ?= ${OCI_REGISTRY}/built-on-envoy

LOADER_IMG ?= ${HUB}/composer-loader:v${VERSION}${IMAGE_NONCE}

# Option to build lite version of the loader.
LOADER_LITE ?= false
ifeq ($(strip ${LOADER_LITE}),true)
LOADER_IMG := ${HUB}/composer-loader-lite:v${VERSION}${IMAGE_NONCE}
LOADER_BUILD_TAG := "lite"
else
LOADER_BUILD_TAG := ""
endif

PLUGIN_NAME ?= example-go/standalone
SANITIZED_NAME := $(subst /,-,${PLUGIN_NAME})
PLUGIN_VERSION ?= $(shell grep "version:" ../../${PLUGIN_NAME}/manifest.yaml | awk '{print $$2}')
PLUGIN_IMG ?= ${HUB}/composer-plugin:$(SANITIZED_NAME)-v${VERSION}-${PLUGIN_VERSION}${IMAGE_NONCE}

# For local development only.
.PHONY: build_loader
build_loader:
	docker build --build-context composer=../.. --build-arg BUILD_TAG=${LOADER_BUILD_TAG} \
	--progress=plain --load -t ${LOADER_IMG} -f ./Dockerfile.loader .

PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: buildx_loader
buildx_loader: ## Build and push docker image for the server for cross-platform support
	docker buildx build --build-context composer=../.. --push --platform=$(PLATFORMS) \
	  --build-arg BUILD_TAG=${LOADER_BUILD_TAG} \
	  --tag ${LOADER_IMG} -f ./Dockerfile.loader .

.PHONY: loader_so
loader_so: build_loader
	docker create --name temp-container ${LOADER_IMG} true
	docker cp temp-container:/libcomposer.so ./libcomposer.so
	docker rm temp-container

.PHONY: build_plugin
build_plugin:
	docker build --build-context composer=../.. \
	--progress=plain --load -t ${PLUGIN_IMG} -f ./Dockerfile.plugin \
	--build-arg PLUGIN_NAME=${PLUGIN_NAME} .

.PHONY: buildx_plugin
buildx_plugin: ## Build and push docker image for the plugin for cross-platform support
	docker buildx build --build-context composer=../.. --push --platform=$(PLATFORMS) \
	  --tag ${PLUGIN_IMG} -f ./Dockerfile.plugin \
	  --build-arg PLUGIN_NAME=${PLUGIN_NAME} .

.PHONY: plugin_so
plugin_so: build_plugin
	docker create --name temp-container ${PLUGIN_IMG} true
	docker cp temp-container:/plugin.so ./plugin.so
	docker rm temp-container

BOE_DATA_HOME ?= $(HOME)/.local/share/boe
ALL_GOPLUGINS := example-go/standalone

.PHONY: build_local_cache
build_local_cache:
	@echo "Installing composer...";
	@mkdir -p $(BOE_DATA_HOME)/extensions/dym/composer/$(VERSION); \
		CGO_ENABLED=1 go build -buildmode=c-shared -o libcomposer.so main.go; \
		mv libcomposer.so $(BOE_DATA_HOME)/extensions/dym/composer/$(VERSION)/libcomposer.so;
	@for plugin in $(ALL_GOPLUGINS); do \
		echo "Installing $$plugin..."; \
		cd ../../$$plugin; \
		plugin_version=$$(grep "version:" manifest.yaml | awk '{print $$2}'); \
		plugin_name=$$(grep "name:" manifest.yaml | awk '{print $$2}'); \
		CGO_ENABLED=1 go build -buildmode=plugin -o $$plugin_name.so .; \
		mkdir -p $(BOE_DATA_HOME)/extensions/goplugin/$$plugin_name/$$plugin_version; \
		mv $$plugin_name.so $(BOE_DATA_HOME)/extensions/goplugin/$$plugin_name/$$plugin_version/plugin.so; \
	done
