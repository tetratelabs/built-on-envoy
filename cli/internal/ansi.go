// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package internal provides internal utilities for the CLI.
package internal

// ANSI escape codes for formatting terminal output.
const (
	// ANSIBold is the ANSI escape code for bold text.
	ANSIBold = "\033[1m"
	// ANSIReset is the ANSI escape code to reset text formatting.
	ANSIReset = "\033[0m"
)
