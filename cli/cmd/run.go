// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// defaultLogLevel is the default Envoy component log level.
const defaultLogLevel = "error"

// Run is a command to run Envoy with extensions.
type Run struct {
	EnvoyVersion     string   `help:"Envoy version to use (e.g., 1.31.0, dev, dev-latest)" env:"ENVOY_VERSION"`
	EnvoyVersionsURL string   `name:"envoy-versions-url" help:"URL of the Envoy versions JSON. Override to use debug builds (see archive-envoy)." env:"ENVOY_VERSIONS_URL" hidden:""`
	EnvoyPath        string   `name:"envoy-path" help:"Path to a custom Envoy binary. Skips Envoy download and version selection." env:"ENVOY_PATH"`
	LogLevel         string   `help:"Envoy component log level." default:"all:error" env:"ENVOY_LOG_LEVEL"`
	RunID            string   `name:"run-id" env:"BOE_RUN_ID" help:"Run identifier for this invocation. Overrides the default timestamp-based ID."`
	ListenPort       uint32   `help:"Port for Envoy listener to accept incoming traffic." default:"10000"`
	AdminPort        uint32   `name:"admin-port" help:"Port for Envoy admin interface." default:"9901" env:"BOE_ADMIN_PORT"`
	Extensions       []string `name:"extension" help:"Extensions to enable (in the format: \"name\" or \"name:version\")."`
	Local            []string `name:"local" sep:"none" help:"Path to a directory containing a local Extension to enable." type:"existingdir"`
	Dev              bool     `help:"Whether to allow downloading dev versions of extensions (with -dev suffix). By default, only stable versions are allowed." default:"false"`
	// sep:"none" disables Kong's default comma-separated splitting for []string flags.
	// JSON config values contain commas (e.g. {"a":"1","b":"2"}) which would otherwise
	// be split into separate invalid fragments, causing protobuf unmarshal failures.
	Configs                 []string     `name:"config" sep:"none" help:"Optional JSON config string for extensions. Applied in order to combined --extension and --local flags."`
	FilterTypes             []string     `name:"filter-type" sep:"none" help:"Set the filter type for an extension. Applied positionally to the combined --extension and --local flags. Accepted values: http, network, listener, udp_listener."`
	NativeHTTPFiltersBefore []string     `name:"native-http-filter-before" sep:"none" help:"Optional YAML/JSON native HTTP filter list (or @filepath) per extension position. Overrides manifest nativeHttpFilters.before."`
	NativeHTTPFiltersAfter  []string     `name:"native-http-filter-after" sep:"none" help:"Optional YAML/JSON native HTTP filter list (or @filepath) per extension position. Overrides manifest nativeHttpFilters.after."`
	Clusters                ClusterFlags `embed:""`
	TestUpstreamHost        string       `name:"test-upstream-host" help:"Hostname for the test upstream cluster. Mutually exclusive with --test-upstream-cluster. Defaults to \"httpbin.org\"."`
	TestUpstreamCluster     string       `name:"test-upstream-cluster" help:"Name of an existing configured cluster to use as the test upstream. The cluster must be configured via --cluster, --cluster-insecure, or --cluster-json. Mutually exclusive with --test-upstream-host."`
	Docker                  bool         `help:"Run Envoy as a Docker container instead of using func-e." default:"false" env:"BOE_RUN_DOCKER"`
	Pull                    string       `name:"pull" help:"Pull policy for the BOE Docker image (missing, always, never). Only applicable when running with --docker." enum:"missing,always,never" default:"missing"`
	DockerImageVersion      string       `name:"docker-image-version" help:"Override the BOE Docker image tag to use when running with --docker. By default, the image version matches the BOE version."`
	OCI                     OCIFlags     `embed:""`

	extensionPositions extensionPositions `kong:"-"` // Internal field: tracks the original position of extensions specified via both --extension and --local flags
	defaultLogLevel    string             `kong:"-"` // Internal field: parsed default log level
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
	if r.EnvoyPath != "" && r.EnvoyVersion != "" {
		return fmt.Errorf("--envoy-path and --envoy-version are mutually exclusive")
	}
	if r.DockerImageVersion != "" && !r.Docker {
		return fmt.Errorf("--docker-image-version can only be used with --docker")
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
			RunID:           r.RunID,
			Arch:            runtime.GOARCH,
			LocalExtensions: r.Local,
			Pull:            r.Pull,
			ImageVersion:    r.DockerImageVersion,
		}
		return runner.Run(ctx)
	}

	// We need to validate the existence here and not in the initial command as the path could be relative to
	// the Docker container when running in Docker.
	if r.EnvoyPath != "" {
		if _, err := os.Stat(r.EnvoyPath); err != nil {
			return fmt.Errorf("specified Envoy binary not found at %s: %w", r.EnvoyPath, err)
		}
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

	if r.EnvoyPath != "" {
		logger.Debug("using custom Envoy binary; skipping Envoy version resolution", "envoy_path", r.EnvoyPath)
	} else {
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
	}

	// Make sure all composer extensions use the same version of composer
	logger.Debug("validating composer version compatibility for extensions")
	if err = validateComposerCompat(extensionsToRun); err != nil {
		return err
	}

	// Warn if multiple local Go extensions are detected, as each is a separate c-shared
	// library with its own Go runtime.
	warnMultipleGoExtensions(extensionsToRun)

	// Collect ext_proc binary paths for process management.
	extProcBinaries := make(map[string]string)
	for _, ext := range extensionsToRun {
		if ext.Type == extensions.TypeExtProc {
			extProcBinaries[ext.Name] = extensions.LocalCacheExtension(dirs, ext)
		}
	}

	runner := &envoy.RunnerFuncE{
		Logger:                  logger,
		EnvoyVersion:            r.EnvoyVersion,
		EnvoyVersionsURL:        r.EnvoyVersionsURL,
		EnvoyPath:               r.EnvoyPath,
		DefaultLogLevel:         r.defaultLogLevel,
		ComponentLogLevel:       r.componentLogLevel,
		Dirs:                    dirs,
		RunID:                   r.RunID,
		ListenPort:              r.ListenPort,
		AdminAddress:            adminAddressForLocal(r.AdminPort),
		Extensions:              extensionsToRun,
		Configs:                 r.Configs,
		FilterTypes:             r.FilterTypes,
		NativeHTTPFiltersBefore: r.NativeHTTPFiltersBefore,
		NativeHTTPFiltersAfter:  r.NativeHTTPFiltersAfter,
		Clusters:                r.Clusters.Secure,
		ClustersInsecure:        r.Clusters.Insecure,
		ClustersJSON:            r.Clusters.JSONSpec,
		TestUpstreamHost:        r.TestUpstreamHost,
		TestUpstreamCluster:     r.TestUpstreamCluster,
		ExtProcBinaries:         extProcBinaries,
	}

	return runner.Run(ctx)
}

// adminAddressForLocal returns the admin address for a locally-running Envoy.
// Inside a Docker container, the BOE_ADMIN_ADDRESS env var is set by RunnerDocker
// to bind on all interfaces; otherwise defaults to loopback.
func adminAddressForLocal(port uint32) string {
	if addr := os.Getenv("BOE_ADMIN_ADDRESS"); addr != "" {
		return addr
	}
	return net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(port), 10))
}

// downloadExtensions downloads the specified extensions using the provided downloader.
func downloadExtensions(ctx context.Context, downloader *extensions.Downloader, refs []string, build bool) ([]*extensions.Manifest, error) {
	downloaded := make([]*extensions.Manifest, 0, len(refs))
	for _, ext := range refs {
		bundle, name, tag := splitRef(ext)

		// The reserved name "goplugin-loader" is not a downloadable extension: it drives
		// the composer's goplugin-loader filter directly from the user-supplied --config.
		// Resolve the composer version, ensure libcomposer is available, and synthesize a
		// manifest so the rest of the pipeline treats it like a dynamic-module extension.
		if name == extensions.GoPluginLoaderName {
			manifest, err := resolveGoPluginLoader(ctx, downloader, tag)
			if err != nil {
				return nil, err
			}
			downloaded = append(downloaded, manifest)
			continue
		}

		_, _ = fmt.Fprintf(os.Stderr, "→ %sFetching %s...%s\n", internal.ANSIBold, name, internal.ANSIReset)
		artifact, err := downloader.DownloadExtension(ctx, bundle, name, tag)
		if err != nil {
			return nil, err
		}
		artifact.ExtensionManifest.Remote = true
		artifact.ExtensionManifest.RemoteRef = ext

		switch artifact.ArtifactType {
		case extensions.ArtifactBinary:
			if artifact.Manifest.Type == extensions.TypeGo && !artifact.Manifest.CShared {
				// Ensure the composer is downloaded before running any extensions that may depend on it.
				if err = extensions.CheckOrDownloadLibComposer(ctx, downloader, artifact.Manifest.ComposerVersion,
					extensions.ComposerArtifactLite); err != nil {
					return nil, fmt.Errorf("failed to download libcomposer %s for extension %s: %w",
						artifact.Manifest.ComposerVersion, name, err)
				}
				composerManifest, _ := extensions.GetComposerManifest(downloader.Dirs, artifact.Manifest.ComposerVersion)
				if composerManifest != nil {
					extensions.ResolveVersionsWithParent(artifact.ExtensionManifest, composerManifest)
				}
			}
			artifact.ExtensionManifest.CShared = artifact.Manifest.CShared
			downloaded = append(downloaded, artifact.ExtensionManifest)
		case extensions.ArtifactSource:
			handleSourceError := handleExtensionSource(ctx, downloader, artifact.Manifest, artifact.ExtensionManifest,
				artifact.Path, downloader.Logger, build)
			if handleSourceError != nil {
				return nil, handleSourceError
			}
			downloaded = append(downloaded, artifact.ExtensionManifest)
		default:
			return nil, fmt.Errorf("unknown artifact type %q for extension %s", artifact.ArtifactType, name)
		}
	}

	return downloaded, nil
}

// resolveGoPluginLoader resolves the composer-lite version for the raw goplugin-loader
// extension, ensures libcomposer.so is present in the local cache, and returns a synthetic
// manifest describing it. The tag (if not "latest") is interpreted as the composer version.
func resolveGoPluginLoader(ctx context.Context, downloader *extensions.Downloader, tag string) (*extensions.Manifest, error) {
	version := tag
	if version == "" || version == "latest" {
		resolved, err := extensions.ResolveLatestComposerVersion(ctx, downloader.Logger)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve composer version for %s: %w", extensions.GoPluginLoaderName, err)
		}
		version = resolved
	}

	_, _ = fmt.Fprintf(os.Stderr, "→ %sPreparing %s (composer %s)...%s\n",
		internal.ANSIBold, extensions.GoPluginLoaderName, version, internal.ANSIReset)

	if err := extensions.CheckOrDownloadLibComposer(ctx, downloader, version, extensions.ComposerArtifactLite); err != nil {
		return nil, fmt.Errorf("failed to download libcomposer %s for %s: %w", version, extensions.GoPluginLoaderName, err)
	}

	manifest := &extensions.Manifest{
		Name:            extensions.GoPluginLoaderName,
		Type:            extensions.TypeGo,
		CShared:         true,
		Parent:          extensions.ComposerBundle,
		Version:         version,
		ComposerVersion: version,
		// Marked remote so extensionPositions.sort can match it by reference, like
		// other downloaded extensions (it is fetched from the registry as composer-lite).
		Remote: true,
	}
	manifest.ApplyDefaults()
	return manifest, nil
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
func loadLocalManifests(ctx context.Context, logger *slog.Logger, downloader *extensions.Downloader,
	paths []string, build bool,
) ([]*extensions.Manifest, error) {
	manifests := make([]*extensions.Manifest, 0, len(paths))

	for _, path := range paths {
		logger.Info("loading local extension manifest", "path", path)

		manifest, err := extensions.LoadLocalManifest(path + "/manifest.yaml")
		if err != nil {
			return nil, fmt.Errorf("%w from %s: %w", errFailedToLoadLocalManifest, path, err)
		}

		// This local extension may be a sub-extension of an extension bundle (e.g., a filter in the composer
		// source tree, a filter in Rust extension bundle and so on).
		// If so, we need to find the root manifest because we treat an extension bundle as a unit for version and
		// compilation management.
		var rootManifest *extensions.Manifest
		var rootPath string
		if manifest.Parent != "" {
			rootManifest, rootPath, err = resolveParentNoFallback(manifest)
			if err != nil {
				return nil, fmt.Errorf("%w from %s: %w", errFailedToLoadLocalManifest, path, err)
			}
			extensions.ResolveVersionsWithParent(manifest, rootManifest)
		}
		if rootManifest == nil {
			rootManifest = manifest
			rootPath = path
		}
		err = handleExtensionSource(ctx, downloader, rootManifest, manifest, rootPath, logger, build)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}

	return manifests, nil
}

func resolveParentNoFallback(m *extensions.Manifest) (*extensions.Manifest, string, error) {
	parent, dir, err := extensions.FindLocalParentManifest(m)
	if err != nil {
		return nil, "", err
	}
	if parent != nil {
		return parent, dir, nil
	}
	return nil, "", fmt.Errorf("parent manifest %q not found locally for extension %s", m.Parent, m.Name)
}

// resolveParent finds the parent manifest locally, falling back to the registry.
func resolveParent(ctx context.Context, downloader *extensions.Downloader, m *extensions.Manifest) (*extensions.Manifest, error) {
	parent, _, err := extensions.FindLocalParentManifest(m)
	if err != nil {
		return nil, err
	}
	if parent != nil {
		return parent, nil
	}

	var dl extensions.DownloadedExtension
	if m.Parent == "composer" {
		version := cmp.Or(m.ComposerVersion, m.Version, "latest")
		dl, err = downloader.DownloadComposer(ctx, version, extensions.ComposerArtifactLite)
	} else {
		version := cmp.Or(m.Version, "latest")
		dl, err = downloader.DownloadExtension(ctx, m.Parent, m.Parent, version)
	}
	if err != nil {
		return nil, fmt.Errorf("downloading parent %s: %w", m.Parent, err)
	}
	return dl.Manifest, nil
}

// splitRef splits an extension reference into bundle, extension name, and tag.
// The format is [bundle/]extension[:tag]. If no bundle prefix is given, the extension
// name is used as the bundle name (standalone extension). If no tag is given, "latest" is used.
//
// Examples:
//
//	"composer/example-go:v1.0.0" → ("composer", "example-go", "v1.0.0")
//	"cors:1.0.0"                 → ("cors", "cors", "1.0.0")
//	"cors"                       → ("cors", "cors", "latest")
func splitRef(ref string) (bundle string, extension string, tag string) {
	tag = "latest"
	name := ref
	if colonIdx := strings.LastIndex(ref, ":"); colonIdx != -1 {
		name = ref[:colonIdx]
		tag = ref[colonIdx+1:]
	}
	if slashIdx := strings.Index(name, "/"); slashIdx != -1 {
		return name[:slashIdx], name[slashIdx+1:], tag
	}
	return name, name, tag
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

// warnMultipleGoExtensions prints a warning to stderr if multiple distinct c-shared Go
// libraries are detected, since each is a separate shared library with its own Go runtime.
// Extensions hosted by the same bundle (e.g. several goplugin-loader configs sharing
// libcomposer.so) collapse to a single entry, as they share one Go runtime: they are
// deduplicated by the shared library identity (bundle-or-name plus version).
func warnMultipleGoExtensions(manifests []*extensions.Manifest) {
	// Deduplicate by shared-library identity (bundle-or-name plus version) so that
	// extensions sharing one runtime collapse to a single entry.
	seen := make(map[string]bool)
	for _, ext := range manifests {
		if ext.Type == extensions.TypeGo && ext.CShared {
			// ModuleName collapses bundle-hosted extensions (parent set, e.g.
			// goplugin-loader) onto the shared bundle library name, so several
			// extensions sharing one composer runtime count as a single library.
			seen[extensions.ModuleName(ext)+":"+ext.Version] = true
		}
	}
	if len(seen) < 2 {
		return
	}
	libs := slices.Collect(maps.Keys(seen))
	slices.Sort(libs)
	fmt.Fprintf(os.Stderr, "\n\033[1;33m⚠ Warning: Multiple Go extensions detected (%s).\033[0m\n"+
		"  Each Go extension is an independent shared library with its own Go runtime.\n"+
		"  In production, only one Go runtime can be loaded per Envoy process.\n"+
		"  Consider compiling all Go extensions into the same binary, or use\n"+
		"  the goplugin loader to load them through a single composer runtime.\n\n",
		strings.Join(libs, ", "))
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

// handleExtensionSource builds the bundle root from source (composer, Go, Rust or ext_proc) and
// resolves the extension's runtime metadata (CShared, inherited versions) from the build result.
// When build is false the build step is skipped entirely — used by config generation and tests
// that only need manifest resolution, not compiled artifacts.
func handleExtensionSource(ctx context.Context, downloader *extensions.Downloader, rootManifest *extensions.Manifest,
	extensionManifest *extensions.Manifest, rootPath string, logger *slog.Logger, build bool,
) error {
	if !build {
		return nil
	}
	// The source artifact must be present on disk before we can build it. A missing directory
	// means the download produced no source tree, so fail fast with a clear message rather than
	// letting the underlying build tool fail obscurely (e.g. chdir into a non-existent path).
	if info, err := os.Stat(rootPath); err != nil || !info.IsDir() {
		return fmt.Errorf("source directory for extension %s does not exist: %s", rootManifest.Name, rootPath)
	}
	switch rootManifest.Type {
	case extensions.TypeComposer:
		fmt.Printf("→ %sBuilding composer for %s...%s\n", internal.ANSIBold, rootManifest.Name, internal.ANSIReset)
		logger.Info("building composer from local source", "name", rootManifest.Name, "version", rootManifest.Version)
		if err := extensions.BuildComposer(logger, downloader.Dirs, rootPath, rootManifest.Version); err != nil {
			return fmt.Errorf("failed to build libcomposer for local extension %s: %w", rootManifest.Name, err)
		}
		extensionManifest.CShared = true
	case extensions.TypeGo:
		fmt.Printf("→ %sBuilding %s...%s\n", internal.ANSIBold, rootManifest.Name, internal.ANSIReset)
		logger.Info("building local Go extension", "name", rootManifest.Name, "version", rootManifest.Version)
		cshared, err := extensions.BuildExtensionFromPath(logger, downloader.Dirs, rootManifest, rootPath)
		if err != nil {
			return fmt.Errorf("failed to build local Go extension %s: %w", rootManifest.Name, err)
		}
		if !cshared {
			// A locally-built old-style Go plugin is loaded via plugin.Open and must link against a
			// libcomposer built from the same source with the same toolchain/deps; a prebuilt lite
			// binary would be ABI-incompatible. Build from source unconditionally rather than via
			// CheckOrDownloadLibComposer: that cache is keyed only by version and shares the
			// dym/composer/<version>/libcomposer.so slot with the downloaded lite binary, so a cached
			// binary (from goplugin-loader or a downloaded plugin) would otherwise be reused here and
			// reintroduce the ABI mismatch.
			if err = extensions.DownloadLibComposerAndBuildIfNeeded(ctx, downloader, rootManifest.ComposerVersion,
				extensions.ComposerArtifactSource); err != nil {
				return fmt.Errorf("failed to build libcomposer %s for extension %s: %w",
					rootManifest.ComposerVersion, rootManifest.Name, err)
			}
			composerManifest, _ := extensions.GetComposerManifest(downloader.Dirs, rootManifest.ComposerVersion)
			if composerManifest != nil {
				extensions.ResolveVersionsWithParent(extensionManifest, composerManifest)
			}
		}
		extensionManifest.CShared = cshared
	case extensions.TypeRust:
		fmt.Printf("→ %sBuilding %s...%s\n", internal.ANSIBold, rootManifest.Name, internal.ANSIReset)
		downloader.Logger.Info("building local Rust extension", "name", rootManifest.Name, "version", rootManifest.Version)
		// Build dynamic module (currently supports Rust)
		if err := extensions.BuildDynamicModule(downloader.Logger, downloader.Dirs, rootManifest, rootPath); err != nil {
			return err
		}
	case extensions.TypeExtProc:
		fmt.Printf("→ %sBuilding %s...%s\n", internal.ANSIBold, rootManifest.Name, internal.ANSIReset)
		downloader.Logger.Info("building local ext_proc extension", "name", rootManifest.Name, "version", rootManifest.Version)
		if err := extensions.BuildExtProcBinary(downloader.Logger, downloader.Dirs, rootManifest, rootPath); err != nil {
			return err
		}
	}
	return nil
}
