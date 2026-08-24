// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package version exposes the composer version to the plugins that are compiled
// into the composer dynamic module.
package version

// Composer is the version of the composer dynamic module, e.g. "0.12.0-dev".
//
// It is parsed from the embedded composer manifest.yaml by the init function in
// the parent composer package (see ../manifest.go), so it is set before main
// runs and must be treated as read-only afterwards.
var Composer string
