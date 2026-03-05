PLUGIN_NAME := add-header-ext
# Default data home layout for boe
BOE_DATA_HOME ?= /Users/varuntalwar/.local/share/boe

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
