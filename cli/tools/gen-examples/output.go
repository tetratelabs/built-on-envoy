// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Captured output has one byte-preserving codec shared by rendering, reordering, and elision.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func renderOutput(value string, actual, display map[string]string, stream string) (string, error) {
	keys := make([]string, 0, len(actual))
	for key := range actual {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value = strings.ReplaceAll(value, actual[key], display[key])
	}
	encoded, err := encodeCapturedOutput(value, stream)
	if err != nil {
		return "", err
	}
	return encoded.markdown(), nil
}

type encodedOutput struct {
	body     string
	metadata string
}

func (output encodedOutput) markdown() string {
	return renderFencedBlock("text", output.body) + "\n" + output.metadata
}

func encodeCapturedOutput(value, stream string) (encodedOutput, error) {
	var out strings.Builder
	lineEndings := make([]string, 0, strings.Count(value, "\n"))
	bareCROffsets := make([]int, 0)
	for index := 0; index < len(value); index++ {
		if value[index] == '\r' {
			if index+1 < len(value) && value[index+1] == '\n' {
				lineEndings = append(lineEndings, "CRLF")
				out.WriteByte('\n')
				index++
			} else {
				bareCROffsets = append(bareCROffsets, index)
				out.WriteString(`\r`)
			}
			continue
		}
		if value[index] == '\n' {
			lineEndings = append(lineEndings, "LF")
		}
		out.WriteByte(value[index])
	}

	normalized := out.String()
	lines := strings.Split(normalized, "\n")
	trailingWhitespace := make([]lineWhitespace, 0)
	for index, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if len(trimmed) != len(line) {
			trailingWhitespace = append(trailingWhitespace, lineWhitespace{Line: index, Text: line[len(trimmed):]})
			lines[index] = trimmed
		}
	}
	normalized = strings.Join(lines, "\n")
	var metadata strings.Builder
	metadata.WriteString("<!-- " + stream)
	if len(lineEndings) > 0 {
		metadata.WriteString("; Line endings: " + strings.Join(lineEndings, ","))
	}
	if len(bareCROffsets) > 0 {
		positions := make([]string, len(bareCROffsets))
		for index, offset := range bareCROffsets {
			positions[index] = strconv.Itoa(offset)
		}
		metadata.WriteString("; Bare CR offsets: " + strings.Join(positions, ","))
	}
	if len(trailingWhitespace) > 0 {
		encoded, err := json.Marshal(trailingWhitespace)
		if err != nil {
			return encodedOutput{}, fmt.Errorf("encode trailing whitespace metadata: %w", err)
		}
		metadata.WriteString("; Trailing whitespace: " + base64.StdEncoding.EncodeToString(encoded))
	}
	if value != "" && !strings.HasSuffix(value, "\n") {
		metadata.WriteString("; No trailing newline")
	}
	metadata.WriteString(" -->\n")
	if !strings.HasSuffix(normalized, "\n") {
		normalized += "\n"
	}
	return encodedOutput{body: normalized, metadata: metadata.String()}, nil
}

type lineWhitespace struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

func renderFencedBlock(language, content string) string {
	fence := markdownFence(content)
	var block strings.Builder
	block.WriteString(fence + language + "\n")
	block.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		block.WriteByte('\n')
	}
	block.WriteString(fence)
	return block.String()
}

func markdownFence(content string) string {
	longest := 0
	current := 0
	for index := 0; index < len(content); index++ {
		if content[index] == '`' {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	if longest < 2 {
		return "```"
	}
	return strings.Repeat("`", longest+1)
}

type capturedOutputMetadata struct {
	stream            string
	endings           []string
	bareCR            []int
	whitespace        []lineWhitespace
	noTrailingNewline bool
}

func isOutputMetadata(line string) bool {
	return (strings.HasPrefix(line, "<!-- stdout") || strings.HasPrefix(line, "<!-- stderr")) && strings.HasSuffix(line, " -->\n")
}

func parseOutputMetadata(line string) (capturedOutputMetadata, bool) {
	var metadata capturedOutputMetadata
	if !isOutputMetadata(line) {
		return metadata, false
	}
	fields := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "<!-- "), " -->\n"), "; ")
	metadata.stream = fields[0]
	if metadata.stream != "stdout" && metadata.stream != "stderr" {
		return metadata, false
	}
	for _, field := range fields[1:] {
		switch {
		case strings.HasPrefix(field, "Line endings: "):
			metadata.endings = strings.Split(strings.TrimPrefix(field, "Line endings: "), ",")
		case strings.HasPrefix(field, "Bare CR offsets: "):
			for _, number := range strings.Split(strings.TrimPrefix(field, "Bare CR offsets: "), ",") {
				offset, err := strconv.Atoi(number)
				if err != nil {
					return metadata, false
				}
				metadata.bareCR = append(metadata.bareCR, offset)
			}
		case strings.HasPrefix(field, "Trailing whitespace: "):
			encoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(field, "Trailing whitespace: "))
			if err != nil {
				return metadata, false
			}
			if err := json.Unmarshal(encoded, &metadata.whitespace); err != nil {
				return metadata, false
			}
		case field == "No trailing newline":
			metadata.noTrailingNewline = true
		default:
			return metadata, false
		}
	}
	return metadata, true
}

func restoreCapturedOutput(body string, metadata *capturedOutputMetadata) (string, bool) {
	lines := transcriptLines(body)
	for _, whitespace := range metadata.whitespace {
		if whitespace.Line < 0 || whitespace.Line >= len(lines) {
			return "", false
		}
		line := &lines[whitespace.Line]
		line.text = strings.TrimSuffix(line.text, "\n") + whitespace.Text + "\n"
	}
	if metadata.noTrailingNewline && len(lines) > 0 {
		last := &lines[len(lines)-1]
		last.text = strings.TrimSuffix(last.text, "\n")
	}
	var restored strings.Builder
	endingIndex := 0
	for _, line := range lines {
		text := line.text
		if strings.HasSuffix(text, "\n") {
			if endingIndex >= len(metadata.endings) {
				return "", false
			}
			switch metadata.endings[endingIndex] {
			case "CRLF":
				text = strings.TrimSuffix(text, "\n") + "\r\n"
			case "LF":
			default:
				return "", false
			}
			endingIndex++
		}
		restored.WriteString(text)
	}
	if endingIndex != len(metadata.endings) {
		return "", false
	}
	return restoreBareCR(restored.String(), metadata.bareCR)
}

func restoreBareCR(escaped string, offsets []int) (string, bool) {
	var restored strings.Builder
	position := 0
	for index, offset := range offsets {
		if offset < 0 || offset > len(escaped) || index > len(escaped)-offset {
			return "", false
		}
		start := offset + index
		if start < position || start+2 > len(escaped) || escaped[start:start+2] != `\r` {
			return "", false
		}
		restored.WriteString(escaped[position:start])
		restored.WriteByte('\r')
		position = start + 2
	}
	restored.WriteString(escaped[position:])
	return restored.String(), true
}

type validatedCapture struct {
	raw    string
	stream string
}

func decodeCapturedOutput(block outputBlock) (validatedCapture, bool) {
	metadata, valid := parseOutputMetadata(block.metadata)
	if !valid {
		return validatedCapture{}, false
	}
	raw, valid := restoreCapturedOutput(block.body, &metadata)
	if !valid {
		return validatedCapture{}, false
	}
	encoded, err := encodeCapturedOutput(raw, metadata.stream)
	// Round-trip validation rejects malformed metadata and noncanonical encodings.
	if err != nil || encoded.body != block.body || encoded.metadata != block.metadata {
		return validatedCapture{}, false
	}
	return validatedCapture{raw: raw, stream: metadata.stream}, true
}
