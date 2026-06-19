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
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/tetratelabs/built-on-envoy/ui"
)

// UI is a command that starts the Extension Manager web UI.
type UI struct {
	Port     int          `help:"HTTP server port." default:"18000"`
	LogLevel string       `help:"Envoy component log level." default:"all:error" env:"ENVOY_LOG_LEVEL"`
	Envoy    EnvoyFlags   `embed:""`
	Local    []string     `name:"local" sep:"none" help:"Path to a directory containing a local Extension to enable." type:"existingdir"`
	Dev      bool         `help:"Whether to allow downloading dev versions of extensions (with -dev suffix). By default, only stable versions are allowed." default:"false"`
	Clusters ClusterFlags `embed:""`
	Docker   DockerFlags  `embed:""`

	openBrowserFunc func(string) error // overridable for tests; nil means use openBrowser
}

//go:embed ui_help.md
var uiHelp string

// Help provides detailed help for the ui command.
func (u *UI) Help() string { return uiHelp }

// Run starts the Extension Manager web server and opens the browser.
func (u *UI) Run(ctx context.Context, logger *slog.Logger) error {
	logger.Debug("handling ui command", "port", u.Port)

	handler, err := ui.NewServer(logger, &ui.RunParams{
		LogLevel:            u.LogLevel,
		EnvoyVersion:        u.Envoy.Version,
		EnvoyVersionsURL:    u.Envoy.VersionsURL,
		EnvoyPath:           u.Envoy.Path,
		Dev:                 u.Dev,
		LocalExtensions:     u.Local,
		ClustersSecure:      u.Clusters.Secure,
		ClustersInsecure:    u.Clusters.Insecure,
		ClustersJSONSpec:    u.Clusters.JSONSpec,
		TestUpstreamHost:    u.Clusters.TestUpstreamHost,
		TestUpstreamCluster: u.Clusters.TestUpstreamCluster,
		Docker:              u.Docker.Enabled,
		DockerImage:         u.Docker.ImageVersion,
		DockerPullPolicy:    u.Docker.Pull,
	})
	if err != nil {
		return fmt.Errorf("failed to create UI server: %w", err)
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", u.Port),
		ReadHeaderTimeout: 5 * time.Second,
		Handler:           handler,
	}

	url := fmt.Sprintf("http://localhost:%d", u.Port)
	fmt.Printf("Built On Envoy UI running at %s\n", url)

	opener := openBrowser
	if u.openBrowserFunc != nil {
		opener = u.openBrowserFunc
	}
	if err := opener(url); err != nil {
		logger.Debug("failed to open browser", "error", err)
		fmt.Println("Open the URL above in your browser to get started.")
	}

	// Shut down gracefully when the context is cancelled
	go func() {
		<-ctx.Done()
		logger.Info("shutting down web UI server")
		if err := srv.Shutdown(context.Background()); err != nil {
			logger.Error("failed to shut down web UI server", "error", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// browserCommand returns the command to open a URL in the default browser for the given OS.
func browserCommand(os string, url string) ([]string, error) {
	switch os {
	case "darwin":
		return []string{"open", url}, nil
	case "linux":
		return []string{"xdg-open", url}, nil
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", url}, nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", os)
	}
}

// openBrowser opens the given URL in the default browser.
func openBrowser(url string) error {
	cmd, err := browserCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	return exec.Command(cmd[0], cmd[1:]...).Start() // #nosec G204
}
