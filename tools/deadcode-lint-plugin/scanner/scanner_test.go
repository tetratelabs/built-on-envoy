// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// These public scanner tests prove FFI direction, initialization, and scope rules
// independently of golangci's package cache and diagnostic processing.
package scanner_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tetratelabs/built-on-envoy/cli/tools/deadcode-lint-plugin/scanner"
)

func TestImportedFFI(t *testing.T) {
	dir := fixture(t)
	target := scanner.Target{
		Directory: dir, Packages: []string{"./impl/..."}, EntryPackages: []string{"./entry"},
		Env: map[string]string{"GOOS": "wasip1", "GOARCH": "wasm", "CGO_ENABLED": "0"},
	}
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "imported SDK", true: "explicit ABI package"}[explicit], func(t *testing.T) {
			cfg := target
			if explicit {
				cfg.EntryPackages = nil
				cfg.FFIPackages = []string{"./sdk"}
			}
			result, err := scanner.Scan([]scanner.Target{cfg})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Targets[0].FFIExports) != 2 {
				t.Fatalf("expected C and Wasm exports, got %v", result.Targets[0].FFIExports)
			}
			var names []string
			for _, finding := range result.Findings {
				names = append(names, finding.Function)
				if finding.Line <= 0 || finding.Column <= 0 || finding.Offset <= 0 || !filepath.IsAbs(finding.Filename) {
					t.Fatalf("invalid diagnostic source coordinates: %+v", finding)
				}
			}
			for _, name := range []string{"Dead", "OnlyOutbound", "genericDead", "(test.invalid/ffi/impl.unused).empty", "orphan.ExportedOrphan"} {
				if !slices.ContainsFunc(names, func(fn string) bool { return strings.Contains(fn, name) }) {
					t.Errorf("missing disconnected function %s: %v", name, names)
				}
			}
			for _, name := range []string{"Live", "FromSDKInit", "WasmOnly", "genericLive", ").sealed"} {
				if slices.ContainsFunc(names, func(fn string) bool { return strings.Contains(fn, name) }) {
					t.Errorf("live or marker function reported: %s: %v", name, names)
				}
			}
		})
	}
}

func TestRootFailures(t *testing.T) {
	t.Run("missing entrypoint", func(t *testing.T) {
		dir := fixture(t)
		_, err := scanner.Scan([]scanner.Target{{Directory: dir, Packages: []string{"./impl"}, DisableFFI: true}})
		if err == nil || !strings.Contains(err.Error(), "no entrypoints resolved") {
			t.Fatalf("expected explicit missing-root error, got %v", err)
		}
	})
	t.Run("invalid C export", func(t *testing.T) {
		dir := fixture(t)
		write(t, dir, "sdk/ffi.go", "package sdk\n//export wrongName\nfunc actualName() {}\n")
		_, err := scanner.Scan([]scanner.Target{{Directory: dir, Packages: []string{"./impl"}, FFIPackages: []string{"./sdk"}}})
		if err == nil || !strings.Contains(err.Error(), "invalid FFI export") {
			t.Fatalf("expected explicit malformed-export error, got %v", err)
		}
	})
	t.Run("missing explicit symbol", func(t *testing.T) {
		dir := fixture(t)
		_, err := scanner.Scan([]scanner.Target{{
			Directory: dir, Packages: []string{"./impl"},
			EntrySymbols: []scanner.EntrySymbol{{Package: "./impl", Name: "Absent"}},
		}})
		if err == nil || !strings.Contains(err.Error(), "not a non-generic package function") {
			t.Fatalf("expected explicit missing-symbol error, got %v", err)
		}
	})
}

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module test.invalid/ffi\n\ngo 1.27.1\n",
		"entry/entry.go": `package entry
import _ "test.invalid/ffi/sdk"
`,
		"sdk/ffi.go": `package sdk
import "test.invalid/ffi/impl"
func init() { impl.FromSDKInit() }
//export hostEntry
func hostEntry() { impl.Live() }
//go:wasmexport different_host_symbol
func wasmEntry() { impl.WasmOnly() }
`,
		"impl/impl.go": `package impl
func Live() { genericLive[int](); genericLive[string]() }
func genericLive[T any]() {}
func genericDead[T any]() {}
func FromSDKInit() {}
func WasmOnly() {}
func Dead() {}
type unused struct{}
func (unused) empty() {}
type sealed interface { sealed() }
func (unused) sealed() {}
//go:wasmimport host outbound
func outbound()
func OnlyOutbound() { outbound() }
`,
		"impl/orphan/orphan.go": `package orphan
import "test.invalid/ffi/impl"
func init() { impl.Dead() }
func ExportedOrphan() {}
//export unlinkedEntry
func unlinkedEntry() { impl.Dead() }
`,
		"impl/impl_test.go": `package impl
import "testing"
func TestDead(t *testing.T) { Dead() }
`,
	}
	for name, contents := range files {
		write(t, dir, name, contents)
	}
	return dir
}

func write(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	// The path is confined to the temporary fixture supplied by this test.
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil { //nolint:gosec
		t.Fatal(err)
	}
}
