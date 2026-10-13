// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This test verifies that importing the embedded adapter publishes the filter's factories to the SDK.
package embedded

import (
	"testing"

	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	"github.com/stretchr/testify/require"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/saml"
)

func TestRegistration(t *testing.T) {
	factories := impl.WellKnownHttpFilterConfigFactories()
	require.NotEmpty(t, factories)
	for name, expected := range factories {
		t.Run(name, func(t *testing.T) {
			registered := sdk.GetHttpFilterConfigFactory(name)
			require.NotNil(t, registered)
			require.IsType(t, expected, registered)
		})
	}
}
