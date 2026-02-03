// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:generate go run github.com/tetratelabs/built-on-envoy/cli/tools/gen-cli-docs -output=website/src/pages/docs/cli -docs-dir=website/src/pages/docs

// gen-cli-docs generates MDX documentation files for CLI commands.
// It uses Kong's internal data model to extract command metadata.
package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"text/template"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/built-on-envoy/cli/cmd"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

const cliName = "boe"

// Flag represents a CLI flag/option for documentation.
type Flag struct {
	Name        string
	Type        string
	Help        string
	Default     string
	EnvVars     []string
	Required    bool
	Short       string
	Placeholder string
}

// Arg represents a positional argument for documentation.
type Arg struct {
	Name     string
	Type     string
	Help     string
	Required bool
}

// Command represents a CLI command for documentation.
type Command struct {
	CLIName     string
	Name        string
	Description string
	Detail      string
	Flags       []Flag
	Args        []Arg
}

// SidebarItem represents an item in the sidebar.
type SidebarItem struct {
	Title string
	Href  string
}

// EnvVar represents an environment variable for documentation.
type EnvVar struct {
	Name    string
	Flag    string
	Help    string
	Default string
}

func main() {
	var docsDir string

	flag.StringVar(&docsDir, "docs-dir", "", "Output directory for the generated files (required)")
	flag.Parse()

	if docsDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: gen-cli-docs -docs-dir <dir>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	var (
		commandsDir = filepath.Join(docsDir, "cli")
		envVarsFile = filepath.Join(docsDir, "reference", "environment-variables.mdx")
		sidebarFile = filepath.Join(docsDir, "sidebar-cli.yaml")
	)

	// Ensure output directories exist
	if err := os.MkdirAll(commandsDir, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create commands directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(envVarsFile), 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create reference directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(sidebarFile), 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create sidebar directory: %v\n", err)
		os.Exit(1)
	}

	// Load templates
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"cliName": func() string { return cliName },
		"join":    strings.Join,
	}).ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse templates: %v\n", err)
		os.Exit(1)
	}

	// Parse CLI model
	commands, envVars, err := parseCommands()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse CLI model: %v\n", err)
		os.Exit(1)
	}

	// Sort lists by name
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})
	sort.Slice(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})

	// Generate MDX file for each command
	var sidebarItems []SidebarItem
	for _, cmd := range commands {
		outputPath := filepath.Join(commandsDir, cmd.Name+".mdx")
		if err := render(tmpl, "command.mdx.tmpl", cmd, outputPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to generate MDX for %s: %v\n", cmd.Name, err)
			os.Exit(1)
		}
		fmt.Printf("Generated: %s\n", outputPath)

		sidebarItems = append(sidebarItems, SidebarItem{
			Title: cmd.Name,
			Href:  "/docs/cli/" + cmd.Name,
		})
	}

	// Generate sidebar file
	if err := render(tmpl, "sidebar-cli.yaml.tmpl", sidebarItems, sidebarFile); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate sidebar: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated: %s\n", sidebarFile)

	// Generate environment variables reference file
	if err := render(tmpl, "environment.mdx.tmpl", envVars, envVarsFile); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate environment variables: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated: %s\n", envVarsFile)
}

// parseCommands creates a Kong parser and extracts command and environment variable information from its model.
func parseCommands() ([]Command, []EnvVar, error) {
	// Create a Kong parser without actually parsing any arguments
	k, err := kong.New(&cmd.CLI{},
		kong.Name("boe"),
		kong.Description("Built On Envoy CLI"),
		kong.Exit(func(int) {}), // Don't exit on help
		cmd.Vars,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Kong parser: %w", err)
	}

	var commands []Command
	envVars := make(map[string]EnvVar) // Use map to deduplicate

	// Extract environment variables from global flags
	for _, f := range k.Model.Flags {
		if f.Hidden || f.Name == "help" {
			continue
		}
		for _, env := range f.Envs {
			envVars[env] = EnvVar{
				Name:    env,
				Flag:    f.Name,
				Help:    f.Help,
				Default: f.Default,
			}
		}
	}

	// Iterate through the children of the root node (these are the commands)
	for _, node := range k.Model.Children {
		if node.Type != kong.CommandNode {
			continue
		}

		cmd := Command{
			Name:        node.Name,
			Description: node.Help,
			Detail:      node.Detail,
		}

		// Extract flags
		for _, f := range node.Flags {
			// Skip hidden flags and the help flag
			if f.Hidden || f.Name == "help" {
				continue
			}

			flag := Flag{
				Name:        f.Name,
				Type:        getTypeName(f.Target),
				Help:        f.Help,
				Default:     f.Default,
				EnvVars:     f.Envs,
				Required:    f.Required,
				Placeholder: f.PlaceHolder,
			}

			if f.Short != 0 {
				flag.Short = string(f.Short)
			}

			cmd.Flags = append(cmd.Flags, flag)

			// Extract environment variables from command flags
			for _, env := range f.Envs {
				// Only add if not already present (global flags take precedence)
				if _, exists := envVars[env]; !exists {
					envVars[env] = EnvVar{
						Name:    env,
						Flag:    f.Name,
						Help:    f.Help,
						Default: f.Default,
					}
				}
			}
		}

		// Extract positional arguments
		for _, p := range node.Positional {
			arg := Arg{
				Name:     p.Name,
				Type:     getTypeName(p.Target),
				Help:     p.Help,
				Required: p.Required,
			}
			cmd.Args = append(cmd.Args, arg)
		}

		commands = append(commands, cmd)
	}

	// Convert env vars map to sorted slice
	envVarsList := make([]EnvVar, 0, len(envVars))
	for _, ev := range envVars {
		envVarsList = append(envVarsList, ev)
	}
	sort.Slice(envVarsList, func(i, j int) bool {
		return envVarsList[i].Name < envVarsList[j].Name
	})

	return commands, envVarsList, nil
}

// getTypeName returns a human-readable type name from a reflect.Value.
func getTypeName(v reflect.Value) string {
	if !v.IsValid() {
		return "unknown"
	}

	t := v.Type()

	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Slice:
		elemType := t.Elem()
		if elemType.Kind() == reflect.Ptr {
			elemType = elemType.Elem()
		}
		return "[]" + elemType.Name()
	case reflect.Map:
		return fmt.Sprintf("map[%s]%s", t.Key().Name(), t.Elem().Name())
	default:
		return t.Name()
	}
}

// render the template to the specified output path.
func render(tmpl *template.Template, name string, data any, outputPath string) error {
	f, err := os.Create(filepath.Clean(outputPath))
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = f.Close() }()
	return tmpl.ExecuteTemplate(f, name, data)
}
