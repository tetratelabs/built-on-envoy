// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package scanner provides whole-program FFI reachability analysis for the
// golangci plugin without executing extension or SDK code.
package scanner

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Target separates packages to inspect from packages that initialize the program.
type Target struct {
	Directory     string            `json:"directory"`
	Packages      []string          `json:"packages"`
	EntryPackages []string          `json:"entry-packages"`
	FFIPackages   []string          `json:"ffi-packages"`
	EntrySymbols  []EntrySymbol     `json:"entry-symbols"`
	BuildTags     []string          `json:"build-tags"`
	Env           map[string]string `json:"env"`
	DisableFFI    bool              `json:"disable-ffi"`
}

// EntrySymbol identifies a package-level function exported without an FFI directive.
type EntrySymbol struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

// Finding uses source coordinates rather than positions owned by a loader's FileSet.
type Finding struct {
	Package  string
	Filename string
	Offset   int
	Line     int
	Column   int
	Function string
}

// Summary exposes the selected roots for inspection and validation.
type Summary struct {
	Roots      int
	FFIExports []string
}

// Result contains canonical findings and one root summary per target.
type Result struct {
	Findings []Finding
	Targets  []Summary
}

// Scan analyzes each target independently and fails explicitly on loading or root errors.
func Scan(targets []Target) (Result, error) {
	if len(targets) == 0 {
		return Result{}, fmt.Errorf("at least one target is required")
	}
	var result Result
	for i := range targets {
		findings, summary, err := scanTarget(&targets[i])
		if err != nil {
			return Result{}, fmt.Errorf("target %d: %w", i+1, err)
		}
		result.Findings = append(result.Findings, findings...)
		result.Targets = append(result.Targets, summary)
	}
	slices.SortFunc(result.Findings, compareFindings)
	result.Findings = slices.Compact(result.Findings)
	return result, nil
}

func scanTarget(target *Target) ([]Finding, Summary, error) {
	cfg, err := loadConfig(target)
	if err != nil {
		return nil, Summary{}, err
	}
	patterns := target.Packages
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	scope, err := packagePaths(cfg, patterns)
	if err != nil {
		return nil, Summary{}, err
	}
	entries, err := packagePaths(cfg, target.EntryPackages)
	if err != nil {
		return nil, Summary{}, err
	}
	ffi, err := packagePaths(cfg, target.FFIPackages)
	if err != nil {
		return nil, Summary{}, err
	}
	symbolPaths := make([]string, len(target.EntrySymbols))
	for i, symbol := range target.EntrySymbols {
		paths, loadErr := packagePaths(cfg, []string{symbol.Package})
		if loadErr != nil {
			return nil, Summary{}, loadErr
		}
		if len(paths) != 1 || symbol.Name == "" {
			return nil, Summary{}, fmt.Errorf("entry symbol requires one package and a function name")
		}
		symbolPaths[i] = paths[0]
	}
	all := slices.Concat(scope, entries, ffi, symbolPaths)
	slices.Sort(all)
	all = slices.Compact(all)
	cfg.Mode = packages.LoadAllSyntax | packages.NeedModule
	initial, err := packages.Load(cfg, all...)
	if err != nil {
		return nil, Summary{}, fmt.Errorf("load program: %w", err)
	}
	if err = packageErrors(initial); err != nil {
		return nil, Summary{}, err
	}
	prog, _ := ssautil.AllPackages(initial, ssa.InstantiateGenerics)
	prog.Build()
	byPath := make(map[string]*packages.Package)
	for _, p := range initial {
		byPath[p.PkgPath] = p
	}
	roots := make(map[*ssa.Function]bool)
	var linked []*packages.Package
	for _, path := range entries {
		p := byPath[path]
		if err = addRoot(roots, prog.Package(p.Types).Func("init")); err != nil {
			return nil, Summary{}, err
		}
		linked = append(linked, p)
	}
	for _, path := range scope {
		p := byPath[path]
		sp := prog.Package(p.Types)
		if p.Name == "main" && sp.Func("main") != nil {
			if err = addRoot(roots, sp.Func("init"), sp.Func("main")); err != nil {
				return nil, Summary{}, err
			}
			linked = append(linked, p)
		}
	}
	for i, symbol := range target.EntrySymbols {
		p := byPath[symbolPaths[i]]
		sp := prog.Package(p.Types)
		fn := sp.Func(symbol.Name)
		if fn == nil || fn.Signature.TypeParams().Len() != 0 {
			return nil, Summary{}, fmt.Errorf("entry symbol %s.%s is not a non-generic package function", p.PkgPath, symbol.Name)
		}
		if err = addRoot(roots, sp.Func("init"), fn); err != nil {
			return nil, Summary{}, err
		}
		linked = append(linked, p)
	}
	for _, path := range ffi {
		linked = append(linked, byPath[path])
	}
	var summary Summary
	if !target.DisableFFI {
		var discoveryErr error
		packages.Visit(linked, nil, func(p *packages.Package) {
			if discoveryErr != nil {
				return
			}
			functions, rootErr := ffiRoots(prog, p)
			if rootErr != nil {
				discoveryErr = rootErr
				return
			}
			if len(functions) == 0 {
				return
			}
			for _, fn := range functions {
				summary.FFIExports = append(summary.FFIExports, fn.String())
			}
			discoveryErr = addRoot(roots, append(functions, prog.Package(p.Types).Func("init"))...)
		})
		if discoveryErr != nil {
			return nil, Summary{}, discoveryErr
		}
	}
	if len(roots) == 0 {
		return nil, Summary{}, fmt.Errorf("no entrypoints resolved; configure entry-packages or ffi-packages")
	}
	rootFunctions := make([]*ssa.Function, 0, len(roots))
	for fn := range roots {
		rootFunctions = append(rootFunctions, fn)
	}
	res := rta.Analyze(rootFunctions, false)
	live := make(map[token.Position]bool)
	for fn := range res.Reachable {
		live[prog.Fset.Position(fn.Pos())] = true
		if origin := fn.Origin(); origin != nil {
			live[prog.Fset.Position(origin.Pos())] = true
		}
	}
	var findings []Finding
	for _, path := range scope {
		p := byPath[path]
		for _, file := range p.Syntax {
			if ast.IsGenerated(file) {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				obj, ok := p.TypesInfo.Defs[fn.Name].(*types.Func)
				if !ok {
					return nil, Summary{}, fmt.Errorf("unresolved function %s in %s", fn.Name, p.PkgPath)
				}
				sf := prog.FuncValue(obj)
				if sf == nil {
					return nil, Summary{}, fmt.Errorf("missing SSA function %s", obj.FullName())
				}
				pos := prog.Fset.Position(sf.Pos())
				if live[pos] || isMarker(fn, obj) {
					continue
				}
				filename, pathErr := filepath.Abs(pos.Filename)
				if pathErr != nil {
					return nil, Summary{}, fmt.Errorf("source path: %w", pathErr)
				}
				filename, pathErr = filepath.EvalSymlinks(filename)
				if pathErr != nil {
					return nil, Summary{}, fmt.Errorf("resolve source path: %w", pathErr)
				}
				findings = append(findings, Finding{
					Package: p.PkgPath, Filename: filename,
					Offset: pos.Offset, Line: pos.Line, Column: pos.Column, Function: sf.String(),
				})
			}
		}
	}
	summary.Roots = len(roots)
	slices.Sort(summary.FFIExports)
	return findings, summary, nil
}

func loadConfig(target *Target) (*packages.Config, error) {
	if target.Directory == "" {
		return nil, fmt.Errorf("directory is required")
	}
	dir, err := filepath.Abs(target.Directory)
	if err != nil {
		return nil, fmt.Errorf("directory: %w", err)
	}
	env := os.Environ()
	for key, value := range target.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("invalid environment override")
		}
		env = append(env, key+"="+value)
	}
	return &packages.Config{
		Dir: dir, Env: env, Tests: false,
		BuildFlags: []string{"-tags=" + strings.Join(target.BuildTags, ",")},
	}, nil
}

func packagePaths(cfg *packages.Config, patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	cfg.Mode = packages.NeedName
	loaded, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("resolve packages: %w", err)
	}
	if len(loaded) == 0 {
		return nil, fmt.Errorf("package patterns matched no packages")
	}
	if err = packageErrors(loaded); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(loaded))
	for _, p := range loaded {
		paths = append(paths, p.PkgPath)
	}
	return paths, nil
}

func packageErrors(initial []*packages.Package) error {
	var failures []string
	packages.Visit(initial, nil, func(p *packages.Package) {
		for _, err := range p.Errors {
			failures = append(failures, err.Error())
		}
	})
	if len(failures) == 0 {
		return nil
	}
	slices.Sort(failures)
	return fmt.Errorf("packages contain errors:\n%s", strings.Join(slices.Compact(failures), "\n"))
}

func addRoot(roots map[*ssa.Function]bool, functions ...*ssa.Function) error {
	for _, fn := range functions {
		if fn == nil {
			return fmt.Errorf("an entrypoint could not be resolved")
		}
		roots[fn] = true
	}
	return nil
}

func ffiRoots(prog *ssa.Program, p *packages.Package) ([]*ssa.Function, error) {
	var roots []*ssa.Function
	for _, file := range p.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil {
				continue
			}
			for _, comment := range fn.Doc.List {
				fields := strings.Fields(comment.Text)
				if len(fields) == 0 || (fields[0] != "//export" && fields[0] != "//go:wasmexport") {
					continue
				}
				if len(fields) != 2 || (fields[0] == "//export" && fields[1] != fn.Name.Name) || fn.Body == nil || fn.Recv != nil {
					return nil, fmt.Errorf("invalid FFI export on %s.%s", p.PkgPath, fn.Name)
				}
				obj, ok := p.TypesInfo.Defs[fn.Name].(*types.Func)
				if !ok || prog.FuncValue(obj) == nil || obj.Signature().TypeParams().Len() != 0 {
					return nil, fmt.Errorf("unresolved FFI export %s.%s", p.PkgPath, fn.Name)
				}
				roots = append(roots, prog.FuncValue(obj))
			}
		}
	}
	return roots, nil
}

func isMarker(fn *ast.FuncDecl, obj *types.Func) bool {
	if fn.Body == nil || len(fn.Body.List) != 0 || fn.Recv == nil || obj.Exported() {
		return false
	}
	sig := obj.Signature()
	if sig.Params().Len() != 0 || sig.Results().Len() != 0 {
		return false
	}
	scope := obj.Pkg().Scope()
	for _, name := range scope.Names() {
		typeName, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		iface, ok := typeName.Type().Underlying().(*types.Interface)
		if !ok {
			continue
		}
		for method := range iface.Methods() {
			if method.Id() == obj.Id() && types.Implements(sig.Recv().Type(), iface) {
				return true
			}
		}
	}
	return false
}

func compareFindings(a, b Finding) int {
	if result := strings.Compare(a.Package, b.Package); result != 0 {
		return result
	}
	if result := strings.Compare(a.Filename, b.Filename); result != 0 {
		return result
	}
	if a.Offset < b.Offset {
		return -1
	}
	if a.Offset > b.Offset {
		return 1
	}
	return strings.Compare(a.Function, b.Function)
}
