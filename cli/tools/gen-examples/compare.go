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
	rfc3339Pattern = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(T)(\d{2}):(\d{2}):(\d{2})(\.(\d+))?(Z|[+-]\d{2}:\d{2})$`)
	rfc1123Pattern = regexp.MustCompile(`^[A-Za-z]{3}, \d{2} [A-Za-z]{3} \d{4} \d{2}:\d{2}:\d{2} ([A-Za-z]{3})$`)
	rfc850Pattern  = regexp.MustCompile(`^[A-Za-z]+, \d{2}-[A-Za-z]{3}-\d{2} \d{2}:\d{2}:\d{2} ([A-Za-z]{3})$`)
	asctimePattern = regexp.MustCompile(`^[A-Za-z]{3} [A-Za-z]{3} ( {1,2})(\d{1,2}) \d{2}:\d{2}:\d{2} \d{4}$`)
	durationPart   = regexp.MustCompile(`(?:\d+(?:\.\d+)?|\.\d+)([ \t]*)(ns|us|µs|ms|s|m|h)`)
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
	kind     string
	body     string
	metadata string
	raw      bool
	start    int
	end      int
}

type normalizedOutput struct {
	segments []bodySegment
	metadata string
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

type comparisonPolicy struct {
	rules   []comparisonRule
	headers map[string]struct{}
}

type preparedOutput struct {
	block      outputBlock
	http       httpOutput
	captures   []capture
	normalized normalizedOutput
}

func compileComparisonPolicy(rules []extensions.ExampleComparisonRule, headers []string) (comparisonPolicy, error) {
	compiled, err := compileComparisonRules(rules)
	if err != nil {
		return comparisonPolicy{}, err
	}
	headerRules, err := compileHeaderRules(headers)
	if err != nil {
		return comparisonPolicy{}, err
	}
	return comparisonPolicy{rules: compiled, headers: headerRules}, nil
}

func (policy comparisonPolicy) prepare(block outputBlock) *preparedOutput {
	analysis := analyzeHTTPOutput(block)
	captures := outputCaptures(block, analysis, policy.rules, policy.headers)
	canonical, remapped := canonicalHeaderOrder(block, analysis, captures)
	normalized := normalizedOutput{
		segments: normalizeSegments(canonical.body, nonOverlappingCaptures(remapped)),
		metadata: canonical.metadata,
	}
	return &preparedOutput{block: block, http: analysis, captures: captures, normalized: normalized}
}

func (policy comparisonPolicy) reconcile(old, current string, elide bool) string {
	oldParts, newParts := splitTranscript(old), splitTranscript(current)
	if len(oldParts) != len(newParts) {
		return current
	}
	var projected strings.Builder
	cursor := 0
	equal := true
	for index, oldPart := range oldParts {
		newPart := newParts[index]
		if oldPart.block == nil || newPart.block == nil {
			if oldPart.block != nil || newPart.block != nil || oldPart.literal != newPart.literal {
				return current
			}
			continue
		}
		previous, fresh := *oldPart.block, *newPart.block
		if previous.kind != fresh.kind || previous.raw != fresh.raw {
			return current
		}
		if !elide || !previous.raw || !hasElision(previous.body) {
			equal = equivalentNormalizedOutput(policy.prepare(previous).normalized, policy.prepare(fresh).normalized) && equal
			continue
		}
		replacement, matched := policy.projectElidedOutput(previous, fresh)
		if !matched {
			equal = false
			continue
		}
		projected.WriteString(current[cursor:fresh.start])
		projected.WriteString(replacement.markdown())
		cursor = fresh.end
		equal = previous.metadata == replacement.metadata && equal
	}
	if equal {
		return old
	}
	projected.WriteString(current[cursor:])
	return projected.String()
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
	offsets := make([]int, len(lines)+1)
	for index, line := range lines {
		offsets[index+1] = offsets[index] + len(line)
	}
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
			block := &outputBlock{kind: "text", body: strings.Join(lines[index+1:end], ""), raw: true, start: offsets[index]}
			parts = append(parts, transcriptPart{block: block})
			literal.WriteString(lines[end])
			index = end + 1
			metadataIndex := index
			if metadataIndex < len(lines) && lines[metadataIndex] == "\n" {
				metadataIndex++
			}
			if metadataIndex < len(lines) && isOutputMetadata(lines[metadataIndex]) {
				literal.WriteString(strings.Join(lines[index:metadataIndex], ""))
				block.metadata = lines[metadataIndex]
				index = metadataIndex + 1
			}
			block.end = offsets[index]
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

func equivalentNormalizedOutput(old, current normalizedOutput) bool {
	return old.metadata == current.metadata && equivalentSegments(old.segments, current.segments)
}

func equivalentSegments(old, current []bodySegment) bool {
	if len(old) != len(current) {
		return false
	}
	for index, previous := range old {
		fresh := current[index]
		if previous.value == nil || fresh.value == nil {
			if previous.value != nil || fresh.value != nil || previous.literal != fresh.literal {
				return false
			}
		} else if !equivalentNormalizedValue(*previous.value, *fresh.value) {
			return false
		}
	}
	return true
}

func normalizeSegments(body string, captures []capture) []bodySegment {
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

func outputCaptures(block outputBlock, analysis httpOutput, rules []comparisonRule, headers map[string]struct{}) []capture {
	body := block.body
	captures := headerCaptures(block, analysis, headers)
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
			if ok && !analysis.intersectsStatus(bodyStart, bodyEnd) {
				captures = append(captures, capture{start: bodyStart, end: bodyEnd, value: value})
			}
		}
	}

	return nonOverlappingCaptures(captures)
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

func headerCaptures(block outputBlock, analysis httpOutput, headers map[string]struct{}) []capture {
	captures := make([]capture, 0)
	for _, section := range analysis.sections {
		for _, header := range section.headers {
			if value, valid := headerValueCapture(block.body, header, headers); valid {
				captures = append(captures, value)
			}
		}
	}
	return captures
}

func headerValueCapture(body string, header responseHeader, headers map[string]struct{}) (capture, bool) {
	if _, found := headers[header.name]; !found {
		return capture{}, false
	}
	value, valid := inferHeaderValue(header.name, body[header.valueStart:header.valueEnd])
	return capture{start: header.valueStart, end: header.valueEnd, value: value}, valid
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
		end := captures[index].end
		for groupEnd < len(captures) && captures[groupEnd].start < end {
			end = max(end, captures[groupEnd].end)
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
		partFormat := durationPartFormat{
			space:         space,
			unit:          unit,
			fraction:      fractionalDigits(number),
			hasPeriod:     strings.Contains(number, "."),
			leadingPeriod: strings.HasPrefix(number, "."),
			leadingZeros:  redundantLeadingZeros(number),
		}
		if unit == "ms" {
			partFormat.fraction = 0
			partFormat.hasPeriod = false
		}
		format.parts = append(format.parts, partFormat)
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
