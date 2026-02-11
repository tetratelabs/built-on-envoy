// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Copyright The OWASP Coraza contributors
// SPDX-License-Identifier: Apache-2.0

package coraza

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed rules
var crs embed.FS

// rulesFS implements fs.FS for the embedded rules.
// Basically, this is a wrapper around `rules` directory in the embedded filesystem while
// making it possible to support "@" prefixed paths which will be resolved to the actual
// file paths in the embedded filesystem.
type rulesFS struct{}

// Open implements [fs.FS.Open]
func (r rulesFS) Open(name string) (fs.File, error) {
	path, err := mapPath(name)
	if err != nil {
		return nil, err
	}
	return crs.Open(path)
}

// ReadDir implements [fs.FS.ReadDir]
func (r rulesFS) ReadDir(name string) ([]fs.DirEntry, error) {
	path, err := mapPath(name)
	if err != nil {
		return nil, err
	}
	return fs.ReadDir(crs, path)
}

// ReadFile implements [fs.FS.ReadFile]
func (r rulesFS) ReadFile(name string) ([]byte, error) {
	path, err := mapPath(name)
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(crs, path)
}

// mapPath maps the "@" prefixed paths to the actual file paths in the embedded filesystem.
func mapPath(p string) (string, error) {
	switch p {
	case "@recommended-conf":
		return "rules/recommended.conf", nil
	case "@ftw-conf":
		return "rules/ftw.conf", nil
	case "@crs-setup-conf":
		return "rules/crs-setup.conf", nil
	default:
		if strings.HasPrefix(p, "@owasp_crs") {
			return strings.Replace(p, "@owasp_crs", "rules/owasp_crs", 1), nil
		}
	}
	return "", fmt.Errorf("unknown path %s", p)
}
