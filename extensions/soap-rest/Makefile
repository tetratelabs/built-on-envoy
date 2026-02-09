# Makefile for the soap-rest extension.
#
# Targets:
#   build   - Compile the Go plugin (.so) using -buildmode=plugin
#   install - Build and copy the .so to boe's extension directory (versioned path)
#   clean   - Remove the compiled .so file
#
# Override BOE_DATA_HOME to install to a custom location:
#   make install BOE_DATA_HOME=/custom/path

PLUGIN_NAME := soap-rest

# Default data home layout for boe. The install target copies the compiled plugin
# into this directory under extensions/goplugin/<name>/<version>/plugin.so,
# which is where boe discovers standalone Go plugins at runtime.
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
