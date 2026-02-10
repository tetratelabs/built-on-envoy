// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

//go:embed templates/create/*
var templateFS embed.FS

// Create is a command to create a new extension template.
type Create struct {
	Type string `help:"Type of the extension. Currently only 'composer' is supported." default:"composer" enum:"composer"`
	Name string `arg:"" help:"Name of the extension."`
	Path string `help:"Output directory for the extension. Defaults to the extension name." type:"path"`
}

//go:embed create_help.md
var createHelp string

// Help returns the help message for the create command.
func (c *Create) Help() string { return createHelp }

// Run executes the create command.
func (c *Create) Run(dirs *xdg.Directories) error {
	switch c.Type {
	case "composer":
		return createComposerHTTPFilter(dirs, c.Path, c.Name)
	default:
		return fmt.Errorf("unsupported extension type: %s", c.Type)
	}
}

func createComposerHTTPFilter(dirs *xdg.Directories, path, name string) error {
	repoPath := filepath.Join(path, name)

	data := map[string]string{
		"Name":               name,
		"LibComposerVersion": extensions.LibComposerVersion,
		"DataHome":           dirs.DataHome,
	}

	// Map of output filename to template filename
	files := map[string]string{
		"plugin.go":          "templates/create/plugin.go.tmpl",
		"manifest.yaml":      "templates/create/manifest.yaml.tmpl",
		"Makefile":           "templates/create/Makefile.tmpl",
		"go.mod":             "templates/create/go.mod.tmpl",
		"Dockerfile":         "templates/create/Dockerfile.tmpl",
		"Dockerfile.code":    "templates/create/Dockerfile.code.tmpl",
		".dockerignore":      "templates/create/.dockerignore.tmpl",
		"embedded/host.go":   "templates/create/host.go.tmpl",
		"standalone/main.go": "templates/create/main.go.tmpl",
	}

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

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run 'go mod tidy': %w\n%s", err, string(output))
	}
	return nil
}
