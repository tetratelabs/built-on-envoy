// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

var (
	errEmptyExtensionReference = errors.New("extension reference cannot be empty")
	errEmptyTag                = errors.New("tag cannot be empty")
)

// Pull is a command to pull an extension from an OCI registry.
type Pull struct {
	Extension string `arg:"" help:"Extension name or OCI repository URL (e.g., cors or ghcr.io/tetratelabs/built-on-envoy/extension-cors:1.0.0)"`
	Path      string `name:"path" help:"Destination path to extract the extension to." type:"path"`
	Insecure  bool   `name:"insecure" help:"Allow pulling from an insecure (HTTP) registry (default: false)" default:"false"`
	Username  string `name:"username" env:"BOE_REGISTRY_USERNAME" help:"Username for the OCI registry."`
	Password  string `name:"password" env:"BOE_REGISTRY_PASSWORD" help:"Password for the OCI registry." type:"password"`

	repository  string     `kong:"-"` // Internal field: parsed repository URL
	tag         string     `kong:"-"` // Internal field: parsed tag
	client      oci.Client `kong:"-"` // Internal field: OCI client
	downloadDir string     `kong:"-"` // Internal field: download directory
}

// Validate is called by Kong after parsing to validate the command arguments.
func (p *Pull) Validate() error {
	repo, tag, err := parseExtensionReference(p.Extension)
	if err != nil {
		return err
	}
	p.repository = repo
	p.tag = tag
	return nil
}

// AfterApply is called by Kong after applying defaults to set computed default values.
func (p *Pull) AfterApply(_ *kong.Context, dirs *xdg.Directories) error {
	var err error
	p.downloadDir = p.downloadDirectory(dirs)
	p.client, err = newOCIClient(p.repository, p.Username, p.Password, p.Insecure)
	return err
}

// Run executes the pull command.
func (p *Pull) Run(ctx context.Context) error {
	fmt.Printf("Pulling %s:%s...\n", p.repository, p.tag)

	digest, err := p.client.Pull(ctx, p.tag, p.downloadDir)
	if err != nil {
		return fmt.Errorf("failed to pull extension: %w", err)
	}

	fmt.Printf(`
%[1]sSuccessfully pulled extension %[3]s (%[4]s)%[2]s
  → %[1]sDigest:%[2]s %[5]s
  → %[1]sExtracted to:%[2]s %[6]s
`, internal.ANSIBold, internal.ANSIReset, p.repository, p.tag, digest, p.downloadDir)

	return nil
}

// setDownloadDirectory sets the download directory if not provided by the user.
func (p *Pull) downloadDirectory(dirs *xdg.Directories) string {
	base := p.Path
	if base == "" {
		base = dirs.DataHome
	}
	return fmt.Sprintf("%s/extensions/%s/%s",
		base, extensions.NameFromRepository(p.repository), p.tag)
}

// parseExtensionReference parses an extension reference string and returns
// the repository URL and tag. The reference can be:
//   - A simple extension name (e.g., "cors") -> uses default registry and "latest" tag
//   - A full OCI reference with tag (e.g., "ghcr.io/org/repo:1.0.0")
//   - A full OCI reference without tag (e.g., "ghcr.io/org/repo") -> uses "latest" tag
func parseExtensionReference(ref string) (repository, tag string, err error) {
	if ref == "" {
		return "", "", errEmptyExtensionReference
	}

	// If no "/" present, it's a simple extension name
	if !strings.Contains(ref, "/") {
		repository = extensions.DefaultOCIRegistry + "/extension-" + ref
		tag = "latest"
		return repository, tag, nil
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
