// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:generate go run main.go ../../../../website/public/extensions.json

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func main() {
	fmt.Println("Generationg extension catalog index...")
	index, err := json.MarshalIndent(extensions.ManifestsForCatalog(), "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(os.Args[1], index, 0o600); err != nil {
		panic(err)
	}
}
