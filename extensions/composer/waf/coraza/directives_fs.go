// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"embed"
	"io/fs"
	"log"
	"strings"

	coreruleset "github.com/corazawaf/coraza-coreruleset/v4"
	"github.com/jcchavezs/mergefs"
	"github.com/jcchavezs/mergefs/io"
)

//go:embed directives
var customConfigsEmbed embed.FS

// embeddedConfigs is the embedded directives directory wrapped with alias resolution.
var embeddedConfigs directivesFS

// combinedDirectivesFS merges three filesystems with the following precedence order:
//  1. Embedded directives — tailored configurations shipped with this extension
//  2. coraza-coreruleset package — upstream unmodified OWASP CRS directives and configs
//  3. Local filesystem — support for user-provided directive files
var combinedDirectivesFS fs.FS

func init() {
	// Strip the "directives/" prefix so embedded directive files are addressable
	// for example as "@coraza.conf" rather than "directives/@coraza.conf".
	sub, err := fs.Sub(customConfigsEmbed, "directives")
	if err != nil {
		log.Fatal(err)
	}
	embeddedConfigs = directivesFS{sub}
	combinedDirectivesFS = mergefs.Merge(embeddedConfigs, coreruleset.FS, io.OSFS)
}

// directivesFS wraps the embedded directives directory, strips directory prefixes
// that Coraza prepends on nested includes, and resolves aliases.
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

// normalizeDirectivePath strips any directory prefix before '@' (Coraza prepends
// the including file's directory when resolving nested Include directives) and
// resolves aliases to their canonical embedded file names.
// Aliases usage is discouraged, but kept for backward compatibility.
func normalizeDirectivePath(name string) string {
	if idx := strings.Index(name, "@"); idx != -1 {
		name = name[idx:]
	}
	switch name {
	case "@recommended.conf", "@recommended-conf":
		return "@coraza.conf"
	case "@crs-setup-conf":
		return "@crs-setup.conf"
	default:
		return name
	}
}
