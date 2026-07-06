// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"encoding/json"
	"fmt"
)

type config struct {
	Command       string   `json:"command"`
	Args          []string `json:"args"`
	ServeFrontend bool     `json:"serve_frontend"` // default true
}

func parseConfig(raw []byte) (*config, error) {
	// ServeFrontend defaults true, an explicit "serve_frontend": false overrides.
	c := &config{Command: "/bin/bash", ServeFrontend: true}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, c); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
	}
	if c.Command == "" {
		return nil, fmt.Errorf("invalid config: command must not be empty")
	}
	return c, nil
}
