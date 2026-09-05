// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection"
)

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.Len(t, factories, 1)
	require.IsType(t, &impl.CustomHttpFilterConfigFactory{}, factories["dynamic-fault-injection"])
}
