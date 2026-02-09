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

const makefileTmpl = `PLUGIN_NAME := {{ .Name }}
# Default data home layout for boe
BOE_DATA_HOME ?= {{ .DataHome }}

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
`

const goModTmpl = `module {{ .Name }}

go 1.25.6

require (
	github.com/envoyproxy/envoy/source/extensions/dynamic_modules v0.0.0-20260129014508-e8c1dc7dcbcd
)

`
