# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

ROOT    := $(shell git rev-parse --show-toplevel)
GO_TOOL := go tool -modfile=$(ROOT)/tools/go.mod

ENVOY_VERSION     ?= distroless-dev
ENVOY_IMAGE       ?= docker.io/envoyproxy/envoy:$(ENVOY_VERSION)
ENVOY_LOG_LEVEL   ?= warn
COMPOSER_LIB_PATH ?= $(CURDIR)/../libcomposer.so
CRS_REPO          ?= https://github.com/coreruleset/coreruleset.git
WAF_CRS_VERSION   ?= $(shell sed -n 's/^\# OWASP CRS ver\.//p' $(CURDIR)/../waf/coraza/directives/@crs-setup.conf | head -n 1)
CRS_REF           ?= v$(WAF_CRS_VERSION)
CRS_DIR           ?= $(CURDIR)/coreruleset/tests/regression/tests

GO_TEST_ARGS += -count=1 -timeout 30m -v

.PHONY: test-setup
test-setup:
	@if [ -z "$(WAF_CRS_VERSION)" ]; then \
		echo "Could not determine WAF CRS version from waf/coraza/directives/@crs-setup.conf"; \
		exit 1; \
	fi
	@if [ ! -d "$(CURDIR)/coreruleset/.git" ]; then \
		echo "Cloning Core Rule Set $(CRS_REF) into coreruleset"; \
		git clone --depth 1 --branch $(CRS_REF) --single-branch $(CRS_REPO) $(CURDIR)/coreruleset; \
	else \
		echo "Syncing coreruleset to $(CRS_REF)"; \
		git -C $(CURDIR)/coreruleset fetch --depth 1 origin refs/tags/$(CRS_REF):refs/tags/$(CRS_REF) && \
		git -C $(CURDIR)/coreruleset checkout --detach tags/$(CRS_REF); \
	fi
	@if [ ! -d "$(CRS_DIR)" ]; then \
		echo "Core Rule Set checkout exists but $(CRS_DIR) is missing at $(CRS_REF)"; \
		exit 1; \
	fi
	@echo "Ensuring integration tools are available..."
	@$(GO_TOOL) albedo --help >/dev/null
	@$(GO_TOOL) go-ftw help >/dev/null

.PHONY: test
test: test-setup
	@echo "Running WAF integration tests..."
	@COMPOSER_LIB_PATH=$(COMPOSER_LIB_PATH) \
		CRS_DIR=$(CRS_DIR) \
		ENVOY_VERSION=$(ENVOY_VERSION) \
		ENVOY_IMAGE=$(ENVOY_IMAGE) \
		DOCKER_PLATFORM=$(DOCKER_PLATFORM) \
		FTW_INCLUDE=$(FTW_INCLUDE) \
		go test -tags integration $(GO_TEST_ARGS) .

.PHONY: clean
clean:
	rm -rf *.log
	rm -rf coreruleset
