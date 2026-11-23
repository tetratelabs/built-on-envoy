// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"cmp"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"

	corazaconf "github.com/corazawaf/coraza-coreruleset/coraza/v3"
	"github.com/jcchavezs/mergefs"
	"github.com/jcchavezs/mergefs/io"

	// Registers the CRS versions embedded by default.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/waf/crs"
	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/internal/crsregistry"
)

//go:embed directives
var customConfigsEmbed embed.FS

// embeddedConfigs is the embedded directives directory wrapped with alias resolution.
// Its files do not depend on the CRS version.
var embeddedConfigs directivesFS

func init() {
	// Strip the "directives/" prefix so embedded directive files are addressable
	// for example as "@coraza.conf" rather than "directives/@coraza.conf".
	sub, err := fs.Sub(customConfigsEmbed, "directives")
	if err != nil {
		log.Fatal(err)
	}
	embeddedConfigs = directivesFS{sub}
}

// crsBundle is a CRS version available to the WAF. Rules and setup files of every bundle are
// served under the same names (@owasp_crs/, @crs-setup.conf, @crs-setup.conf.example), so the
// directives do not depend on the selected CRS version.
type crsBundle struct {
	// version is the exact CRS version, e.g. "4.25.2".
	version string
	// fs merges, in order of precedence:
	//  1. Embedded directives — tailored configurations shipped with this extension
	//  2. CRS setup — @crs-setup.conf tailored for this CRS version
	//  3. CRS rules — upstream unmodified OWASP CRS rules and setup file
	//  4. coraza-coreruleset coraza/v3 module — upstream unmodified Coraza recommended config
	//  5. Local filesystem — support for user-provided directive files
	fs fs.FS
	// wafs caches the WAFs built from fs.
	wafs *sharedWAFCache
}

func newCRSBundle(e crsregistry.Entry) *crsBundle {
	return &crsBundle{
		version: e.Version.Version,
		fs:      mergefs.Merge(embeddedConfigs, directivesFS{e.Setup}, crsRulesFS{crsRulesView{e.Rules, e.LTSNames}}, corazaconf.FS, io.OSFS),
		wafs:    newSharedWAFCache(),
	}
}

// crsBundles are the CRS versions the WAF uses, built once from the registry when the first WAF
// configuration is loaded. Building them freezes the registry: a Register, SetAlias or SetDefault
// after that panics, because the WAF would never see the change.
var crsBundles = sync.OnceValues(func() (*crsBundleSet, error) {
	snap, err := crsregistry.Global.Freeze()
	if err != nil {
		return nil, err
	}
	set := &crsBundleSet{aliases: map[string]*crsBundle{}}
	byVersion := map[string]*crsBundle{}
	for _, e := range snap.Entries {
		b := newCRSBundle(e)
		set.bundles = append(set.bundles, b)
		byVersion[b.version] = b
	}
	set.defaultBundle = byVersion[snap.Default]
	for alias, version := range snap.Aliases {
		set.aliases[alias] = byVersion[version]
	}
	return set, nil
})

// crsBundleSet are the CRS versions available to the WAF, in registration order.
type crsBundleSet struct {
	bundles       []*crsBundle
	defaultBundle *crsBundle
	aliases       map[string]*crsBundle
}

// lookup returns the CRS version selected by the alias, or the default one if version is empty.
func (s *crsBundleSet) lookup(version string) (*crsBundle, error) {
	if version == "" {
		return s.defaultBundle, nil
	}
	if b, ok := s.aliases[version]; ok {
		return b, nil
	}
	err := fmt.Errorf("unsupported crs_version %q, supported values are: %s", version, strings.Join(s.values(), ", "))
	if by := s.aliasesOf(version); len(by) > 0 {
		// An embedded version, not selectable by its number: say which alias selects it.
		err = fmt.Errorf("%w; CRS %s is selected by %s", err, version, strings.Join(by, " or "))
	}
	return nil, err
}

// aliasesOf returns the aliases that select the CRS version, sorted.
func (s *crsBundleSet) aliasesOf(version string) []string {
	var by []string
	for alias, b := range s.aliases {
		if b.version == version {
			by = append(by, strconv.Quote(alias))
		}
	}
	slices.Sort(by)
	return by
}

// values returns the accepted crs_version values, each with the CRS version it selects.
func (s *crsBundleSet) values() []string {
	var versions, names []string
	for alias, b := range s.aliases {
		if alias == b.version {
			versions = append(versions, alias)
		} else {
			names = append(names, alias+" ("+b.version+")")
		}
	}
	slices.SortFunc(versions, func(a, b string) int { return compareVersions(b, a) })
	slices.Sort(names)
	return append(versions, names...)
}

// compareVersions compares two major.minor.patch versions.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(as), len(bs)) {
		ai, _ := strconv.Atoi(as[i])
		bi, _ := strconv.Atoi(bs[i])
		if c := cmp.Compare(ai, bi); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(as), len(bs))
}

// lookupCRSBundle returns the CRS version for the crs_version setting.
func lookupCRSBundle(version string) (*crsBundle, error) {
	set, err := crsBundles()
	if err != nil {
		return nil, err
	}
	return set.lookup(version)
}

// directivesFS wraps the embedded directives directory or the tailored setup of a CRS version,
// strips directory prefixes that Coraza prepends on nested includes, and resolves aliases.
type directivesFS struct {
	subFS fs.FS
}

func (d directivesFS) Open(name string) (fs.File, error) {
	return d.subFS.Open(normalizeDirectivePath(name))
}

func (d directivesFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(d.subFS, normalizeDirectivePath(name))
}

func (d directivesFS) ReadFile(name string) ([]byte, error) {
	return fs.ReadFile(d.subFS, normalizeDirectivePath(name))
}

func (d directivesFS) Glob(pattern string) ([]string, error) {
	normalized := normalizeDirectivePath(pattern)
	if !strings.HasPrefix(normalized, "@") {
		// Embedded files are only accessible by explicit @-prefixed names,
		// not via generic glob patterns which should fall through to OSFS.
		return nil, nil
	}
	return fs.Glob(d.subFS, normalized)
}

// normalizeDirectivePath strips the directory prefix that Coraza prepends when
// resolving nested Include directives (it joins the including file's directory
// with the included path) and resolves aliases to their canonical embedded file
// names.
// The cut is anchored to the last path segment starting with '@' ("/@") rather
// than to the first '@', as the including file's directory may itself contain
// an '@' (e.g. "/etc/waf@prod/" or a Go module cache path "repo@v1.2.3/").
// Aliases usage is discouraged, but kept for backward compatibility.
func normalizeDirectivePath(name string) string {
	name = trimToAlias(name)
	switch name {
	case "@recommended.conf", "@recommended-conf":
		return "@coraza.conf"
	case "@crs-setup-conf":
		return "@crs-setup.conf"
	default:
		return name
	}
}

// trimToAlias returns name starting at its last path segment that begins with '@',
// or name unchanged if there is none.
func trimToAlias(name string) string {
	if idx := strings.LastIndex(name, "/@"); idx != -1 {
		return name[idx+1:]
	}
	return name
}

const (
	crsRulesDir     = "@owasp_crs"
	crsSetupExample = "@crs-setup.conf.example"
	ltsRulesDir     = "@owasp_crs_lts"
	ltsSetupExample = "@crs-setup-lts.conf.example"
	ltsSetupPrefix  = "@crs-setup-lts"
)

// crsRulesFS serves the rules of a CRS version under the names of the crs/v4 module:
// @owasp_crs/ for the rules and @crs-setup.conf.example[-nodefaultact] for the setup example.
// The lts/v4 module's own names (@owasp_crs_lts/, @crs-setup-lts.conf.example) are hidden, so
// that every CRS version exposes the same names, also to globs and directory listings.
type crsRulesFS struct {
	crsRulesView
}

// Glob matches against the renamed listings of the view, so that a pattern matches the same
// names whatever module the rules come from, and the matches are names the view serves.
//
// A pattern that names the lts/v4 module's own files fails: they are not served, so the glob
// would match nothing and load no rules without an error. Only the bare names fail: a path to a
// local directory of that name is left to the local filesystem.
func (c crsRulesFS) Glob(pattern string) ([]string, error) {
	if strings.HasPrefix(pattern, ltsRulesDir) || strings.HasPrefix(pattern, ltsSetupPrefix) {
		return nil, fmt.Errorf("%q: %s/ and %s* are not served; use %s/ and %s for every crs_version",
			pattern, ltsRulesDir, ltsSetupExample, crsRulesDir, crsSetupExample)
	}
	return fs.Glob(c.crsRulesView, trimToAlias(pattern))
}

// crsRulesView maps the crs/v4 names to the names of the rules' module. It has no Glob method,
// so that fs.Glob walks its renamed listings.
type crsRulesView struct {
	fs fs.FS
	// lts is true if fs uses the names of the lts/v4 module.
	lts bool
}

func (v crsRulesView) name(name string) (string, bool) {
	if !v.lts {
		return trimToAlias(name), true
	}
	return toLTSName(name)
}

func (v crsRulesView) Open(name string) (fs.File, error) {
	n, ok := v.name(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return v.fs.Open(n)
}

func (v crsRulesView) ReadFile(name string) ([]byte, error) {
	n, ok := v.name(name)
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadFile(v.fs, n)
}

func (v crsRulesView) ReadDir(name string) ([]fs.DirEntry, error) {
	n, ok := v.name(name)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries, err := fs.ReadDir(v.fs, n)
	if err != nil || !v.lts || n != "." {
		return entries, err
	}
	for i, e := range entries {
		entries[i] = renamedEntry{e, fromLTSName(e.Name())}
	}
	return entries, nil
}

// renamedEntry is a directory entry listed under another name.
type renamedEntry struct {
	fs.DirEntry
	name string
}

func (e renamedEntry) Name() string { return e.name }

// fromLTSName maps a top-level lts/v4 name to its crs/v4 name.
func fromLTSName(name string) string {
	switch {
	case name == ltsRulesDir:
		return crsRulesDir
	case strings.HasPrefix(name, ltsSetupExample):
		return crsSetupExample + strings.TrimPrefix(name, ltsSetupExample)
	default:
		return name
	}
}

// toLTSName maps a crs/v4 name to its lts/v4 name, after stripping the directory prefix
// Coraza prepends on nested includes. It returns false for the lts/v4 module's own
// names, which are not exposed.
func toLTSName(name string) (string, bool) {
	name = trimToAlias(name)
	switch {
	case strings.HasPrefix(name, ltsRulesDir), strings.HasPrefix(name, ltsSetupExample):
		return "", false
	case name == crsRulesDir:
		return ltsRulesDir, true
	case strings.HasPrefix(name, crsRulesDir+"/"):
		return ltsRulesDir + strings.TrimPrefix(name, crsRulesDir), true
	case strings.HasPrefix(name, crsSetupExample):
		return ltsSetupExample + strings.TrimPrefix(name, crsSetupExample), true
	default:
		return name, true
	}
}
