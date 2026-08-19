// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package host

import (
	"testing"

	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	"github.com/stretchr/testify/require"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection"
)

func TestPackageInitialization(t *testing.T) {
	factory := sdk.GetHttpFilterConfigFactory("dynamic-fault-injection")
	require.IsType(t, &impl.CustomHttpFilterConfigFactory{}, factory)
}
