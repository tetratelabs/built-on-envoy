// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/core/testing"
)

func TestPluginResponseHeaders(t *testing.T) {
	for _, tc := range []string{"embedded", "standalone"} {
		t.Run(tc, func(t *testing.T) {
			proxyPort := internaltesting.BOERun(t, "--local", tc)

			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/status/200", proxyPort))
			require.NoError(t, err)
			require.Equal(t, "example-value", resp.Header.Get("x-example-response-header"))
		})
	}
}
