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
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// defaultLogLevel is the default Envoy component log level.
const defaultLogLevel = "error"

// Run is a command to run Envoy with extensions.
type Run struct {
	EnvoyVersion string   `help:"Envoy version to use (e.g., 1.31.0)" env:"ENVOY_VERSION"`
	LogLevel     string   `help:"Envoy component log level." default:"all:error"`
	RunID        string   `name:"run-id" env:"BOE_RUN_ID" help:"Run identifier for this invocation. Defaults to timestamp-based ID or $BOE_RUN_ID. Use '0' for Docker/Kubernetes."`
	ListenPort   uint32   `help:"Port for Envoy listener to accept incoming traffic." default:"10000"`
	AdminPort    uint32   `help:"Port for Envoy admin interface." default:"9901"`
	Extensions   []string `name:"extension" help:"Extensions to enable (in the format: \"name\" or \"name:version\")."`
	Local        []string `name:"local" sep:"none" help:"Path to a directory containing a local Extension to enable." type:"existingdir"`
	Dev          bool     `help:"Whether to allow downloading dev versions of extensions (with -dev suffix). By default, only stable versions are allowed." default:"false"`
	// sep:"none" disables Kong's default comma-separated splitting for []string flags.
	// JSON config values contain commas (e.g. {"a":"1","b":"2"}) which would otherwise
	// be split into separate invalid fragments, causing protobuf unmarshal failures.
	Configs             []string     `name:"config" sep:"none" help:"Optional JSON config string for extensions. Applied in order to combined --extension and --local flags."`
	Clusters            ClusterFlags `embed:""`
	TestUpstreamHost    string       `name:"test-upstream-host" help:"Hostname for the test upstream cluster. Mutually exclusive with --test-upstream-cluster. Defaults to \"httpbin.org\"."`
	TestUpstreamCluster string       `name:"test-upstream-cluster" help:"Name of an existing configured cluster to use as the test upstream. The cluster must be configured via --cluster, --cluster-insecure, or --cluster-json. Mutually exclusive with --test-upstream-host."`
	Docker              bool         `help:"Run Envoy as a Docker container instead of using func-e." default:"false" env:"BOE_RUN_DOCKER"`
	Pull                string       `name:"pull" help:"Pull policy for the BOE Docker image (missing, always, never). Only applicable when running with --docker." enum:"missing,always,never" default:"missing"`
	OCI                 OCIFlags     `embed:""`

	extensionPositions extensionPositions `kong:"-"` // Internal field: tracks the original position of extensions specified via both --extension and --local flags
	defaultLogLevel    string             `kong:"-"` // Internal field: parsed defaut log level
	componentLogLevel  string             `kong:"-"` // Internal field: parsed component log levels
}

// OCIFlags holds flags for OCI registry authentication and configuration.
type OCIFlags struct {
	Registry string `name:"registry" env:"BOE_REGISTRY" help:"OCI registry URL for the extensions." default:"${default_registry}"`
	Insecure bool   `name:"insecure" env:"BOE_REGISTRY_INSECURE" help:"Allow connecting to an insecure (HTTP) registry." default:"false"`
	Username string `name:"username" env:"BOE_REGISTRY_USERNAME" help:"Username for the OCI registry."`
	Password string `name:"password" env:"BOE_REGISTRY_PASSWORD" help:"Password for the OCI registry." type:"password" sensitive:"true"`
}

// ClusterFlags holds flags for additional Envoy clusters.
type ClusterFlags struct {
	Secure   []string `name:"cluster" help:"Optional additional Envoy cluster provided in the host:tlsPort pattern." `
	Insecure []string `name:"cluster-insecure" help:"Optional additional Envoy cluster (with TLS transport disabled) provided in the host:port pattern." `
	JSONSpec []string `name:"cluster-json" sep:"none" help:"Optional additional Envoy cluster providing the complete cluster config in JSON format." `
}

//go:embed run_help.md
var runHelp string

// Help provides detailed help for the run command.
func (r *Run) Help() string { return runHelp }

// BeforeResolve is called by Kong before resolving the command to save the positions of extensions specified
// via --extension and --local flags, to ensure they are considered in the expected order.
func (r *Run) BeforeResolve() error {
	var err error
	r.extensionPositions, err = saveExtensionPositions(os.Args)
	return err
}

// BeforeApply is called by Kong before applying defaults to set computed default values.
func (r *Run) BeforeApply() error {
	// Set RunID default if not provided
	if r.RunID == "" {
		r.RunID = generateRunID(time.Now())
	}
	return nil
}

// Validate is called by Kong after parsing to validate the command arguments.
func (r *Run) Validate() error {
	var err error
	r.defaultLogLevel, r.componentLogLevel, err = parseLogLevels(r.LogLevel)
	if err != nil {
		return err
	}
	if r.TestUpstreamHost != "" && r.TestUpstreamCluster != "" {
		return fmt.Errorf("--test-upstream-host and --test-upstream-cluster are mutually exclusive")
	}
	return nil
}

// Run executes the run command
func (r *Run) Run(ctx context.Context, dirs *xdg.Directories, logger *slog.Logger) error {
	logger.Debug("handling run command", "cmd", internal.RedactSensitive(r))
	if r.Docker {
		runner := &envoy.RunnerDocker{
			Logger:          logger,
			Registry:        r.OCI.Registry,
			ListenPort:      r.ListenPort,
			AdminPort:       r.AdminPort,
			Dirs:            dirs,
			Arch:            runtime.GOARCH,
			LocalExtensions: r.Local,
			Pull:            r.Pull,
		}
		return runner.Run(ctx)
	}

	downloader := &extensions.Downloader{
		Logger:      logger,
		Registry:    r.OCI.Registry,
		Username:    r.OCI.Username,
		Password:    r.OCI.Password,
		Insecure:    r.OCI.Insecure,
		Dirs:        dirs,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		DevVersions: r.Dev,
	}

	downloaded, err := downloadExtensions(ctx, downloader, r.Extensions, true)
	if err != nil {
		return err
	}

	local, err := loadLocalManifests(ctx, logger, downloader, r.Local, true)
	if err != nil {
		return err
	}
	extensionsToRun, err := r.extensionPositions.sort(append(downloaded, local...))
	if err != nil {
		return err
	}

	// If no Envoy version is specified, check if the extensions have Envoy version constraints defined
	// and if so, use them to determine a compatible Envoy version to run.
	if r.EnvoyVersion == "" {
		r.EnvoyVersion, err = extensions.ResolveMinimumCompatibleEnvoyVersion(extensionsToRun)
		if err != nil {
			return err
		}
		logger.Debug("resolved Envoy version from manifests", "envoy_version", r.EnvoyVersion)
	} else {
		logger.Debug("validating Envoy version compatibility for extensions", "envoy_version", r.EnvoyVersion)
		if err = validateEnvoyCompat(r.EnvoyVersion, extensionsToRun); err != nil {
			return err
		}
	}

	// Make sure all composer extensions use the same version of composer
	logger.Debug("validating composer version compatibility for extensions")
	if err = validateComposerCompat(extensionsToRun); err != nil {
		return err
	}

	runner := &envoy.RunnerFuncE{
		Logger:              logger,
		EnvoyVersion:        r.EnvoyVersion,
		DefaultLogLevel:     r.defaultLogLevel,
		ComponentLogLevel:   r.componentLogLevel,
		Dirs:                dirs,
		RunID:               r.RunID,
		ListenPort:          r.ListenPort,
		AdminPort:           r.AdminPort,
		Extensions:          extensionsToRun,
		Configs:             r.Configs,
		Clusters:            r.Clusters.Secure,
		ClustersInsecure:    r.Clusters.Insecure,
		ClustersJSON:        r.Clusters.JSONSpec,
		TestUpstreamHost:    r.TestUpstreamHost,
		TestUpstreamCluster: r.TestUpstreamCluster,
	}

	return runner.Run(ctx)
}

// downloadExtensions downloads the specified extensions using the provided downloader.
func downloadExtensions(ctx context.Context, downloader *extensions.Downloader, refs []string, build bool) ([]*extensions.Manifest, error) {
	downloaded := make([]*extensions.Manifest, 0, len(refs))
	for _, ext := range refs {
		name, tag := splitRef(ext)
		_, _ = fmt.Fprintf(os.Stderr, "→ %sFetching %s...%s\n", internal.ANSIBold, name, internal.ANSIReset)
		artifact, err := downloader.DownloadExtension(ctx, name, tag)
		if err != nil {
			return nil, err
		}

		switch artifact.ArtifactType {
		case extensions.ArtifactBinary:
			if artifact.Manifest.Type == extensions.TypeGo {
				// Ensure the composer is downloaded before running any extensions that may depend on it.
				if err = extensions.CheckOrDownloadLibComposer(ctx, downloader, artifact.Manifest.ComposerVersion, extensions.ComposerArtifactLite); err != nil {
					return nil, fmt.Errorf("failed to download libcomposer %s for extension %s: %w",
						artifact.Manifest.ComposerVersion, name, err)
				}
			}
			downloaded = append(downloaded, artifact.Manifest)

		case extensions.ArtifactSource:
			// If the downloaded artifact is the composer bundle, we need to find the path to the extension
			// inside the composer source tree.
			if artifact.Manifest.Type == extensions.TypeComposer {
				var manifest *extensions.Manifest
				extensionSrc := extensions.LocalCacheComposerExtensionSourceDir(downloader.Dirs, artifact.Manifest, name)
				if extensionSrc == "" {
					return nil, fmt.Errorf("invalid source artifact for Go extension %s: missing expected source directory: %s", name, artifact.Path)
				}
				downloader.Logger.Info("loading downloaded extension manifest", "path", extensionSrc)

				manifest, err = extensions.LoadLocalManifest(filepath.Join(extensionSrc, "manifest.yaml"))
				if err != nil {
					return nil, fmt.Errorf("failed to load manifest for composer extension %s from source artifact %q: %w",
						name, extensionSrc, err)
				}
				manifest.Remote = true // Mark the manifest as remote since it is from a downloaded artifact
				// Composer source artifacts contains manifests without version information (just the parent reference).
				// We need to set the versions here.
				manifest.Version = artifact.Manifest.Version
				manifest.ComposerVersion = artifact.Manifest.ComposerVersion

				if build {
					fmt.Printf("→ %sBuilding %s...%s\n", internal.ANSIBold, name, internal.ANSIReset)
					downloader.Logger.Info("building downloaded Go extension", "name", manifest.Name, "version", artifact.Manifest.Version)
					// Build libcomposer from the downloaded source if it does not exist in the local cache.
					if _, err = os.Stat(extensions.LocalCacheComposerLib(downloader.Dirs, artifact.Manifest.Version)); err != nil {
						if err = extensions.BuildLibComposer(downloader.Logger, downloader.Dirs, artifact.Path, artifact.Manifest.Version, false); err != nil {
							return nil, fmt.Errorf("failed to build libcomposer %s for extension %s: %w",
								artifact.Manifest.Version, name, err)
						}
					}
					if err = extensions.BuildExtensionFromPath(downloader.Logger, downloader.Dirs, manifest, extensionSrc); err != nil {
						return nil, fmt.Errorf("failed to build Go extension %s from source artifact: %w", name, err)
					}
				}

				downloaded = append(downloaded, manifest)
			} else {
				if artifact.Manifest.Type == extensions.TypeRust && build {
					fmt.Printf("→ %sBuilding %s...%s\n", internal.ANSIBold, name, internal.ANSIReset)
					downloader.Logger.Info("building downloaded Rust extension", "name", artifact.Manifest.Name, "version", artifact.Manifest.Version)

					if err = extensions.CheckOrBuildDynamicModule(downloader.Logger, downloader.Dirs, artifact.Manifest, artifact.Path); err != nil {
						return nil, fmt.Errorf("failed to build Rust extension %s from source artifact: %w", name, err)
					}
				}
				downloaded = append(downloaded, artifact.Manifest)
			}
		default:
			return nil, fmt.Errorf("unknown artifact type %q for extension %s", artifact.ArtifactType, name)
		}
	}

	return downloaded, nil
}

// generateRunID generates a unique run identifier based on the current time.
// Defaults to the same convention as func-e: "YYYYMMDD_HHMMSS_UUU" format.
// Last 3 digits of microseconds to allow concurrent runs.
func generateRunID(now time.Time) string {
	micro := now.Nanosecond() / 1000 % 1000
	return fmt.Sprintf("%s_%03d", now.Format("20060102_150405"), micro)
}

// parseLogLevels parses a log level string in the format "component:level,component2:level".
// It extracts the "all" component (if present) for the --log-level flag and returns the
// remaining components for --component-log-level. If "all" is not specified, it defaults
// to DefaultLogLevel.
func parseLogLevels(logLevel string) (string, string, error) {
	if logLevel == "" {
		return defaultLogLevel, "", nil
	}

	var (
		baseLevel       = defaultLogLevel
		componentLevels []string
	)
	for part := range strings.SplitSeq(logLevel, ",") {
		component, level, found := strings.Cut(strings.TrimSpace(part), ":")
		if !found {
			return "", "", fmt.Errorf("invalid log level format %q: expected component:level", part)
		}

		component = strings.TrimSpace(component)
		level = strings.TrimSpace(level)

		if component == "" {
			return "", "", fmt.Errorf("invalid log level format %q: component cannot be empty", part)
		}
		if level == "" {
			return "", "", fmt.Errorf("invalid log level format %q: level cannot be empty", part)
		}

		if component == "all" {
			baseLevel = level
		} else {
			componentLevels = append(componentLevels, component+":"+level)
		}
	}

	return baseLevel, strings.Join(componentLevels, ","), nil
}

var errFailedToLoadLocalManifest = errors.New("failed to load local manifest")

// loadLocalManifests loads extension manifests from the specified local paths.
func loadLocalManifests(ctx context.Context, logger *slog.Logger, downloader *extensions.Downloader, paths []string, build bool) ([]*extensions.Manifest, error) {
	manifests := make([]*extensions.Manifest, 0, len(paths))

	for _, path := range paths {
		logger.Info("loading local extension manifest", "path", path)

		manifest, err := extensions.LoadLocalManifest(path + "/manifest.yaml")
		if err != nil {
			return nil, fmt.Errorf("%w from %s: %w", errFailedToLoadLocalManifest, path, err)
		}

		if err := extensions.ResolveLocalVersions(manifest); err != nil {
			return nil, fmt.Errorf("%w from %s: %w", errFailedToLoadLocalManifest, path, err)
		}

		if build {
			switch manifest.Type {
			case extensions.TypeGo:
				fmt.Printf("→ %sBuilding %s...%s\n", internal.ANSIBold, manifest.Name, internal.ANSIReset)
				downloader.Logger.Info("building local Go extension", "name", manifest.Name, "version", manifest.Version)
				if err := extensions.BuildExtensionFromPath(downloader.Logger, downloader.Dirs, manifest, path); err != nil {
					return nil, err
				}
				if err := extensions.DownloadLibComposerAndBuildIfNeeded(ctx, downloader, manifest.ComposerVersion, extensions.ComposerArtifactSource); err != nil {
					return nil, err
				}
			case extensions.TypeRust:
				fmt.Printf("→ %sBuilding %s...%s\n", internal.ANSIBold, manifest.Name, internal.ANSIReset)
				downloader.Logger.Info("building local Rust extension", "name", manifest.Name, "version", manifest.Version)
				// Build dynamic module (currently supports Rust)
				if err := extensions.BuildDynamicModule(downloader.Logger, downloader.Dirs, manifest, path); err != nil {
					return nil, err
				}
			}
		}

		manifests = append(manifests, manifest)
	}

	return manifests, nil
}

// extractTag extracts the tag from a full OCI reference.
func splitRef(ref string) (repo string, tag string) {
	if colonIdx := strings.LastIndex(ref, ":"); colonIdx != -1 {
		return ref[:colonIdx], ref[colonIdx+1:]
	}
	return ref, "latest"
}

// errInvalidManifest is returned when some extension is not compatible with the requested Envoy version.
var errIncompatibleEnvoyVersion = errors.New("incompatible Envoy version")

// validateEnvoyCompat checks if the given manifest is compatible with the specified Envoy version.
func validateEnvoyCompat(envoyVersion string, extensions []*extensions.Manifest) error {
	var errs []error

	for _, ext := range extensions {
		if !ext.SupportsEnvoyVersion(envoyVersion) {
			errs = append(errs, fmt.Errorf("%w %s: extension %s (%s) requires Envoy %q",
				errIncompatibleEnvoyVersion, envoyVersion, ext.Name, ext.Version, ext.EnvoyConstraints()))
		}
	}

	return errors.Join(errs...)
}

// validateComposerCompat validates that all extensions use the same composer version.
func validateComposerCompat(manifests []*extensions.Manifest) error {
	versions := make(map[string][]string)
	for _, ext := range manifests {
		if ext.Type == extensions.TypeGo {
			versions[ext.ComposerVersion] = append(versions[ext.ComposerVersion], ext.Name)
		}
	}

	if len(versions) > 1 {
		var b strings.Builder
		sortedVersions := slices.Collect(maps.Keys(versions))
		slices.Sort(sortedVersions)

		for _, version := range sortedVersions {
			fmt.Fprintf(&b, "  - version %s used by extensions: %s\n",
				version, strings.Join(versions[version], ", "))
		}
		return fmt.Errorf("incompatible Go versions found:\n%s"+
			"all Go extensions must use the same composer version", b.String())
	}

	return nil
}
