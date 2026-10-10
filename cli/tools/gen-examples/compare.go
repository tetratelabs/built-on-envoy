// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// gen-examples compares rendered examples while retaining deliberate output
// formatting changes that make regenerated examples reviewable.
package main

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

const (
	dateRuleType     = "date"
	durationRuleType = "duration"
)

var (
	rawHeaderPattern    = regexp.MustCompile(`^([^:\r\n]+)([ \t]*:[ \t]*)([^\r\n]*)(\r?)(?:\n)?$`)
	rawStatusPattern    = regexp.MustCompile(`^HTTP/[0-9.]+[ \t]+([0-9]{3})(?:[ \t]|\r?$)`)
	requestStartPattern = regexp.MustCompile(`^> [A-Z]+ [^\r\n]* HTTP/[0-9.]+\r?(?:\n)?$`)
	rfc3339Pattern      = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(T)(\d{2}):(\d{2}):(\d{2})(\.(\d+))?(Z|[+-]\d{2}:\d{2})$`)
	rfc1123Pattern      = regexp.MustCompile(`^[A-Za-z]{3}, \d{2} [A-Za-z]{3} \d{4} \d{2}:\d{2}:\d{2} ([A-Za-z]{3})$`)
	rfc850Pattern       = regexp.MustCompile(`^[A-Za-z]+, \d{2}-[A-Za-z]{3}-\d{2} \d{2}:\d{2}:\d{2} ([A-Za-z]{3})$`)
	asctimePattern      = regexp.MustCompile(`^[A-Za-z]{3} [A-Za-z]{3} ( {1,2})(\d{1,2}) \d{2}:\d{2}:\d{2} \d{4}$`)
	durationPart        = regexp.MustCompile(`(?:\d+(?:\.\d+)?|\.\d+)([ \t]*)(ns|us|µs|ms|s|m|h)`)
)

type comparisonRule struct {
	typeName string
	pattern  *regexp.Regexp
	value    int
}

type transcriptPart struct {
	literal string
	block   *outputBlock
}

type outputBlock struct {
	kind string
	body string
	raw  bool
}

type bodySegment struct {
	literal string
	value   *normalizedValue
}

type normalizedValue struct {
	kind     string
	date     string
	duration durationFormat
}

type durationFormat struct {
	sign  string
	parts []durationPartFormat
}

type durationPartFormat struct {
	space         string
	unit          string
	fraction      int
	hasPeriod     bool
	leadingPeriod bool
	leadingZeros  int
}

type capture struct {
	start int
	end   int
	value normalizedValue
}

// equivalentTranscript compares unmodified rendering outside output blocks.
func equivalentTranscript(old, current string, rules []extensions.ExampleComparisonRule, headers []string) (bool, error) {
	comparisonRules, err := compileComparisonRules(rules)
	if err != nil {
		return false, err
	}

	headerRules, err := compileHeaderRules(headers)
	if err != nil {
		return false, err
	}

	oldParts := splitTranscript(old)
	newParts := splitTranscript(current)
	if len(oldParts) != len(newParts) {
		return false, nil
	}

	for index := range oldParts {
		oldPart, newPart := oldParts[index], newParts[index]
		if oldPart.block == nil || newPart.block == nil {
			if oldPart.block != nil || newPart.block != nil || oldPart.literal != newPart.literal {
				return false, nil
			}
			continue
		}
		if oldPart.block.kind != newPart.block.kind || oldPart.block.raw != newPart.block.raw || !equivalentOutputBlock(*oldPart.block, *newPart.block, comparisonRules, headerRules) {
			return false, nil
		}
	}

	return true, nil
}

func compileComparisonRules(rules []extensions.ExampleComparisonRule) ([]comparisonRule, error) {
	compiled := make([]comparisonRule, 0, len(rules))
	for index, rule := range rules {
		if err := validateVolatileType(rule.Type); err != nil {
			return nil, fmt.Errorf("example comparison rule %d: %w", index, err)
		}
		pattern, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("example comparison rule %d pattern: %w", index, err)
		}
		value := -1
		for captureIndex, name := range pattern.SubexpNames() {
			if name != "value" {
				continue
			}
			if value != -1 {
				return nil, fmt.Errorf("example comparison rule %d pattern must contain exactly one named value capture", index)
			}
			value = captureIndex
		}
		if value == -1 {
			return nil, fmt.Errorf("example comparison rule %d pattern must contain exactly one named value capture", index)
		}
		compiled = append(compiled, comparisonRule{typeName: rule.Type, pattern: pattern, value: value})
	}
	return compiled, nil
}

func compileHeaderRules(headers []string) (map[string]struct{}, error) {
	compiled := make(map[string]struct{}, len(headers)+1)
	compiled["date"] = struct{}{}
	for index, header := range headers {
		name := strings.ToLower(header)
		if !validHeaderName(name) {
			return nil, fmt.Errorf("volatile header %d has invalid HTTP field name %q", index, header)
		}
		if _, exists := compiled[name]; exists {
			continue
		}
		compiled[name] = struct{}{}
	}
	return compiled, nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		switch character {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		default:
			return false
		}
	}
	return true
}

func validateVolatileType(typeName string) error {
	switch typeName {
	case dateRuleType, durationRuleType:
		return nil
	default:
		return fmt.Errorf("unsupported volatile type %q", typeName)
	}
}

func splitTranscript(transcript string) []transcriptPart {
	lines := strings.SplitAfter(transcript, "\n")
	parts := make([]transcriptPart, 0)
	var literal strings.Builder
	for index := 0; index < len(lines); {
		if fence, info, found := fenceOpening(lines[index]); found {
			end := index + 1
			for end < len(lines) && !fenceClosing(lines[end], fence) {
				end++
			}
			if end == len(lines) {
				literal.WriteString(lines[index])
				index++
				continue
			}
			if info != "text" {
				literal.WriteString(strings.Join(lines[index:end+1], ""))
				index = end + 1
				continue
			}
			literal.WriteString(lines[index])
			if literal.Len() != 0 {
				parts = append(parts, transcriptPart{literal: literal.String()})
				literal.Reset()
			}
			parts = append(parts, transcriptPart{block: &outputBlock{kind: "text", body: strings.Join(lines[index+1:end], ""), raw: true}})
			literal.WriteString(lines[end])
			index = end + 1
			continue
		}
		kind, endMarker, isOutput := outputBlockStart(lines[index])
		if !isOutput {
			literal.WriteString(lines[index])
			index++
			continue
		}

		end := index + 1
		for end < len(lines) && lines[end] != endMarker {
			end++
		}
		if end == len(lines) {
			literal.WriteString(lines[index])
			index++
			continue
		}

		if literal.Len() != 0 {
			parts = append(parts, transcriptPart{literal: literal.String()})
			literal.Reset()
		}
		parts = append(parts, transcriptPart{block: &outputBlock{kind: kind, body: strings.Join(lines[index+1:end], "")}})
		index = end + 1
	}
	if literal.Len() != 0 {
		parts = append(parts, transcriptPart{literal: literal.String()})
	}
	return parts
}

func fenceOpening(line string) (int, string, bool) {
	count := 0
	for count < len(line) && line[count] == '`' {
		count++
	}
	if count < 3 {
		return 0, "", false
	}
	info := strings.TrimSpace(strings.TrimSuffix(line[count:], "\n"))
	if strings.ContainsRune(info, '`') {
		return 0, "", false
	}
	return count, info, true
}

func fenceClosing(line string, minimum int) bool {
	count := 0
	for count < len(line) && line[count] == '`' {
		count++
	}
	if count < minimum {
		return false
	}
	return strings.TrimSpace(strings.TrimSuffix(line[count:], "\n")) == ""
}

func outputBlockStart(line string) (kind, endMarker string, found bool) {
	switch line {
	case "# Output:\n":
		return "output", "# End output\n", true
	case "# Stderr:\n":
		return "stderr", "# End output\n", true
	default:
		return "", "", false
	}
}

func equivalentOutputBlock(old, current outputBlock, rules []comparisonRule, headers map[string]struct{}) bool {
	oldSegments := normalizeOutputBlock(old, rules, headers)
	newSegments := normalizeOutputBlock(current, rules, headers)
	if len(oldSegments) != len(newSegments) {
		return false
	}
	for index := range oldSegments {
		oldSegment, newSegment := oldSegments[index], newSegments[index]
		if oldSegment.value == nil || newSegment.value == nil {
			if oldSegment.value != nil || newSegment.value != nil || oldSegment.literal != newSegment.literal {
				return false
			}
			continue
		}
		if !equivalentNormalizedValue(*oldSegment.value, *newSegment.value) {
			return false
		}
	}
	return true
}

func normalizeOutputBlock(block outputBlock, rules []comparisonRule, headers map[string]struct{}) []bodySegment {
	body := block.body
	captures := headerCaptures(block, headers)
	raw, positions := rawOutput(block)
	for _, rule := range rules {
		for _, match := range rule.pattern.FindAllStringSubmatchIndex(raw, -1) {
			start, end := match[2*rule.value], match[2*rule.value+1]
			if start == -1 {
				continue
			}
			bodyStart, bodyEnd := positions[start], positions[end]
			if body[bodyStart:bodyEnd] != raw[start:end] {
				continue
			}
			value, ok := normalizeValue(rule.typeName, raw[start:end])
			if ok {
				captures = append(captures, capture{start: bodyStart, end: bodyEnd, value: value})
			}
		}
	}

	captures = nonOverlappingCaptures(captures)
	segments := make([]bodySegment, 0, len(captures)*2+1)
	start := 0
	for _, capture := range captures {
		if capture.start > start {
			segments = append(segments, bodySegment{literal: body[start:capture.start]})
		}
		value := capture.value
		segments = append(segments, bodySegment{value: &value})
		start = capture.end
	}
	if start < len(body) || len(segments) == 0 {
		segments = append(segments, bodySegment{literal: body[start:]})
	}
	return segments
}

func rawOutput(block outputBlock) (string, []int) {
	body := block.body
	if block.raw {
		positions := make([]int, len(body)+1)
		for index := range positions {
			positions[index] = index
		}
		return body, positions
	}
	lines := make([]transcriptLine, 0)
	for _, line := range transcriptLines(body) {
		if !outputMetadataLine(line.text) && (strings.HasPrefix(line.text, "# ") || line.text == "#\n") {
			lines = append(lines, line)
		}
	}
	output := strings.Builder{}
	positions := make([]int, 0, len(body)+1)
	for lineIndex, line := range lines {
		payloadStart := rawPayloadStart(line)
		if lineIndex == 0 {
			positions = append(positions, payloadStart)
		}
		payloadEnd := line.offset + len(line.text)
		if strings.HasSuffix(line.text, "\n") {
			payloadEnd--
		}
		for index := payloadStart; index < payloadEnd; index++ {
			output.WriteByte(body[index])
			positions = append(positions, index+1)
		}
		if lineIndex+1 < len(lines) {
			output.WriteByte('\n')
			positions = append(positions, rawPayloadStart(lines[lineIndex+1]))
		}
	}
	return output.String(), positions
}

func rawPayloadStart(line transcriptLine) int {
	if line.text == "#\n" {
		return line.offset + 1
	}
	return line.offset + 2
}

func outputMetadataLine(line string) bool {
	if line == "# No trailing newline\n" {
		return true
	}
	if !strings.HasPrefix(line, "# Line endings: ") {
		return false
	}
	endings := strings.TrimSuffix(strings.TrimPrefix(line, "# Line endings: "), "\n")
	if endings == "" {
		return false
	}
	for _, ending := range strings.Split(endings, ",") {
		if ending != "LF" && ending != "CRLF" {
			return false
		}
	}
	return true
}

func headerCaptures(block outputBlock, headers map[string]struct{}) []capture {
	if block.raw {
		return rawHeaderCaptures(block.body, headers)
	}
	return legacyHeaderCaptures(block.body, headers)
}

func legacyHeaderCaptures(body string, headers map[string]struct{}) []capture {
	lines := transcriptLines(body)
	index := nextPayloadLine(lines, 0)
	if index >= len(lines) {
		return nil
	}
	status, ok := responseLine(lines[index], "# ")
	if !ok || !rawStatusPattern.MatchString(status) {
		return nil
	}
	return responseHeaderCaptures(lines, index, "# ", headers)
}

func nextPayloadLine(lines []transcriptLine, index int) int {
	for index < len(lines) && outputMetadataLine(lines[index].text) {
		index++
	}
	return index
}

func rawHeaderCaptures(body string, headers map[string]struct{}) []capture {
	lines := transcriptLines(body)
	if len(lines) == 0 {
		return nil
	}
	if rawStatusPattern.MatchString(lines[0].text) {
		return responseHeaderCaptures(lines, 0, "", headers)
	}
	if !requestStartPattern.MatchString(lines[0].text) {
		return nil
	}
	index := 1
	for index < len(lines) {
		line := lines[index].text
		if line == ">\n" || line == "> \n" {
			index++
			break
		}
		if !strings.HasPrefix(line, "> ") || rawHeaderPattern.FindStringSubmatchIndex(line[2:]) == nil {
			return nil
		}
		index++
	}
	if index >= len(lines) || !strings.HasPrefix(lines[index].text, "< ") || !rawStatusPattern.MatchString(lines[index].text[2:]) {
		return nil
	}
	return responseHeaderCaptures(lines, index, "< ", headers)
}

func responseHeaderCaptures(lines []transcriptLine, index int, prefix string, headers map[string]struct{}) []capture {
	captures := make([]capture, 0)
	for index < len(lines) {
		status, ok := responseLine(lines[index], prefix)
		if !ok {
			return captures
		}
		statusMatch := rawStatusPattern.FindStringSubmatch(status)
		if statusMatch == nil {
			return captures
		}
		statusCode := statusMatch[1]
		index++
		for index < len(lines) {
			text, ok := responseLine(lines[index], prefix)
			if !ok {
				return captures
			}
			if text == "\n" || text == "\r\n" || text == "" {
				index++
				break
			}
			match := rawHeaderPattern.FindStringSubmatchIndex(text)
			if match == nil {
				return captures
			}
			nameStart, nameEnd := match[2], match[3]
			name := strings.ToLower(text[nameStart:nameEnd])
			if _, found := headers[name]; found {
				valueStart, valueEnd := match[6], match[7]
				valueText := text[valueStart:valueEnd]
				value, valid := inferHeaderValue(name, valueText)
				if valid {
					captures = append(captures, capture{start: lines[index].offset + len(prefix) + valueStart, end: lines[index].offset + len(prefix) + valueEnd, value: value})
				}
			}
			index++
		}
		if statusCode != "100" && statusCode != "102" && statusCode != "103" {
			return captures
		}
	}
	return captures
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

func inferHeaderValue(name, value string) (normalizedValue, bool) {
	if name == "date" {
		return normalizeValue(dateRuleType, value)
	}
	return inferValue(value)
}

func inferValue(value string) (normalizedValue, bool) {
	if normalized, ok := normalizeValue(dateRuleType, value); ok {
		return normalized, true
	}
	return normalizeValue(durationRuleType, value)
}

type transcriptLine struct {
	offset int
	text   string
}

func transcriptLines(value string) []transcriptLine {
	lines := make([]transcriptLine, 0, strings.Count(value, "\n")+1)
	for offset := 0; offset < len(value); {
		end := strings.IndexByte(value[offset:], '\n')
		if end == -1 {
			lines = append(lines, transcriptLine{offset: offset, text: value[offset:]})
			break
		}
		end += offset + 1
		lines = append(lines, transcriptLine{offset: offset, text: value[offset:end]})
		offset = end
	}
	return lines
}

func nonOverlappingCaptures(captures []capture) []capture {
	sort.Slice(captures, func(left, right int) bool {
		if captures[left].start != captures[right].start {
			return captures[left].start < captures[right].start
		}
		return captures[left].end < captures[right].end
	})

	selected := make([]capture, 0, len(captures))
	for index := 0; index < len(captures); {
		groupEnd := index + 1
		for groupEnd < len(captures) && captures[groupEnd].start < captures[groupEnd-1].end {
			groupEnd++
		}
		if groupEnd == index+1 {
			selected = append(selected, captures[index])
		} else if capturesHaveSameValueSpan(captures[index:groupEnd]) {
			selected = append(selected, captures[index])
		}
		index = groupEnd
	}
	return selected
}

func capturesHaveSameValueSpan(captures []capture) bool {
	first := captures[0]
	for _, capture := range captures[1:] {
		if capture.start != first.start || capture.end != first.end || !equivalentNormalizedValue(capture.value, first.value) {
			return false
		}
	}
	return true
}

func normalizeValue(typeName, value string) (normalizedValue, bool) {
	switch typeName {
	case dateRuleType:
		format, ok := dateFormat(value)
		return normalizedValue{kind: dateRuleType, date: format}, ok
	case durationRuleType:
		format, ok := durationSignature(value)
		return normalizedValue{kind: durationRuleType, duration: format}, ok
	default:
		return normalizedValue{}, false
	}
}

func dateFormat(value string) (string, bool) {
	if match := rfc3339Pattern.FindStringSubmatch(value); match != nil {
		if _, err := time.Parse(time.RFC3339Nano, value); err == nil {
			fraction := 0
			if match[9] != "" {
				fraction = len(match[9])
			}
			zone := match[10]
			if zone != "Z" {
				zone = zone[:1] + "offset"
			}
			return fmt.Sprintf("rfc3339:T:fraction=%d:zone=%s", fraction, zone), true
		}
	}

	if _, err := http.ParseTime(value); err != nil {
		return "", false
	}
	if match := rfc1123Pattern.FindStringSubmatch(value); match != nil {
		return "http-rfc1123:zone=" + match[1], true
	}
	if match := rfc850Pattern.FindStringSubmatch(value); match != nil {
		return "http-rfc850:zone=" + match[1], true
	}
	if match := asctimePattern.FindStringSubmatch(value); match != nil {
		return fmt.Sprintf("http-asctime:day-space=%s:day-width=%d", match[1], len(match[2])), true
	}
	return "", false
}

func durationSignature(value string) (durationFormat, bool) {
	bare := value
	sign := ""
	if strings.HasPrefix(bare, "+") || strings.HasPrefix(bare, "-") {
		sign, bare = bare[:1], bare[1:]
	}
	if isBareNumber(bare) {
		if number, err := strconv.ParseFloat(value, 64); err == nil && !isInfinite(number) {
			return durationFormat{sign: sign, parts: []durationPartFormat{{
				fraction:      fractionalDigits(bare),
				hasPeriod:     strings.Contains(bare, "."),
				leadingPeriod: strings.HasPrefix(bare, "."),
				leadingZeros:  redundantLeadingZeros(bare),
			}}}, true
		}
		return durationFormat{}, false
	}

	parts := durationPart.FindAllStringSubmatchIndex(bare, -1)
	if len(parts) == 0 {
		return durationFormat{}, false
	}
	var compact strings.Builder
	compact.WriteString(sign)
	position := 0
	format := durationFormat{sign: sign, parts: make([]durationPartFormat, 0, len(parts))}
	for _, part := range parts {
		if part[0] != position {
			return durationFormat{}, false
		}
		matched := bare[part[0]:part[1]]
		space := bare[part[2]:part[3]]
		unit := bare[part[4]:part[5]]
		number := strings.TrimSuffix(matched, space+unit)
		format.parts = append(format.parts, durationPartFormat{
			space:         space,
			unit:          unit,
			fraction:      fractionalDigits(number),
			hasPeriod:     strings.Contains(number, "."),
			leadingPeriod: strings.HasPrefix(number, "."),
			leadingZeros:  redundantLeadingZeros(number),
		})
		compact.WriteString(number)
		compact.WriteString(unit)
		position = part[1]
	}
	if position != len(bare) {
		return durationFormat{}, false
	}
	if _, err := time.ParseDuration(compact.String()); err != nil {
		return durationFormat{}, false
	}
	return format, true
}

func isBareNumber(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '.' {
		return len(value) > 1 && allDigits(value[1:])
	}
	whole, fraction, hasPeriod := strings.Cut(value, ".")
	if !allDigits(whole) {
		return false
	}
	return !hasPeriod || (fraction != "" && allDigits(fraction))
}

func allDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return value != ""
}

func fractionalDigits(value string) int {
	_, fraction, found := strings.Cut(value, ".")
	if !found {
		return 0
	}
	return len(fraction)
}

func redundantLeadingZeros(value string) int {
	whole, _, _ := strings.Cut(value, ".")
	zeroes := 0
	for zeroes < len(whole) && whole[zeroes] == '0' {
		zeroes++
	}
	if zeroes == len(whole) && zeroes > 0 {
		return zeroes - 1
	}
	return zeroes
}

func isInfinite(value float64) bool {
	return value > 1.7976931348623157e+308 || value < -1.7976931348623157e+308
}

func equivalentNormalizedValue(old, current normalizedValue) bool {
	if old.kind != current.kind {
		return false
	}
	if old.kind == dateRuleType {
		return old.date == current.date
	}
	if old.duration.sign != current.duration.sign || len(old.duration.parts) != len(current.duration.parts) {
		return false
	}
	for index := range old.duration.parts {
		if old.duration.parts[index] != current.duration.parts[index] {
			return false
		}
	}
	return true
}
