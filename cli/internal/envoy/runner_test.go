// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLogLevels(t *testing.T) {
	tests := []struct {
		name               string
		input              string
		wantBaseLevel      string
		wantComponentLevel string
	}{
		{
			name:               "empty string defaults to warning",
			input:              "",
			wantBaseLevel:      "error",
			wantComponentLevel: "",
		},
		{
			name:               "all component only",
			input:              "all:debug",
			wantBaseLevel:      "debug",
			wantComponentLevel: "",
		},
		{
			name:               "single component without all",
			input:              "upstream:debug",
			wantBaseLevel:      "error",
			wantComponentLevel: "upstream:debug",
		},
		{
			name:               "multiple components without all",
			input:              "upstream:debug,connection:trace",
			wantBaseLevel:      "error",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
		{
			name:               "all with other components",
			input:              "all:info,upstream:debug,connection:trace",
			wantBaseLevel:      "info",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
		{
			name:               "all at the end",
			input:              "upstream:debug,all:error",
			wantBaseLevel:      "error",
			wantComponentLevel: "upstream:debug",
		},
		{
			name:               "all in the middle",
			input:              "upstream:debug,all:info,connection:trace",
			wantBaseLevel:      "info",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
		{
			name:               "handles whitespace",
			input:              " upstream:debug , all:info , connection:trace ",
			wantBaseLevel:      "info",
			wantComponentLevel: "upstream:debug,connection:trace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseLevel, componentLevels := parseLogLevels(tt.input)
			require.Equal(t, tt.wantBaseLevel, baseLevel)
			require.Equal(t, tt.wantComponentLevel, componentLevels)
		})
	}
}
