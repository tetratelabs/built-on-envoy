// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This file verifies the runtime plugin through the pinned golangci command so
// plugin loading, whole-program roots, and cross-package cache invalidation are covered together.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/types"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/tetratelabs/built-on-envoy/cli/tools/deadcode-lint-plugin/scanner"
)

func TestRuntimePluginCommand(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skipf("Go runtime plugins are unsupported on %s", runtime.GOOS)
	}
	cgo, err := exec.Command("go", "env", "CGO_ENABLED").Output()
	if err != nil {
		t.Fatalf("read CGO_ENABLED: %v", err)
	}
	if strings.TrimSpace(string(cgo)) != "1" {
		t.Skip("Go runtime plugins require CGO_ENABLED=1")
	}

	pluginDir, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	toolsMod := filepath.Join(pluginDir, "..", "go.mod")
	tempDir := t.TempDir()
	pluginPath := filepath.Join(tempDir, "ffideadcode.so")
	build := exec.Command("go", "build", "-buildmode=plugin", "-o", pluginPath, "./runtime-plugin") //nolint:gosec // The output is confined to this test's temporary directory.
	build.Dir = pluginDir
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build runtime plugin: %v\n%s", buildErr, output)
	}

	fixtureDir := filepath.Join(tempDir, "fixture")
	if copyErr := copyTree(filepath.Join(pluginDir, "testdata", "fixture"), fixtureDir); copyErr != nil {
		t.Fatalf("copy fixture: %v", copyErr)
	}
	fixturePath, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(fixturePath, "probe", "manifest.yaml")
	if manifestErr := os.WriteFile(manifestPath, []byte("type: go\n"), 0o600); manifestErr != nil { //nolint:gosec // The manifest is in the copied temporary fixture.
		t.Fatalf("write extension manifest: %v", manifestErr)
	}
	cacheDir := filepath.Join(tempDir, "golangci-cache")
	configPath := filepath.Join(tempDir, "golangci.yml")
	config := map[string]any{
		"version": "2",
		"linters": map[string]any{
			"default": "none",
			"enable":  []string{"govet"},
			"settings": map[string]any{
				"custom": map[string]any{
					"ffideadcode": map[string]any{
						"type": "goplugin",
						"path": pluginPath,
						"settings": map[string]any{
							"module":   "github.com/tetratelabs/built-on-envoy/cli/tools/deadcode-lint-plugin/fixture",
							"composer": scanner.ComposerDiscovery{Directory: "."},
						},
					},
				},
			},
		},
	}
	encodedConfig, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("encode golangci config: %v", err)
	}
	if configWriteErr := os.WriteFile(configPath, encodedConfig, 0o600); configWriteErr != nil { //nolint:gosec // The config path is inside this test's temporary directory.
		t.Fatalf("write golangci config: %v", configWriteErr)
	}

	runLintFrom := func(directory string) ([]byte, error) {
		cmd := exec.Command("go", "tool", "-modfile="+toolsMod, "golangci-lint", "run", "--config", configPath, "--enable", linterName, "./...") //nolint:gosec // Paths point to this test's temporary fixture, config, cache, and repository module file.
		cmd.Dir = directory
		cmd.Env = append(os.Environ(), "GOLANGCI_LINT_CACHE="+cacheDir)
		return cmd.CombinedOutput()
	}
	runLint := func() ([]byte, error) { return runLintFrom(fixturePath) }
	assertDiagnostics := func(label string, output []byte, runErr error, want []string, absent []string) {
		t.Helper()
		var exitError *exec.ExitError
		if !errors.As(runErr, &exitError) || exitError.ExitCode() != 1 {
			t.Fatalf("%s: expected diagnostics exit status 1, got %v\n%s", label, runErr, output)
		}
		text := string(output)
		if got := strings.Count(text, "unreachable function:"); got != len(want) {
			t.Fatalf("%s: expected %d unreachable diagnostics, got %d\n%s", label, len(want), got, output)
		}
		for _, name := range want {
			if !strings.Contains(text, name) {
				t.Errorf("%s: missing diagnostic for %s:\n%s", label, name, output)
			}
		}
		for _, name := range absent {
			if strings.Contains(text, name) {
				t.Errorf("%s: unexpected diagnostic for %s:\n%s", label, name, output)
			}
		}
	}
	want := []string{"ExportedOrphan", "ExportedTestOnly", "unusedHelper", "unregisteredFilter).OnRequestHeaders"}
	for _, label := range []string{"initial scan", "cached scan"} {
		output, runErr := runLint()
		assertDiagnostics(label, output, runErr, want, []string{"genericLive"})
	}
	nestedOutput, nestedErr := runLintFrom(filepath.Join(fixturePath, "probe"))
	assertDiagnostics("module subdirectory", nestedOutput, nestedErr, want, []string{"genericLive"})

	pluginSourcePath := filepath.Join(fixturePath, "probe", "plugin.go")
	pluginSource, pluginReadErr := os.ReadFile(pluginSourcePath) //nolint:gosec // The source file is in the copied temporary fixture.
	if pluginReadErr != nil {
		t.Fatalf("read plugin source: %v", pluginReadErr)
	}
	withSuppression := bytes.Replace(pluginSource, []byte("func ExportedTestOnly()"), []byte("//nolint:ffideadcode // This fixture intentionally keeps a test-only helper.\nfunc ExportedTestOnly()"), 1)
	if bytes.Equal(withSuppression, pluginSource) {
		t.Fatal("could not find the test-only function to suppress")
	}
	if suppressErr := os.WriteFile(pluginSourcePath, withSuppression, 0o600); suppressErr != nil { //nolint:gosec // The source file is in the copied temporary fixture.
		t.Fatalf("add suppression: %v", suppressErr)
	}
	for _, label := range []string{"preceding nolint comment", "cached nolint comment"} {
		output, runErr := runLint()
		assertDiagnostics(label, output, runErr, []string{"ExportedOrphan", "unusedHelper", "unregisteredFilter).OnRequestHeaders"}, []string{"ExportedTestOnly"})
	}
	if unsuppressErr := os.WriteFile(pluginSourcePath, pluginSource, 0o600); unsuppressErr != nil { //nolint:gosec // The source file is in the copied temporary fixture.
		t.Fatalf("remove suppression: %v", unsuppressErr)
	}
	output, runErr := runLint()
	assertDiagnostics("removed nolint comment", output, runErr, want, nil)

	// Calls from a second extension must not hide a function dead in the first extension.
	secondFiles := map[string]string{
		"second/manifest.yaml": "type: go\n",
		"second/filter.go": `package second
import "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { return nil }
func unusedSecond() {}
`,
		"second/embedded/register.go": `package embedded
import (
 sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
 first "github.com/tetratelabs/built-on-envoy/cli/tools/deadcode-lint-plugin/fixture/probe"
 second "github.com/tetratelabs/built-on-envoy/cli/tools/deadcode-lint-plugin/fixture/second"
)
func init() {
 sdk.RegisterHttpFilterConfigFactories(second.WellKnownHttpFilterConfigFactories())
 first.ExportedTestOnly()
}
`,
	}
	for relative, contents := range secondFiles {
		path := filepath.Join(fixturePath, relative)
		if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o750); mkdirErr != nil { //nolint:gosec // The path is in the copied temporary fixture.
			t.Fatalf("create second extension: %v", mkdirErr)
		}
		if writeErr := os.WriteFile(path, []byte(contents), 0o600); writeErr != nil { //nolint:gosec // The path is in the copied temporary fixture.
			t.Fatalf("write second extension: %v", writeErr)
		}
	}
	for _, label := range []string{"discovered second extension", "cached second extension"} {
		output, runErr = runLint()
		assertDiagnostics(label, output, runErr, append(append([]string(nil), want...), "unusedSecond"), nil)
	}
	secondManifest := filepath.Join(fixturePath, "second", "manifest.yaml")
	if removeErr := os.Remove(secondManifest); removeErr != nil { //nolint:gosec // The manifest is in the copied temporary fixture.
		t.Fatalf("remove second extension manifest: %v", removeErr)
	}
	output, runErr = runLint()
	assertDiagnostics("removed second extension manifest", output, runErr, want, []string{"unusedSecond"})

	embeddedPath := filepath.Join(fixturePath, "probe", "embedded", "host.go")
	embeddedSource, sourceReadErr := os.ReadFile(embeddedPath) //nolint:gosec // This path identifies a source file in the copied temporary fixture.
	if sourceReadErr != nil {
		t.Fatalf("read embedded host: %v", sourceReadErr)
	}
	withNewRoot := bytes.Replace(embeddedSource, []byte("func init() { sdk.RegisterHttpFilterConfigFactories(impl.WellKnownHttpFilterConfigFactories()) }"), []byte("func init() { sdk.RegisterHttpFilterConfigFactories(impl.WellKnownHttpFilterConfigFactories()); impl.ExportedTestOnly() }"), 1)
	if bytes.Equal(withNewRoot, embeddedSource) {
		t.Fatal("could not find embedded registration initializer to extend")
	}
	if addRootErr := os.WriteFile(embeddedPath, withNewRoot, 0o600); addRootErr != nil { //nolint:gosec // This path identifies a source file in the copied temporary fixture.
		t.Fatalf("add cross-package root: %v", addRootErr)
	}
	output, runErr = runLint()
	assertDiagnostics("new cross-package root", output, runErr, []string{"ExportedOrphan", "unusedHelper", "unregisteredFilter).OnRequestHeaders"}, []string{"ExportedTestOnly"})

	if restoreErr := os.WriteFile(embeddedPath, embeddedSource, 0o600); restoreErr != nil { //nolint:gosec // This path identifies a source file in the copied temporary fixture.
		t.Fatalf("restore embedded host: %v", restoreErr)
	}
	output, runErr = runLint()
	assertDiagnostics("restored cross-package root", output, runErr, want, nil)

	brokenSource := append(append([]byte(nil), embeddedSource...), []byte("\nfunc init() { impl.MissingSymbol() }\n")...)
	if breakErr := os.WriteFile(embeddedPath, brokenSource, 0o600); breakErr != nil { //nolint:gosec // This path identifies a source file in the copied temporary fixture.
		t.Fatalf("break embedded source: %v", breakErr)
	}
	output, runErr = runLint()
	if runErr == nil || !strings.Contains(string(output), "packages contain errors") {
		t.Fatalf("expected package loading failure to be explicit, got %v\n%s", runErr, output)
	}

	// A broken Composer tree must not affect a different module using the same config.
	unrelatedDir := filepath.Join(tempDir, "cli")
	if mkdirErr := os.Mkdir(unrelatedDir, 0o750); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	for name, contents := range map[string]string{
		"go.mod": "module test.invalid/cli\n\ngo 1.27.1\n",
		"cli.go": "package cli\nfunc Keep() {}\n",
	} {
		if writeErr := os.WriteFile(filepath.Join(unrelatedDir, name), []byte(contents), 0o600); writeErr != nil { //nolint:gosec // Files are confined to this test's temporary module.
			t.Fatal(writeErr)
		}
	}
	for _, enabled := range []bool{false, true} {
		args := []string{"tool", "-modfile=" + toolsMod, "golangci-lint", "run", "--config", configPath, "--verbose"}
		if enabled {
			args = append(args, "--enable", linterName)
		}
		args = append(args, "./...")
		cmd := exec.Command("go", args...) //nolint:gosec // Arguments use repository tools and temporary test paths.
		cmd.Dir = unrelatedDir
		cmd.Env = append(os.Environ(), "GOLANGCI_LINT_CACHE="+cacheDir)
		otherOutput, otherErr := cmd.CombinedOutput()
		if otherErr != nil || !strings.Contains(string(otherOutput), "Loaded "+pluginPath+": "+linterName) {
			t.Fatalf("unrelated module (enabled=%t): expected loaded plugin without scanning, got %v\n%s", enabled, otherErr, otherOutput)
		}
	}
}

func TestModuleScope(t *testing.T) {
	for _, test := range []struct {
		name         string
		module       string
		nestedModule string
		wantError    string
	}{
		{name: "unrelated module", module: "module test.invalid/cli\n"},
		{name: "module name prefix", module: "module test.invalid/composer-extra\n"},
		{name: "no module"},
		{name: "nested unrelated module", module: "module test.invalid/composer\n", nestedModule: "module test.invalid/cli\n"},
		{name: "active module preserves discovery errors", module: "module test.invalid/composer\n", wantError: "read composer directory"},
		{name: "malformed module", module: "not a module file\n", wantError: "parse working module"},
		{name: "missing module directive", module: "go 1.27.1\n", wantError: "has no module directive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.module != "" {
				if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(test.module), 0o600); err != nil { //nolint:gosec // The module file is in this test's temporary directory.
					t.Fatal(err)
				}
			}
			child := filepath.Join(dir, "child")
			if err := os.Mkdir(child, 0o750); err != nil {
				t.Fatal(err)
			}
			if test.nestedModule != "" {
				if err := os.WriteFile(filepath.Join(child, "go.mod"), []byte(test.nestedModule), 0o600); err != nil { //nolint:gosec // The nested module is in this test's temporary directory.
					t.Fatal(err)
				}
			}
			t.Chdir(child)
			analyzers, err := New(map[string]any{
				"module":   "test.invalid/composer",
				"composer": scanner.ComposerDiscovery{Directory: "missing"},
			})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("expected %q, got %v", test.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unrelated module attempted discovery: %v", err)
			}
			for _, analyzer := range analyzers {
				_, runErr := analyzer.Run(&analysis.Pass{
					Pkg: types.NewPackage("test.invalid/cli", "cli"),
					Report: func(diagnostic analysis.Diagnostic) {
						t.Errorf("unexpected diagnostic: %s", diagnostic.Message)
					},
				})
				if runErr != nil {
					t.Fatal(runErr)
				}
			}
		})
	}
}

func TestConfigurationErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		conf map[string]any
		want string
	}{
		{name: "mixed discovery and explicit targets", conf: map[string]any{
			"composer": scanner.ComposerDiscovery{Directory: "."},
			"targets":  []scanner.Target{{Directory: "."}},
		}, want: "configure composer discovery or explicit targets, not both"},
		{name: "unknown discovery option", conf: map[string]any{
			"composer": map[string]any{"directory": ".", "misspelled": true},
		}, want: "unknown field"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.conf)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750) //nolint:gosec // The directory is inside this test's temporary fixture.
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported fixture entry %q", path)
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // WalkDir starts from the repository's fixed fixture path.
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0o600) //nolint:gosec // The copied file is inside this test's temporary fixture.
	})
}
