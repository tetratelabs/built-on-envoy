// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package example

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/testing"
)

func TestPluginResponseHeaders(t *testing.T) {
	proxyPort := internaltesting.BOERun(t, "--local", ".")

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/status/200", proxyPort))
	require.NoError(t, err)
	require.Equal(t, "example-value", resp.Header.Get("x-example-response-header"))
}
