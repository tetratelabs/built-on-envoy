// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseConfig(t *testing.T) {
	c, err := parseConfig(nil)
	require.NoError(t, err)
	require.Equal(t, "/bin/bash", c.Command)

	c, err = parseConfig([]byte(`{"command":"sh","args":["-c","x"]}`))
	require.NoError(t, err)
	require.Equal(t, "sh", c.Command)
	require.Equal(t, []string{"-c", "x"}, c.Args)
	require.True(t, c.ServeFrontend) // defaults on

	c, err = parseConfig([]byte(`{"serve_frontend":false}`))
	require.NoError(t, err)
	require.False(t, c.ServeFrontend)

	_, err = parseConfig([]byte(`{`))
	require.Error(t, err)
	_, err = parseConfig([]byte(`{"command":""}`))
	require.Error(t, err)
}
