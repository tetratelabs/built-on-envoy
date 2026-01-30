// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

func TestDefaultProxy(t *testing.T) {
	proxyPort, adminPort := internaltesting.RunEnvoy(t, cliBin, nil, "run")

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, fmt.Sprintf("http://localhost:%d/status/200", proxyPort), internaltesting.EqualStatus(200)))
	require.NoError(t, internaltesting.CheckGet(ctx, fmt.Sprintf("http://localhost:%d/server_info", adminPort), internaltesting.EqualStatus(200)))
}

func TestCustomPorts(t *testing.T) {
	_, _ = internaltesting.RunEnvoy(t, cliBin, nil, "run", "--listen-port", "11000", "--admin-port", "12000")

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, "http://localhost:11000/status/200", internaltesting.EqualStatus(200)))
	require.NoError(t, internaltesting.CheckGet(ctx, "http://localhost:12000/server_info", internaltesting.EqualStatus(200)))
}

func TestLuaLocalExtension(t *testing.T) {
	proxyPort, _ := internaltesting.RunEnvoy(t, cliBin, nil,
		"run",
		"--log-level", "lua:info",
		"--local", "testdata/lua",
	)

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	checkHeader := func(r *http.Response) bool {
		return r.Header.Get("x-e2e-lua") == "lua-e2e-test"
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)

	require.NoError(t, internaltesting.CheckGet(ctx, url, checkHeader))
}
