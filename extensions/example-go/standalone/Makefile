# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

PLUGIN_NAME := example-go
# Default data home layout for boe
BOE_DATA_HOME ?= $(HOME)/.local/share/boe

.PHONY: build
build:
	go build -buildmode=plugin -o $(PLUGIN_NAME).so .

.PHONY: install
install: build
	@echo "Installing $(PLUGIN_NAME)..."
	@version=$$(grep "version:" manifest.yaml | awk '{print $$2}'); \
	mkdir -p $(BOE_DATA_HOME)/extensions/goplugin/$(PLUGIN_NAME)/$$version; \
	cp $(PLUGIN_NAME).so $(BOE_DATA_HOME)/extensions/goplugin/$(PLUGIN_NAME)/$$version/plugin.so;
	@echo "Installed to $(BOE_DATA_HOME)/extensions/goplugin/$(PLUGIN_NAME)"

.PHONY: clean
clean:
	rm -f $(PLUGIN_NAME).so
