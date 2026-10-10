// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Embed the repository toolchain here so the CLI can scaffold it without a checkout.
package builtonenvoy

import _ "embed"

// RustToolchain contains the shared Rust toolchain configuration.
//
//go:embed rust-toolchain.toml
var RustToolchain []byte
