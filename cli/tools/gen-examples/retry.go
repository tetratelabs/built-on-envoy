// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This selector accepts only complete curl response framing so retries cannot select
// status-looking request or body text as an HTTP result.
package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type retryCapture struct {
	include  bool
	verbose  bool
	writeOut bool
}

// retryHTTPStatus returns the final status from curl --include stdout or filtered
// curl --verbose stderr. A retry is safe only when the response framing is complete.
func retryHTTPStatus(stdout, filteredStderr string, capture retryCapture) (int, error) {
	var (
		stdoutStatus int
		stdoutFound  bool
		stderrStatus int
		stderrFound  bool
		err          error
	)
	if capture.include || capture.writeOut {
		stdoutStatus, stdoutFound, err = includedHTTPStatus(stdout)
		if err != nil {
			return 0, fmt.Errorf("parse curl stdout status capture: %w", err)
		}
	}
	if capture.verbose {
		stderrStatus, stderrFound, err = verboseHTTPStatus(filteredStderr)
		if err != nil {
			return 0, fmt.Errorf("parse curl --verbose stderr: %w", err)
		}
	}
	if capture.verbose && (capture.include || capture.writeOut) && (!stdoutFound || !stderrFound) {
		return 0, fmt.Errorf("curl selected both stdout and stderr status capture, but one capture is incomplete")
	}

	if !stdoutFound && !stderrFound {
		return 0, fmt.Errorf("curl output contains no complete initial HTTP response")
	}
	if stdoutFound && stderrFound && stdoutStatus != stderrStatus {
		return 0, fmt.Errorf("curl status captures disagree: %d and %d", stdoutStatus, stderrStatus)
	}
	if stdoutFound {
		return stdoutStatus, nil
	}
	return stderrStatus, nil
}

// retryCaptureMode accepts only curl forms whose selected output stream cannot be
// mistaken for an arbitrary response body while selecting a retry status.
func retryCaptureMode(argv []string) (retryCapture, error) {
	if len(argv) == 0 || filepath.Base(argv[0]) != "curl" {
		return retryCapture{}, fmt.Errorf("retry is supported only for a direct curl executable")
	}
	if len(argv) < 2 || (argv[1] != "--disable" && argv[1] != "-q" && !strings.HasPrefix(argv[1], "-q")) {
		return retryCapture{}, fmt.Errorf("retry requires curl --disable or -q as its first option to disable implicit curl configuration")
	}

	var (
		capture                retryCapture
		output                 string
		hasOutput              bool
		writeOut               string
		hasWriteOut            bool
		urlCount               int
		hasHTTPSURL            bool
		hasGlobURL             bool
		globOff                bool
		suppressConnectHeaders bool
	)
	options, err := parseRetryOptions(argv[1:])
	if err != nil {
		return retryCapture{}, err
	}
	for _, option := range options {
		switch option.name {
		case "", "--url":
			if err := addRetryURL(option.value, &urlCount, &hasHTTPSURL, &hasGlobURL); err != nil {
				return retryCapture{}, err
			}
		case "--include":
			capture.include = true
		case "--verbose":
			capture.verbose = true
		case "--suppress-connect-headers":
			suppressConnectHeaders = true
		case "--globoff":
			globOff = true
		case "--output":
			if hasOutput {
				return retryCapture{}, fmt.Errorf("retry does not support multiple curl --output options")
			}
			hasOutput, output = true, option.value
		case "--write-out":
			if hasWriteOut {
				return retryCapture{}, fmt.Errorf("retry does not support multiple curl --write-out options")
			}
			hasWriteOut, writeOut, capture.writeOut = true, option.value, true
		}
	}

	if urlCount != 1 {
		return retryCapture{}, fmt.Errorf("retry requires exactly one HTTP(S) URL operand")
	}
	if hasGlobURL && !globOff {
		return retryCapture{}, fmt.Errorf("retry URL with curl glob characters requires --globoff or -g")
	}
	if capture.include && capture.writeOut {
		return retryCapture{}, fmt.Errorf("retry does not support curl --include together with --write-out")
	}
	if capture.include && hasOutput {
		return retryCapture{}, fmt.Errorf("retry does not support curl --include with --output")
	}
	if capture.include && hasHTTPSURL && !suppressConnectHeaders {
		return retryCapture{}, fmt.Errorf("retry curl --include for HTTPS requires --suppress-connect-headers")
	}
	if capture.writeOut {
		if !hasOutput || output != "/dev/null" {
			return retryCapture{}, fmt.Errorf("retry curl --write-out requires --output /dev/null")
		}
		if !strings.HasPrefix(writeOut, "HTTP/%{http_version} %{http_code}\n") && !strings.HasPrefix(writeOut, `HTTP/%{http_version} %{http_code}\n`) {
			return retryCapture{}, fmt.Errorf("retry curl --write-out must begin with HTTP/%%{http_version} %%{http_code} followed by a newline")
		}
	}
	if !capture.include && !capture.verbose && !capture.writeOut {
		return retryCapture{}, fmt.Errorf("retry requires curl --include, --verbose, or canonical --write-out status capture")
	}
	return capture, nil
}

type retryOption struct {
	name  string
	value string
}

// Short clusters and long options share validation after aliases are expanded.
// Value-taking short options consume the rest of their cluster, as curl does.
func parseRetryOptions(argv []string) ([]retryOption, error) {
	var options []retryOption
	endOfOptions := false
	for index := 0; index < len(argv); index++ {
		arg := argv[index]
		if endOfOptions || !strings.HasPrefix(arg, "-") || arg == "-" {
			options = append(options, retryOption{value: arg})
			continue
		}
		if arg == "--" {
			endOfOptions = true
			continue
		}
		long := strings.HasPrefix(arg, "--")
		for position := 1; position < len(arg); position++ {
			var name, value, spelling string
			var hasValue bool
			if long {
				name, value, hasValue = strings.Cut(arg, "=")
				spelling = arg
			} else {
				spelling = "-" + string(arg[position])
				name = retryShortOptionNames[arg[position]]
				if name == "" {
					return nil, retryUnsupportedOption(spelling)
				}
			}
			takesValue, err := retryOptionTakesValue(name, spelling)
			if err != nil {
				return nil, err
			}
			if takesValue {
				if !long && position+1 < len(arg) {
					value, hasValue = arg[position+1:], true
				}
				value, index, err = retryOptionValue(argv, index, spelling, value, hasValue)
				if err != nil {
					return nil, err
				}
			} else if hasValue {
				return nil, retryUnsupportedOption(spelling)
			}
			options = append(options, retryOption{name: name, value: value})
			if long || takesValue {
				break
			}
		}
	}
	return options, nil
}

// Short-only options stay distinct so normalization cannot admit new long spellings.
var retryShortOptionNames = map[byte]string{
	'q': "--disable", 's': "--silent", 'S': "--show-error", 'k': "--insecure",
	'f': "--fail", 'G': "--get", 'g': "--globoff", 'i': "--include", 'v': "--verbose",
	'I': "--head", 'L': "--location", 'Z': "--parallel", 'K': "--config",
	'D': "-D", 'p': "--proxytunnel", 'o': "--output", 'w': "--write-out",
	'A': "--user-agent", 'm': "--max-time", 'X': "--request", 'H': "--header",
	'd': "--data", 'F': "--form", 'b': "--cookie", 'c': "--cookie-jar",
	'e': "--referer", 'x': "--proxy", 'u': "--user", 'U': "--proxy-user",
	'E': "--cert", 'r': "--range", 'T': "-T",
}

func retryOptionTakesValue(name, spelling string) (bool, error) {
	switch name {
	case "--include", "--verbose", "--disable", "--suppress-connect-headers", "--globoff",
		"--silent", "--show-error", "--insecure", "--fail", "--fail-with-body", "--get", "--compressed", "--path-as-is", "--http1.0", "--http1.1", "--http2", "--http2-prior-knowledge", "--ipv4", "--ipv6", "--no-alpn", "--no-npn":
		return false, nil
	case "--output", "--write-out", "--url",
		"--user-agent", "--max-time", "--connect-timeout", "--request", "--header", "--data", "--data-raw", "--data-binary", "--data-ascii", "--data-urlencode", "--json", "--form", "--form-string", "--cookie", "--cookie-jar", "--referer", "--proxy", "--noproxy", "--user", "--proxy-user", "--cacert", "--cert", "--key", "--resolve", "--connect-to", "--range", "--request-target", "--tls-max", "--proto", "--proto-redir", "--interface", "--local-port", "--limit-rate", "--expect100-timeout", "--oauth2-bearer", "-T":
		return true, nil
	case "--location", "--location-trusted", "--next", "--parallel", "--config", "--head", "--http0.9", "--stderr", "--trace", "--trace-ascii", "--proxytunnel", "--proxy-tunnel", "--retry", "--retry-delay", "--retry-max-time", "-D":
		return false, fmt.Errorf("retry does not support curl option %s because it makes status capture ambiguous", spelling)
	default:
		return false, retryUnsupportedOption(spelling)
	}
}

func retryOptionValue(argv []string, index int, option, value string, hasValue bool) (string, int, error) {
	if hasValue && value != "" {
		return value, index, nil
	}
	if hasValue || index+1 >= len(argv) {
		return "", index, fmt.Errorf("curl option %s requires a value", option)
	}
	return argv[index+1], index + 1, nil
}

func addRetryURL(value string, count *int, hasHTTPSURL, hasGlobURL *bool) error {
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return fmt.Errorf("retry accepts only an HTTP(S) URL operand, got %q", value)
	}
	if strings.HasPrefix(lower, "https://") {
		*hasHTTPSURL = true
	}
	if strings.ContainsAny(value, "{}[]") {
		*hasGlobURL = true
	}
	*count++
	return nil
}

func retryUnsupportedOption(option string) error {
	return fmt.Errorf("retry does not support curl option %s", option)
}

func includedHTTPStatus(output string) (int, bool, error) {
	lines := transcriptLines(output)
	if len(lines) == 0 || !strings.HasPrefix(trimHTTPLine(lines[0].text), "HTTP/") {
		return 0, false, nil
	}
	if !retryStatusLine(lines[0].text, "").valid {
		return 0, true, fmt.Errorf("invalid initial response status line")
	}
	status, _, err := retryResponseSequence(lines, 0, "")
	if err != nil {
		return 0, true, err
	}
	return status, true, nil
}

func verboseHTTPStatus(output string) (int, bool, error) {
	lines := transcriptLines(output)
	if len(lines) == 0 {
		return 0, false, nil
	}
	method, index, found := retryRequestStart(lines[0].text)
	if !found {
		if strings.HasPrefix(trimHTTPLine(lines[0].text), "> ") {
			return 0, true, fmt.Errorf("invalid initial request line")
		}
		return 0, false, nil
	}
	if method == "CONNECT" {
		return 0, true, fmt.Errorf("proxy CONNECT responses are not retryable")
	}

	var err error
	index, err = retryHeaderBlockEnd(lines, index, "> ", ">")
	if err != nil {
		return 0, true, fmt.Errorf("invalid initial request headers: %w", err)
	}
	if index >= len(lines) || !retryStatusLine(lines[index].text, "< ").valid {
		return 0, true, fmt.Errorf("initial request has no complete response")
	}
	status, next, err := retryResponseSequence(lines, index, "< ")
	if err != nil {
		return 0, true, err
	}
	for ; next < len(lines); next++ {
		if retryStatusLine(lines[next].text, "< ").valid {
			return 0, true, fmt.Errorf("multiple verbose responses are ambiguous")
		}
		if _, _, found := retryRequestStart(lines[next].text); found {
			return 0, true, fmt.Errorf("multiple verbose requests are ambiguous")
		}
	}
	return status, true, nil
}

func retryResponseSequence(lines []transcriptLine, index int, prefix string) (int, int, error) {
	for {
		status := retryStatusLine(lines[index].text, prefix)
		if !status.valid {
			return 0, index, fmt.Errorf("invalid response status line")
		}
		if status.code < 100 || status.code > 599 {
			return 0, index, fmt.Errorf("response status %d is outside 100-599", status.code)
		}

		var err error
		index, err = retryHeaderBlockEnd(lines, index+1, prefix, strings.TrimSpace(prefix))
		if err != nil {
			return 0, index, fmt.Errorf("invalid response headers: %w", err)
		}
		if status.code < 200 {
			if index >= len(lines) || !retryStatusLine(lines[index].text, prefix).valid {
				return 0, index, fmt.Errorf("interim response has no following final response")
			}
			continue
		}
		return status.code, index, nil
	}
}

func retryHeaderBlockEnd(lines []transcriptLine, index int, prefix, blankPrefix string) (int, error) {
	for index < len(lines) {
		if retryBlankLine(lines[index].text, prefix, blankPrefix) {
			return index + 1, nil
		}
		if !retryHeaderLine(lines[index].text, prefix) {
			return index, fmt.Errorf("header block is truncated or contains an invalid header")
		}
		index++
	}
	return index, fmt.Errorf("header block has no terminating blank line")
}

func retryBlankLine(line, prefix, blankPrefix string) bool {
	if prefix == "" {
		return trimHTTPLine(line) == ""
	}
	if !strings.HasPrefix(line, blankPrefix) {
		return false
	}
	return strings.TrimSpace(trimHTTPLine(line[len(blankPrefix):])) == ""
}

func retryHeaderLine(line, prefix string) bool {
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	line = trimHTTPLine(line[len(prefix):])
	colon := strings.IndexByte(line, ':')
	return colon > 0 && validHeaderName(strings.ToLower(line[:colon])) && !strings.ContainsRune(line[colon+1:], '\r')
}

type retryStatus struct {
	code  int
	valid bool
}

func retryStatusLine(line, prefix string) retryStatus {
	if !strings.HasPrefix(line, prefix) {
		return retryStatus{}
	}
	fields := strings.Fields(trimHTTPLine(line[len(prefix):]))
	if len(fields) < 2 || !validRetryHTTPVersion(fields[0]) || len(fields[1]) != 3 {
		return retryStatus{}
	}
	for _, character := range fields[1] {
		if character < '0' || character > '9' {
			return retryStatus{}
		}
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return retryStatus{}
	}
	return retryStatus{code: code, valid: true}
}

func retryRequestStart(line string) (string, int, bool) {
	line = trimHTTPLine(line)
	if !strings.HasPrefix(line, "> ") {
		return "", 0, false
	}
	fields := strings.Fields(line[2:])
	if len(fields) != 3 || !validHeaderName(strings.ToLower(fields[0])) || fields[0] != strings.ToUpper(fields[0]) || !validRetryHTTPVersion(fields[2]) {
		return "", 0, false
	}
	return fields[0], 1, true
}

func validRetryHTTPVersion(value string) bool {
	if !strings.HasPrefix(value, "HTTP/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "HTTP/"), ".")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func trimHTTPLine(line string) string {
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}
