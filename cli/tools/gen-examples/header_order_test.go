// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Header-order tests use rendered captures to prove that only independent response fields can move.
package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestEquivalentTranscriptResponseHeaderOrder(t *testing.T) {
	for _, mode := range []string{"include", "verbose", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			oldHeaders := "X-Z: last\nSet-Cookie: a=1\nX-Time: 12.3ms\nSet-Cookie: b=2\nX-A: first\n"
			for _, tc := range []struct {
				name    string
				headers string
				want    bool
			}{
				{name: "different names", headers: "X-A: first\nSet-Cookie: a=1\nSet-Cookie: b=2\nX-Time: 98.765ms\nX-Z: last\n", want: true},
				{name: "same name order", headers: "X-A: first\nSet-Cookie: b=2\nSet-Cookie: a=1\nX-Time: 98ms\nX-Z: last\n"},
				{name: "removed", headers: "Set-Cookie: a=1\nSet-Cookie: b=2\nX-Time: 98ms\nX-Z: last\n"},
				{name: "added", headers: oldHeaders + "X-New: new\n"},
				{name: "duplicate", headers: oldHeaders + "Set-Cookie: a=1\n"},
				{name: "literal value", headers: strings.ReplaceAll(oldHeaders, "last", "changed")},
				{name: "case", headers: strings.ReplaceAll(oldHeaders, "X-Z", "x-z")},
				{name: "separator", headers: strings.ReplaceAll(oldHeaders, "X-Z: ", "X-Z:\t")},
				{name: "volatile format", headers: strings.ReplaceAll(oldHeaders, "12.3ms", "12.3s")},
				{name: "malformed name", headers: strings.ReplaceAll(oldHeaders, "X-Z", "X Bad")},
			} {
				t.Run(tc.name, func(t *testing.T) {
					old := headerOrderTranscript(t, mode, "HTTP/1.1 200 OK\n"+oldHeaders+"\nbody\n")
					current := headerOrderTranscript(t, mode, "HTTP/1.1 200 OK\n"+tc.headers+"\nbody\n")
					equal, err := equivalentTranscript(old, current, nil, []string{"x-time"})
					require.NoError(t, err)
					require.Equal(t, tc.want, equal)
				})
			}
		})
	}
}

func TestEquivalentTranscriptHeaderOrderPreservesBytesAndScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		old     string
		current string
		want    bool
	}{
		{name: "whitespace and mixed endings", old: "HTTP/1.1 200 OK\r\nX-Z: z \t\r\nX-A: a\n\r\nbody", current: "HTTP/1.1 200 OK\r\nX-A: a\nX-Z: z \t\r\n\r\nbody", want: true},
		{name: "bare carriage returns in body", old: "HTTP/1.1 200 OK\nX-Z: long\nX-A: a\n\nbody\rtext\\r\rmore\n", current: "HTTP/1.1 200 OK\nX-A: a\nX-Z: long\n\nbody\rtext\\r\rmore\n", want: true},
		{name: "bare carriage return in header", old: "HTTP/1.1 200 OK\nX-Z: z\rtext\nX-A: a\n\n", current: "HTTP/1.1 200 OK\nX-A: a\nX-Z: z\rtext\n\n"},
		{name: "mixed case repeated name", old: "HTTP/1.1 200 OK\nSet-Cookie: a=1\nX-A: a\nset-cookie: b=2\n\n", current: "HTTP/1.1 200 OK\nX-A: a\nset-cookie: b=2\nSet-Cookie: a=1\n\n"},
		{name: "changed whitespace", old: "HTTP/1.1 200 OK\nX-Z: z \nX-A: a\n\n", current: "HTTP/1.1 200 OK\nX-A: a\nX-Z: z\t\n\n"},
		{name: "changed line ending", old: "HTTP/1.1 200 OK\nX-Z: z\r\nX-A: a\n\n", current: "HTTP/1.1 200 OK\nX-A: a\nX-Z: z\n\n"},
		{name: "changed body", old: "HTTP/1.1 200 OK\nX-Z: z\nX-A: a\n\nX-Z: z\nX-A: a\n", current: "HTTP/1.1 200 OK\nX-A: a\nX-Z: z\n\nX-A: a\nX-Z: z\n"},
		{name: "interim sections", old: "HTTP/1.1 103 Early Hints\nX-Z: hint\nX-A: a\n\nHTTP/1.1 200 OK\nX-Z: final\nX-A: b\n\nbody\n", current: "HTTP/1.1 103 Early Hints\nX-A: a\nX-Z: hint\n\nHTTP/1.1 200 OK\nX-A: b\nX-Z: final\n\nbody\n", want: true},
		{name: "moved across interim sections", old: "HTTP/1.1 103 Early Hints\nX-Z: hint\nX-A: a\n\nHTTP/1.1 200 OK\nX-Z: final\nX-A: b\n\n", current: "HTTP/1.1 103 Early Hints\nX-A: b\nX-Z: hint\n\nHTTP/1.1 200 OK\nX-A: a\nX-Z: final\n\n"},
		{name: "partial headers", old: "HTTP/1.1 200 OK\nX-Z: z\nX-A: a\n", current: "HTTP/1.1 200 OK\nX-A: a\nX-Z: z\n"},
		{name: "preamble", old: "preamble\nHTTP/1.1 200 OK\nX-Z: z\nX-A: a\n\n", current: "preamble\nHTTP/1.1 200 OK\nX-A: a\nX-Z: z\n\n"},
		{name: "status", old: "HTTP/1.1 200 OK\nX-Z: z\nX-A: a\n\n", current: "HTTP/1.1 503 Service Unavailable\nX-A: a\nX-Z: z\n\n"},
		{name: "request order", old: "> GET / HTTP/1.1\n> X-Z: z\n> X-A: a\n>\n< HTTP/1.1 200 OK\n< X-Z: z\n< X-A: a\n<\n", current: "> GET / HTTP/1.1\n> X-A: a\n> X-Z: z\n>\n< HTTP/1.1 200 OK\n< X-A: a\n< X-Z: z\n<\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, err := renderOutput(tc.old, nil, nil, "stdout")
			require.NoError(t, err)
			current, err := renderOutput(tc.current, nil, nil, "stdout")
			require.NoError(t, err)
			equal, err := equivalentTranscript(old, current, nil, nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, equal)
		})
	}
}

func TestEquivalentTranscriptHeaderOrderCustomRules(t *testing.T) {
	rules := []extensions.ExampleComparisonRule{{Type: "duration", Pattern: `(?m)^X-Time: (?P<value>\S+)$`}}
	old := headerOrderTranscript(t, "include", "HTTP/1.1 200 OK\nX-Z: literal\nX-Time: 12.3ms\n\n")
	current := headerOrderTranscript(t, "include", "HTTP/1.1 200 OK\nX-Time: 98.765ms\nX-Z: literal\n\n")
	equal, err := equivalentTranscript(old, current, rules, nil)
	require.NoError(t, err)
	require.True(t, equal)
}

func TestEquivalentTranscriptHeaderOrderInvalidMetadataRemainsLiteral(t *testing.T) {
	oldBody := "HTTP/1.1 200 OK\nX-Z: z\nX-A: a\n\n"
	newBody := "HTTP/1.1 200 OK\nX-A: a\nX-Z: z\n\n"
	for _, metadata := range []string{
		"<!-- stdout; Unknown: value -->\n",
		"<!-- stdout; Line endings: LF,LF,invalid,LF -->\n",
		"<!-- stdout; Line endings: LF,LF,LF,LF; Trailing whitespace: invalid -->\n",
		"<!-- stdout; Line endings: LF,LF,LF,LF; Trailing whitespace: ew== -->\n",
		"<!-- stdout; Line endings: LF,LF,LF,LF; Bare CR offsets: not-a-number -->\n",
		"<!-- stdout; Line endings: LF,LF,LF,LF; Bare CR offsets: 999 -->\n",
	} {
		t.Run(metadata, func(t *testing.T) {
			old := fencedText(oldBody, metadata, "```")
			current := fencedText(newBody, metadata, "```")
			equal, err := equivalentTranscript(old, current, nil, nil)
			require.NoError(t, err)
			require.False(t, equal, "invalid metadata must not be normalized away")
			equal, err = equivalentTranscript(old, old, nil, nil)
			require.NoError(t, err)
			require.True(t, equal, "identical malformed captures still compare literally")
		})
	}
}

func headerOrderTranscript(t *testing.T, mode, payload string) string {
	t.Helper()
	if mode == "legacy" {
		return renderedOutput("# " + strings.ReplaceAll(strings.TrimSuffix(payload, "\n"), "\n", "\n# ") + "\n")
	}
	stream := "stdout"
	if mode == "verbose" {
		payload = "> GET / HTTP/1.1\n> Host: example.test\n>\n< " + strings.ReplaceAll(strings.TrimSuffix(payload, "\n"), "\n", "\n< ") + "\n"
		stream = "stderr"
	}
	rendered, err := renderOutput(payload, nil, nil, stream)
	require.NoError(t, err)
	return rendered
}
