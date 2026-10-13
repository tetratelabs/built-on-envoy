// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package scanner discovers Composer Go extensions from their manifests so
// analysis profiles stay aligned with the modules Composer can build.
package scanner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
	"gopkg.in/yaml.v3"
)

const composerFFIPackage = "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"

// ComposerDiscovery configures automatic discovery of Go extensions directly
// under a Composer directory.
type ComposerDiscovery struct {
	Directory string            `json:"directory"`
	BuildTags []string          `json:"build-tags"`
	Env       map[string]string `json:"env"`
	Include   []string          `json:"include"`
}

// DiscoverComposer returns scanner targets for all selected Go extensions.
func DiscoverComposer(profile ComposerDiscovery) ([]Target, error) {
	if profile.Directory == "" {
		return nil, fmt.Errorf("composer directory is required")
	}
	dir, err := filepath.Abs(profile.Directory)
	if err != nil {
		return nil, fmt.Errorf("composer directory: %w", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("read composer directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("composer directory %q is not a directory", dir)
	}
	include, err := includeSet(profile.Include)
	if err != nil {
		return nil, err
	}
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read composer directory %q: %w", dir, err)
	}
	var targets []Target
	seen := make(map[string]bool)
	for _, child := range children {
		name := child.Name()
		if include != nil && !include[name] {
			continue
		}
		if child.Type()&os.ModeSymlink != 0 {
			targetInfo, targetErr := os.Stat(filepath.Join(dir, name))
			if targetErr != nil {
				return nil, fmt.Errorf("inspect Composer entry %q: %w", name, targetErr)
			}
			if targetInfo.IsDir() {
				return nil, fmt.Errorf("composer candidate %q is a symlink", name)
			}
			continue
		}
		if !child.IsDir() {
			continue
		}
		moduleDir := filepath.Join(dir, name)
		linkInfo, statErr := os.Lstat(moduleDir)
		if statErr != nil {
			return nil, fmt.Errorf("inspect composer candidate %q: %w", name, statErr)
		}
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("composer candidate %q is a symlink", name)
		}
		manifestPath := filepath.Join(moduleDir, "manifest.yaml")
		// Child names come from ReadDir within the caller-selected Composer directory.
		manifest, readErr := os.ReadFile(manifestPath) //nolint:gosec
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("read manifest for %q: %w", name, readErr)
		}
		var metadata struct {
			Type string `yaml:"type"`
		}
		if unmarshalErr := yaml.Unmarshal(manifest, &metadata); unmarshalErr != nil {
			return nil, fmt.Errorf("parse manifest for %q: %w", name, unmarshalErr)
		}
		if metadata.Type != "go" {
			continue
		}
		seen[name] = true
		target, targetErr := composerTarget(dir, moduleDir, name, profile)
		if targetErr != nil {
			return nil, fmt.Errorf("discover Go extension %q: %w", name, targetErr)
		}
		targets = append(targets, target)
	}
	if include != nil {
		var unknown []string
		for name := range include {
			if !seen[name] {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) != 0 {
			slices.Sort(unknown)
			return nil, fmt.Errorf("include entries do not name Go extensions: %s", strings.Join(unknown, ", "))
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no Go extensions found in Composer directory %q", dir)
	}
	slices.SortFunc(targets, func(a, b Target) int {
		return strings.Compare(a.Packages[0], b.Packages[0])
	})
	return targets, nil
}

func includeSet(names []string) (map[string]bool, error) {
	if names == nil {
		return nil, nil
	}
	include := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") || filepath.Base(name) != name {
			return nil, fmt.Errorf("invalid Composer include entry %q", name)
		}
		include[name] = true
	}
	return include, nil
}

func composerTarget(composerDir, moduleDir, name string, profile ComposerDiscovery) (Target, error) {
	cfg, err := loadConfig(&Target{Directory: composerDir, Env: profile.Env, BuildTags: profile.BuildTags})
	if err != nil {
		return Target{}, err
	}
	cfg.Mode = packages.NeedName | packages.NeedFiles
	rootPattern := "./" + name
	root, err := packages.Load(cfg, rootPattern)
	if err != nil {
		return Target{}, fmt.Errorf("load extension root package: %w", err)
	}
	if err = packageErrors(root); err != nil {
		return Target{}, fmt.Errorf("load extension root package: %w", err)
	}
	if len(root) != 1 || len(root[0].GoFiles) == 0 {
		return Target{}, fmt.Errorf("extension root package %q is missing or has no Go files", rootPattern)
	}
	if err = requireFactory(root[0]); err != nil {
		return Target{}, err
	}
	entryPackages := []string{rootPattern}
	var entrySymbols []EntrySymbol
	embeddedDir, err := optionalAdapterDirectory(moduleDir, "embedded")
	if err != nil {
		return Target{}, err
	}
	if embeddedDir {
		entryPackages = []string{rootPattern + "/embedded"}
		registration, packageErr := loadRequiredPackage(cfg, entryPackages[0])
		if packageErr != nil {
			return Target{}, fmt.Errorf("embedded entry package: %w", packageErr)
		}
		if err = requireRegistration(registration); err != nil {
			return Target{}, fmt.Errorf("embedded entry package: %w", err)
		}
	} else {
		if err = requireRegistration(root[0]); err != nil {
			return Target{}, fmt.Errorf("root entry package: %w", err)
		}
	}
	standaloneDir, dirErr := optionalAdapterDirectory(moduleDir, "standalone")
	if dirErr != nil {
		return Target{}, dirErr
	}
	if standaloneDir {
		standalone := rootPattern + "/standalone"
		standalonePackage, packageErr := loadRequiredPackage(cfg, standalone)
		if packageErr != nil {
			return Target{}, fmt.Errorf("standalone entry package: %w", packageErr)
		}
		if err = requireFactory(standalonePackage); err != nil {
			return Target{}, fmt.Errorf("standalone entry package: %w", err)
		}
		entrySymbols = []EntrySymbol{{Package: standalone, Name: "WellKnownHttpFilterConfigFactories"}}
	}
	return Target{
		Directory:     composerDir,
		Packages:      []string{rootPattern + "/..."},
		EntryPackages: entryPackages,
		FFIPackages:   []string{composerFFIPackage},
		EntrySymbols:  entrySymbols,
		BuildTags:     slices.Clone(profile.BuildTags),
		Env:           maps.Clone(profile.Env),
	}, nil
}

func optionalAdapterDirectory(moduleDir, name string) (bool, error) {
	path := filepath.Join(moduleDir, name)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s adapter directory: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("%s adapter directory is a symlink", name)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s adapter path is not a directory", name)
	}
	return true, nil
}

func loadRequiredPackage(cfg *packages.Config, pattern string) (*packages.Package, error) {
	loaded, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("load package %q: %w", pattern, err)
	}
	if err = packageErrors(loaded); err != nil {
		return nil, fmt.Errorf("load package %q: %w", pattern, err)
	}
	if len(loaded) != 1 || len(loaded[0].GoFiles) == 0 {
		return nil, fmt.Errorf("package %q is missing or has no selected Go files", pattern)
	}
	return loaded[0], nil
}

func requireFactory(pkg *packages.Package) error {
	for _, filename := range pkg.GoFiles {
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			return fmt.Errorf("parse extension root source %q: %w", filename, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && fn.Name.Name == "WellKnownHttpFilterConfigFactories" {
				return nil
			}
		}
	}
	return fmt.Errorf("extension root package %q is missing package-level WellKnownHttpFilterConfigFactories", pkg.PkgPath)
}

func requireRegistration(pkg *packages.Package) error {
	type sourceFile struct {
		file    *ast.File
		aliases map[string]bool
	}
	files := make([]sourceFile, 0, len(pkg.GoFiles))
	for _, filename := range pkg.GoFiles {
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			return fmt.Errorf("parse registration source %q: %w", filename, err)
		}
		aliases := make(map[string]bool)
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return fmt.Errorf("parse import path in %q: %w", filename, unquoteErr)
			}
			if importPath != "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go" {
				continue
			}
			alias := "sdk"
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias != "_" && alias != "." {
				aliases[alias] = true
			}
		}
		files = append(files, sourceFile{file: file, aliases: aliases})
	}
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "init" || fn.Body == nil {
				continue
			}
			registered := false
			for _, statement := range fn.Body.List {
				expression, ok := statement.(*ast.ExprStmt)
				if !ok {
					continue
				}
				call, ok := expression.X.(*ast.CallExpr)
				if !ok {
					continue
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "RegisterHttpFilterConfigFactories" {
					continue
				}
				qualifier, ok := selector.X.(*ast.Ident)
				if ok && qualifier.Obj == nil && source.aliases[qualifier.Name] {
					registered = true
				}
			}
			if registered {
				return nil
			}
		}
	}
	return fmt.Errorf("package %q has no init call to sdk.RegisterHttpFilterConfigFactories", pkg.PkgPath)
}
