// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// HTTP framing is analyzed once so comparison and elision share response boundaries.
package main

import (
	"regexp"
	"strings"
)

var (
	rawHeaderPattern    = regexp.MustCompile(`^([^:\r\n]+)([ \t]*:[ \t]*)([^\r\n]*)(\r?)(?:\n)?$`)
	rawStatusPattern    = regexp.MustCompile(`^HTTP/[0-9.]+[ \t]+([0-9]{3})(?:[ \t]|\r?\n?$)`)
	requestStartPattern = regexp.MustCompile(`^> [A-Z]+ [^\r\n]* HTTP/[0-9.]+\r?(?:\n)?$`)
)

type responseHeader struct {
	name       string
	line       int
	valueStart int
	valueEnd   int
}

type responseHeaderSection struct {
	headers   []responseHeader
	separator int
	prefix    string
}

func nextPayloadLine(lines []transcriptLine, index int) int {
	for index < len(lines) && outputMetadataLine(lines[index].text) {
		index++
	}
	return index
}

func responseHeaderStart(block outputBlock, lines []transcriptLine) (int, string, bool) {
	if len(lines) == 0 {
		return 0, "", false
	}
	if !block.raw {
		index := nextPayloadLine(lines, 0)
		if index < len(lines) {
			status, ok := responseLine(lines[index], "# ")
			if ok && rawStatusPattern.MatchString(status) {
				return index, "# ", true
			}
		}
		return 0, "", false
	}
	if rawStatusPattern.MatchString(lines[0].text) {
		return 0, "", true
	}
	if !requestStartPattern.MatchString(lines[0].text) {
		return 0, "", false
	}
	index := 1
	for index < len(lines) {
		line := lines[index].text
		if line == ">\n" || line == "> \n" {
			index++
			break
		}
		if !strings.HasPrefix(line, "> ") {
			return 0, "", false
		}
		match := rawHeaderPattern.FindStringSubmatchIndex(line[2:])
		if match == nil || !validHeaderName(strings.ToLower(line[2+match[2]:2+match[3]])) {
			return 0, "", false
		}
		index++
	}
	if index >= len(lines) || !strings.HasPrefix(lines[index].text, "< ") || !rawStatusPattern.MatchString(lines[index].text[2:]) {
		return 0, "", false
	}
	return index, "< ", true
}

type httpOutput struct {
	lines    []transcriptLine
	statuses []int
	sections []responseHeaderSection
}

func (output httpOutput) intersectsStatus(start, end int) bool {
	for _, index := range output.statuses {
		line := output.lines[index]
		if start < line.offset+len(line.text) && end > line.offset {
			return true
		}
	}
	return false
}

func analyzeHTTPOutput(block outputBlock) httpOutput {
	output := httpOutput{lines: transcriptLines(block.body)}
	index, prefix, found := responseHeaderStart(block, output.lines)
	if !found {
		return output
	}
	for index < len(output.lines) {
		status, ok := responseLine(output.lines[index], prefix)
		if !ok {
			return output
		}
		match := rawStatusPattern.FindStringSubmatch(status)
		if match == nil {
			return output
		}
		output.statuses = append(output.statuses, index)
		section := responseHeaderSection{prefix: prefix}
		index++
		for {
			if index >= len(output.lines) {
				return output
			}
			text, ok := responseLine(output.lines[index], prefix)
			if !ok {
				return output
			}
			if text == "\n" || text == "\r\n" || text == "" {
				section.separator = index
				output.sections = append(output.sections, section)
				index++
				break
			}
			header, ok := parseResponseHeader(output.lines[index], prefix)
			if !ok {
				return output
			}
			header.line = index
			section.headers = append(section.headers, header)
			index++
		}
		if match[1] != "100" && match[1] != "102" && match[1] != "103" {
			return output
		}
	}
	return output
}

func parseResponseHeader(line transcriptLine, prefix string) (responseHeader, bool) {
	text, ok := responseLine(line, prefix)
	if !ok {
		return responseHeader{}, false
	}
	match := rawHeaderPattern.FindStringSubmatchIndex(text)
	if match == nil {
		return responseHeader{}, false
	}
	name := strings.ToLower(text[match[2]:match[3]])
	if !validHeaderName(name) {
		return responseHeader{}, false
	}
	return responseHeader{
		name:       name,
		valueStart: line.offset + len(prefix) + match[6],
		valueEnd:   line.offset + len(prefix) + match[7],
	}, true
}

func responseLine(line transcriptLine, prefix string) (string, bool) {
	if (prefix == "# " && line.text == "#\n") || (prefix == "< " && line.text == "<\n") {
		return "\n", true
	}
	if !strings.HasPrefix(line.text, prefix) {
		return "", false
	}
	return line.text[len(prefix):], true
}
