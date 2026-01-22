// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	internaltesting "github.com/envoy-ecosystem/cli/internal/testing"
	"github.com/stretchr/testify/require"
)

func TestAdminEndpoints(t *testing.T) {
	adminPort := runEnvoy(t, cliBin, nil, "run")

	adminEndpoint := fmt.Sprintf("http://localhost:%d/server_info", adminPort)
	condition := func(r *http.Response) bool { return r.StatusCode == 200 }

	internaltesting.RequireEventuallyNoError(t, func() error {
		return checkEndpointAvailable(t.Context(), adminEndpoint, condition)
	}, 120*time.Second, 2*time.Second, "endpoint %q never became available", adminEndpoint)
}

func TestDefaultProxy(t *testing.T) {
	_ = runEnvoy(t, cliBin, nil, "run")

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/status/200", proxyPort))
	_ = resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	resp, err = http.Get(fmt.Sprintf("http://localhost:%d/status/404", proxyPort))
	_ = resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode)
}
