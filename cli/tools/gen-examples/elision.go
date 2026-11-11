// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Output elision belongs beside comparison so omitted lines cannot weaken execution or retained-value checks.
package main

import "strings"

func hasElision(body string) bool {
	for _, line := range transcriptLines(body) {
		if isElisionLine(line.text) {
			return true
		}
	}
	return false
}

func isElisionLine(line string) bool { return line == "...\n" || line == "..." }

func (policy comparisonPolicy) projectElidedOutput(old, current outputBlock) (encodedOutput, bool) {
	captured, valid := decodeCapturedOutput(current)
	if !valid {
		return encodedOutput{}, false
	}
	if old.metadata != "" {
		previous, parsed := parseOutputMetadata(old.metadata)
		if !parsed || previous.stream != captured.stream {
			return encodedOutput{}, false
		}
	}
	current.metadata = ""
	matcher := newElisionMatcher(old.body, policy.prepare(current), policy)
	if !matcher.match(0, 0) {
		return encodedOutput{}, false
	}
	selected := make(map[int]bool, len(matcher.selected))
	for _, index := range matcher.selected {
		selected[index] = true
	}
	for _, index := range matcher.statuses {
		if !selected[index] {
			return encodedOutput{}, false
		}
	}
	rawLines := transcriptLines(captured.raw)
	if len(rawLines) != len(matcher.actual) {
		return encodedOutput{}, false
	}
	var output strings.Builder
	for _, index := range matcher.selected {
		if index < 0 {
			output.WriteString("...\n")
		} else {
			output.WriteString(rawLines[index].text)
		}
	}
	encoded, err := encodeCapturedOutput(output.String(), captured.stream)
	return encoded, err == nil
}

type elisionPosition struct{ expected, actual int }

type elisionMatcher struct {
	expected []transcriptLine
	actual   []transcriptLine
	selected []int
	statuses []int
	failed   map[elisionPosition]bool
	sections map[int]responseHeaderSection
	values   map[int][]bodySegment
	output   *preparedOutput
	policy   comparisonPolicy
}

func newElisionMatcher(pattern string, output *preparedOutput, policy comparisonPolicy) *elisionMatcher {
	m := &elisionMatcher{
		expected: transcriptLines(pattern), actual: output.http.lines,
		failed: make(map[elisionPosition]bool), sections: make(map[int]responseHeaderSection),
		values: make(map[int][]bodySegment), output: output, policy: policy,
		statuses: output.http.statuses,
	}
	m.selected = make([]int, len(m.expected))
	for _, section := range output.http.sections {
		if len(section.headers) > 0 {
			m.sections[section.headers[0].line] = section
		}
	}
	byLine := make(map[int][]capture)
	for _, value := range output.captures {
		for index, line := range m.actual {
			if value.start >= line.offset && value.end <= line.offset+len(line.text) {
				value.start -= line.offset
				value.end -= line.offset
				byLine[index] = append(byLine[index], value)
				break
			}
		}
	}
	for index, captures := range byLine {
		m.values[index] = normalizeSegments(m.actual[index].text, captures)
	}
	return m
}

func (m *elisionMatcher) match(expected, actual int) bool {
	position := elisionPosition{expected: expected, actual: actual}
	if m.failed[position] {
		return false
	}
	if expected == len(m.expected) {
		return actual == len(m.actual)
	}
	if section, found := m.sections[actual]; found {
		if next, handled, matched := m.matchHeaders(expected, section); handled {
			if matched && m.match(next, section.headers[len(section.headers)-1].line+1) {
				return true
			}
			m.failed[position] = true
			return false
		}
	}
	if isElisionLine(m.expected[expected].text) {
		m.selected[expected] = -1
		for end := actual + 1; end <= len(m.actual); end++ {
			if m.match(expected+1, end) {
				return true
			}
		}
	} else if actual < len(m.actual) && m.lineMatches(expected, actual) {
		m.selected[expected] = actual
		if m.match(expected+1, actual+1) {
			return true
		}
	}
	m.failed[position] = true
	return false
}

func (m *elisionMatcher) lineMatches(expected, actual int) bool {
	pattern, line := m.expected[expected].text, m.actual[actual]
	if pattern == line.text {
		return true
	}
	values, volatile := m.values[actual]
	if !volatile {
		return false
	}
	if len(m.policy.rules) > 0 {
		// Custom patterns must also match the candidate in its complete original context.
		candidate := m.output.block
		candidate.body = candidate.body[:line.offset] + pattern + candidate.body[line.offset+len(line.text):]
		return equivalentNormalizedOutput(m.policy.prepare(candidate).normalized, m.output.normalized)
	}
	// Header recognition comes from the full capture, never an isolated candidate line.
	prefix := ""
	if strings.HasPrefix(line.text, "< ") {
		prefix = "< "
	}
	header, valid := parseResponseHeader(transcriptLine{text: pattern}, prefix)
	if !valid {
		return false
	}
	value, valid := headerValueCapture(pattern, header, m.policy.headers)
	return valid && equivalentSegments(normalizeSegments(pattern, []capture{value}), values)
}

func (m *elisionMatcher) matchHeaders(start int, section responseHeaderSection) (next int, handled, matched bool) {
	prefix := section.prefix
	separator := section.separator
	queues := make(map[string][]int)
	for _, header := range section.headers {
		queues[header.name] = append(queues[header.name], header.line)
	}
	var names []string
	elisions := 0
	for next = start; next < len(m.expected); next++ {
		line := m.expected[next].text
		if line == m.actual[separator].text {
			break
		}
		if isElisionLine(line) {
			elisions++
			names = append(names, "")
			continue
		}
		header, valid := parseResponseHeader(m.expected[next], prefix)
		if !valid {
			return 0, false, false
		}
		names = append(names, header.name)
	}
	if next == len(m.expected) {
		return 0, false, false
	}
	retained := len(names) - elisions
	omitted := len(section.headers) - retained
	if elisions == 0 && omitted != 0 || elisions > 0 && omitted < elisions {
		return next, true, false
	}
	for offset, name := range names {
		index := start + offset
		m.selected[index] = -1
		if name == "" {
			continue
		}
		found := false
		for len(queues[name]) > 0 {
			candidate := queues[name][0]
			queues[name] = queues[name][1:]
			if m.lineMatches(index, candidate) {
				m.selected[index], found = candidate, true
				break
			}
			if elisions == 0 {
				return next, true, false
			}
		}
		if !found {
			return next, true, false
		}
	}
	return next, true, true
}
