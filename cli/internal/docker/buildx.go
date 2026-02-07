// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package docker provides utilities for building and pushing Docker images
// using docker buildx CLI.
package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/client"
)

var customBuilderNameKey = "customMultiPlatformBuilderName"

// BuildAndPushOptions contains options for building and pushing Docker images.
type BuildAndPushOptions struct {
	Context         string   // Extension directory path
	PluginName      string   // Plugin name from manifest
	ImageRef        string   // Full image reference (registry/extension-name:version)
	Platforms       []string // Target platforms (e.g., ["linux/amd64", "linux/arm64"])
	Dockerfile      string   // Path to Dockerfile
	Username        string   // Registry credentials
	Password        string
	Insecure        bool
	Version         string // Extension version
	Description     string // Extension description
	Author          string // Extension author
	License         string // Extension license
	ComposerVersion string // Composer version for composer-type extensions
	ExtensionType   string // Extension type (lua, wasm, composer, etc.)
}

// BuildAndPushImage builds multi-platform image and pushes to registry using docker buildx.
func BuildAndPushImage(ctx context.Context, opts *BuildAndPushOptions) error {
	// Check docker and buildx availability
	if err := CheckDockerBuildx(ctx); err != nil {
		return err
	}

	// Validate platforms
	for _, platform := range opts.Platforms {
		if err := ValidatePlatform(platform); err != nil {
			return err
		}
	}

	// Deduplicate platforms
	platformSet := make(map[string]bool)
	var uniquePlatforms []string
	for _, p := range opts.Platforms {
		if !platformSet[p] {
			platformSet[p] = true
			uniquePlatforms = append(uniquePlatforms, p)
		}
	}
	opts.Platforms = uniquePlatforms

	// Login to registry if credentials provided
	// TODO(wbpcode): do we need to do this? Ideally we should resuse existing docker credentials
	// and let docker handle authentication automatically.
	if opts.Username != "" || opts.Password != "" {
		if err := dockerLogin(ctx, opts); err != nil {
			return fmt.Errorf("failed to login to registry: %w", err)
		}
	}

	// Build and push with buildx
	if err := buildxBuildAndPush(ctx, opts, getCustomBuilderName(ctx)); err != nil {
		return fmt.Errorf("failed to build and push: %w", err)
	}

	return nil
}

// dockerLogin logs into the Docker registry using secure credential passing.
func dockerLogin(ctx context.Context, opts *BuildAndPushOptions) error {
	registry := extractRegistry(opts.ImageRef)

	fmt.Printf("Logging in to %s...\n", registry)

	args := []string{"login", "--password-stdin"}

	if opts.Username != "" {
		args = append(args, "--username", opts.Username)
	}

	args = append(args, registry)

	cmd := exec.CommandContext(ctx, "docker", args...)

	// Pass password via stdin for security (not visible in process list)
	if opts.Password != "" {
		cmd.Stdin = strings.NewReader(opts.Password)
	} else {
		// If no password provided but username is, let Docker prompt
		cmd.Stdin = os.Stdin
	}

	// Suppress stdout for security, but show stderr for errors
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("registry authentication failed: %w\nHint: Ensure credentials are correct", err)
	}

	fmt.Printf("✓ Successfully authenticated to %s\n", registry)
	return nil
}

// CheckDockerBuildx checks if Docker daemon and buildx are available.
func CheckDockerBuildx(ctx context.Context) error {
	// Check docker daemon
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer func() {
		_ = cli.Close()
	}()

	_, err = cli.Ping(ctx)
	if err != nil {
		return fmt.Errorf("docker daemon not available: %w", err)
	}

	// Check buildx command
	cmd := exec.CommandContext(ctx, "docker", "buildx", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker buildx not available: %w (ensure docker buildx plugin is installed)", err)
	}

	return nil
}

// checkOrCreateBuilder creates a new buildx builder instance.
func checkOrCreateBuilder(ctx context.Context, name, configPath string) error {
	// Check if builder already exist and skip creation if it does to avoid
	// unnecessary overhead.
	cmd := exec.CommandContext(ctx, "docker", "buildx", "inspect", name)
	if err := cmd.Run(); err == nil {
		fmt.Printf("Builder already exists: %s\n", name)
		return nil // Builder exists, no need to create
	}

	fmt.Printf("Creating builder: %s\n", name)

	cmd = exec.CommandContext(ctx, "docker", "buildx", "create",
		"--name", name,
		"--driver", "docker-container",
		"--bootstrap",
		"--config", configPath,
	)

	// Suppress output for cleaner logs
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create builder: %w", err)
	}

	fmt.Printf("✓ Builder created: %s\n", name)
	return nil
}

// removeBuilder removes a buildx builder instance.
func removeBuilder(ctx context.Context, name string) error {
	fmt.Printf("Cleaning up builder: %s\n", name)

	cmd := exec.CommandContext(ctx, "docker", "buildx", "rm", "--force", name)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove builder: %w", err)
	}

	fmt.Printf("✓ Builder cleaned up: %s\n", name)
	return nil
}

// Get custom builder name from context for testing purposes.
// In production, we use a default builder and default configuration from host.
func getCustomBuilderName(ctx context.Context) string {
	builderName := ""
	// Check for custom builder name in context (used for testing)
	if ctx != nil {
		if v := ctx.Value(customBuilderNameKey); v != nil {
			builderName = v.(string)
		}
	}
	return builderName
}

// buildxBuildAndPush builds and pushes image using docker buildx.
func buildxBuildAndPush(ctx context.Context, opts *BuildAndPushOptions, builderName string) error {
	platformsStr := strings.Join(opts.Platforms, ",")

	fmt.Printf("\nBuilding multi-platform image...\n")
	fmt.Printf("  Image: %s\n", opts.ImageRef)
	fmt.Printf("  Platforms: %s\n", platformsStr)
	fmt.Printf("  Plugin: %s\n\n", opts.PluginName)

	// Build command arguments
	args := []string{
		"buildx", "build",
		"--platform", platformsStr,
		"--build-arg", "PLUGIN_NAME=" + opts.PluginName,
		"--tag", opts.ImageRef,
		"--file", opts.Dockerfile,
		"--output", "type=registry,oci-mediatypes=true",
		"--provenance=false",
	}
	// If builderName is provided (e.g., in tests), use it. Otherwise, rely on default builder
	// which should be pre-configured on host.
	if builderName != "" {
		args = append(args, "--builder", builderName)
	}

	// Detect git information if available for annotations.
	gitInfo := detectGitInfo(ctx, opts.Context)

	// Get the appropriate annotation prefix based on platform count
	annotationPrefix := AnnotationPrefix(len(opts.Platforms))

	// Add OCI annotations
	timestamp := time.Now().UTC().Format(time.RFC3339)
	annotations := map[string]string{
		annotationPrefix + AnnotationLicenses:          opts.License,
		annotationPrefix + AnnotationTitle:             opts.PluginName,
		annotationPrefix + AnnotationVersion:           opts.Version,
		annotationPrefix + AnnotationDescription:       opts.Description,
		annotationPrefix + AnnotationCreated:           timestamp,
		annotationPrefix + AnnotationAuthors:           opts.Author,
		annotationPrefix + AnnotationExtensionType:     opts.ExtensionType,
		annotationPrefix + AnnotationExtensionArtifact: AnnotationExtensionArtifactBinary,
		annotationPrefix + AnnotationSource:            gitInfo.RemoteURL,
		annotationPrefix + AnnotationRevision:          gitInfo.CommitSHA,
	}

	// Add composer version if available
	if opts.ComposerVersion != "" {
		annotations[annotationPrefix+AnnotationExtensionComposerVersion] = opts.ComposerVersion
	}

	// Add annotation flags
	for key, value := range annotations {
		if value != "" {
			args = append(args, "--annotation", fmt.Sprintf("%s=%s", key, value))
		}
	}

	// Add build context as the last argument
	args = append(args, opts.Context)

	// #nosec G204
	cmd := exec.CommandContext(ctx, "docker", args...)

	// Stream output to user
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set working directory to context
	cmd.Dir = opts.Context

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("buildx build failed: %w", err)
	}

	fmt.Printf("\n✓ Successfully pushed multi-platform image: %s\n", opts.ImageRef)
	fmt.Printf("  Platforms: %s\n", platformsStr)

	return nil
}

// extractRegistry extracts registry from image reference.
func extractRegistry(imageRef string) string {
	parts := strings.SplitN(imageRef, "/", 2)
	// If the reference does not contain a valid registry, let the authentication fail.
	if len(parts) > 1 {
		return parts[0]
	}
	return "docker.io"
}

// Supported platforms for building
var supportedPlatforms = map[string]bool{
	"linux/amd64": true,
	"linux/arm64": true,
}

// ValidatePlatform validates that the platform is supported.
func ValidatePlatform(platform string) error {
	platform = strings.TrimSpace(platform)

	if !supportedPlatforms[platform] {
		return fmt.Errorf("unsupported platform: %s (supported: linux/amd64, linux/arm64)", platform)
	}

	return nil
}

// CheckDockerAvailable checks if Docker daemon is available.
func CheckDockerAvailable(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer func() {
		_ = cli.Close()
	}()

	_, err = cli.Ping(ctx)
	if err != nil {
		return fmt.Errorf("docker daemon not available: %w", err)
	}

	return nil
}

// ParsePlatforms parses comma-separated platform string.
func ParsePlatforms(platformStr string) []string {
	platforms := strings.Split(platformStr, ",")
	var result []string
	for _, p := range platforms {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// GitInfo contains git repository information.
type GitInfo struct {
	RemoteURL string // Git remote URL
	CommitSHA string // Current commit SHA
}

// detectGitInfo attempts to detect git repository information from the given directory.
func detectGitInfo(ctx context.Context, dir string) GitInfo {
	info := GitInfo{}

	// Get remote URL
	remoteCmd := exec.CommandContext(ctx, "git", "-C", dir, "config", "--get", "remote.origin.url")
	if output, err := remoteCmd.Output(); err == nil {
		info.RemoteURL = strings.TrimSpace(string(output))
	}

	// Get commit SHA
	commitCmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	if output, err := commitCmd.Output(); err == nil {
		info.CommitSHA = strings.TrimSpace(string(output))
	}

	// Return info even if partially populated
	return info
}
