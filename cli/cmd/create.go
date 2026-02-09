// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// Create is a command to create a new extension template.
type Create struct {
	Type string `help:"Type of the extension. Currently only 'composer' is supported." default:"composer" enum:"composer"`
	Name string `arg:"" help:"Name of the extension."`
	Path string `help:"Output directory for the extension. Defaults to the extension name." type:"path"`
}

//go:embed create_help.md
var createHelp string

// Help returns the help message for the create command.
func (c *Create) Help() string { return createHelp }

// Run executes the create command.
func (c *Create) Run(dirs *xdg.Directories) error {
	if c.Path == "" {
		c.Path = c.Name
	}

	switch c.Type {
	case "composer":
		return createComposerHTTPFilter(dirs, c.Path, c.Name)
	default:
		return fmt.Errorf("unsupported extension type: %s", c.Type)
	}
}

func createComposerHTTPFilter(dirs *xdg.Directories, path, name string) error {
	repoPath := filepath.Join(path, name)
	err := os.MkdirAll(repoPath, 0o750)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %w", repoPath, err)
	}

	data := map[string]string{
		"Name":               name,
		"LibComposerVersion": extensions.LibComposerVersion,
		"DataHome":           dirs.DataHome,
	}

	files := map[string]string{
		"plugin.go":     pluginGoTmpl,
		"manifest.yaml": manifestYamlTmpl,
		"Makefile":      makefileTmpl,
		"go.mod":        goModTmpl,
		"Dockerfile":    dockerfileTmpl,
	}

	for name, tmpl := range files {
		path := filepath.Join(repoPath, name)
		// #nosec G304
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", path, err)
		}
		defer func() {
			err = f.Close()
			if err != nil {
				fmt.Printf("Warning: failed to close file %s: %v\n", path, err)
			}
		}()

		t, err := template.New(name).Parse(tmpl)
		if err != nil {
			return fmt.Errorf("failed to parse template for %s: %w", name, err)
		}

		if err := t.Execute(f, data); err != nil {
			return fmt.Errorf("failed to execute template for %s: %w", name, err)
		}
		fmt.Printf("Created %s\n", path)
	}

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run 'go mod tidy': %w\n%s", err, string(output))
	}
	return nil
}

const pluginGoTmpl = `package main

import (
	"encoding/json"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// Config represents the JSON configuration for this filter.
type customHttpFilterConfig struct {
	HeaderValue string ` + "`json:\"header_value\"`" + `
}

// This is the implementation of the HTTP filter.
type customHttpFilter struct {
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *customHttpFilterConfig
}

func (f *customHttpFilter) OnRequestHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	// TODO: To implement your own custom logic here.
	headers.Add("x-{{ .Name }}", f.config.HeaderValue)
	f.handle.Log(shared.LogLevelInfo, "{{ .Name }}: OnRequestHeaders called")
	return shared.HeadersStatusContinue
}

func (f *customHttpFilter) OnRequestBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
	return shared.BodyStatusContinue
}

func (f *customHttpFilter) OnRequestTrailers(trailers shared.HeaderMap) shared.TrailersStatus {
	return shared.TrailersStatusContinue
}

func (f *customHttpFilter) OnResponseHeaders(headers shared.HeaderMap, endStream bool) shared.HeadersStatus {
	// TODO: To implement your own custom logic here.
	headers.Add("x-{{ .Name }}", f.config.HeaderValue)
	f.handle.Log(shared.LogLevelInfo, "{{ .Name }}: OnResponseHeaders called")
	return shared.HeadersStatusContinue
}

func (f *customHttpFilter) OnResponseBody(body shared.BodyBuffer, endStream bool) shared.BodyStatus {
	return shared.BodyStatusContinue
}

func (f *customHttpFilter) OnResponseTrailers(trailers shared.HeaderMap) shared.TrailersStatus {
	return shared.TrailersStatusContinue
}

// This is the factory for the HTTP filter.
type customHttpFilterFactory struct {
	config *customHttpFilterConfig
}

func (f *customHttpFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &customHttpFilter{handle: handle, config: f.config}
}

// This is the configuration factory for the HTTP filter.
type customHttpFilterConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *customHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	// Parse JSON configuration
	// TODO: To implement your own configuration parsing and validation logic here.
	cfg := &customHttpFilterConfig{
		HeaderValue: "example",
	}
	if len(config) > 0 {
		if err := json.Unmarshal(config, cfg); err != nil {
			handle.Log(shared.LogLevelError, "{{ .Name }}: failed to parse config: "+err.Error())
			return nil, err
		}
	}
	handle.Log(shared.LogLevelInfo, "{{ .Name }}: loaded config with header_value="+cfg.HeaderValue)
	return &customHttpFilterFactory{config: cfg}, nil
}

func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	return map[string]shared.HttpFilterConfigFactory{
		"{{ .Name }}": &customHttpFilterConfigFactory{},
	}
}
`

const manifestYamlTmpl = `name: {{ .Name }}
version: 0.0.1
categories:
  - Misc
author: Unknown
description: A custom Go extension.
longDescription: |
  A custom Go extension.
type: composer
composerVersion: {{ .LibComposerVersion }}
tags:
  - go
  - http
  - filter
license: Apache-2.0
examples: []
`

const makefileTmpl = `# WARNING: This Makefile is auto-generated. Do not modify it directly.
# If you need to customize the build process, consider using environment variables
# or wrapper scripts instead of editing this file.

# This IMAGE_NONCE will be appended to the final image tag if set.
# It is intended to be used in CI environments to ensure that each image
# built gets a unique tag.
IMAGE_NONCE  ?= ""
OCI_REGISTRY ?= ghcr.io/tetratelabs

PLUGIN_NAME := {{ .Name }}
# Default data home layout for boe
BOE_DATA_HOME ?= {{ .DataHome }}

# Labels for this plugin image.
NAME             := $(shell grep "^name:" manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
VERSION          := $(shell grep "^version:" manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
DESCRIPTION      := $(shell grep "^description:" manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
TIMESTAMP        := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COMMIT_SHA       := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
AUTHOR           := $(shell grep "^author:" manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
COMPOSER_VERSION := $(shell grep "^composerVersion:" manifest.yaml | sed 's/[^:]*:[[:space:]]*//g' | tr -d '"')
LICENSE          := Apache-2.0

HUB    := $(OCI_REGISTRY)/built-on-envoy
IMAGE  := $(HUB)/extension-$(NAME):$(VERSION)$(IMAGE_NONCE)
SOURCE := https://$(subst ghcr.io.,github.com,$(HUB))

OS   ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH ?= $(shell uname -m)
ifeq ($(ARCH),x86_64)
  ARCH := amd64
else ifeq ($(ARCH),aarch64)
  ARCH := arm64
endif

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

# For single local platform build, we will add the OS and ARCH to the image tag to avoid confusion.
.PHONY: image
image:
	docker buildx build \
		--output type=image,oci-mediatypes=true \
		--provenance=false \
		--annotation "org.opencontainers.image.source=$(SOURCE)" \
		--annotation "org.opencontainers.image.licenses=$(LICENSE)" \
		--annotation "org.opencontainers.image.title=$(NAME)" \
		--annotation "org.opencontainers.image.version=$(VERSION)" \
		--annotation "org.opencontainers.image.description=$(DESCRIPTION)" \
		--annotation "org.opencontainers.image.created=$(TIMESTAMP)" \
		--annotation "org.opencontainers.image.revision=$(COMMIT_SHA)" \
		--annotation "org.opencontainers.image.authors=$(AUTHOR)" \
		--annotation "io.tetratelabs.built-on-envoy.extension.type=composer" \
		--annotation "io.tetratelabs.built-on-envoy.extension.composer_version=$(COMPOSER_VERSION)" \
		-t $(IMAGE)-$(OS)-$(ARCH) \
		-f ./Dockerfile .

# For push, we default it would be multiple architectures image on Linux.
PLATFORMS ?= linux/arm64,linux/amd64
BUILDER_NAME := $(NAME)-builder-$(shell date +%s)
.PHONY: push
push: ## Build and push docker image for the plugin for cross-platform support
	@echo "Creating new builder: $(BUILDER_NAME)"
	docker buildx create --name $(BUILDER_NAME) --use
	@echo "Building and pushing image..."
	docker buildx build --platform=$(PLATFORMS) \
		--output type=registry,oci-mediatypes=true \
		--provenance=false \
		--annotation "index,manifest:org.opencontainers.image.source=$(SOURCE)" \
		--annotation "index,manifest:org.opencontainers.image.licenses=$(LICENSE)" \
		--annotation "index,manifest:org.opencontainers.image.title=$(NAME)" \
		--annotation "index,manifest:org.opencontainers.image.version=$(VERSION)" \
		--annotation "index,manifest:org.opencontainers.image.description=$(DESCRIPTION)" \
		--annotation "index,manifest:org.opencontainers.image.created=$(TIMESTAMP)" \
		--annotation "index,manifest:org.opencontainers.image.revision=$(COMMIT_SHA)" \
		--annotation "index,manifest:org.opencontainers.image.authors=$(AUTHOR)" \
		--annotation "index,manifest:io.tetratelabs.built-on-envoy.extension.type=composer" \
		--annotation "index,manifest:io.tetratelabs.built-on-envoy.extension.composer_version=$(COMPOSER_VERSION)" \
		--tag $(IMAGE) \
		-f ./Dockerfile .
	@echo "Removing builder: $(BUILDER_NAME)"
	docker buildx rm $(BUILDER_NAME)

.PHONY: clean
clean:
	rm -f $(PLUGIN_NAME).so
`

const goModTmpl = `module {{ .Name }}

go 1.25.6

require (
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-20260129014508-e8c1dc7dcbcd
)

`

const dockerfileTmpl = `# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

# WARNING: This Dockerfile is auto-generated. Do not modify it directly.
# If you need to customize the build process, consider creating a separate
# Dockerfile (e.g., Dockerfile.custom) instead of editing this file.

# Build the manager binary
FROM golang:1.25.6 AS builder

WORKDIR /workspace
COPY . .

RUN go mod download all

# Build go plugin
RUN CGO_ENABLED=1 go build -buildmode=plugin -o plugin.so .

FROM scratch AS final

COPY --from=builder /workspace/plugin.so /
`
