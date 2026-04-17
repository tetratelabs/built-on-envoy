// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

// indexURL is the URL for the extensions index JSON.
const indexURL = "https://builtonenvoy.io/extensions.json"

// List is a command that lists available extensions.
type List struct {
	output   io.Writer `kong:"-"` // Internal field for testing
	indexURL string    `kong:"-"` // Internal field for testing
}

//go:embed list_help.md
var listHelp string

// Help provides detailed help for the list command.
func (l *List) Help() string { return listHelp }

// Run executes the list command
func (l *List) Run(logger *slog.Logger) error {
	logger.Debug("handling list command", "cmd", l)

	url := indexURL
	if l.indexURL != "" {
		url = l.indexURL
	}
	index, err := fetchIndex(url)
	if err != nil {
		return fmt.Errorf("failed to fetch extensions index: %w", err)
	}

	out := l.output
	if out == nil {
		out = os.Stdout
	}

	// Create a tabwriter for nicely formatted table output
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tVERSION\tTYPE\tFILTER_TYPE\tDESCRIPTION")

	for _, m := range index {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			m.Name,
			m.Version,
			m.Type,
			m.FilterType,
			truncateDescription(m.Description, 60),
		)
	}

	return w.Flush()
}

// fetchIndex fetches the extensions index from the given URL.
func fetchIndex(url string) ([]*extensions.ManifestIndexEntry, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()

	var index []*extensions.ManifestIndexEntry
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, err
	}

	slices.SortFunc(index, func(a, b *extensions.ManifestIndexEntry) int {
		return strings.Compare(a.Name, b.Name)
	})

	return index, nil
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
