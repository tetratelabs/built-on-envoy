// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// These public API tests check manifest discovery and validate derived roots
// against selected Go files without requiring extension dependencies.
package scanner_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tetratelabs/built-on-envoy/cli/tools/deadcode-lint-plugin/scanner"
)

func TestDiscoverComposerProfilesAndFiltering(t *testing.T) {
	dir := composerFixture(t)
	profile := scanner.ComposerDiscovery{Directory: dir, BuildTags: []string{"selected"}, Env: map[string]string{"DISCOVERY_TEST_VALUE": "set"}}
	targets, err := scanner.DiscoverComposer(profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].Packages[0] != "./alpha/..." {
		t.Fatalf("expected sorted alpha and beta targets, got %+v", targets)
	}
	alpha := targets[0]
	if alpha.Directory != dir || !slices.Equal(alpha.BuildTags, profile.BuildTags) || alpha.Env["DISCOVERY_TEST_VALUE"] != "set" {
		t.Fatalf("derived target lost composer profile: %+v", alpha)
	}
	if !slices.Equal(alpha.EntryPackages, []string{"./alpha/embedded"}) || !slices.Equal(alpha.FFIPackages, []string{"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"}) {
		t.Fatalf("unexpected embedded extension profile: %+v", alpha)
	}
	if len(alpha.EntrySymbols) != 1 || alpha.EntrySymbols[0] != (scanner.EntrySymbol{Package: "./alpha/standalone", Name: "WellKnownHttpFilterConfigFactories"}) {
		t.Fatalf("standalone entry symbol was not derived: %+v", alpha.EntrySymbols)
	}
	if !slices.Equal(targets[1].EntryPackages, []string{"./beta"}) || len(targets[1].EntrySymbols) != 0 {
		t.Fatalf("self-registering root fallback not derived: %+v", targets[1])
	}
	filtered, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: dir, BuildTags: []string{"selected"}, Include: []string{"beta"}})
	if err != nil || len(filtered) != 1 || filtered[0].Packages[0] != "./beta/..." {
		t.Fatalf("include filter returned unexpected targets %v, error %v", filtered, err)
	}
	empty, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: dir, Include: []string{}})
	if err == nil || len(empty) != 0 || !strings.Contains(err.Error(), "no Go extensions found") {
		t.Fatalf("explicit empty include must select no extensions, got %v, error %v", empty, err)
	}
}

func TestDiscoverComposerUsesSelectedFilesAndFactoryName(t *testing.T) {
	dir := t.TempDir()
	writeDiscoveryFixture(t, dir, "selected", "type: go\n", map[string]string{
		"impl.go":              "package selected\n",
		"zz_public_api.go":     "//go:build enabled\npackage selected\nfunc WellKnownHttpFilterConfigFactories() {}\n",
		"embedded/register.go": "package embedded\nimport dynsdk \"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go\"\nfunc init() { dynsdk.RegisterHttpFilterConfigFactories(nil) }\n",
	})
	_, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: dir})
	if err == nil || !strings.Contains(err.Error(), "WellKnownHttpFilterConfigFactories") {
		t.Fatalf("expected missing selected factory error, got %v", err)
	}
	targets, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: dir, BuildTags: []string{"enabled"}})
	if err != nil || len(targets) != 1 {
		t.Fatalf("expected selected extension, got %+v, error %v", targets, err)
	}
}

func TestDiscoverComposerExplicitErrors(t *testing.T) {
	t.Run("unknown include", func(t *testing.T) {
		_, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: composerFixture(t), Include: []string{"missing"}})
		if err == nil || !strings.Contains(err.Error(), "include entries") {
			t.Fatalf("expected unknown include error, got %v", err)
		}
	})
	t.Run("unsafe include", func(t *testing.T) {
		_, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: composerFixture(t), Include: []string{"../alpha"}})
		if err == nil || !strings.Contains(err.Error(), "invalid Composer include") {
			t.Fatalf("expected unsafe include error, got %v", err)
		}
	})
	t.Run("malformed manifest", func(t *testing.T) {
		dir := composerFixture(t)
		writeDiscoveryFile(t, dir, "bad/manifest.yaml", "type: [\n")
		_, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: dir})
		if err == nil || !strings.Contains(err.Error(), "parse manifest for \"bad\"") {
			t.Fatalf("expected malformed manifest error, got %v", err)
		}
	})
	t.Run("missing registration", func(t *testing.T) {
		dir := t.TempDir()
		writeDiscoveryFixture(t, dir, "missing", "type: go\n", map[string]string{
			"plugin.go":        "package missing\nfunc WellKnownHttpFilterConfigFactories() {}\n",
			"embedded/host.go": "package embedded\nfunc init() {}\n",
		})
		_, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: dir})
		if err == nil || !strings.Contains(err.Error(), "RegisterHttpFilterConfigFactories") {
			t.Fatalf("expected missing registration error, got %v", err)
		}
	})
	t.Run("empty embedded package", func(t *testing.T) {
		dir := t.TempDir()
		writeDiscoveryFixture(t, dir, "empty", "type: go\n", map[string]string{
			"plugin.go":         "package empty\nfunc WellKnownHttpFilterConfigFactories() {}\n",
			"embedded/data.txt": "no source",
		})
		_, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: dir})
		if err == nil || !strings.Contains(err.Error(), "no Go files") {
			t.Fatalf("expected empty embedded package error, got %v", err)
		}
	})
	t.Run("symlink adapter", func(t *testing.T) {
		dir := t.TempDir()
		writeDiscoveryFixture(t, dir, "linked", "type: go\n", map[string]string{"plugin.go": "package linked\nfunc WellKnownHttpFilterConfigFactories() {}\n"})
		if err := os.Symlink(t.TempDir(), filepath.Join(dir, "linked", "embedded")); err != nil {
			t.Fatal(err)
		}
		_, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: dir})
		if err == nil || !strings.Contains(err.Error(), "adapter directory is a symlink") {
			t.Fatalf("expected symlink adapter error, got %v", err)
		}
	})
	t.Run("no Go extensions", func(t *testing.T) {
		dir := t.TempDir()
		writeDiscoveryFile(t, dir, "rust/manifest.yaml", "type: rust\n")
		_, err := scanner.DiscoverComposer(scanner.ComposerDiscovery{Directory: dir})
		if err == nil || !strings.Contains(err.Error(), "no Go extensions found") {
			t.Fatalf("expected no extensions error, got %v", err)
		}
	})
}

func composerFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeDiscoveryFile(t, dir, "go.mod", "module test.invalid/composer\n\ngo 1.27.1\n\nrequire github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go v0.0.0\nreplace github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go => ./fake-sdk\n")
	writeDiscoveryFile(t, dir, "fake-sdk/go.mod", "module github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go\n\ngo 1.27.1\n")
	writeDiscoveryFile(t, dir, "fake-sdk/sdk.go", "package sdk\nfunc RegisterHttpFilterConfigFactories(any) {}\n")
	writeDiscoveryFixture(t, dir, "alpha", "type: go\n", map[string]string{
		"randomly_named_factory.go": "package alpha\nfunc WellKnownHttpFilterConfigFactories() {}\n",
		"embedded/register.go":      "package embedded\nimport \"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go\"\nfunc init() { sdk.RegisterHttpFilterConfigFactories(nil) }\n",
		"standalone/main.go":        "package main\nfunc WellKnownHttpFilterConfigFactories() {}\n",
	})
	writeDiscoveryFixture(t, dir, "beta", "type: go\n", map[string]string{
		"plugin.go": "package beta\nimport \"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go\"\nfunc WellKnownHttpFilterConfigFactories() {}\nfunc init() { sdk.RegisterHttpFilterConfigFactories(nil) }\n",
	})
	writeDiscoveryFile(t, dir, "rust/manifest.yaml", "type: rust\n")
	return dir
}

func writeDiscoveryFixture(t *testing.T, dir, name, manifest string, files map[string]string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		writeDiscoveryFile(t, dir, "go.mod", "module test.invalid/composer\n\ngo 1.27.1\n\nrequire github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go v0.0.0\nreplace github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go => ./fake-sdk\n")
	}
	if _, err := os.Stat(filepath.Join(dir, "fake-sdk", "go.mod")); err != nil {
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		writeDiscoveryFile(t, dir, "fake-sdk/go.mod", "module github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go\n\ngo 1.27.1\n")
		writeDiscoveryFile(t, dir, "fake-sdk/sdk.go", "package sdk\nfunc RegisterHttpFilterConfigFactories(any) {}\n")
	}
	writeDiscoveryFile(t, dir, filepath.Join(name, "manifest.yaml"), manifest)
	for path, contents := range files {
		writeDiscoveryFile(t, dir, filepath.Join(name, path), contents)
	}
}

func writeDiscoveryFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil { //nolint:gosec // Test paths are rooted under t.TempDir.
		t.Fatal(err)
	}
}
