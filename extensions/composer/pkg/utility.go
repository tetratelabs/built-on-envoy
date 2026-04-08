// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package pkg

import "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

// GetMostSpecificConfig is a helper function to get the most specific config of type T from
// the filter handle.
func GetMostSpecificConfig[T any](handle shared.HttpFilterHandle) T {
	var zero T
	mostSpecificConfig := handle.GetMostSpecificConfig()
	if mostSpecificConfig == nil {
		return zero
	}

	config, ok := mostSpecificConfig.(T)
	if !ok {
		handle.Log(shared.LogLevelDebug, "most specific config is not of expected type: %T", mostSpecificConfig)
		return zero
	}

	return config
}
