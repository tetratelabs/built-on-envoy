// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package host

import "testing"

// TestInitRegisters exists so this package is compiled and its init() runs,
// registering the network filter with the SDK.
func TestInitRegisters(*testing.T) {}
