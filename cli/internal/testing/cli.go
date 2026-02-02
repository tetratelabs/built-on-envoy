// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"bytes"
	"go/doc"
	"strings"
)

// WrapHelp wraps the given help text to 80 characters width with proper indentation.
// It uses the same logic the CLI would use to format help texts.
// This is useful to compare expected help texts in tests.
func WrapHelp(text string) string {
	p := new(doc.Package)
	pr := p.Printer()
	pr.TextCodePrefix = "    "
	pr.TextWidth = 80

	w := bytes.NewBuffer(nil)
	w.Write(pr.Text(p.Parser().Parse(text)))
	godocText := w.String()

	out := bytes.NewBuffer(nil)
	for _, line := range strings.Split(strings.TrimSpace(godocText), "\n") {
		out.WriteString(strings.TrimRight(line, " "))
		out.WriteString("\n")
	}

	return out.String()
}
