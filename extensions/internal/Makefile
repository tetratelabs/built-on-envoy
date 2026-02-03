# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

ROOT    := $(shell git rev-parse --show-toplevel)
GO_TOOL := go tool -modfile=$(ROOT)/cli/tools/go.mod

GO_TEST_ARGS ?=

# Some IDEs like Goland place `.go` files in the `.idea` directory when using code templates. Using a
# git command to find the files ensures that only relevant files are formatted and that git-ignored
# files do not get in the way
GO_FILES=$(shell git ls-files --cached --others --exclude-standard | grep '\.go$$')
.PHONY: format
format: 
	@echo "format => *.go"
	@$(GO_TOOL) golangci-lint fmt $(GO_FILES)
	@echo "licenses => **"
	@$(GO_TOOL) license-eye -c $(ROOT)/.licenserc.yaml header fix

.PHONY: lint
lint: 
	@echo "golangci-lint => ./..."
	@$(GO_TOOL) golangci-lint run

.PHONY: test
test:
	@echo "Running tests..."
	@go test $(GO_TEST_ARGS) ./...
