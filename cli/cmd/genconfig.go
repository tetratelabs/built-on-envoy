// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/envoy"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// GenConfig is a command to generate Envoy configuration with specified extensions.
type GenConfig struct {
	Minimal    bool     `help:"Generate configuration with only extension-generated resources (HTTP filters and clusters)."`
	ListenPort uint32   `help:"Port for Envoy listener to accept incoming traffic." default:"10000"`
	AdminPort  uint32   `help:"Port for Envoy admin interface." default:"9901"`
	Extensions []string `name:"extension" help:"Extensions to enable (in the format: \"name\" or \"name:version\")." sep:","`
	Local      []string `name:"local" help:"Path to a directory containing a local Extension to enable." type:"existingdir" sep:","`
	// sep:"none" disables Kong's default comma-separated splitting for []string flags.
	// JSON config values contain commas (e.g. {"a":"1","b":"2"}) which would otherwise
	// be split into separate invalid fragments, causing protobuf unmarshal failures.
	Configs  []string     `name:"config" sep:"none" help:"Optional JSON config string for extensions. Applied in order to combined --extension and --local flags."`
	Clusters ClusterFlags `embed:""`
	OCI      OCIFlags     `embed:""`
	Output   string       `name:"output" help:"Directory to put the generated config into. Use \"-\" to print it to the standard output." default:"-" type:"path"`

	extensionPositions extensionPositions `kong:"-"` // Internal field: tracks the original position of extensions specified via both --extension and --local flags
	stdout             io.Writer          `kong:"-"` // Internal field for testing
}

//go:embed genconfig_help.md
var genConfigHelp string

// Help provides detailed help for the config command.
func (g *GenConfig) Help() string { return genConfigHelp }

// BeforeResolve is called by Kong before resolving the command to save the positions of extensions specified
// via --extension and --local flags, to ensure they are considered in the expected order.
func (g *GenConfig) BeforeResolve() error {
	var err error
	g.extensionPositions, err = saveExtensionPositions(os.Args)
	return err
}

// Run executes the GenConfig command.
func (g *GenConfig) Run(ctx context.Context, dirs *xdg.Directories, logger *slog.Logger) error {
	logger.Debug("handling genconfig command", "cmd", internal.RedactSensitive(g))

	stdout := g.stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	exportConfig := g.Output != "-"
	if exportConfig {
		logger.Debug("creating config export diretory", "path", g.Output)
		if err := os.MkdirAll(g.Output, 0o750); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	downloader := &extensions.Downloader{
		Logger:   logger,
		Registry: g.OCI.Registry,
		Username: g.OCI.Username,
		Password: g.OCI.Password,
		Insecure: g.OCI.Insecure,
		Dirs:     dirs,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
	}

	// If we're only generating config to print to the stdout, we can skip building the extensions
	// but if we're exporting it, we need ot build to make sure the extension files exist.
	downloaded, err := downloadExtensions(ctx, downloader, g.Extensions, exportConfig)
	if err != nil {
		return err
	}
	// Set the OCI registry info on downloaded manifests so that config generation
	// produces oci:// URLs for remote extensions.
	for _, m := range downloaded {
		m.SourceRegistry = downloader.Registry
		m.SourceTag = m.Version
	}
	local, err := loadLocalManifests(ctx, logger, downloader, g.Local, exportConfig)
	if err != nil {
		return err
	}
	extensions, err := g.extensionPositions.sort(append(downloaded, local...))
	if err != nil {
		return err
	}

	var renderer envoy.ConfigRenderer
	if g.Minimal {
		renderer = envoy.MinimalConfigRenderer
	} else {
		renderer = envoy.FullConfigRenderer
	}

	config, err := envoy.RenderConfig(&envoy.ConfigGenerationParams{
		Logger:           logger,
		AdminPort:        g.AdminPort,
		ListenerPort:     g.ListenPort,
		Dirs:             dirs,
		Extensions:       extensions,
		Configs:          g.Configs,
		Clusters:         g.Clusters.Secure,
		ClustersInsecure: g.Clusters.Insecure,
		ClustersJSON:     g.Clusters.JSONSpec,
	}, renderer)
	if err != nil {
		return err
	}

	if !exportConfig {
		_, _ = fmt.Fprintln(stdout, config)
		return nil
	}

	files, err := g.writeConfig(config, extensions, dirs, logger)
	if err != nil {
		return err
	}
	printExportSummary(stdout, g.Output, files)
	return nil
}

// writeConfig writes the Envoy configuration and copies the extension files to the given path,
// so that the configuration can be easily loaded by func-e, Docker, etc.
func (g *GenConfig) writeConfig(
	config string,
	manifests []*extensions.Manifest,
	dirs *xdg.Directories,
	logger *slog.Logger,
) ([]string, error) {
	var files []string

	logger.Info("writing configuration", "path", g.Output)
	envoyConfig := filepath.Join(g.Output, "envoy.yaml")
	if err := os.WriteFile(envoyConfig, []byte(config), 0o600); err != nil {
		return nil, fmt.Errorf("failed to save Envoy config: %w", err)
	}
	files = append(files, envoyConfig)

	for _, m := range manifests {
		var (
			srcExtensionFile = extensions.LocalCacheExtension(dirs, m)
			dstExtensionFile string
		)

		switch m.Type {
		case extensions.TypeLua:
			// Lua extensions are rendered inline, so there is nothing to copy
			continue
		case extensions.TypeGo:
			// If it is a Go extension we need to copy the composer library too
			composerFile := extensions.LocalCacheComposerLib(dirs, m.ComposerVersion)
			dst := filepath.Join(g.Output, filepath.Base(composerFile))
			if err := copyFile(composerFile, dst, logger); err != nil {
				return nil, err
			}
			files = append(files, dst)
			// We also copy the Go plugin file just for convenience, as the config will be generated
			// with an 'oci://' path or with a 'file://' path pointing to the local cache, so this file
			// will not be actually used, but it's conveient to copy it as well to let users easily play
			// with the raw Envoy configs.
			dstExtensionFile = filepath.Join(g.Output, m.Name+".so")
		default:
			dstExtensionFile = filepath.Join(g.Output, filepath.Base(srcExtensionFile))
		}

		if err := copyFile(srcExtensionFile, dstExtensionFile, logger); err != nil {
			return nil, err
		}
		files = append(files, dstExtensionFile)
	}

	return files, nil
}

// copyFile copies the given source file to the destination.
func copyFile(srcPath, dstPath string, logger *slog.Logger) error {
	logger.Debug("copying extension", "src", srcPath, "dst", dstPath)
	src, err := os.Open(filepath.Clean(srcPath))
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(filepath.Clean(dstPath))
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	if _, err = io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy %q: %w", srcPath, err)
	}
	return nil
}

// printExportSummary prints information about how to use the exported configuration.
func printExportSummary(stdout io.Writer, outputPath string, files []string) {
	_, _ = fmt.Fprintf(stdout, "\n%v✓ Config exported to:%v %s\n",
		internal.ANSIBold, internal.ANSIReset, outputPath)
	for _, f := range files {
		_, _ = fmt.Fprintf(stdout, "    - %s\n", filepath.Base(f))
	}
	_, _ = fmt.Fprintf(stdout, `
%[1]s→ Run localy with with func-e:%[2]s (https://func-e.io/)
    export ENVOY_DYNAMIC_MODULES_SEARCH_PATH=%[3]s
    export GODEBUG=cgocheck=0
    func-e run -c %[3]s/envoy.yaml --log-level info --component-log-level dynamic_modules:debug
`, internal.ANSIBold, internal.ANSIReset, outputPath)
}
