// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package crsregistry holds the OWASP CRS versions available to the WAF. Use it through the
// public waf/crs package.
package crsregistry

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Version is a CRS version available to the WAF.
type Version struct {
	// Version is the exact CRS version, e.g. "4.25.2".
	Version string
	// Rules serves the CRS rules and the upstream setup example, either with the names of the
	// coraza-coreruleset crs/v4 module (@owasp_crs/, @crs-setup.conf.example) or with the names
	// of its lts/v4 module (@owasp_crs_lts/, @crs-setup-lts.conf.example). Either way, the WAF
	// serves them as @owasp_crs/ and @crs-setup.conf.example.
	Rules fs.FS
	// Setup serves the @crs-setup.conf tailored for this CRS version.
	Setup fs.FS
}

// Entry is a registered CRS version.
type Entry struct {
	Version
	// LTSNames is true if Rules uses the names of the lts/v4 module.
	LTSNames bool
}

// Registry holds the registered CRS versions, the default one and the aliases. It is locked by
// Freeze.
type Registry struct {
	mu         sync.Mutex
	entries    []Entry
	defaultVer string
	aliases    map[string]string
	frozen     bool
}

// Snapshot is the content of a frozen registry.
type Snapshot struct {
	// Entries are the registered CRS versions, in registration order.
	Entries []Entry
	// Default is the CRS version used when the WAF configuration does not select one.
	Default string
	// Aliases maps each crs_version value, e.g. "lts", to the CRS version it selects.
	Aliases map[string]string
}

// Global is the registry used by the WAF.
var Global = &Registry{}

var (
	versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	aliasRe   = regexp.MustCompile(`^[a-z]+$`)
)

// Register adds a CRS version. crs_version selects it only through its aliases (see SetAlias).
// It fails if the version is already registered, if its files do not match the version, or if
// the registry is frozen.
func (r *Registry) Register(v Version) error {
	if !versionRe.MatchString(v.Version) {
		return fmt.Errorf("crs: invalid version %q, expected major.minor.patch", v.Version)
	}
	ltsNames, err := checkFiles(v)
	if err != nil {
		return fmt.Errorf("crs: version %s: %w", v.Version, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("crs: cannot register version %s after the WAF configuration was loaded", v.Version)
	}
	if slices.ContainsFunc(r.entries, func(e Entry) bool { return e.Version.Version == v.Version }) {
		return fmt.Errorf("crs: version %s already registered", v.Version)
	}
	r.entries = append(r.entries, Entry{Version: v, LTSNames: ltsNames})
	return nil
}

// SetDefault sets the CRS version used when the WAF configuration does not select one. The
// version must be registered.
func (r *Registry) SetDefault(version string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("crs: cannot set the default version to %s after the WAF configuration was loaded", version)
	}
	if !slices.ContainsFunc(r.entries, func(e Entry) bool { return e.Version.Version == version }) {
		return fmt.Errorf("crs: cannot set the default to unregistered version %s", version)
	}
	r.defaultVer = version
	return nil
}

// SetAlias makes a crs_version value, e.g. "lts", select a registered CRS version. Setting an
// alias again replaces its version.
func (r *Registry) SetAlias(alias, version string) error {
	switch {
	case aliasRe.MatchString(alias):
	case versionRe.MatchString(alias):
		if alias != version {
			return fmt.Errorf("crs: the alias %s is a version, it cannot select version %s", alias, version)
		}
	default:
		return fmt.Errorf("crs: invalid alias %q, expected lowercase letters or major.minor.patch", alias)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("crs: cannot set the alias %s after the WAF configuration was loaded", alias)
	}
	if !slices.ContainsFunc(r.entries, func(e Entry) bool { return e.Version.Version == version }) {
		return fmt.Errorf("crs: cannot set the alias %s to unregistered version %s", alias, version)
	}
	if r.aliases == nil {
		r.aliases = map[string]string{}
	}
	r.aliases[alias] = version
	return nil
}

// Freeze locks the registry, so that the available versions, the default and the aliases
// cannot change once a WAF configuration uses them, and returns them.
func (r *Registry) Freeze() (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
	if r.defaultVer == "" {
		return Snapshot{}, errors.New("crs: no default version set")
	}
	return Snapshot{Entries: slices.Clone(r.entries), Default: r.defaultVer, Aliases: maps.Clone(r.aliases)}, nil
}

// checkFiles verifies that the files of v are those of CRS v.Version, and reports whether its
// rules use the lts/v4 module names.
func checkFiles(v Version) (bool, error) {
	if v.Rules == nil || v.Setup == nil {
		return false, errors.New("the rules and the tailored setup are required")
	}

	setup, err := fs.ReadFile(v.Setup, "@crs-setup.conf")
	if err != nil {
		return false, fmt.Errorf("reading the tailored setup: %w", err)
	}
	if !strings.Contains(string(setup), "# OWASP CRS ver."+v.Version+"\n") {
		return false, fmt.Errorf("@crs-setup.conf is not the setup of CRS %s", v.Version)
	}

	ltsNames := false
	initRules, err := fs.ReadFile(v.Rules, "@owasp_crs/REQUEST-901-INITIALIZATION.conf")
	if errors.Is(err, fs.ErrNotExist) {
		ltsNames = true
		initRules, err = fs.ReadFile(v.Rules, "@owasp_crs_lts/REQUEST-901-INITIALIZATION.conf")
	}
	if err != nil {
		return false, fmt.Errorf("reading the rules: %w", err)
	}
	if !strings.Contains(string(initRules), "ver:'OWASP_CRS/"+v.Version+"'") {
		return false, fmt.Errorf("the rules are not those of CRS %s", v.Version)
	}
	return ltsNames, nil
}
