// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	_ "embed"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

// List is a command that lists available extensions.
type List struct {
	output io.Writer `kong:"-"` // Internal field for testing
}

//go:embed list_help.md
var listHelp string

// Help provides detailed help for the list command.
func (l *List) Help() string { return listHelp }

// Run executes the list command
func (l *List) Run() error {
	out := l.output
	if out == nil {
		out = os.Stdout
	}

	// Get all extension names and sort them alphabetically
	names := slices.Collect(maps.Keys(extensions.Manifests))
	sort.Strings(names)

	// Create a tabwriter for nicely formatted table output
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	// Print header
	_, _ = fmt.Fprintln(w, "NAME\tVERSION\tTYPE\tDESCRIPTION")

	// Print each extension
	for _, name := range names {
		m := extensions.Manifests[name]
		version := m.Version
		if version == "" {
			version = "N/A"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			m.Name,
			version,
			m.Type,
			truncateDescription(m.Description, 60),
		)
	}

	return w.Flush()
}

// truncateDescription truncates a description to the specified max length,
// adding "..." if truncated.
func truncateDescription(desc string, maxLen int) string {
	// Replace newlines with spaces for single-line display
	desc = strings.ReplaceAll(desc, "\n", " ")
	desc = strings.TrimSpace(desc)

	if len(desc) <= maxLen {
		return desc
	}
	return desc[:maxLen-3] + "..."
}
