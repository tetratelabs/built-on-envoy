// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package crs selects the OWASP CRS versions the WAF embeds, and the default one.
//
// The WAF configuration picks a version with crs_version. Every version is included with the
// same names (@crs-setup.conf, @crs-setup.conf.example, @owasp_crs/), so the directives do
// not depend on it.
//
// This package embeds the newest CRS release and the CRS LTS release, selected only by the
// "latest" and "lts" aliases, and makes the LTS release the default: each CRS bump replaces both
// versions, so they are not selected by their exact version.
package crs

import (
	"embed"
	"io/fs"
	"strings"

	crslatest "github.com/corazawaf/coraza-coreruleset/crs/v4"
	crslts "github.com/corazawaf/coraza-coreruleset/lts/v4"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/internal/crsregistry"
)

// The aliases registered by this package. Their version follows the CRS releases embedded by
// each extension release.
const (
	// AliasLatest selects the newest CRS release.
	AliasLatest = "latest"
	// AliasLTS selects the newest CRS LTS release.
	AliasLTS = "lts"
)

// The CRS versions embedded by this package, selected by AliasLatest and AliasLTS.
var (
	// LatestVersion is the newest CRS release, e.g. "4.30.0".
	LatestVersion = strings.TrimPrefix(crslatest.Version, "v")
	// LTSVersion is the newest CRS LTS release, e.g. "4.25.2".
	LTSVersion = strings.TrimPrefix(crslts.Version, "v")
)

// Version is a CRS version available to the WAF. See crsregistry.Version for its fields.
type Version = crsregistry.Version

// Register makes a CRS version available to the WAF. crs_version selects it only through its
// aliases (see SetAlias). It panics if the version is malformed or already registered, if its
// files are not those of the version, or if a WAF configuration has already been loaded.
func Register(v Version) {
	if err := crsregistry.Global.Register(v); err != nil {
		panic(err)
	}
}

// SetAlias makes a crs_version value, e.g. "lts", select a registered CRS version. It panics if
// the version is not registered, if the alias is invalid, or if a WAF configuration has already
// been loaded.
func SetAlias(alias, version string) {
	if err := crsregistry.Global.SetAlias(alias, version); err != nil {
		panic(err)
	}
}

// SetDefault sets the CRS version used when the WAF configuration does not set crs_version. The
// default is a version, not an alias: moving an alias with SetAlias does not move it. It panics if
// the version is not registered or if a WAF configuration has already been loaded.
func SetDefault(version string) {
	if err := crsregistry.Global.SetDefault(version); err != nil {
		panic(err)
	}
}

// setupEmbed holds the @crs-setup.conf tailored for each CRS version registered by this
// package, as setup/v<version>/@crs-setup.conf.
//
//go:embed setup
var setupEmbed embed.FS

// setup returns the directory of the tailored @crs-setup.conf for a CRS version registered by
// this package.
func setup(version string) fs.FS {
	sub, err := fs.Sub(setupEmbed, "setup/v"+version)
	if err != nil {
		panic(err)
	}
	return sub
}

func init() {
	// Only the aliases select them: a CRS bump replaces both versions.
	Register(Version{Version: LatestVersion, Rules: crslatest.FS, Setup: setup(LatestVersion)})
	Register(Version{Version: LTSVersion, Rules: crslts.FS, Setup: setup(LTSVersion)})
	SetAlias(AliasLatest, LatestVersion)
	SetAlias(AliasLTS, LTSVersion)
	SetDefault(LTSVersion)
}
