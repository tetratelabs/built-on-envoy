// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// These tests exercise generated manifest edits and the command surface that users copy from docs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestPatchExampleCodesPreservesOtherManifestText(t *testing.T) {
	source := "# heading\nname: sample\nexamples:\n  - title: First\n    description: keep this\n    code: |- # preserve comment\n      old output\n    # retain nearby note\n  - title: Last\n    commands: []\nextensionSet: true # keep root metadata\n"
	updated, err := patchExampleCodes([]byte(source), map[int]string{0: "new output\nsecond line\n"})
	require.NoError(t, err)
	for _, preserved := range []string{"# heading\n", "description: keep this\n", "# preserve comment\n", "# retain nearby note\n", "extensionSet: true # keep root metadata\n"} {
		require.Contains(t, string(updated), preserved)
	}
	require.NotContains(t, string(updated), "old output")
	var parsed struct {
		Examples []struct {
			Title string `yaml:"title"`
			Code  string `yaml:"code"`
		} `yaml:"examples"`
		ExtensionSet bool `yaml:"extensionSet"`
	}
	require.NoError(t, yaml.Unmarshal(updated, &parsed))
	require.Equal(t, "new output\nsecond line\n", parsed.Examples[0].Code)
	require.Contains(t, string(updated), "    code: | # preserve comment\n      new output\n      second line\n")
	require.True(t, parsed.ExtensionSet)
}

func TestPatchMissingCodeStaysInsideLastExample(t *testing.T) {
	source := "name: sample\nexamples:\n  - title: Last\n    description: entry\n# root comment\nextensionSet: true\n"
	updated, err := patchExampleCodes([]byte(source), map[int]string{0: "generated\n"})
	require.NoError(t, err)
	var parsed struct {
		Examples []struct {
			Code string `yaml:"code"`
		} `yaml:"examples"`
		ExtensionSet bool `yaml:"extensionSet"`
	}
	require.NoError(t, yaml.Unmarshal(updated, &parsed))
	require.Equal(t, "generated\n", parsed.Examples[0].Code)
	require.True(t, parsed.ExtensionSet)
	require.Contains(t, string(updated), "# root comment\nextensionSet: true\n")
}

func TestPatchRejectsFlowStyleExamples(t *testing.T) {
	_, err := patchExampleCodes([]byte("name: sample\nexamples: [{title: one, code: old}]\n"), map[int]string{0: "new\n"})
	require.ErrorContains(t, err, "flow-style")
}

func TestShellRenderingExpandsOnlyDeclaredPlaceholders(t *testing.T) {
	argv := []string{"printf", "%s\\n", "${PROXY_URL}/a?x=1&y=2", "${WORK_DIR}/a'b", "$HOME", "back`tick"}
	line := "set -- " + shellJoin(argv) + "; printf '%s\\n' \"$@\""
	// The test exercises shellJoin's escaping and only invokes fixed printf commands.
	cmd := exec.Command("/bin/sh", "-c", line) // #nosec G204
	cmd.Env = append(os.Environ(), "PROXY_URL=http://127.0.0.1:10000", "WORK_DIR=/tmp/fixture")
	output, err := cmd.Output()
	require.NoError(t, err)
	require.Equal(t, "printf\n%s\\n\nhttp://127.0.0.1:10000/a?x=1&y=2\n/tmp/fixture/a'b\n$HOME\nback`tick\n", string(output))
	_, err = expandString("${lowercase}", map[string]string{})
	require.ErrorContains(t, err, "unknown placeholder")
}

func TestRenderOutputPreservesLineEndingStyleAndTrailingNewline(t *testing.T) {
	values := map[string]string{}
	require.Equal(t, "```text\none\ntwo\n```\n<!-- stdout; Line endings: CRLF,LF -->\n", renderOutputForTest(t, "one\r\ntwo\n", values, values, "stdout"))
	require.Equal(t, "```text\none\n```\n<!-- stdout; No trailing newline -->\n", renderOutputForTest(t, "one", values, values, "stdout"))
	require.Equal(t, "```text\nfirst\n\nlast\n```\n<!-- stdout; Line endings: LF,LF,LF -->\n", renderOutputForTest(t, "first\n\nlast\n", values, values, "stdout"))
}

func TestRenderOutputUsesDisplayedDefaultsForCapturedValues(t *testing.T) {
	actual := map[string]string{
		"PROXY_URL":        "http://127.0.0.1:41923",
		"ADMIN_URL":        "http://127.0.0.1:47211",
		"UPSTREAM_ADDRESS": "httpbin.org:443",
		"WORK_DIR":         "/tmp/private-workdir",
	}
	display := map[string]string{
		"PROXY_URL":        "http://localhost:10000",
		"ADMIN_URL":        "http://127.0.0.1:9901",
		"UPSTREAM_ADDRESS": "httpbin.org:443",
		"WORK_DIR":         ".",
	}
	actual["PROXY_AUTHORITY"] = "127.0.0.1:41923"
	actual["ADMIN_AUTHORITY"] = "127.0.0.1:47211"
	display["PROXY_AUTHORITY"] = "localhost:10000"
	display["ADMIN_AUTHORITY"] = "127.0.0.1:9901"
	output := "http://127.0.0.1:41923 127.0.0.1:47211 httpbin.org:443 /tmp/private-workdir\n"
	require.Equal(t, "```text\nhttp://localhost:10000 127.0.0.1:9901 httpbin.org:443 .\n```\n<!-- stdout; Line endings: LF -->\n", renderOutputForTest(t, output, actual, display, "stdout"))
}

func renderOutputForTest(t *testing.T, value string, actual, display map[string]string, stream string) string {
	t.Helper()
	rendered, err := renderOutput(value, actual, display, stream)
	require.NoError(t, err)
	return rendered
}

func TestWaitAdminReportsFakeBoeEarlyExit(t *testing.T) {
	dir := t.TempDir()
	boePath := filepath.Join(dir, "fake-boe")
	require.NoError(t, os.WriteFile(boePath, []byte("#!/bin/sh\nexit 17\n"), 0o600))
	require.NoError(t, os.Chmod(boePath, 0o700)) // #nosec G302 -- the fake BOE must be executable by the process lifecycle test.
	proc, err := startBoe(context.Background(), boePath, nil, dir, dir, dir, dir, dir)
	require.NoError(t, err)
	err = waitAdmin(context.Background(), proc, "127.0.0.1:0", 5*time.Second)
	require.ErrorContains(t, err, "boe exited before Envoy became ready")
	require.Error(t, stopProcess(proc))
}

func TestWriteManifestChangesRefusesConcurrentEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	require.NoError(t, os.WriteFile(path, []byte("changed\n"), 0o600))
	err := writeManifestChanges([]manifestChange{{path: path, old: []byte("original\n"), data: []byte("generated\n")}})
	require.ErrorContains(t, err, "changed while examples were running")
	contents, readErr := os.ReadFile(path) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, readErr)
	require.Equal(t, "changed\n", string(contents))
}

func TestGenerateCheckUpdateAndFailureAtomicity(t *testing.T) {
	extensionPath := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(extensionPath, "examples"), 0o700))
	fixturePath := filepath.Join(extensionPath, "examples", "run.sh")
	require.NoError(t, os.WriteFile(fixturePath, []byte("printf 'HTTP/1.1 200 OK\\r\\nDate: Tue, 03 Jan 2006 16:05:06 GMT\\r\\nX-Test-Date: Tue, 03 Jan 2006 16:05:06 GMT\\r\\nX-Test-Duration: 10ms\\r\\nX-Test-Exact: stable\\r\\n\\r\\nbody\\r\\n'\n"), 0o600))
	prepareFixture := filepath.Join(extensionPath, "examples", "prepare.sh")
	prepareScript := "printf '%s\\n' \"$WORK_DIR\"; printf ready > \"$WORK_DIR/prepared.conf\"; printf 'pre-start stdout\\n'; printf 'pre-start stderr\\n' >&2\n"
	require.NoError(t, os.WriteFile(prepareFixture, []byte(prepareScript), 0o600))
	curlBin := t.TempDir()
	fakeCurl := filepath.Join(curlBin, "curl")
	curlScript := `#!/bin/sh
case "$*" in
	*/retry-slow*)
		counter="$WORK_DIR/retry-slow-count"
		count=0
		[ ! -f "$counter" ] || read -r count < "$counter"
		count=$((count + 1))
		printf '%s' "$count" > "$counter"
		sleep 0.6
		status=200
		[ "$count" -le 1 ] || status=503
		printf 'HTTP/1.1 %s Generated\r\nContent-Type: text/plain\r\n\r\nattempt-%s-status-%s\n' "$status" "$count" "$status"
		printf 'attempt-%s-stderr-%s\n' "$count" "$status" >&2
		exit 0
		;;
	*/retry-503*)
		counter="$WORK_DIR/retry-503-count"
		count=0
		[ ! -f "$counter" ] || read -r count < "$counter"
		count=$((count + 1))
		printf '%s' "$count" > "$counter"
		status=200
		[ "$count" -le 1 ] || status=503
		printf 'HTTP/1.1 %s Generated\r\nContent-Type: text/plain\r\n\r\nattempt-%s-status-%s\n' "$status" "$count" "$status"
		printf 'attempt-%s-stderr-%s\n' "$count" "$status" >&2
		exit 0
		;;
	*/retry-200*)
		counter="$WORK_DIR/retry-200-count"
		count=0
		[ ! -f "$counter" ] || read -r count < "$counter"
		count=$((count + 1))
		printf '%s' "$count" > "$counter"
		status=503
		[ "$count" -le 1 ] || status=200
		printf 'HTTP/1.1 503 body-lookalike\r\nContent-Type: text/plain\r\n\r\nattempt-%s-status-%s\n' "$count" "$status"
		printf '* Trying loopback...\r\n> GET /retry-200 HTTP/1.1\r\n> Host: localhost\r\n> \r\n< HTTP/1.1 100 Continue\r\n< \r\n< HTTP/1.1 %s Generated\r\n< Content-Type: text/plain\r\n< \r\nattempt-%s-stderr-%s\n' "$status" "$count" "$status" >&2
		exit 0
		;;
	*/retry-body-503*)
		printf 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nHTTP/1.1 503 body text\n'
		exit 0
		;;
	*/retry-no-http*)
		printf 'not an HTTP response\n'
		exit 0
		;;
	*/retry-exit*)
		printf 'HTTP/1.1 503 Retry\r\nContent-Type: text/plain\r\n\r\nexit failure\n'
		exit 5
		;;
	*/retry-disagree*)
		printf 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n'
		printf '> GET /retry-disagree HTTP/1.1\r\n> \r\n< HTTP/1.1 503 Service Unavailable\r\n< \r\n' >&2
		exit 0
		;;
	*/retry-missing-stream*)
		printf 'body without an included response\n'
		printf '> GET /retry-missing-stream HTTP/1.1\r\n> \r\n< HTTP/1.1 503 Service Unavailable\r\n< \r\n' >&2
		exit 0
		;;
	*/retry-writeout*)
		printf 'HTTP/1.1 503\nContent-Type: text/plain\n\n'
		exit 0
		;;
	*/retry-invalid*)
		printf executed > "$GEN_RETRY_ATTEMPT_MARKER"
		printf 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nHTTP/1.1 503 body text\n'
		exit 0
		;;
esac
` +
		"printf 'HTTP/1.1 200 OK\\r\\nHost: %s\\r\\n\\r\\nbody " + "```" + `\r\nbare\rreturn\r\n' "${PROXY_URL#http://}"` + "\n" +
		`printf '* Trying 127.0.0.1:10000...\r\n} [316 bytes data]\r\n> GET /post HTTP/1.1\r\n> \r\n< HTTP/1.1 200 OK\r\n< \r\ncurl: (56) preserved error\r\n' >&2` + "\n"
	require.NoError(t, os.WriteFile(fakeCurl, []byte(curlScript), 0o600))
	require.NoError(t, os.Chmod(fakeCurl, 0o700)) // #nosec G302 -- the fake curl is executed by the public generator integration test.
	t.Setenv("PATH", curlBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	boePath := fakeBoe(t)
	loopbackListener, err := net.Listen("tcp", "127.0.0.1:0")
	if errors.Is(err, syscall.EPERM) || (err != nil && strings.Contains(err.Error(), "operation not permitted")) {
		t.Skip("sandbox does not allow loopback listeners")
	}
	require.NoError(t, err)
	require.NoError(t, loopbackListener.Close())
	config := map[string]any{
		"endpoint":         "${PROXY_URL}",
		"admin":            "${ADMIN_URL}",
		"upstream":         "https://${UPSTREAM_ADDRESS}",
		"workingDirectory": "${WORK_DIR}",
		"preparedFile":     "${WORK_DIR}/prepared.conf",
		"authored":         map[string]any{"names": []any{"O'Reilly", map[string]any{"enabled": true}}},
	}
	commentMarker := filepath.Join(t.TempDir(), "comment-ran")
	comment := "Send a verbose request:\r\n  Keep $HOME, $(touch " + commentMarker + "), `literal`, and O'Reilly as text.  \r\n\r\n"
	afterRetryMarker := filepath.Join(t.TempDir(), "after-retry")
	wrappedArgv := []string{"printf", "%s\\n", strings.Repeat("x", 64), strings.Repeat("y", 50), "O'Reilly", `"$HOME" && printf bad || printf worse`, "", "back\\slash", "first line\n" + strings.Repeat("last", 12), strings.Repeat("界", 35)}
	manifest := extensions.Manifest{
		Name: "example-test", Version: "1.0.0", Categories: []string{"Examples"},
		Author: "Test", Description: "Example generator test", LongDescription: "Generator test.",
		Type: extensions.TypeLua, Lua: &extensions.Lua{Inline: "function envoy_on_request(handle) end"},
		Tags: []string{"example"}, License: "Apache-2.0",
		Examples: []extensions.Example{{
			Title: "Executable", Description: "Runs a fixture.", Code: "old transcript\n",
			Config: &config,
			PreStart: []extensions.ExampleCommand{
				{Argv: []string{"sh", "${WORK_DIR}/prepare.sh"}},
				{Argv: []string{"sh", "-c", "printf 'expected-exit\\n' >&2; exit 7"}, ExpectedExit: 7},
			},
			Commands: []extensions.ExampleCommand{
				{Argv: []string{"sh", "run.sh"}},
				{Argv: []string{"printf", "%s\\n", "${PROXY_URL}"}},
				{Argv: []string{"printf", "%s\\n", "${WORK_DIR}"}},
				{Argv: []string{"curl", "--verbose", "--user-agent", "boe-example", "${PROXY_URL}/post"}, Comment: comment},
				{Argv: []string{"curl", "--disable", "--silent", "--include", "${PROXY_URL}/retry-503"}, Retry: &extensions.ExampleRetry{HTTPStatus: 503, MaxAttempts: 3}},
				{Argv: []string{"curl", "--disable", "-sv", "${PROXY_URL}/retry-200"}, Retry: &extensions.ExampleRetry{HTTPStatus: 200, MaxAttempts: 3}},
				{Argv: []string{"curl", "--disable", "--output", "/dev/null", "--write-out", "HTTP/%{http_version} %{http_code}\\nContent-Type: text/plain\\n\\n", "${PROXY_URL}/retry-writeout"}, Retry: &extensions.ExampleRetry{HTTPStatus: 503, MaxAttempts: 1}},
				{Argv: []string{"touch", afterRetryMarker}},
			},
			VolatileHeaders: []string{"x-test-date", "x-test-duration"},
		}, {
			Title: "Scalar config", Description: "Keeps scalar configuration compact.",
			Config:   mapConfig(map[string]any{"enabled": true}),
			Commands: []extensions.ExampleCommand{{Argv: []string{"printf", "%s\\n", "scalar"}}},
		}, {
			Title: "Empty config", Description: "Keeps empty configuration compact.",
			Config:   mapConfig(map[string]any{}),
			Commands: []extensions.ExampleCommand{{Argv: []string{"printf", "%s\\n", "empty"}}},
		}, {
			Title: "Wrapped command", Description: "Preserves argument values across continuations.",
			Config:   mapConfig(map[string]any{}),
			Commands: []extensions.ExampleCommand{{Argv: wrappedArgv}},
		}},
	}
	writeManifest := func() {
		data, marshalErr := yaml.Marshal(&manifest)
		require.NoError(t, marshalErr)
		require.NoError(t, os.WriteFile(filepath.Join(extensionPath, "manifest.yaml"), data, 0o600))
	}
	manifestPath := filepath.Join(extensionPath, "manifest.yaml")
	writeManifest()
	noFixturePath := t.TempDir()
	noFixtureManifest := extensions.Manifest{
		Name: "example-test-no-fixtures", Version: "1.0.0", Categories: []string{"Examples"},
		Author: "Test", Description: "Example generator test", LongDescription: "Generator test.",
		Type: extensions.TypeLua, Lua: &extensions.Lua{Inline: "function envoy_on_request(handle) end"},
		Tags: []string{"example"}, License: "Apache-2.0",
		Examples: []extensions.Example{{
			Title: "No fixtures", Description: "Runs commands without staged fixtures.",
			Config: mapConfig(map[string]any{"preparedFile": "${WORK_DIR}/prepared.conf"}),
			PreStart: []extensions.ExampleCommand{
				{Argv: []string{"printf", "%s\\n", "${WORK_DIR}"}},
				{Argv: []string{"sh", "-c", `printf ready > "${WORK_DIR}/prepared.conf"`}},
			},
			Commands: []extensions.ExampleCommand{{Argv: []string{"printf", "%s\\n", "${WORK_DIR}"}}},
		}},
	}
	noFixtureManifestPath := filepath.Join(noFixturePath, "manifest.yaml")
	noFixtureBytes, err := yaml.Marshal(&noFixtureManifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(noFixtureManifestPath, noFixtureBytes, 0o600))
	opts := &options{
		extensions: stringList{extensionPath, noFixturePath}, boe: boePath, envoyPath: "/fake/envoy",
		envoyVersion: "1.38.0", timeout: 5 * time.Second,
	}
	t.Setenv("GEN_EXPECT_PRESTART", "ready")
	startupMarker := filepath.Join(t.TempDir(), "boe-started")
	t.Setenv("GEN_BOE_STARTED_FILE", startupMarker)
	boeArgsFile := filepath.Join(t.TempDir(), "boe-args")
	t.Setenv("GEN_BOE_ARGS_FILE", boeArgsFile)
	retryAttemptMarker := filepath.Join(t.TempDir(), "retry-attempted")
	t.Setenv("GEN_RETRY_ATTEMPT_MARKER", retryAttemptMarker)

	before, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	fixtureLink := filepath.Join(extensionPath, "examples", "link")
	require.NoError(t, os.Symlink(manifestPath, fixtureLink))
	err = generate(context.Background(), opts, &strings.Builder{}, &strings.Builder{})
	require.ErrorContains(t, err, "fixture symlinks are not supported")
	afterSymlinkFailure, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, before, afterSymlinkFailure, "rejected fixtures must not update the manifest")
	require.NoError(t, os.Remove(fixtureLink))
	manifest.Examples[0].PreStart[1].ExpectedExit = 0
	writeManifest()
	failureBefore, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	preStartFailure := generate(context.Background(), opts, &strings.Builder{}, &strings.Builder{})
	require.ErrorContains(t, preStartFailure, "pre-start command")
	require.ErrorContains(t, preStartFailure, "exited 7, expected 0")
	_, err = os.Stat(startupMarker)
	require.ErrorIs(t, err, os.ErrNotExist, "BOE must not start after a failed pre-start command")
	failureAfter, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, failureBefore, failureAfter, "pre-start failures must not update manifests")
	manifest.Examples[0].PreStart[1].ExpectedExit = 7
	writeManifest()
	manifest.Examples[0].PreStart = append(manifest.Examples[0].PreStart, extensions.ExampleCommand{
		Argv: []string{"sh", "-c", "sleep 5 & wait"}, ExpectedExit: 137,
	})
	writeManifest()
	timeoutBefore, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	opts.timeout = 100 * time.Millisecond
	timeoutStart := time.Now()
	timeoutErr := generate(context.Background(), opts, &strings.Builder{}, &strings.Builder{})
	require.ErrorContains(t, timeoutErr, "timed out or canceled")
	require.ErrorContains(t, timeoutErr, "pre-start command 3")
	require.Less(t, time.Since(timeoutStart), 2*time.Second, "timeout must terminate shell children holding the output pipes")
	_, err = os.Stat(startupMarker)
	require.ErrorIs(t, err, os.ErrNotExist, "timed out pre-start commands must not start BOE")
	timeoutAfter, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, timeoutBefore, timeoutAfter, "timed out pre-start commands must not update manifests")
	opts.timeout = 5 * time.Second
	manifest.Examples[0].PreStart = manifest.Examples[0].PreStart[:2]
	writeManifest()
	var checkOutput, checkDiff strings.Builder
	opts.check = true
	err = generate(context.Background(), opts, &checkOutput, &checkDiff)
	require.ErrorContains(t, err, "generated examples are stale")
	afterCheck, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, before, afterCheck)
	require.Contains(t, checkDiff.String(), "generated examples differ")
	require.Contains(t, checkDiff.String(), "```sh")
	require.Contains(t, checkDiff.String(), "pre-start stdout")

	opts.check = false
	var updateOutput, updateErrors strings.Builder
	require.NoError(t, generate(context.Background(), opts, &updateOutput, &updateErrors))
	generated, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Contains(t, string(generated), "Date: Tue, 03 Jan 2006 16:05:06 GMT")
	require.Contains(t, string(generated), "<!-- stdout; Line endings: CRLF,CRLF,CRLF,CRLF,CRLF,CRLF,CRLF -->")
	var generatedManifest extensions.Manifest
	require.NoError(t, yaml.Unmarshal(generated, &generatedManifest))
	require.Equal(t, []string{"x-test-date", "x-test-duration"}, generatedManifest.Examples[0].VolatileHeaders)
	transcript := generatedManifest.Examples[0].Code
	require.Contains(t, transcript, "boe run --extension example-test")
	require.NotContains(t, transcript, "--local")
	require.Contains(t, transcript, "# Terminal 1 (from the repository root)")
	require.Contains(t, transcript, "# Terminal 2 (after Envoy is ready, from the repository root)")
	require.Contains(t, transcript, "http://localhost:10000")
	require.Contains(t, transcript, "http://127.0.0.1:9901")
	require.Contains(t, transcript, "https://httpbin.org:443")
	require.Contains(t, transcript, "http://localhost:10000")
	require.Contains(t, transcript, "\n.\n```")
	root, err := moduleRoot()
	require.NoError(t, err)
	relExtension, err := filepath.Rel(root, extensionPath)
	require.NoError(t, err)
	displayFixturePath := filepath.ToSlash(filepath.Join(relExtension, "examples"))
	require.Contains(t, transcript, "cd \\\n  "+shellQuote(displayFixturePath)+"\n")
	require.Contains(t, transcript, "pre-start stdout")
	require.Contains(t, transcript, "pre-start stderr")
	require.Contains(t, transcript, "expected-exit\\n")
	require.Less(t, strings.Index(transcript, "pre-start stdout"), strings.Index(transcript, "boe run"), "pre-start output should precede BOE startup")
	require.Less(t, strings.Index(transcript, "expected-exit\\n"), strings.Index(transcript, "boe run"), "all pre-start commands should precede BOE startup")
	require.Contains(t, transcript, "```text\n"+displayFixturePath+"\npre-start stdout\n```")
	require.Contains(t, transcript, "```text\npre-start stderr\n```")
	require.Contains(t, transcript, `"`+displayFixturePath+`/prepared.conf"`)
	require.NotContains(t, transcript, "$ ")
	require.Contains(t, transcript, `"workingDirectory": "`+displayFixturePath+`"`)
	require.Contains(t, transcript, "--config '\n  {\n")
	require.Contains(t, transcript, `"authored": {`)
	require.Contains(t, transcript, `"names": [`)
	require.Contains(t, transcript, "# Send a verbose request:\n#   Keep $HOME, $(touch "+commentMarker+"), `literal`, and O'Reilly as text.\n#\n")
	require.NotContains(t, transcript, "$ ")
	_, err = os.Stat(commentMarker)
	require.ErrorIs(t, err, os.ErrNotExist, "comment text must never be evaluated")
	require.Contains(t, generatedManifest.Examples[1].Code, `'{"enabled":true}'`)
	require.Contains(t, generatedManifest.Examples[2].Code, "'{}'")
	require.Contains(t, generatedManifest.Examples[1].Code, `printf '%s\n' scalar`)
	require.Contains(t, generatedManifest.Examples[2].Code, `printf '%s\n' empty`)
	noFixtureGenerated, err := os.ReadFile(noFixtureManifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	var noFixtureUpdated extensions.Manifest
	require.NoError(t, yaml.Unmarshal(noFixtureGenerated, &noFixtureUpdated))
	require.Contains(t, noFixtureUpdated.Examples[0].Code, `printf '%s\n' .`)
	require.Contains(t, noFixtureUpdated.Examples[0].Code, "boe run --extension example-test-no-fixtures")
	require.Contains(t, noFixtureUpdated.Examples[0].Code, "# Terminal 1\n")
	require.Contains(t, noFixtureUpdated.Examples[0].Code, "# Terminal 2 (after Envoy is ready)\n")
	require.NotContains(t, noFixtureUpdated.Examples[0].Code, "from the repository root")
	boeArgs, err := os.ReadFile(boeArgsFile) // #nosec G304 -- test reads its own temporary argument capture.
	require.NoError(t, err)
	require.Contains(t, string(boeArgs), "\x00--local\x00"+noFixturePath+"\x00", "generation must execute the local checkout")
	require.NotContains(t, string(boeArgs), "\x00--extension\x00", "generation must not fetch a published extension")
	require.Equal(t, 2, strings.Count(noFixtureUpdated.Examples[0].Code, "```text\n.\n```"), "pre-start and later commands without fixtures use the repository-root work directory")
	requireShellJSONArgument(t, transcript, map[string]any{
		"endpoint":         "http://localhost:10000",
		"admin":            "http://127.0.0.1:9901",
		"upstream":         "https://httpbin.org:443",
		"workingDirectory": displayFixturePath,
		"preparedFile":     displayFixturePath + "/prepared.conf",
		"authored":         map[string]any{"names": []any{"O'Reilly", map[string]any{"enabled": true}}},
	})
	require.NoError(t, os.Remove(afterRetryMarker))
	assertRetryFailure := func(mutate func(*extensions.Example), expected ...string) {
		require.NoError(t, yaml.Unmarshal(generated, &manifest))
		mutate(&manifest.Examples[0])
		writeManifest()
		for _, marker := range []string{afterRetryMarker, retryAttemptMarker} {
			removeErr := os.Remove(marker)
			if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				require.NoError(t, removeErr)
			}
		}
		beforeFailure, readErr := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
		require.NoError(t, readErr)
		var failureOutput, failureErrors strings.Builder
		failureErr := generate(context.Background(), opts, &failureOutput, &failureErrors)
		require.Error(t, failureErr)
		for _, fragment := range expected {
			require.ErrorContains(t, failureErr, fragment)
		}
		afterFailure, readErr := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
		require.NoError(t, readErr)
		require.Equal(t, beforeFailure, afterFailure, "retry failures must leave the manifest unchanged")
		_, readErr = os.Stat(afterRetryMarker)
		require.ErrorIs(t, readErr, os.ErrNotExist, "commands after a failed retry must not run")
		_, readErr = os.Stat(retryAttemptMarker)
		require.ErrorIs(t, readErr, os.ErrNotExist, "invalid retry capture modes must fail before executing curl")
		require.NoError(t, os.WriteFile(manifestPath, generated, 0o600)) // #nosec G703 -- test restores its own temporary manifest.
	}
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Retry.MaxAttempts = 1
	}, "exhausted after 1 attempts", "last HTTP status 200")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Retry.MaxAttempts = 1
		example.Commands[4].Argv = []string{"curl", "--disable", "--include", "${PROXY_URL}/retry-body-503"}
	}, "exhausted after 1 attempts", "last HTTP status 200")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--include", "${PROXY_URL}/retry-no-http"}
	}, "contains no complete initial HTTP response")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--include", "${PROXY_URL}/retry-exit"}
	}, "exited 5, expected 0")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "${PROXY_URL}/retry-invalid"}
	}, "validate retry capture mode")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--include", "${PROXY_URL}/retry-invalid"}
	}, "validate retry capture mode")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--write-out", "HTTP/%{http_version} %{http_code}\\n", "${PROXY_URL}/retry-invalid"}
	}, "validate retry capture mode")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--output", "/dev/null", "--write-out", "HTTP/%{http_code}\\n", "${PROXY_URL}/retry-invalid"}
	}, "validate retry capture mode")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--include", "-L", "${PROXY_URL}/retry-invalid"}
	}, "validate retry capture mode")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--include", "${PROXY_URL}/retry-invalid", "--next", "${PROXY_URL}/retry-invalid"}
	}, "validate retry capture mode")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--include", "${PROXY_URL}/retry-invalid", "${PROXY_URL}/retry-invalid"}
	}, "validate retry capture mode")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--include", "--verbose", "${PROXY_URL}/retry-disagree"}
	}, "status captures disagree")
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--include", "--verbose", "${PROXY_URL}/retry-missing-stream"}
	}, "one capture is incomplete")
	for _, argv := range [][]string{
		{"curl", "--disable", "--include", "--retry", "1", "${PROXY_URL}/retry-invalid"},
		{"curl", "--disable", "--include", "${PROXY_URL}/retry-invalid/{one,two}"},
		{"curl", "--disable", "--include", "https://example.invalid/retry-invalid"},
	} {
		assertRetryFailure(func(example *extensions.Example) {
			example.Commands[4].Argv = argv
		}, "validate retry capture mode")
	}
	oldTimeout := opts.timeout
	opts.timeout = time.Second
	assertRetryFailure(func(example *extensions.Example) {
		example.Commands[4].Argv = []string{"curl", "--disable", "--include", "${PROXY_URL}/retry-slow"}
	}, "timed out or canceled")
	opts.timeout = oldTimeout
	wrappedBlock := strings.SplitN(strings.Split(generatedManifest.Examples[3].Code, "```sh\n")[2], "\n```", 2)[0]
	require.Equal(t, wrappedArgv[1:], copiedCommandArguments(t, wrappedBlock, "printf"), "wrapping must preserve every argument")
	wrappedCommand := wrappedBlock[strings.Index(wrappedBlock, "\nprintf ")+1:]
	require.Contains(t, wrappedCommand, " \\\n  ")
	for _, line := range strings.Split(wrappedCommand, "\n") {
		require.LessOrEqual(t, utf8.RuneCountInString(line), 80, "command line: %s", line)
	}
	require.Contains(t, wrappedCommand, `'"$HOME" && printf bad || printf worse'`, "shell operators inside arguments remain literal")
	for _, unwanted := range []string{"export ", "-serve-upstream", "-upstream-address", "--listen-port", "--admin-port", "--cluster-insecure", "--test-upstream-cluster", "BOE_PID", "UPSTREAM_PID", "mktemp"} {
		require.NotContains(t, transcript, unwanted)
	}
	require.Contains(t, transcript, "````text\nHTTP/1.1 200 OK\nHost: localhost:10000")
	require.Contains(t, transcript, "<!-- stderr; Line endings:")
	require.Contains(t, transcript, "> GET /post HTTP/1.1")
	require.Contains(t, transcript, "< HTTP/1.1 200 OK")
	require.Contains(t, transcript, "curl: (56) preserved error")
	require.Contains(t, transcript, "# Repeat until HTTP 503, up to 3 attempts.")
	require.Contains(t, transcript, "# Repeat until HTTP 200, up to 3 attempts.")
	require.Contains(t, transcript, "--output /dev/null")
	require.Contains(t, transcript, `HTTP/%{http_version} %{http_code}\nContent-Type: text/plain\n\n`)
	require.Contains(t, transcript, "attempt-2-status-503")
	require.Contains(t, transcript, "attempt-2-status-200")
	require.Contains(t, transcript, "attempt-2-stderr-503")
	require.Contains(t, transcript, "attempt-2-stderr-200")
	require.NotContains(t, transcript, "attempt-1-status-200")
	require.NotContains(t, transcript, "attempt-1-status-503")
	require.NotContains(t, transcript, "attempt-1-stderr-200")
	require.NotContains(t, transcript, "attempt-1-stderr-503")
	require.NotContains(t, transcript, "* Trying")
	require.NotContains(t, transcript, "316 bytes data")
	require.Contains(t, transcript, "Trailing whitespace: ")
	require.Contains(t, transcript, "Bare CR offsets: ")
	comparisonCandidate := strings.ReplaceAll(transcript, "Tue, 03 Jan 2006 16:05:06 GMT", "Wed, 04 Jan 2006 17:06:07 GMT")
	comparisonCandidate = strings.ReplaceAll(comparisonCandidate, "10ms", "25ms")
	equal, compareErr := equivalentTranscript(transcript, comparisonCandidate, generatedManifest.Examples[0].Comparison, generatedManifest.Examples[0].VolatileHeaders)
	require.NoError(t, compareErr)
	require.True(t, equal, "captured volatile headers should normalize in the generated transcript")
	require.NoError(t, yaml.Unmarshal(generated, &manifest))
	beforeEquivalent, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	fixture := "printf 'HTTP/1.1 200 OK\\r\\nDate: Wed, 04 Jan 2006 17:06:07 GMT\\r\\nX-Test-Date: Wed, 04 Jan 2006 17:06:07 GMT\\r\\nX-Test-Duration: 25ms\\r\\nX-Test-Exact: stable\\r\\n\\r\\nbody\\r\\n'\n"
	require.NoError(t, os.WriteFile(fixturePath, []byte(fixture), 0o600))
	var equalOutput, equalErrors strings.Builder
	opts.check = true
	err = generate(context.Background(), opts, &equalOutput, &equalErrors)
	if err != nil {
		t.Log(equalErrors.String())
	}
	require.NoError(t, err)
	afterEquivalent, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, beforeEquivalent, afterEquivalent, "volatile response headers should preserve the reviewed transcript")
	require.Contains(t, equalOutput.String(), "generated examples are current")

	var commentChangedManifest extensions.Manifest
	require.NoError(t, yaml.Unmarshal(beforeEquivalent, &commentChangedManifest))
	commentChangedManifest.Examples[0].Commands[3].Comment = "A changed authored note."
	changedManifestBytes, err := yaml.Marshal(&commentChangedManifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, changedManifestBytes, 0o600))
	beforeCommentCheck, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	var commentCheckOutput, commentCheckDiff strings.Builder
	require.ErrorContains(t, generate(context.Background(), opts, &commentCheckOutput, &commentCheckDiff), "generated examples are stale")
	afterCommentCheck, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, beforeCommentCheck, afterCommentCheck, "authored comment changes must be literal and check mode must not write")
	require.Contains(t, commentCheckDiff.String(), "A changed authored note.")
	require.NoError(t, os.WriteFile(manifestPath, beforeEquivalent, 0o600)) // #nosec G703 -- restores the test's own temporary manifest.

	require.NoError(t, yaml.Unmarshal(beforeEquivalent, &manifest))
	fixtureLF := strings.ReplaceAll(fixture, `\r\n`, `\n`)
	require.NoError(t, os.WriteFile(fixturePath, []byte(fixtureLF), 0o600))
	var lineEndingOutput, lineEndingDiff strings.Builder
	require.ErrorContains(t, generate(context.Background(), opts, &lineEndingOutput, &lineEndingDiff), "generated examples are stale")
	require.Contains(t, lineEndingDiff.String(), "Line endings: LF")
	afterLineEnding, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, beforeEquivalent, afterLineEnding, "line ending changes must be reported without writing in check mode")
	fixtureWithoutFinalNewline := strings.TrimSuffix(fixtureLF, `\n'`+"\n") + "'\n"
	require.NoError(t, os.WriteFile(fixturePath, []byte(fixtureWithoutFinalNewline), 0o600))
	var trailingNewlineOutput, trailingNewlineDiff strings.Builder
	require.ErrorContains(t, generate(context.Background(), opts, &trailingNewlineOutput, &trailingNewlineDiff), "generated examples are stale")
	require.Contains(t, trailingNewlineDiff.String(), "No trailing newline")
	afterTrailingNewline, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, beforeEquivalent, afterTrailingNewline, "a missing final newline must be reported without writing in check mode")
	require.NoError(t, os.WriteFile(fixturePath, []byte(fixture), 0o600))
	literalBackslashScript := strings.Replace(curlScript, `bare\rreturn`, `bare\\rreturn`, 1)
	require.NoError(t, os.WriteFile(fakeCurl, []byte(literalBackslashScript), 0o600))
	var bareCROutput, bareCRDiff strings.Builder
	require.ErrorContains(t, generate(context.Background(), opts, &bareCROutput, &bareCRDiff), "generated examples are stale")
	require.Contains(t, bareCRDiff.String(), "Bare CR offsets: ")
	require.NoError(t, os.WriteFile(fakeCurl, []byte(strings.Replace(curlScript, `> \r\n`, `>\r\n`, 1)), 0o600))
	var whitespaceOutput, whitespaceDiff strings.Builder
	require.ErrorContains(t, generate(context.Background(), opts, &whitespaceOutput, &whitespaceDiff), "generated examples are stale")
	require.Contains(t, whitespaceDiff.String(), "Trailing whitespace: ")
	require.NoError(t, os.WriteFile(fakeCurl, []byte(curlScript), 0o600))
	require.NoError(t, os.WriteFile(fixturePath, []byte(strings.ReplaceAll(fixture, "25ms", "25.5 ms")), 0o600))
	beforeFormatChange, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	var formatOutput, formatDiff strings.Builder
	require.ErrorContains(t, generate(context.Background(), opts, &formatOutput, &formatDiff), "generated examples are stale")
	afterFormatChange, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, beforeFormatChange, afterFormatChange, "format changes should be reported without writing in check mode")

	manifest.Examples[0].Code = "old first transcript\n"
	manifest.Examples = append(manifest.Examples, extensions.Example{
		Title: "Fails later", Description: "Failure should not partially update the manifest.", Code: "old second transcript\n",
		Config:   mapConfig(map[string]any{}),
		Commands: []extensions.ExampleCommand{{Argv: []string{"sh", "-c", "printf failure >&2; exit 5"}}},
	})
	writeManifest()
	beforeFailure, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	opts.check = false
	err = generate(context.Background(), opts, &strings.Builder{}, &strings.Builder{})
	require.ErrorContains(t, err, `command "sh -c 'printf failure >&2; exit 5'" exited 5, expected 0`)
	require.ErrorContains(t, err, "stderr: failure")
	afterFailure, err := os.ReadFile(manifestPath) // #nosec G304 -- test reads its own temporary manifest.
	require.NoError(t, err)
	require.Equal(t, beforeFailure, afterFailure, "a later command failure must prevent all manifest writes")
}

func mapConfig(config map[string]any) *map[string]any {
	return &config
}

func requireShellJSONArgument(t *testing.T, transcript string, want map[string]any) {
	t.Helper()
	var firstBlock string
	for _, block := range strings.Split(transcript, "```sh\n")[1:] {
		if strings.Contains(block, "\nboe run ") {
			firstBlock = strings.SplitN(block, "\n```", 2)[0]
			break
		}
	}
	require.NotEmpty(t, firstBlock, "generated transcript must contain a BOE command block")
	args := copiedCommandArguments(t, firstBlock, "boe")
	configIndex := -1
	for i, arg := range args {
		if arg == "--config" {
			configIndex = i + 1
			break
		}
	}
	require.Positive(t, configIndex, "copied command should pass --config")
	require.Less(t, configIndex, len(args))
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(args[configIndex]), &got))
	require.Equal(t, want, got, "shell quoting must preserve the configuration JSON value")
}

func copiedCommandArguments(t *testing.T, block, executable string) []string {
	t.Helper()
	argsFile := filepath.Join(t.TempDir(), "boe-args")
	script := executable + "() { command printf '%s\\000' \"$@\" > \"$GEN_EXAMPLE_ARGS\"; }\n" + block
	cmd := exec.Command("/bin/sh", "-c", script) // #nosec G204 -- executes generated shell with a capture function.
	root, err := moduleRoot()
	require.NoError(t, err)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GEN_EXAMPLE_ARGS="+argsFile)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	encodedArgs, err := os.ReadFile(argsFile) // #nosec G304 -- test reads its own temporary file.
	require.NoError(t, err)
	return strings.Split(strings.TrimSuffix(string(encodedArgs), "\x00"), "\x00")
}

func fakeBoe(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "fake-boe")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\000' \"$@\" > \"$GEN_BOE_ARGS_FILE\"\nGO_WANT_GEN_EXAMPLES_HELPER=1 exec %s -test.run=TestGenExamplesHelperProcess\n", shellQuote(executable))
	require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
	require.NoError(t, os.Chmod(path, 0o700)) // #nosec G302 -- the test helper wrapper must be executable by the generator.
	return path
}

func TestGenExamplesHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_GEN_EXAMPLES_HELPER") != "1" {
		return
	}
	if marker := os.Getenv("GEN_BOE_STARTED_FILE"); marker != "" {
		if err := os.WriteFile(marker, []byte("started\n"), 0o600); err != nil { // #nosec G703 -- marker is in the parent test's temporary directory.
			t.Fatal(err)
		}
	}
	if expected := os.Getenv("GEN_EXPECT_PRESTART"); expected != "" {
		argsData, err := os.ReadFile(os.Getenv("GEN_BOE_ARGS_FILE")) // #nosec G304,G703 -- path is in the parent test's temporary directory.
		if err != nil {
			t.Fatalf("read captured BOE arguments: %v", err)
		}
		args := strings.Split(strings.TrimSuffix(string(argsData), "\x00"), "\x00")
		var configValue string
		for i, arg := range args {
			if arg == "--config" && i+1 < len(args) {
				configValue = args[i+1]
				break
			}
		}
		var config map[string]any
		if decodeErr := json.Unmarshal([]byte(configValue), &config); decodeErr != nil {
			t.Fatalf("decode BOE config: %v", decodeErr)
		}
		if config["workingDirectory"] != nil || config["preparedFile"] != nil {
			preparedPath, ok := config["preparedFile"].(string)
			if !ok || preparedPath == "" {
				t.Fatal("BOE config did not include the pre-started file")
			}
			prepared, preparedErr := os.ReadFile(preparedPath) // #nosec G304 -- path is derived from BOE's isolated test config directory.
			if preparedErr != nil {
				t.Fatalf("read pre-started config before BOE startup: %v", preparedErr)
			}
			if string(prepared) != expected {
				t.Fatalf("pre-started config = %q, want %q", prepared, expected)
			}
		}
	}
	address := os.Getenv("BOE_ADMIN_ADDRESS")
	http.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "LIVE")
	})
	server := &http.Server{Addr: address, Handler: http.DefaultServeMux, ReadHeaderTimeout: time.Second}
	if err := server.ListenAndServe(); err != nil {
		t.Fatal(err)
	}
}
