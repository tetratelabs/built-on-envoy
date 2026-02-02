// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/alecthomas/kong"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
)

// Push is a command to push an extension to an OCI registry.
type Push struct {
	Local    string `arg:"" name:"local extension" help:"Path to a directory containing the extension to push." type:"existingdir"`
	Registry string `name:"registry" env:"BOE_REGISTRY" help:"OCI registry URL to push the extension to." default:"${default_registry}"`
	Insecure bool   `name:"insecure" env:"BOE_REGISTRY_INSECURE" help:"Allow pushing to an insecure (HTTP) registry." default:"false"`
	Username string `name:"username" env:"BOE_REGISTRY_USERNAME" help:"Username for the OCI registry."`
	Password string `name:"password" env:"BOE_REGISTRY_PASSWORD" help:"Password for the OCI registry." type:"password"`

	manifest  *extensions.Manifest `kong:"-"` // Internal field: loaded extension manifest
	reference string               `kong:"-"` // Internal field: full OCI repository reference
	client    oci.RepositoryClient `kong:"-"` // Internal field: OCI client
}

//go:embed push_help.md
var pushHelp string

// Help provides detailed help for the push command.
func (p *Push) Help() string { return pushHelp }

// errInvalidManifest is returned when the extension manifest is invalid.
var errInvalidManifest = fmt.Errorf("invalid extension manifest")

// Validate is called by Kong after parsing to validate the command arguments.
func (p *Push) Validate() error {
	manifest, err := extensions.LoadLocalManifest(p.Local + "/manifest.yaml")
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}
	if err := extensions.ValidateManifest(manifest); err != nil {
		return fmt.Errorf("%w: %w", errInvalidManifest, err)
	}
	p.manifest = manifest
	return nil
}

// AfterApply is called by Kong after applying defaults to set computed default values.
func (p *Push) AfterApply(*kong.Context) error {
	var err error
	p.reference = extensions.RepositoryName(p.Registry, p.manifest.Name)
	p.client, err = newOCIRepositoryClient(p.reference, p.Username, p.Password, p.Insecure)
	return err
}

// Run executes the push command.
func (p *Push) Run(ctx context.Context) error {
	tag := p.manifest.Version
	fmt.Printf("Pushing extension %q (%s)...\n", p.manifest.Name, tag)

	annotations := extensions.OCIAnnotationsForManifest(p.manifest)
	// Add source annotation if pushing to default registry so that artifacts are
	// linked to the repo.
	// See: https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#pushing-container-images
	if p.Registry == extensions.DefaultOCIRegistry {
		annotations[ocispec.AnnotationSource] = extensions.DefaultOCISource
	}

	digest, err := p.client.Push(ctx, p.Local, tag, annotations)
	if err != nil {
		return fmt.Errorf("failed to push extension: %w", err)
	}

	fmt.Printf(`
%[1]sSuccessfully pushed extension %[3]q (%[4]s)%[2]s
  → %[1]sDigest:%[2]s %[5]s
  → %[1]sReference:%[2]s %[6]s:%[4]s
`, internal.ANSIBold, internal.ANSIReset, p.manifest.Name, tag, digest, p.reference)

	return nil
}

// newOCIRepositoryClient creates and assigns a new OCI client to the Push command.
func newOCIRepositoryClient(repository, username, password string, insecure bool) (oci.RepositoryClient, error) {
	opts := &oci.ClientOptions{PlainHTTP: insecure}
	if username != "" || password != "" {
		opts.Credentials = &oci.Credentials{
			Username: username,
			Password: password,
		}
	}

	// Instantiate the OCI client
	repo, err := oci.NewRemoteRepository(repository, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	return oci.NewRepositoryClient(repo), nil
}
