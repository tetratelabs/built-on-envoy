// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package main

import (
	"testing"

	"github.com/proxy-wasm/proxy-wasm-go-sdk/proxywasm/proxytest"
	"github.com/proxy-wasm/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/stretchr/testify/require"
)

func TestResponseHeader(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		expected string
	}{
		{name: "default", config: "", expected: "example"},
		{name: "custom", config: `{"header_value":"my-value"}`, expected: "my-value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := proxytest.NewEmulatorOption().WithVMContext(&vmContext{})
			if tt.config != "" {
				opt = opt.WithPluginConfiguration([]byte(tt.config))
			}
			host, reset := proxytest.NewHostEmulator(opt)
			defer reset()

			require.Equal(t, types.OnPluginStartStatusOK, host.StartPlugin())

			id := host.InitializeHttpContext()
			require.Equal(t, types.ActionContinue, host.CallOnResponseHeaders(id, nil, false))

			headers := host.GetCurrentResponseHeaders(id)
			var got string
			for _, h := range headers {
				if h[0] == responseHeaderName {
					got = h[1]
				}
			}
			require.Equal(t, tt.expected, got)
		})
	}
}
