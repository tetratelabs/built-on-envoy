// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package envoy provides functionality to run Envoy using func-e.
package envoy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	funce "github.com/tetratelabs/func-e"
	"github.com/tetratelabs/func-e/api"
	"github.com/tetratelabs/func-e/experimental/admin"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// Runner handles running Envoy via func-e
type Runner struct {
	Logger *slog.Logger

	// EnvoyVersion specifies the Envoy version to run. If empty, func-e's default version is used.
	EnvoyVersion string
	// DefaultLogLevel specifies the base Envoy log level.
	DefaultLogLevel string
	// ComponentLogLevel specifies the Envoy component log level.
	ComponentLogLevel string
	// Dirs specifies XDG directories for func-e
	Dirs *xdg.Directories
	// RunID specifies the run identifier for this invocation.
	RunID string
	// ListenPort is the port for Envoy listener to accept incoming traffic.
	ListenPort uint32
	// AdminPort is the port for Envoy admin interface.
	AdminPort uint32
	// Extensions specifies the extensions to enable.
	Extensions []*extensions.Manifest
	// Configs specifies optional JSON config strings for each extension (by index).
	Configs []string
	// Clusters specifies additional Envoy cluster JSON strings to include in the configuration.
	Clusters []string
}

// Run starts Envoy using func-e as a library
func (r *Runner) Run(ctx context.Context) error {
	params := &ConfigGenerationParams{
		Logger:       r.Logger,
		AdminPort:    r.AdminPort,
		ListenerPort: r.ListenPort,
		Dirs:         r.Dirs,
		Extensions:   r.Extensions,
		Configs:      r.Configs,
		Clusters:     r.Clusters,
	}
	config, err := RenderConfig(params, FullConfigRenderer)
	if err != nil {
		return err
	}

	// Create a temporary directory with hard links to all dynamic module libraries
	// TODO(wbpcode): once Envoy support to specify lib path directly, we can remove this hack.
	searchPath, cleanup, err := setupDynamicModuleSearchPath(params)
	if err != nil {
		return fmt.Errorf("failed to setup dynamic module search path: %w", err)
	}
	defer cleanup()

	r.Logger.Debug("setting up dynamic module search path", "path", searchPath)

	err = os.Setenv("ENVOY_DYNAMIC_MODULES_SEARCH_PATH", searchPath)
	if err != nil {
		return fmt.Errorf("failed to set ENVOY_DYNAMIC_MODULES_SEARCH_PATH: %w", err)
	}

	// Disable cgo pointer checks as Envoy may hold pointers to Go memory.
	err = os.Setenv("GODEBUG", "cgocheck=0")
	if err != nil {
		return fmt.Errorf("failed to set GODEBUG: %w", err)
	}

	names := make([]string, 0, len(r.Extensions))
	for _, ext := range r.Extensions {
		names = append(names, ext.Name)
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s✓ Starting Envoy with extensions: %v...%s\n",
		internal.ANSIBold, names, internal.ANSIReset)

	// Define startup hook that will be called when Envoy admin is ready
	start := time.Now()
	startupHook := func(_ context.Context, adminClient admin.AdminClient, _ string) error {
		startDuration := time.Since(start).Round(100 * time.Millisecond)
		_, _ = fmt.Fprintf(os.Stderr, `
%[4]s✓ Envoy is ready after %[3]v%[5]s
  → %[4]sProxy:%[5]s http://localhost:%[1]d
  → %[4]sAdmin:%[5]s http://localhost:%[2]d

%[4]sTest with:%[5]s
  curl http://localhost:%[1]d/

Press Ctrl+C to stop
`, r.ListenPort, adminClient.Port(), startDuration, internal.ANSIBold, internal.ANSIReset)
		return nil
	}

	// Build func-e options
	opts := []api.RunOption{
		api.Out(os.Stdout),
		api.EnvoyOut(os.Stdout),
		api.EnvoyErr(os.Stderr),
		api.ConfigHome(r.Dirs.ConfigHome),
		api.DataHome(r.Dirs.DataHome),
		api.StateHome(r.Dirs.StateHome),
		api.RuntimeDir(r.Dirs.RuntimeDir),
		api.RunID(r.RunID),
		admin.WithStartupHook(startupHook),
	}
	if r.EnvoyVersion != "" {
		opts = append(opts, api.EnvoyVersion(r.EnvoyVersion))
	}

	// Run Envoy with embedded config
	args := []string{"--config-yaml", config, "--log-level", r.DefaultLogLevel}
	if r.ComponentLogLevel != "" {
		args = append(args, "--component-log-level", r.ComponentLogLevel)
	}

	return funce.Run(ctx, args, opts...)
}

// setupDynamicModuleSearchPath creates a temporary directory and populates it with hard links
// to all dynamic module libraries (both composer and Rust dynamic modules).
// Returns the path to the temporary directory and a cleanup function.
func setupDynamicModuleSearchPath(params *ConfigGenerationParams) (string, func(), error) {
	// Create a temporary directory for dynamic module libraries
	tempDir, err := os.MkdirTemp("", "boe-dynamic-modules-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	// Collect all dynamic module libraries that need to be linked
	var composerVersion string
	for _, ext := range params.Extensions {
		switch ext.Type {
		case extensions.TypeComposer:
			// At this point all extensions are guaranteed to use the same version of
			// composer.
			composerVersion = ext.ComposerVersion

		case extensions.TypeDynamicModule:
			// Get the path to the Rust dynamic module library
			libPath := extensions.LocalCacheExtension(params.Dirs, ext)
			if _, err := os.Stat(libPath); os.IsNotExist(err) {
				cleanup()
				return "", nil, fmt.Errorf("library not found at %s for extension %s", libPath, ext.Name)
			}

			// Create hard link in the temporary directory
			linkPath := filepath.Join(tempDir, filepath.Base(libPath))
			if err := os.Link(libPath, linkPath); err != nil {
				cleanup()
				return "", nil, fmt.Errorf("failed to create hard link for %s: %w", ext.Name, err)
			}
		}
	}

	// If there are composer extensions, link libcomposer.so as well
	if composerVersion != "" {
		composerPath := extensions.LocalCacheComposerLib(params.Dirs, composerVersion)
		if _, err := os.Stat(composerPath); err == nil {
			linkPath := filepath.Join(tempDir, filepath.Base(composerPath))

			params.Logger.Debug("linking libcomposer for composer extensions", "path", composerPath, "linkPath", linkPath)

			if err := os.Link(composerPath, linkPath); err != nil {
				cleanup()
				return "", nil, fmt.Errorf("failed to create hard link for libcomposer: %w", err)
			}
		}
	}

	return tempDir, cleanup, nil
}
