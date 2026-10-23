// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Comparison keeps HTTP field order independent without changing recorded curl output or byte metadata.
package main

import (
	"sort"
	"strings"
)

func canonicalHeaderOrder(block outputBlock, analysis httpOutput, captures []capture) (outputBlock, []capture) {
	lines := analysis.lines
	order := make([]int, len(lines))
	for index := range order {
		order[index] = index
	}
	sections := analysis.sections
	for _, section := range sections {
		ordered := append([]responseHeader(nil), section.headers...)
		sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].name < ordered[right].name })
		for index, header := range ordered {
			order[section.headers[index].line] = header.line
		}
	}
	var body strings.Builder
	offsets := make([]int, len(lines))
	changed := false
	for index, original := range order {
		changed = changed || index != original
		offsets[original] = body.Len()
		body.WriteString(lines[original].text)
	}
	if !changed {
		return block, captures
	}
	remapped, valid := remapHeaderCaptures(captures, sections, lines, offsets)
	if !valid {
		return block, captures
	}
	metadata, valid := reorderOutputMetadata(block, analysis, order, body.String())
	if !valid {
		return block, captures
	}
	block.body, block.metadata = body.String(), metadata
	return block, remapped
}

func remapHeaderCaptures(captures []capture, sections []responseHeaderSection, lines []transcriptLine, offsets []int) ([]capture, bool) {
	remapped := append([]capture(nil), captures...)
	for index, value := range remapped {
		for _, section := range sections {
			for _, header := range section.headers {
				line := lines[header.line]
				end := line.offset + len(line.text)
				if value.end <= line.offset || value.start >= end {
					continue
				}
				// Cross-line comparison captures can encode order-sensitive context.
				if value.start < line.offset || value.end > end {
					return nil, false
				}
				delta := offsets[header.line] - line.offset
				remapped[index].start += delta
				remapped[index].end += delta
			}
		}
	}
	return remapped, true
}

func reorderOutputMetadata(block outputBlock, analysis httpOutput, order []int, body string) (string, bool) {
	if block.metadata == "" {
		return "", true
	}
	captured, valid := decodeCapturedOutput(block)
	if !valid {
		return "", false
	}
	lines := transcriptLines(captured.raw)
	if len(lines) != len(order) {
		return "", false
	}
	for _, section := range analysis.sections {
		for _, header := range section.headers {
			if strings.ContainsRune(strings.TrimSuffix(lines[header.line].text, "\r\n"), '\r') {
				return "", false
			}
		}
	}
	var reordered strings.Builder
	for _, original := range order {
		reordered.WriteString(lines[original].text)
	}
	encoded, err := encodeCapturedOutput(reordered.String(), captured.stream)
	if err != nil {
		return "", false
	}
	return encoded.metadata, encoded.body == body
}
