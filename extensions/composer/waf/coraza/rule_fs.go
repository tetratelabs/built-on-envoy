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
)

type subFS interface {
	Open(name string) (fs.File, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	Glob(pattern string) ([]string, error)
}

//go:embed rules
var localRules embed.FS

var embeddedRulesFS fs.FS

func init() {
	sub, err := fs.Sub(localRules, "rules")
	if err != nil {
		log.Fatal(err)
	}
	embeddedRulesFS = rulesFS{sub.(subFS)}
}

// rulesFS exposes the embedded local rules directory and normalizes @-prefixed paths.
// This matches how Coraza resolves includes relative to the parent directive file.
type rulesFS struct {
	subFS
}

func (r rulesFS) Open(name string) (fs.File, error) {
	return r.subFS.Open(normalizeRulePath(name))
}

func (r rulesFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return r.subFS.ReadDir(normalizeRulePath(name))
}

func (r rulesFS) ReadFile(name string) ([]byte, error) {
	return r.subFS.ReadFile(normalizeRulePath(name))
}

func (r rulesFS) Glob(pattern string) ([]string, error) {
	return r.subFS.Glob(normalizeRulePath(pattern))
}

func normalizeRulePath(name string) string {
	idx := strings.Index(name, "@")
	if idx != -1 {
		name = name[idx:]
	}

	switch name {
	case "@recommended.conf", "@recommended-conf":
		return "@coraza.conf-recommended"
	default:
		return name
	}
}
