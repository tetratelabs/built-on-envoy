// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package pkg

import (
	"unsafe"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// UnsafeBufferFromString creates an UnsafeEnvoyBuffer from a Go string without copying the underlying data.
// This is only meant to be used for testing
func UnsafeBufferFromString(s string) shared.UnsafeEnvoyBuffer {
	return shared.UnsafeEnvoyBuffer{
		Ptr: unsafe.StringData(s), // nolint:gosec
		Len: uint64(len(s)),
	}
}
