// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package envoy provides functionality to run Envoy using func-e.
package envoy

import (
	"context"
	"fmt"
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
}

// Run starts Envoy using func-e as a library
func (r *Runner) Run(ctx context.Context) error {
	config, err := RenderConfig(ConfigGenerationParams{
		AdminPort:    r.AdminPort,
		ListenerPort: r.ListenPort,
		DataHome:     r.Dirs.DataHome,
		Extensions:   r.Extensions,
		Configs:      r.Configs,
	}, FullConfigRenderer)
	if err != nil {
		return err
	}

	// For now only golang dynamic modules are supported and will be built into same libcomposer.so.
	// So, only need to expose path of libcomposer.so to Envoy.
	// TODO(wbpcode): make this more general when other dynamic module types are supported.
	composerPath := getComposerPath(r.Dirs.DataHome, extensions.LibComposerVersion)
	composerParentDir := filepath.Dir(composerPath)
	err = os.Setenv("ENVOY_DYNAMIC_MODULES_SEARCH_PATH", composerParentDir)
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
