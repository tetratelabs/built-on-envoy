// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"embed"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"text/template"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

//go:embed templates/create/*
var templateFS embed.FS

// Create is a command to create a new extension template.
type Create struct {
	Type       string `help:"Type of the extension (go, rust)." default:"go" enum:"go,rust"`
	FilterType string `help:"Filter type (http, network). Network filters are only supported for rust." default:"http" enum:"http,network"`
	Name       string `arg:"" help:"Name of the extension."`
	Path       string `help:"Output directory for the extension. Defaults to the extension name." type:"path"`
}

//go:embed create_help.md
var createHelp string

// Help returns the help message for the create command.
func (c *Create) Help() string { return createHelp }

// Validate is called by Kong after parsing to validate the command arguments.
func (c *Create) Validate() error {
	if c.Type == "go" && c.FilterType == "network" {
		return fmt.Errorf("network filter scaffolding is not supported for Go extensions; use --type rust")
	}
	return nil
}

// Run executes the create command.
func (c *Create) Run(dirs *xdg.Directories, logger *slog.Logger) error {
	logger.Debug("handling create command", "cmd", c)

	switch c.Type {
	case "go":
		return createGoExtension(logger, dirs, c.Path, c.Name)
	case "rust":
		return createRustExtension(logger, c.Path, c.Name, c.FilterType)
	default:
		return fmt.Errorf("unsupported extension type: %s", c.Type)
	}
}

func createGoExtension(logger *slog.Logger, dirs *xdg.Directories, path, name string) error {
	repoPath := filepath.Join(path, name)

	data := map[string]string{
		"Name":               name,
		"LibComposerVersion": extensions.LibComposerVersion,
		"DataHome":           dirs.DataHome,
	}

	// Map of output filename to template filename
	files := map[string]string{
		"plugin.go":          "templates/create/plugin.go.tmpl",
		"plugin_test.go":     "templates/create/plugin_test.go.tmpl",
		"manifest.yaml":      "templates/create/manifest.yaml.tmpl",
		"Makefile":           "templates/create/Makefile.tmpl",
		"go.mod":             "templates/create/go.mod.tmpl",
		"Dockerfile":         "templates/create/Dockerfile.tmpl",
		"Dockerfile.code":    "templates/create/Dockerfile.code.tmpl",
		".dockerignore":      "templates/create/.dockerignore.tmpl",
		"embedded/host.go":   "templates/create/host.go.tmpl",
		"standalone/main.go": "templates/create/main.go.tmpl",
	}

	logger.Info("creating Go extension", "name", name, "path", repoPath, "files", slices.Collect(maps.Keys(files)))

	createFilesErr := createFilesFromTemplate(files, data, repoPath)
	if createFilesErr != nil {
		return createFilesErr
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = repoPath

	logger.Info("running 'go mod tidy' to initialize the module dependencies")

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run 'go mod tidy': %w\n%s", err, string(output))
	}
	return nil
}

func createFilesFromTemplate(files map[string]string, data map[string]string, repoPath string) error {
	for outputName, tmplPath := range files {
		outputPath := filepath.Join(repoPath, outputName)

		// Read template from embedded filesystem
		tmplContent, err := templateFS.ReadFile(tmplPath)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", tmplPath, err)
		}
		fileDir := filepath.Dir(outputPath)
		if err = os.MkdirAll(fileDir, 0o750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", fileDir, err)
		}

		// #nosec G304
		f, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", outputPath, err)
		}
		defer func() {
			err = f.Close()
			if err != nil {
				fmt.Printf("Warning: failed to close file %s: %v\n", outputPath, err)
			}
		}()

		t, err := template.New(outputName).Parse(string(tmplContent))
		if err != nil {
			return fmt.Errorf("failed to parse template for %s: %w", outputName, err)
		}

		if err := t.Execute(f, data); err != nil {
			return fmt.Errorf("failed to execute template for %s: %w", outputName, err)
		}
		fmt.Printf("Created %s\n", outputPath)
	}
	return nil
}

func createRustExtension(logger *slog.Logger, path, name, filterType string) error {
	repoPath := filepath.Join(path, name)

	data := map[string]string{
		"Name": name,
		// Convert name to lib_name (replace hyphens with underscores for Rust crate name)
		"LibName": extensions.RustLibNameFromName(name),
	}

	libTemplate := "templates/create/rust/lib_http.rs.tmpl"
	manifestTemplate := "templates/create/rust/manifest_http.yaml.tmpl"
	if filterType == "network" {
		libTemplate = "templates/create/rust/lib_network.rs.tmpl"
		manifestTemplate = "templates/create/rust/manifest_network.yaml.tmpl"
	}

	// Map of output filename to template filename
	files := map[string]string{
		"src/lib.rs":         libTemplate,
		"Cargo.toml":         "templates/create/rust/Cargo.toml.tmpl",
		"manifest.yaml":      manifestTemplate,
		".gitignore":         "templates/create/rust/gitignore.tmpl",
		".dockerignore":      "templates/create/rust/dockerignore.tmpl",
		".cargo/config.toml": "templates/create/rust/cargo-config.toml.tmpl",
		"Dockerfile":         "templates/create/rust/Dockerfile.tmpl",
		"Dockerfile.code":    "templates/create/rust/Dockerfile.code.tmpl",
		"Makefile":           "templates/create/rust/Makefile.tmpl",
	}

	logger.Info("creating Rust dynamic module extension",
		"name", name, "path", repoPath, "filterType", filterType, "files", slices.Collect(maps.Keys(files)))

	return createFilesFromTemplate(files, data, repoPath)
}
