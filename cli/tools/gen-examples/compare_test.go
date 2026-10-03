// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// gen-examples verifies volatile comparison only affects declared rendered output values.
package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestEquivalentTranscriptHTTPDateHeader(t *testing.T) {
	old := renderedOutput("# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n# \n# body stays here\n")
	current := renderedOutput("# HTTP/1.1 200 OK\n# Date: Tue, 03 Jan 2006 16:05:06 GMT\n# \n# body stays here\n")

	equivalent, err := equivalentTranscript(old, current, nil, nil)
	require.NoError(t, err)
	require.True(t, equivalent)
}

func TestEquivalentTranscriptFencedTextResponseHeaders(t *testing.T) {
	old := fencedText("HTTP/1.1 200 OK\nDate: Mon, 02 Jan 2006 15:04:05 GMT\nX-Latency: 12.3ms\n\nbody\n", "<!-- stdout; Line endings: LF,LF,LF,LF,LF -->\n", "```")
	current := fencedText("HTTP/1.1 200 OK\nDate: Tue, 03 Jan 2006 16:05:06 GMT\nX-Latency: 98.7ms\n\nbody\n", "<!-- stdout; Line endings: LF,LF,LF,LF,LF -->\n", "```")

	equivalent, err := equivalentTranscript(old, current, nil, []string{"x-latency"})
	require.NoError(t, err)
	require.True(t, equivalent)

	current = fencedText("HTTP/1.1 200 OK\nDate: Tue, 03 Jan 2006 16:05:06 GMT\nX-Latency: 98.7ms\n\nbody\n", "<!-- stdout; Line endings: CRLF,CRLF,CRLF,CRLF,CRLF -->\n", "```")
	equivalent, err = equivalentTranscript(old, current, nil, []string{"x-latency"})
	require.NoError(t, err)
	require.False(t, equivalent, "metadata is part of the displayed transcript")
}

func TestEquivalentTranscriptFencedVerboseResponseScope(t *testing.T) {
	old := fencedText("> GET / HTTP/1.1\n> Host: example.test\n> User-Agent: boe-example\n>\n< HTTP/1.1 200 OK\n< Date: Mon, 02 Jan 2006 15:04:05 GMT\n< X-Latency: 12ms\n<\n< HTTP/1.1 200 OK\n< Date: Mon, 02 Jan 2006 15:04:05 GMT\n", "<!-- stderr; Line endings: LF,LF,LF,LF,LF,LF,LF,LF,LF,LF -->\n", "````")
	current := fencedText("> GET / HTTP/1.1\n> Host: example.test\n> User-Agent: boe-example\n>\n< HTTP/1.1 200 OK\n< Date: Tue, 03 Jan 2006 16:05:06 GMT\n< X-Latency: 98ms\n<\n< HTTP/1.1 200 OK\n< Date: Wed, 04 Jan 2006 17:06:07 GMT\n", "<!-- stderr; Line endings: LF,LF,LF,LF,LF,LF,LF,LF,LF,LF -->\n", "````")

	equivalent, err := equivalentTranscript(old, current, nil, []string{"x-latency"})
	require.NoError(t, err)
	require.False(t, equivalent, "a status-like body line remains literal")

	current = fencedText("> GET / HTTP/1.1\n> Host: example.test\n> User-Agent: other-agent\n>\n< HTTP/1.1 200 OK\n< Date: Tue, 03 Jan 2006 16:05:06 GMT\n< X-Latency: 98ms\n<\n< HTTP/1.1 200 OK\n< Date: Mon, 02 Jan 2006 15:04:05 GMT\n", "<!-- stderr; Line endings: LF,LF,LF,LF,LF,LF,LF,LF,LF,LF -->\n", "````")
	equivalent, err = equivalentTranscript(old, current, nil, []string{"x-latency"})
	require.NoError(t, err)
	require.False(t, equivalent, "outgoing request lines remain literal")
}

func TestEquivalentTranscriptFencedTextCustomRulesAndFenceSyntax(t *testing.T) {
	rules := []extensions.ExampleComparisonRule{{Type: durationRuleType, Pattern: `(?m)^latency: (?P<value>\S+)$`}}
	old := "```sh\n$ curl --verbose\n```\n" + fencedText("latency: 12.3ms\n", "<!-- stdout; Line endings: LF -->\n", "```")
	current := "```sh\n$ curl --verbose\n```\n" + fencedText("latency: 98.7ms\n", "<!-- stdout; Line endings: LF -->\n", "```")

	equivalent, err := equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.True(t, equivalent)

	current = "```sh\n$ curl --verbose\n```\n" + fencedText("latency: 98.7ms\n", "<!-- stdout; Line endings: LF -->\n", "````")
	equivalent, err = equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.False(t, equivalent, "fence syntax is visible transcript formatting")
}

func TestEquivalentTranscriptHTTPDateHeaderPreservesSyntax(t *testing.T) {
	old := renderedOutput("# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n# \n")
	current := renderedOutput("# HTTP/1.1 200 OK\n# Date: 2006-01-03T16:05:06Z\n# \n")

	equivalent, err := equivalentTranscript(old, current, nil, nil)
	require.NoError(t, err)
	require.False(t, equivalent)
}

func TestEquivalentTranscriptDoesNotTreatResponseBodyDateAsHeader(t *testing.T) {
	old := renderedOutput("# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n# \n# Date: Tue, 03 Jan 2006 16:05:06 GMT\n")
	current := renderedOutput("# HTTP/1.1 200 OK\n# Date: Wed, 04 Jan 2006 17:06:07 GMT\n# \n# Date: Thu, 05 Jan 2006 18:07:08 GMT\n")

	equivalent, err := equivalentTranscript(old, current, nil, nil)
	require.NoError(t, err)
	require.False(t, equivalent)
}

func TestEquivalentTranscriptBlankRenderedLinesPreserveBodyBoundary(t *testing.T) {
	old := renderedOutput("# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n#\n# Date: Tue, 03 Jan 2006 16:05:06 GMT\n")
	current := renderedOutput("# HTTP/1.1 200 OK\n# Date: Wed, 04 Jan 2006 17:06:07 GMT\n#\n# Date: Thu, 05 Jan 2006 18:07:08 GMT\n")
	equivalent, err := equivalentTranscript(old, current, nil, nil)
	require.NoError(t, err)
	require.False(t, equivalent)

	rules := []extensions.ExampleComparisonRule{{Type: "duration", Pattern: `(?m)^latency: (?P<value>\S+)$`}}
	old = renderedOutput("# heading\n#\n# latency: 12ms\n")
	current = renderedOutput("# heading\n#\n# latency: 45ms\n")
	equivalent, err = equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.True(t, equivalent)
}

func TestEquivalentTranscriptDeclaredHeaderDuration(t *testing.T) {
	headers := []string{"x-fault-inserted-latency"}
	old := renderedOutput("# HTTP/1.1 200 OK\n# x-fault-inserted-latency: 12.34 ms\n# \n")
	current := renderedOutput("# HTTP/1.1 200 OK\n# X-Fault-Inserted-Latency: 987.65 ms\n# \n")

	equivalent, err := equivalentTranscript(old, current, nil, headers)
	require.NoError(t, err)
	require.False(t, equivalent, "header spelling is part of the rendered format")

	current = renderedOutput("# HTTP/1.1 200 OK\n# x-fault-inserted-latency: 987.65 ms\n# \n")
	equivalent, err = equivalentTranscript(old, current, nil, headers)
	require.NoError(t, err)
	require.True(t, equivalent)
}

func TestEquivalentTranscriptDurationPreservesFormat(t *testing.T) {
	headers := []string{"x-duration"}
	cases := []struct {
		name    string
		old     string
		current string
		want    bool
	}{
		{name: "magnitude", old: "12.34ms", current: "987.65ms", want: true},
		{name: "leading zero padding", old: "1ms", current: "001ms", want: false},
		{name: "same leading zero padding", old: "012ms", current: "034ms", want: true},
		{name: "unit", old: "12ms", current: "12s", want: false},
		{name: "fraction precision", old: "12.3ms", current: "12.34ms", want: false},
		{name: "spacing", old: "12 ms", current: "13  ms", want: false},
		{name: "sign", old: "+12ms", current: "-13ms", want: false},
		{name: "bare number", old: "12.34", current: "987.65", want: true},
		{name: "bare leading zero padding", old: "1.2", current: "001.3", want: false},
		{name: "bare same leading zero padding", old: "00.1", current: "01.2", want: true},
		{name: "bare precision", old: "12.3", current: "987.65", want: false},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			old := renderedOutput("# HTTP/1.1 200 OK\n# x-duration: " + test.old + "\n# \n")
			current := renderedOutput("# HTTP/1.1 200 OK\n# x-duration: " + test.current + "\n# \n")
			equivalent, err := equivalentTranscript(old, current, nil, headers)
			require.NoError(t, err)
			require.Equal(t, test.want, equivalent)
		})
	}
}

func TestEquivalentTranscriptInfersDeclaredHeaderKinds(t *testing.T) {
	tests := []struct {
		name    string
		old     string
		current string
		want    bool
	}{
		{name: "date value", old: "Mon, 02 Jan 2006 15:04:05 GMT", current: "Tue, 03 Jan 2006 16:05:06 GMT", want: true},
		{name: "date format change", old: "Mon, 02 Jan 2006 15:04:05 GMT", current: "2006-01-03T16:05:06Z", want: false},
		{name: "duration value", old: "12.3ms", current: "98.7ms", want: true},
		{name: "bare number", old: "12.3", current: "98.7", want: true},
		{name: "date to duration", old: "Mon, 02 Jan 2006 15:04:05 GMT", current: "12ms", want: false},
		{name: "duration to date", old: "12ms", current: "Mon, 02 Jan 2006 15:04:05 GMT", want: false},
		{name: "valid to invalid", old: "12ms", current: "twelve ms", want: false},
		{name: "unknown exact", old: "stable", current: "stable", want: true},
		{name: "unknown changed", old: "stable", current: "changed", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			old := renderedOutput("# HTTP/1.1 200 OK\n# X-Value: " + test.old + "\n#\n")
			current := renderedOutput("# HTTP/1.1 200 OK\n# X-Value: " + test.current + "\n#\n")
			equivalent, err := equivalentTranscript(old, current, nil, []string{"x-value"})
			require.NoError(t, err)
			require.Equal(t, test.want, equivalent)
		})
	}
}

func TestEquivalentTranscriptDeclaredHeaderSelectionPreservesSyntax(t *testing.T) {
	old := renderedOutput("# HTTP/1.1 200 OK\n# X-Value: 12ms\n#\n# x-value: 12ms\n")
	current := renderedOutput("# HTTP/1.1 200 OK\n# X-Value: 99ms\n#\n# x-value: 99ms\n")
	equivalent, err := equivalentTranscript(old, current, nil, []string{"X-VALUE", "x-value"})
	require.NoError(t, err)
	require.False(t, equivalent, "declarations are case insensitive, but header syntax and body text remain literal")

	current = renderedOutput("# HTTP/1.1 200 OK\n# x-value: 99ms\n#\n# x-value: 12ms\n")
	equivalent, err = equivalentTranscript(old, current, nil, []string{"X-VALUE"})
	require.NoError(t, err)
	require.False(t, equivalent, "header spelling remains part of the literal output")
}

func TestBuiltinDateHeaderRemainsDateOnly(t *testing.T) {
	old := renderedOutput("# HTTP/1.1 200 OK\n# Date: 12\n#\n")
	current := renderedOutput("# HTTP/1.1 200 OK\n# Date: 98\n#\n")
	equivalent, err := equivalentTranscript(old, current, nil, []string{"date"})
	require.NoError(t, err)
	require.False(t, equivalent, "Date values are date-only even though bare numbers are otherwise inferred as durations")
}

func TestHeaderNormalizationStopsAtFinalResponseBody(t *testing.T) {
	tests := []struct {
		name    string
		old     string
		current string
	}{
		{
			name:    "fake status and headers in body",
			old:     "# HTTP/1.1 200 OK\n#\n# body\n# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n# X-Value: 12ms\n",
			current: "# HTTP/1.1 200 OK\n#\n# body\n# HTTP/1.1 200 OK\n# Date: Tue, 03 Jan 2006 16:05:06 GMT\n# X-Value: 98ms\n",
		},
		{
			name:    "101 upgrade body",
			old:     "# HTTP/1.1 101 Switching Protocols\n#\n# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n",
			current: "# HTTP/1.1 101 Switching Protocols\n#\n# HTTP/1.1 200 OK\n# Date: Tue, 03 Jan 2006 16:05:06 GMT\n",
		},
		{
			name:    "preamble before status",
			old:     "# preface\n# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n#\n",
			current: "# preface\n# HTTP/1.1 200 OK\n# Date: Tue, 03 Jan 2006 16:05:06 GMT\n#\n",
		},
		{
			name:    "second final response",
			old:     "# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n#\n# next response\n# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n",
			current: "# HTTP/1.1 200 OK\n# Date: Tue, 03 Jan 2006 16:05:06 GMT\n#\n# next response\n# HTTP/1.1 200 OK\n# Date: Wed, 04 Jan 2006 17:06:07 GMT\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			equivalent, err := equivalentTranscript(renderedOutput(test.old), renderedOutput(test.current), nil, []string{"x-value"})
			require.NoError(t, err)
			require.False(t, equivalent)
		})
	}
}

func TestHeaderNormalizationAllowsInterimResponses(t *testing.T) {
	old := renderedOutput("# HTTP/1.1 100 Continue\n# X-Value: 12ms\n#\n# HTTP/1.1 103 Early Hints\n# X-Value: 23ms\n#\n# HTTP/1.1 200 OK\n# Date: Mon, 02 Jan 2006 15:04:05 GMT\n# X-Value: 34ms\n#\n# body\n")
	current := renderedOutput("# HTTP/1.1 100 Continue\n# X-Value: 98ms\n#\n# HTTP/1.1 103 Early Hints\n# X-Value: 87ms\n#\n# HTTP/1.1 200 OK\n# Date: Tue, 03 Jan 2006 16:05:06 GMT\n# X-Value: 76ms\n#\n# body\n")
	equivalent, err := equivalentTranscript(old, current, nil, []string{"x-value"})
	require.NoError(t, err)
	require.True(t, equivalent)
}

func TestEquivalentTranscriptCustomRulesOnlyApplyInsideOutput(t *testing.T) {
	rules := []extensions.ExampleComparisonRule{
		{Type: dateRuleType, Pattern: `created=(?P<value>\S+)`},
		{Type: durationRuleType, Pattern: `latency=(?P<value>[+\-0-9. ]+(?:ns|us|µs|ms|s|m|h))`},
	}
	old := "# Command: boe run --config created=2006-01-02T15:04:05.1Z\n" +
		renderedOutput("# created=2006-01-02T15:04:05.1Z latency=12.3 ms\n# payload=preserved\n")
	current := "# Command: boe run --config created=2006-01-03T16:05:06.1Z\n" +
		renderedOutput("# created=2006-01-03T16:05:06.1Z latency=987.6 ms\n# payload=preserved\n")

	equivalent, err := equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.False(t, equivalent, "the invocation must not be normalized")

	current = "# Command: boe run --config created=2006-01-02T15:04:05.1Z\n" +
		renderedOutput("# created=2006-01-03T16:05:06.1Z latency=987.6 ms\n# payload=preserved\n")
	equivalent, err = equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.True(t, equivalent)
}

func TestEquivalentTranscriptCustomRulesMatchRawMultilineOutput(t *testing.T) {
	rules := []extensions.ExampleComparisonRule{{Type: durationRuleType, Pattern: `(?m)^latency: (?P<value>\S+)$`}}
	old := renderedOutput("# # response body heading\n# latency: 12.3ms\n# Line endings: CRLF,CRLF\n")
	current := renderedOutput("# # response body heading\n# latency: 987.6ms\n# Line endings: CRLF,CRLF\n")

	equivalent, err := equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.True(t, equivalent)
}

func TestEquivalentTranscriptDoesNotNormalizeInvalidValues(t *testing.T) {
	rules := []extensions.ExampleComparisonRule{
		{Type: dateRuleType, Pattern: `date=(?P<value>\S+)`},
		{Type: durationRuleType, Pattern: `duration=(?P<value>\S+)`},
	}
	old := renderedOutput("# date=2006-02-30T15:04:05Z duration=12fortnights\n")
	current := renderedOutput("# date=2006-02-31T15:04:05Z duration=13fortnights\n")

	equivalent, err := equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.False(t, equivalent)
}

func TestEquivalentTranscriptPreservesOutputBodiesAndStderr(t *testing.T) {
	rules := []extensions.ExampleComparisonRule{{Type: durationRuleType, Pattern: `duration: (?P<value>\S+)`}}
	old := renderedOutput("# duration: 12ms\n# stable output\n") + renderedStderr("# duration: 8ms\n")
	current := renderedOutput("# duration: 99ms\n# changed output\n") + renderedStderr("# duration: 7ms\n")

	equivalent, err := equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.False(t, equivalent)

	current = renderedOutput("# duration: 99ms\n# stable output\n") + renderedStderr("# duration: 7ms\n")
	equivalent, err = equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.True(t, equivalent)
}

func TestEquivalentTranscriptDoesNotConfuseRenderedMarkerTextWithBlockEnd(t *testing.T) {
	rules := []extensions.ExampleComparisonRule{{Type: durationRuleType, Pattern: `duration=(?P<value>\S+)`}}
	old := renderedOutput("# # End output\n# duration=12ms\n")
	current := renderedOutput("# # End output\n# duration=99ms\n")

	equivalent, err := equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.True(t, equivalent)
}

func TestEquivalentTranscriptDoesNotNormalizeOutsideRenderedBlocks(t *testing.T) {
	rules := []extensions.ExampleComparisonRule{{Type: durationRuleType, Pattern: `duration=(?P<value>\S+)`}}
	old := "duration=12ms\n"
	current := "duration=99ms\n"

	equivalent, err := equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.False(t, equivalent)
}

func TestEquivalentTranscriptRejectsInvalidRules(t *testing.T) {
	transcript := renderedOutput("# stable\n")
	cases := []struct {
		name    string
		rules   []extensions.ExampleComparisonRule
		headers []string
	}{
		{name: "invalid regexp", rules: []extensions.ExampleComparisonRule{{Type: dateRuleType, Pattern: "("}}},
		{name: "missing value capture", rules: []extensions.ExampleComparisonRule{{Type: dateRuleType, Pattern: `date=\S+`}}},
		{name: "duplicate value capture", rules: []extensions.ExampleComparisonRule{{Type: dateRuleType, Pattern: `(?P<value>a)(?P<value>b)`}}},
		{name: "unknown rule type", rules: []extensions.ExampleComparisonRule{{Type: "timestamp", Pattern: `(?P<value>\S+)`}}},
		{name: "invalid header token", headers: []string{"x bad"}},
		{name: "non-ASCII header token", headers: []string{"x-ünicode"}},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := equivalentTranscript(transcript, transcript, test.rules, test.headers)
			require.Error(t, err)
		})
	}
}

func renderedOutput(body string) string {
	return "# Output:\n" + body + "# End output\n"
}

func renderedStderr(body string) string {
	return "# Stderr:\n" + body + "# End output\n"
}

func fencedText(body, metadata, fence string) string {
	return fence + "text\n" + body + fence + "\n" + metadata
}
