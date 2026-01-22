// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminEndpoints(t *testing.T) {
	adminPort := runEnvoy(t, cliBin, nil, "run")

	require.NoError(t, checkEndpoint(t.Context(),
		fmt.Sprintf("http://localhost:%d/server_info", adminPort),
		statusEq(200),
	))
}

func TestDefaultProxy(t *testing.T) {
	_ = runEnvoy(t, cliBin, nil, "run")

	require.NoError(t, checkEndpoint(t.Context(),
		fmt.Sprintf("http://localhost:%d/status/200", proxyPort),
		statusEq(200),
	))

	require.NoError(t, checkEndpoint(t.Context(),
		fmt.Sprintf("http://localhost:%d/status/404", proxyPort),
		statusEq(404),
	))
}
