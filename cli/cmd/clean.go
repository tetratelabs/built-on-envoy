// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// Clean is a command to clean the cache directories.
type Clean struct {
	All            bool `help:"Clean all cache directories." default:"false"`
	ExtensionCache bool `help:"Clean the extension cache directory." default:"false"`
	ConfigCache    bool `help:"Clean the config cache directory." default:"false"`
	DataCache      bool `help:"Clean the data cache directory." default:"false"`
	StateCache     bool `help:"Clean the state cache directory." default:"false"`
	RuntimeCache   bool `help:"Clean the runtime cache directory." default:"false"`
}

//go:embed clean_help.md
var cleanHelp string

// Help provides detailed help for the clean command.
func (c *Clean) Help() string { return cleanHelp }

// Run executes the clean command.
func (c *Clean) Run(dirs *xdg.Directories) error {
	if c.All || c.ExtensionCache {
		fmt.Println("→ Cleaning extension cache...")
		extCacheDir := filepath.Join(dirs.DataHome, "extensions")
		if err := os.RemoveAll(extCacheDir); err != nil {
			return fmt.Errorf("failed to clean extension cache: %w", err)
		}
	}
	if c.All || c.ConfigCache {
		fmt.Println("→ Cleaning config cache...")
		if err := os.RemoveAll(dirs.ConfigHome); err != nil {
			return fmt.Errorf("failed to clean config cache: %w", err)
		}
	}
	if c.All || c.DataCache {
		fmt.Println("→ Cleaning data cache...")
		if err := os.RemoveAll(dirs.DataHome); err != nil {
			return fmt.Errorf("failed to clean data cache: %w", err)
		}
	}
	if c.All || c.StateCache {
		fmt.Println("→ Cleaning state cache...")
		if err := os.RemoveAll(dirs.StateHome); err != nil {
			return fmt.Errorf("failed to clean state cache: %w", err)
		}
	}
	if c.All || c.RuntimeCache {
		fmt.Println("→ Cleaning runtime cache...")
		if err := os.RemoveAll(dirs.RuntimeDir); err != nil {
			return fmt.Errorf("failed to clean runtime cache: %w", err)
		}
	}
	return nil
}
