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
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	funce "github.com/tetratelabs/func-e"
	"github.com/tetratelabs/func-e/api"
	"github.com/tetratelabs/func-e/experimental/admin"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// Runner is the interface for running Envoy.
type Runner interface {
	// Run starts Envoy with the configured extensions.
	Run(ctx context.Context) error
}

// RunnerFuncE handles running Envoy via func-e.
type RunnerFuncE struct {
	Logger *slog.Logger
	// EnvoyVersion specifies the Envoy version to run. If empty, the default version is used.
	EnvoyVersion string
	// DefaultLogLevel specifies the base Envoy log level.
	DefaultLogLevel string
	// ComponentLogLevel specifies the Envoy component log level.
	ComponentLogLevel string
	// Dirs specifies XDG directories.
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
	// Clusters specifies additional Envoy cluster (with TLS) from short names to include in the configuration.
	Clusters []string
	// ClustersInsecure specifies additional Envoy cluster (without TLS) from short names to include in the configuration.
	ClustersInsecure []string
	// ClustersJSON specifies additional Envoy cluster JSON strings to include in the configuration.
	ClustersJSON []string
	// TestUpstreamHost specifies the hostname for the test upstream cluster. Defaults to "httpbin.org".
	// Mutually exclusive with TestUpstreamCluster.
	TestUpstreamHost string
	// TestUpstreamCluster specifies the name of an existing configured cluster to use as the test upstream.
	// Mutually exclusive with TestUpstreamHost.
	TestUpstreamCluster string
	// ExtProcBinaries maps ext_proc extension names to their binary paths.
	ExtProcBinaries map[string]string
}

// Run starts Envoy using func-e as a library.
func (r *RunnerFuncE) Run(ctx context.Context) error {
	params := &ConfigGenerationParams{
		Logger:              r.Logger,
		AdminPort:           r.AdminPort,
		ListenerPort:        r.ListenPort,
		Dirs:                r.Dirs,
		Extensions:          r.Extensions,
		Configs:             r.Configs,
		Clusters:            r.Clusters,
		ClustersInsecure:    r.ClustersInsecure,
		ClustersJSON:        r.ClustersJSON,
		TestUpstreamHost:    r.TestUpstreamHost,
		TestUpstreamCluster: r.TestUpstreamCluster,
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

	if err = os.Setenv("ENVOY_DYNAMIC_MODULES_SEARCH_PATH", searchPath); err != nil {
		return fmt.Errorf("failed to set ENVOY_DYNAMIC_MODULES_SEARCH_PATH: %w", err)
	}

	// Disable cgo pointer checks as Envoy may hold pointers to Go memory.
	if err = os.Setenv("GODEBUG", "cgocheck=0"); err != nil {
		return fmt.Errorf("failed to set GODEBUG: %w", err)
	}

	names := make([]string, 0, len(r.Extensions))
	for _, ext := range r.Extensions {
		names = append(names, ext.Name)
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s✓ Starting Envoy with extensions: %v...%s\n",
		internal.ANSIBold, names, internal.ANSIReset)

	r.Logger.Info("running Envoy with func-e", "envoy_version", r.EnvoyVersion, "extensions", names)

	// ext-proc servers will be started once Envoy starts. func-e assumes that the first child process
	// is the envoy process, and starting the ext_proc servers before may cause issues.
	var extProcCmds []*exec.Cmd
	defer stopExtProcServers(r.Logger, extProcCmds)

	// Define startup hook that will be called when Envoy admin is ready
	start := time.Now()
	startupHook := func(_ context.Context, adminClient admin.AdminClient, _ string) error {
		extProcCmds, err = startExtProcServers(ctx, r.Logger, params.Extensions, r.ExtProcBinaries)
		if err != nil {
			return fmt.Errorf("failed to start ext_proc servers: %w", err)
		}
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
	if err := os.MkdirAll(params.Dirs.RuntimeDir, 0o750); err != nil {
		return "", nil, fmt.Errorf("failed to create runtime directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(params.Dirs.RuntimeDir, "boe-dynamic-modules-*")
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
		case extensions.TypeGo:
			// At this point all extensions are guaranteed to use the same version of
			// composer.
			composerVersion = ext.ComposerVersion

		case extensions.TypeRust:
			// Get the path to the Rust dynamic module library
			libPath := extensions.LocalCacheExtension(params.Dirs, ext)
			if _, err := os.Stat(libPath); os.IsNotExist(err) {
				cleanup()
				return "", nil, fmt.Errorf("library not found at %s for extension %s", libPath, ext.Name)
			}

			// Create hard link in the temporary directory
			linkPath := filepath.Join(tempDir, filepath.Base(libPath))
			if err := os.Symlink(libPath, linkPath); err != nil {
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

			if err := os.Symlink(composerPath, linkPath); err != nil {
				cleanup()
				return "", nil, fmt.Errorf("failed to create hard link for libcomposer: %w", err)
			}
		}
	}

	params.Logger.Debug("setting up dynamic module search path", "path", tempDir)

	return tempDir, func() {}, nil
}

// extProcServerReadyTimeout is how long to wait for an ext_proc server to accept connections.
const extProcServerReadyTimeout = 10 * time.Second

// startExtProcServers starts ext_proc server processes for all ext_proc extensions.
// It waits for each server to be ready before returning.
func startExtProcServers(ctx context.Context, logger *slog.Logger, exts []*extensions.Manifest, binaries map[string]string) ([]*exec.Cmd, error) {
	var cmds []*exec.Cmd

	for _, ext := range exts {
		if ext.Type != extensions.TypeExtProc {
			continue
		}

		binPath, ok := binaries[ext.Name]
		if !ok {
			return nil, fmt.Errorf("ext_proc binary not found for extension %s", ext.Name)
		}

		port := ext.ExtProc.GRPCPort
		if port == 0 {
			port = 50051
		}

		// #nosec G204
		cmd := exec.CommandContext(ctx, binPath, "--port", fmt.Sprintf("%d", port))
		cmd.Stdout = &prefixedWriter{prefix: fmt.Sprintf("[%s] ", ext.Name), w: os.Stderr}
		cmd.Stderr = &prefixedWriter{prefix: fmt.Sprintf("[%s] ", ext.Name), w: os.Stderr}

		logger.Debug("starting ext_proc server", "extension", ext.Name, "port", port, "binary", binPath)

		if err := cmd.Start(); err != nil {
			stopExtProcServers(logger, cmds)
			return nil, fmt.Errorf("failed to start ext_proc server for %s: %w", ext.Name, err)
		}

		cmds = append(cmds, cmd)

		_, _ = fmt.Fprintf(os.Stderr, "→ %sStarting ext_proc server for %s on port %d...%s\n",
			internal.ANSIBold, ext.Name, port, internal.ANSIReset)

		addr := fmt.Sprintf("127.0.0.1:%d", port)
		if err := waitForTCPReady(addr, extProcServerReadyTimeout); err != nil {
			stopExtProcServers(logger, cmds)
			return nil, fmt.Errorf("ext_proc server for %s did not become ready: %w", ext.Name, err)
		}

		logger.Debug("ext_proc server is ready", "extension", ext.Name, "port", port)
	}

	return cmds, nil
}

// stopExtProcServers sends SIGTERM to all ext_proc server processes.
func stopExtProcServers(logger *slog.Logger, cmds []*exec.Cmd) {
	for _, cmd := range cmds {
		if cmd.Process == nil {
			continue
		}
		logger.Debug("stopping ext_proc server", "pid", cmd.Process.Pid)
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
}

// waitForTCPReady polls addr until a TCP connection succeeds or the timeout elapses.
func waitForTCPReady(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s to be ready after %s", addr, timeout)
}

// prefixedWriter writes to w with each write prefixed by prefix.
type prefixedWriter struct {
	prefix string
	w      *os.File
}

func (p *prefixedWriter) Write(b []byte) (int, error) {
	_, _ = fmt.Fprintf(p.w, "%s", p.prefix)
	return p.w.Write(b)
}

const (
	// dockerImage the image to use to run BOE in Docker.
	dockerImage = "boe"
	// ContainerCacheVolumeName is the name of the Docker volume used to persist cache and other data across runs.
	ContainerCacheVolumeName = "boe-cache"
	// containerVolumeDir is the base directory for all volumes in the container.
	containerVolumeDir = "/var/boe"
	// containerConfigHome is the XDG config home inside the container.
	containerConfigHome = containerVolumeDir + "/config"
	// containerDataHome is the XDG data home inside the container.
	containerDataHome = containerVolumeDir + "/data"
	// containerStateHome is the XDG state home inside the container.
	containerStateHome = containerVolumeDir + "/state"
	// containerRuntimeDir is the XDG runtime dir inside the container.
	// This much match the one in the CLI Dockerfile
	containerRuntimeDir = containerVolumeDir + "/run"
	// containerLocalExtensionsDir is the directory inside the container where local extensions are mounted.
	containerLocalExtensionsDir = containerRuntimeDir + "/extensions"
)

// RunnerDocker handles running Envoy as a Docker container.
type RunnerDocker struct {
	Logger          *slog.Logger
	Registry        string
	ListenPort      uint32
	AdminPort       uint32
	Dirs            *xdg.Directories
	Arch            string
	LocalExtensions []string
	Pull            string
}

// Run starts Envoy in a Docker container.
func (r *RunnerDocker) Run(ctx context.Context) error {
	var (
		version = imageVersion(internal.CurrentVersion())
		image   = fmt.Sprintf("%s/%s:%s", r.Registry, dockerImage, version)
	)

	// Process local extensions to mount them in the container and get the corresponding container paths.
	localExtArgs, err := r.processLocalExtensions(r.LocalExtensions)
	if err != nil {
		return fmt.Errorf("failed to process local extensions: %w", err)
	}

	// Create a Docker volume for cache directories to enable caching across runs.
	cmd := exec.CommandContext(ctx, "docker", "volume", "create", "--name", ContainerCacheVolumeName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create Docker volume for cache: %w\nOutput: %s", err, string(output))
	}

	args := []string{
		"run", "--rm",
		"--pull", r.Pull,
		"--platform", "linux/" + r.Arch,
		"-p", fmt.Sprintf("%d:%d", r.ListenPort, r.ListenPort),
		"-p", fmt.Sprintf("%d:%d", r.AdminPort, r.AdminPort),
		"-v", ContainerCacheVolumeName + ":" + containerVolumeDir,
		"-e", "BOE_CONFIG_HOME=" + containerConfigHome,
		"-e", "BOE_DATA_HOME=" + containerDataHome,
		"-e", "BOE_STATE_HOME=" + containerStateHome,
		"-e", "BOE_RUNTIME_DIR=" + containerRuntimeDir,
	}

	args = append(args, localExtArgs...)                  // local extension volumes
	args = append(args, passthroughEnvVars()...)          // passthrough BOE_ env vars
	args = append(args, image, "/boe")                    // container image and command
	args = append(args, r.processCommandArgs(os.Args)...) // command-line args

	fmt.Printf("→ %sRunning Envoy in Docker (%v)...%s\n", internal.ANSIBold, image, internal.ANSIReset)

	cmd = exec.CommandContext(ctx, "docker", args...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Cancel = func() error {
		// Send SIGTERM to let Docker gracefully stop the container
		return cmd.Process.Signal(syscall.SIGTERM)
	}

	return cmd.Run()
}

// imageVersion returns the image version to use for the Docker runner. For dev versions, it returns "latest"
// to pull the most recent image. For release versions, it returns the specific version tag to pull the corresponding image.
func imageVersion(version internal.Version) string {
	if version.CommitsAhead != 0 || version.ClosestTag == "" || version.Sha == "" {
		return "latest"
	}
	return strings.TrimPrefix(version.ClosestTag, "v")
}

// passthroughEnvVars returns environment variables with BOE_ prefix to the Docker run arguments.
func passthroughEnvVars() []string {
	var args []string
	// Pass through BOE_ environment variables to the Docker container,
	// so that users can set registry credentials or other configs via env vars instead of CLI flags.
	// We don't passthrough the XDG variables as we'll mount hte host directories on a fixed location in the container.
	passthroughEnv := slices.DeleteFunc(os.Environ(), func(arg string) bool {
		return strings.HasPrefix(arg, "BOE_CONFIG_HOME=") ||
			strings.HasPrefix(arg, "BOE_DATA_HOME=") ||
			strings.HasPrefix(arg, "BOE_STATE_HOME=") ||
			strings.HasPrefix(arg, "BOE_RUNTIME_DIR=")
	})

	for _, e := range passthroughEnv {
		if strings.HasPrefix(e, "BOE_") {
			args = append(args, "-e", e)
		}
	}

	return args
}

// processLocalExtensions processes local extensions and returns Docker volume arguments and container paths.
func (r *RunnerDocker) processLocalExtensions(localExts []string) ([]string, error) {
	var (
		args     []string
		mappings = make(map[string]string)
	)
	for _, ext := range localExts {
		absPath, containerPath, err := localExtensionContainerPath(ext)
		if err != nil {
			return nil, fmt.Errorf("failed to get container path for local extension %q: %w", ext, err)
		}

		args = append(args, "-v", absPath+":"+containerPath)
	}

	r.Logger.Debug("processed local extensions for Docker", "volumes", args, "mappings", mappings)

	return args, nil
}

// localExtensionContainerPath returns the container path for a given local extension path.
func localExtensionContainerPath(ext string) (string, string, error) {
	absPath, err := filepath.Abs(ext)
	if err != nil {
		return "", "", fmt.Errorf("failed to get absolute path for local extension %q: %w", ext, err)
	}
	return absPath, containerLocalExtensionsDir + "/" + filepath.Base(absPath), nil
}

// processCommandArgs filters out Docker-specific arguments from the original command-line arguments to pass through to the container.
func (r *RunnerDocker) processCommandArgs(args []string) []string {
	var processed []string

	for i := 1; i < len(args); i++ {
		arg := args[i]

		// Skip the --docker and --pull flags as they are only relevant to the host CLI
		// and should not be passed to the container.
		// Need to do prefix match not equality because flags could be in the form of --docker=true or --pull=always.
		if strings.HasPrefix(arg, "--docker") {
			continue
		}
		// Handle --pull=value
		if strings.HasPrefix(arg, "--pull=") {
			continue
		}
		// Handle --pull value
		if arg == "--pull" && i+1 < len(args) {
			i++ // skip next arg (the value for --pull)
			continue
		}

		// Handle --local=value
		if strings.HasPrefix(arg, "--local=") {
			parts := strings.SplitN(arg, "=", 2)
			if _, newVal, err := localExtensionContainerPath(parts[1]); err == nil {
				processed = append(processed, "--local="+newVal)
				continue
			}
		}

		// Handle --local value
		if arg == "--local" && i+1 < len(args) {
			val := args[i+1]
			if _, newVal, err := localExtensionContainerPath(val); err == nil {
				processed = append(processed, "--local", newVal)
				i++ // skip next arg (the original value)
				continue
			}
		}

		processed = append(processed, arg)
	}

	r.Logger.Debug("processed command-line arguments for Docker", "original_args", args, "processed_args", processed)

	return processed
}
