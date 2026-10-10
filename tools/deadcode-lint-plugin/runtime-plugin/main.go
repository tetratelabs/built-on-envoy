// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package main adapts whole-program FFI scanning to the pinned golangci runtime
// plugin contract without changing its executable or ordinary linters.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/analysis"

	"github.com/tetratelabs/built-on-envoy/cli/tools/deadcode-lint-plugin/scanner"
)

const linterName = "ffideadcode"

type settings struct {
	Module   string                     `json:"module"`
	Targets  []scanner.Target           `json:"targets"`
	Composer *scanner.ComposerDiscovery `json:"composer"`
}

// New resolves whole-program findings before golangci consults its package cache.
func New(conf any) ([]*analysis.Analyzer, error) {
	raw, err := json.Marshal(conf)
	if err != nil {
		return nil, fmt.Errorf("encode settings: %w", err)
	}
	var cfg settings
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode settings: %w", err)
	}
	if cfg.Composer != nil && len(cfg.Targets) != 0 {
		return nil, fmt.Errorf("configure composer discovery or explicit targets, not both")
	}
	if cfg.Module != "" {
		dir, moduleErr := moduleDirectory(cfg.Module)
		if moduleErr != nil {
			return nil, moduleErr
		}
		if dir == "" {
			return []*analysis.Analyzer{newReporter(nil)}, nil
		}
		if cfg.Composer != nil && cfg.Composer.Directory != "" && !filepath.IsAbs(cfg.Composer.Directory) {
			cfg.Composer.Directory = filepath.Join(dir, cfg.Composer.Directory)
		}
		for i := range cfg.Targets {
			if !filepath.IsAbs(cfg.Targets[i].Directory) {
				cfg.Targets[i].Directory = filepath.Join(dir, cfg.Targets[i].Directory)
			}
		}
	}
	targets := cfg.Targets
	if cfg.Composer != nil {
		targets, err = scanner.DiscoverComposer(*cfg.Composer)
		if err != nil {
			return nil, fmt.Errorf("discover composer extensions: %w", err)
		}
	}
	result, err := scanner.Scan(targets)
	if err != nil {
		return nil, err
	}
	byPackage := make(map[string][]scanner.Finding)
	for _, finding := range result.Findings {
		byPackage[finding.Package] = append(byPackage[finding.Package], finding)
	}
	encoded, err := json.Marshal(struct {
		Version  string
		Settings settings
		Targets  []scanner.Target
		Findings []scanner.Finding
	}{Version: "ffi-roots-v2", Settings: cfg, Targets: targets, Findings: result.Findings})
	if err != nil {
		return nil, fmt.Errorf("encode findings: %w", err)
	}
	digest := sha256.Sum256(encoded)
	// Golangci v2.13.2 hashes analyzer names for its issue cache. Import-only package
	// hashes cannot invalidate a finding when a different caller package changes.
	cacheSeed := &analysis.Analyzer{
		Name: fmt.Sprintf("ffideadcodecache_%x", digest),
		Doc:  "Keys cached diagnostics by the complete whole-program findings.",
		Run:  func(*analysis.Pass) (any, error) { return nil, nil },
	}
	return []*analysis.Analyzer{newReporter(byPackage), cacheSeed}, nil
}

func newReporter(byPackage map[string][]scanner.Finding) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: linterName,
		Doc:  "Reports functions unreachable from executable and imported SDK FFI entrypoints.",
		Run: func(pass *analysis.Pass) (any, error) {
			return nil, report(pass, byPackage[pass.Pkg.Path()])
		},
	}
}

func moduleDirectory(expected string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	for {
		filename := filepath.Join(dir, "go.mod")
		data, readErr := os.ReadFile(filename) //nolint:gosec // The module file is resolved from the process working directory.
		if readErr == nil {
			module, parseErr := modfile.Parse(filename, data, nil)
			if parseErr != nil {
				return "", fmt.Errorf("parse working module: %w", parseErr)
			}
			if module.Module == nil {
				return "", fmt.Errorf("working module %q has no module directive", filename)
			}
			if module.Module.Mod.Path == expected {
				return dir, nil
			}
			return "", nil
		}
		if !errors.Is(readErr, fs.ErrNotExist) {
			return "", fmt.Errorf("read working module: %w", readErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func report(pass *analysis.Pass, findings []scanner.Finding) error {
	if len(findings) == 0 {
		return nil
	}
	remaining := make(map[scanner.Finding]bool)
	for _, finding := range findings {
		remaining[finding] = true
	}
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			pos := pass.Fset.Position(fn.Name.Pos())
			filename, err := filepath.Abs(pos.Filename)
			if err != nil {
				return fmt.Errorf("diagnostic source path: %w", err)
			}
			filename, err = filepath.EvalSymlinks(filename)
			if err != nil {
				return fmt.Errorf("resolve diagnostic source path: %w", err)
			}
			for finding := range remaining {
				if finding.Filename == filename && finding.Offset == pos.Offset {
					pass.Reportf(fn.Name.Pos(), "unreachable function: %s", finding.Function)
					delete(remaining, finding)
				}
			}
		}
	}
	if len(remaining) != 0 {
		for _, finding := range findings {
			if remaining[finding] {
				return fmt.Errorf("could not map diagnostic %s at %s offset %d into %s; source may have changed during analysis", finding.Function, finding.Filename, finding.Offset, pass.Pkg.Path())
			}
		}
	}
	return nil
}
