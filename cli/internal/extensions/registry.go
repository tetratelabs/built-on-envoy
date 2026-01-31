// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import "strings"

// DefaultOCIRegistry is the default OCI registry to push extensions to.
const DefaultOCIRegistry = "ghcr.io/tetratelabs/built-on-envoy"

// RepositoryName constructs the repository name for an extension based
// on the registry and name.
func RepositoryName(registry string, name string) string {
	return registry + "/extension-" + name
}

// NameFromRepository extracts the extension name from the repository URL.
func NameFromRepository(repository string) string {
	// repository is like ghcr.io/tetratelabs/built-on-envoy/extension-cors
	parts := strings.Split(repository, "/")
	if len(parts) == 0 {
		return repository
	}
	lastPart := parts[len(parts)-1]
	return strings.TrimPrefix(lastPart, "extension-")
}
