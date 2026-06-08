// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package builtonenvoy

import _ "embed"

// ExtensionCatalog is an embedded JSON file containing metadata for all built-in extensions.
//
//go:embed website/public/extensions.json
var ExtensionCatalog []byte
