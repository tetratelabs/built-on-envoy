// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/docker"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/oci"
)

// Push is a command to push an extension to an OCI registry.
type Push struct {
	Local string   `arg:"" name:"local extension" help:"Path to a directory containing the extension to push." type:"existingdir"`
	OCI   OCIFlags `embed:""`

	Build     bool   `name:"build" help:"Build and push Docker image with pre-compiled plugin.so using Docker buildx."`
	Platforms string `name:"platforms" help:"Target platforms (comma-separated). Supported: linux/amd64, linux/arm64" default:"linux/amd64,linux/arm64"`

	manifest     *extensions.Manifest `kong:"-"` // Internal field: loaded extension manifest
	reference    string               `kong:"-"` // Internal field: full OCI repository reference (for binary image)
	srcReference string               `kong:"-"` // Internal field: source code OCI repository reference
	client       oci.RepositoryClient `kong:"-"` // Internal field: OCI client
}

//go:embed push_help.md
var pushHelp string

//go:embed Dockerfile.goplugin
var goPluginBuildDockerfile string

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

	// Validate build flag only works with composer type
	if p.Build && manifest.Type != extensions.TypeComposer {
		return fmt.Errorf("--build flag only supported for composer type extensions, got: %s", manifest.Type)
	}

	p.manifest = manifest
	return nil
}

// AfterApply is called by Kong after applying defaults to set computed default values.
func (p *Push) AfterApply(*kong.Context) error {
	// Source code repository: src-<name>
	p.srcReference = extensions.RepositoryName(p.OCI.Registry, "src-"+p.manifest.Name)

	// Binary repository: <name> (only if building)
	if p.Build {
		p.reference = extensions.RepositoryName(p.OCI.Registry, p.manifest.Name)
	}

	// Create client for source repository
	var err error
	p.client, err = newOCIRepositoryClient(p.srcReference, p.OCI.Username, p.OCI.Password, p.OCI.Insecure)
	return err
}

// Run executes the push command.
func (p *Push) Run(ctx context.Context) error {
	tag := p.manifest.Version

	// Step 1: Push source code (always)
	fmt.Printf("Pushing source extension %q (%s)...\n", "src-"+p.manifest.Name, tag)
	if err := p.pushSource(ctx, tag); err != nil {
		return err
	}

	// Step 2: Build and push Docker image (if --build flag set)
	if p.Build {
		fmt.Printf("\nBuilding and pushing pre-compiled extension %q (%s)...\n", p.manifest.Name, tag)
		if err := p.buildAndPushImage(ctx, tag); err != nil {
			return err
		}
	}

	return nil
}

// pushSource pushes the source code as tar.gz OCI artifact.
func (p *Push) pushSource(ctx context.Context, tag string) error {
	annotations := extensions.OCIAnnotationsForManifest(p.manifest)
	// Add source annotation if pushing to default registry so that artifacts are
	// linked to the repo.
	// See: https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#pushing-container-images
	if p.OCI.Registry == extensions.DefaultOCIRegistry {
		annotations[ocispec.AnnotationSource] = extensions.DefaultOCISource
	}

	digest, err := p.client.Push(ctx, p.Local, tag, annotations)
	if err != nil {
		return fmt.Errorf("failed to push source extension: %w", err)
	}

	fmt.Printf(`
%[1]sSuccessfully pushed source extension %[3]q (%[4]s)%[2]s
  → %[1]sDigest:%[2]s %[5]s
  → %[1]sReference:%[2]s %[6]s:%[4]s
`, internal.ANSIBold, internal.ANSIReset, "src-"+p.manifest.Name, tag, digest, p.srcReference)

	return nil
}

// buildAndPushImage builds and pushes Docker image with pre-compiled plugin.so.
func (p *Push) buildAndPushImage(ctx context.Context, tag string) error {
	// Check Docker and buildx availability
	if err := docker.CheckDockerBuildx(ctx); err != nil {
		return fmt.Errorf("docker buildx not available: %w", err)
	}

	// Create a temporary directory to store the dockerfile
	tempDir, err := os.MkdirTemp("/tmp", "boe-push-buildx")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Write the Dockerfile to the temporary directory
	dockerfilePath := tempDir + "/Dockerfile"
	if err := os.WriteFile(dockerfilePath, []byte(goPluginBuildDockerfile), 0o600); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Build and push
	imageRef := p.reference + ":" + tag
	platforms := docker.ParsePlatforms(p.Platforms)

	opts := &docker.BuildAndPushOptions{
		Context:         p.Local,
		PluginName:      p.manifest.Name,
		ImageRef:        imageRef,
		Platforms:       platforms,
		Dockerfile:      dockerfilePath,
		Username:        p.OCI.Username,
		Password:        p.OCI.Password,
		Insecure:        p.OCI.Insecure,
		Version:         p.manifest.Version,
		Description:     p.manifest.Description,
		Author:          p.manifest.Author,
		License:         p.manifest.License,
		ComposerVersion: p.manifest.ComposerVersion,
		ExtensionType:   string(p.manifest.Type),
	}

	fmt.Printf("Building for platforms: %s\n", strings.Join(platforms, ", "))

	if err := docker.BuildAndPushImage(ctx, opts); err != nil {
		return fmt.Errorf("failed to build and push image: %w", err)
	}

	fmt.Printf(`
%[1]sSuccessfully built and pushed extension image %[3]q (%[4]s)%[2]s
  → %[1]sReference:%[2]s %[5]s
  → %[1]sPlatforms:%[2]s %[6]s
`, internal.ANSIBold, internal.ANSIReset, p.manifest.Name, tag, imageRef, p.Platforms)

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
