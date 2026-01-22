// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDefaultConfigIsValid(t *testing.T) {
	require.NotEmpty(t, defaultConfig)

	var config map[string]any
	err := yaml.Unmarshal([]byte(defaultConfig), &config)
	require.NoError(t, err)
}
