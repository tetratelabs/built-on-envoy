// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

// extensionPositions keeps track of the original position of extensions specified via --extension and --local flags.
type extensionPositions struct {
	local  map[string][]int // maps local extension flag values to their positions
	remote map[string][]int // maps remote extension flag values to their positions
}

// sort takes a list of extension manifests and returns a new list sorted according to the original positions of the extension
// specified via --extension and --local flags.
func (e extensionPositions) sort(manifests []*extensions.Manifest) ([]*extensions.Manifest, error) {
	sorted := make([]*extensions.Manifest, len(manifests))
	for _, m := range manifests {
		if m.Remote {
			pos := e.remote[m.Name][0]
			sorted[pos] = m
			e.remote[m.Name] = e.remote[m.Name][1:]
		} else {
			flagValue, err := filepath.Abs(filepath.Dir(m.Path))
			if err != nil {
				return nil, fmt.Errorf("failed to get absolute path for manifest %s: %w", m.Path, err)
			}
			pos := e.local[flagValue][0]
			sorted[pos] = m
			e.local[flagValue] = e.local[flagValue][1:]
		}
	}
	return sorted, nil
}

// saveExtensionPositions iterates through os.Args to find the positions of --extension and --local flags and
// saves them in GenConfig.extensionPositions.
func saveExtensionPositions(args []string) (extensionPositions, error) {
	var (
		extensionIndex int
		positions      = extensionPositions{
			local:  make(map[string][]int),
			remote: make(map[string][]int),
		}
	)

	for i, arg := range args {
		switch arg {
		case "--local":
			key, err := filepath.Abs(args[i+1])
			if err != nil {
				return positions, fmt.Errorf("failed to get absolute path for local extension flag value %s: %w", args[i+1], err)
			}
			positions.local[key] = append(positions.local[key], extensionIndex)
			extensionIndex++
		case "--extension":
			key := args[i+1]
			positions.remote[key] = append(positions.remote[key], extensionIndex)
			extensionIndex++
		}
	}

	return positions, nil
}
