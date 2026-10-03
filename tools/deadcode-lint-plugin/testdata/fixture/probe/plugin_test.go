// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This test reference must not make a production-dead function live.
package probe

import "testing"

func TestOnly(t *testing.T) { ExportedTestOnly() }
