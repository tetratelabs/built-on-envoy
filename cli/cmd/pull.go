// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

var (
	errEmptyExtensionReference = errors.New("extension reference cannot be empty")
	errEmptyTag                = errors.New("tag cannot be empty")
)

// Pull is a command to pull an extension from an OCI registry.
type Pull struct {
	Extension string   `arg:"" help:"Extension name or OCI repository URL (e.g., cache or ${default_registry}/extension-cache:1.0.0)"`
	Path      string   `name:"path" help:"Destination path to extract the extension to." type:"path"`
	OCI       OCIFlags `embed:""`

	repository string `kong:"-"` // Internal field: parsed repository URL
	tag        string `kong:"-"` // Internal field: parsed tag
}

//go:embed pull_help.md
var pullHelp string

// Help provides detailed help for the pull command.
func (p *Pull) Help() string { return pullHelp }

// Validate is called by Kong after parsing to validate the command arguments.
func (p *Pull) Validate() error {
	repo, tag, err := parseExtensionReference(p.OCI.Registry, p.Extension)
	if err != nil {
		return err
	}
	p.repository = repo
	p.tag = tag
	return nil
}

// Run executes the pull command.
func (p *Pull) Run(ctx context.Context, dirs *xdg.Directories) error {
	fmt.Printf("Pulling %s:%s...\n", p.repository, p.tag)

	downloader := &extensions.Downloader{
		Username: p.OCI.Username,
		Password: p.OCI.Password,
		Insecure: p.OCI.Insecure,
		Dirs:     dirs,
	}

	downloadDir, digest, err := downloader.Download(ctx, p.repository, p.tag, p.Path)
	if err != nil {
		return fmt.Errorf("failed to pull extension: %w", err)
	}

	fmt.Printf(`
%[1]sSuccessfully pulled extension %[3]s (%[4]s)%[2]s
  → %[1]sDigest:%[2]s %[5]s
  → %[1]sExtracted to:%[2]s %[6]s
`, internal.ANSIBold, internal.ANSIReset, p.repository, p.tag, digest, downloadDir)

	return nil
}

// parseExtensionReference parses an extension reference string and returns
// the repository URL and tag. The reference can be:
//   - A simple extension name (e.g., "cors") -> uses default registry and "latest" tag
//   - A full OCI reference with tag (e.g., "ghcr.io/org/repo:1.0.0")
//   - A full OCI reference without tag (e.g., "ghcr.io/org/repo") -> uses "latest" tag
func parseExtensionReference(defaultRegistry string, ref string) (repository, tag string, err error) {
	if ref == "" {
		return "", "", errEmptyExtensionReference
	}
	if defaultRegistry == "" {
		defaultRegistry = extensions.DefaultOCIRegistry
	}

	// If no "/" present, it's a simple extension name
	if !strings.Contains(ref, "/") {
		name, version := splitRef(ref)
		repository = extensions.RepositoryName(defaultRegistry, name)
		return repository, version, nil
	}

	// It's a full OCI reference, check for tag
	// The tag is after the last ":" but we need to be careful with ports (e.g., localhost:5000/repo)
	// A tag is only after the last "/" segment
	lastSlash := strings.LastIndex(ref, "/")
	afterSlash := ref[lastSlash+1:]

	if colonIdx := strings.LastIndex(afterSlash, ":"); colonIdx != -1 {
		// Has a tag
		repository = ref[:lastSlash+1+colonIdx]
		tag = afterSlash[colonIdx+1:]
		if tag == "" {
			return "", "", fmt.Errorf("invalid extension reference %q: %w", ref, errEmptyTag)
		}
	} else {
		// No tag, use "latest"
		repository = ref
		tag = "latest"
	}

	return repository, tag, nil
}
