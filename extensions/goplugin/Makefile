# Copyright (c) Tetrate, Inc 2025 All Rights Reserved.

BUF      := github.com/bufbuild/buf/cmd/buf@v1.31.0
SHELL    := /bin/bash
repo_dir := $(shell git rev-parse --show-toplevel)

########################
# Setup
########################

.PHONY: all
all: gen

.PHONY: gen
gen: buf

#####################
# proto generation
#####################

# This is a warning we want to ignore and silently drop from the output. It appears when running buf
# on some paths but not all of them, and warns about missing dependencies that are not actually missing.
dep_warning := "is not found in your local files or direct dependencies, but is found in the transitive dependency"

.PHONY: buf
buf:
	@echo "Building protos..."
	@go run $(BUF) build

	@echo "Generating Go code..."
	@go run $(BUF) generate --template buf.gen.go.yaml

#####################
# Cleanup
#####################

.PHONY: clean
clean:
	@echo "Cleaning generated code..."
	@find . -name '*.gen.go' -delete
	@find . -name '*.gen.json' -delete
	@find . -name '*.pb.*' -delete
