# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

GODYM_IMG ?= builtonenvoy/composer-godym:latest

VERSION ?= 0.2.1

PLUGIN_NAME ?= goplugin_example
PLUGIN_IMG ?= builtonenvoy/composer-plugin-${PLUGIN_NAME}:latest

# For local development only.
.PHONY: build_godym
build_godym:
	docker build --progress=plain --load -t ${GODYM_IMG} -f ./Dockerfile.godym .

PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: buildx_godym
buildx_godym: ## Build and push docker image for the server for cross-platform support
	- docker buildx create --name composer-builder --driver-opt network=host
	- docker buildx use composer-builder
	- docker buildx build --push --platform=$(PLATFORMS) \
	  --tag ${GODYM_IMG} -f ./Dockerfile.godym .
	- docker buildx rm composer-builder

.PHONY: godym_so
godym_so: build_godym
	docker create --name temp-container ${GODYM_IMG} true
	docker cp temp-container:/libcomposer.so ./libcomposer.so
	docker rm temp-container

.PHONY: build_plugin
build_plugin:
	docker build --progress=plain --load -t ${PLUGIN_IMG} -f ./Dockerfile.plugin \
		--build-arg PLUGIN_NAME=${PLUGIN_NAME} .

.PHONY: buildx_plugin
buildx_plugin: ## Build and push docker image for the plugin for cross-platform support
	- docker buildx create --name composer-builder --driver-opt network=host
	- docker buildx use composer-builder
	- docker buildx build --push --platform=$(PLATFORMS) \
	  --tag ${PLUGIN_IMG} -f ./Dockerfile.plugin \
	  --build-arg PLUGIN_NAME=${PLUGIN_NAME} .
	- docker buildx rm composer-builder

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
		go build -buildmode=c-shared -o libcomposer.so main.go; \
		mv libcomposer.so $(BOE_DATA_HOME)/extensions/dym/composer/$(VERSION)/libcomposer.so;
	@for plugin in $(ALL_GOPLUGINS); do \
		echo "Installing $$plugin..."; \
		cd ../../$$plugin; \
		plugin_version=$$(grep "version:" manifest.yaml | awk '{print $$2}'); \
		plugin_name=$$(grep "name:" manifest.yaml | awk '{print $$2}'); \
		go build -buildmode=plugin -o $$plugin_name.so .; \
		mkdir -p $(BOE_DATA_HOME)/extensions/goplugin/$$plugin_name/$$plugin_version; \
		mv $$plugin_name.so $(BOE_DATA_HOME)/extensions/goplugin/$$plugin_name/$$plugin_version/plugin.so; \
	done
