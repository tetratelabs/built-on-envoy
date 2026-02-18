// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:generate go run main.go ../../../website/public/extensions.json

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func main() {
	fmt.Println("Generationg extension catalog index...")

	manifests := extensions.ManifestsForCatalog()
	slices.SortFunc(manifests, func(a, b *extensions.Manifest) int {
		return strings.Compare(a.Name, b.Name)
	})

	index, err := json.MarshalIndent(manifests, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(os.Args[1], index, 0o600); err != nil {
		panic(err)
	}
}
