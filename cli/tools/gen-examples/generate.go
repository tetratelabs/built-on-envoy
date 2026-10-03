// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This file owns the example run lifecycle and source-preserving manifest update so generated
// transcripts can be refreshed without reformatting authors' surrounding YAML.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

var (
	placeholderPattern        = regexp.MustCompile(`\$\{([^}]+)\}`)
	curlVerboseBodyDiagnostic = regexp.MustCompile(`^[{}] \[[0-9]+ bytes data\]$`)
)

type manifestChange struct {
	path string
	old  []byte
	data []byte
}

type commandTranscript struct {
	displayed      string
	output         []string
	stdout         string
	filteredStderr string
}

func generate(ctx context.Context, opts *options, stdout, stderr io.Writer) (returnErr error) {
	if opts.timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	root, rootErr := moduleRoot()
	if rootErr != nil {
		return rootErr
	}
	type extensionInput struct {
		path     string
		manifest *extensions.Manifest
		raw      []byte
		examples []extensions.Example
	}
	inputs := make([]extensionInput, 0, len(opts.extensions))
	hasExecutable := false
	for _, extension := range opts.extensions {
		path, pathErr := filepath.Abs(extension)
		if pathErr != nil {
			return fmt.Errorf("resolve extension directory %s: %w", extension, pathErr)
		}
		manifestPath := filepath.Join(path, "manifest.yaml")
		manifest, manifestErr := extensions.LoadLocalManifest(manifestPath)
		if manifestErr != nil {
			return fmt.Errorf("load extension manifest %s: %w", manifestPath, manifestErr)
		}
		// The directory and manifest are explicit generator inputs supplied by the extension build.
		raw, readErr := os.ReadFile(manifestPath) // #nosec G304
		if readErr != nil {
			return fmt.Errorf("read extension manifest: %w", readErr)
		}
		var sourceManifest struct {
			Examples []extensions.Example `yaml:"examples"`
		}
		if decodeErr := yaml.Unmarshal(raw, &sourceManifest); decodeErr != nil {
			return fmt.Errorf("decode extension examples: %w", decodeErr)
		}
		if len(sourceManifest.Examples) != len(manifest.Examples) {
			return fmt.Errorf("manifest example count changed while loading")
		}
		manifest.Path = manifestPath
		for exampleIndex := range sourceManifest.Examples {
			example := &sourceManifest.Examples[exampleIndex]
			if _, compareErr := equivalentTranscript("", "", example.Comparison, example.VolatileHeaders); compareErr != nil {
				return fmt.Errorf("invalid comparison rules in %s example %q: %w", manifestPath, example.Title, compareErr)
			}
		}
		for exampleIndex := range sourceManifest.Examples {
			hasExecutable = hasExecutable || len(sourceManifest.Examples[exampleIndex].Commands) > 0
		}
		inputs = append(inputs, extensionInput{path: path, manifest: manifest, raw: raw, examples: sourceManifest.Examples})
	}
	if !hasExecutable {
		if _, printErr := fmt.Fprintln(stdout, "no executable examples found"); printErr != nil {
			return fmt.Errorf("write generator result: %w", printErr)
		}
		return nil
	}
	boePath, cleanupBuild, err := resolveBoe(root, opts.boe)
	if err != nil {
		return err
	}
	defer cleanupBuild()

	sharedData, err := os.MkdirTemp("", "boe-example-data-*")
	if err != nil {
		return fmt.Errorf("create isolated extension data directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(sharedData) }()
	if opts.envoyPath == "" {
		if cacheErr := prepareEnvoyCache(root, sharedData, opts.envoyVersion); cacheErr != nil {
			return fmt.Errorf("prepare Envoy download cache: %w", cacheErr)
		}
	}
	var changed []manifestChange
	stale := false
	for _, input := range inputs {
		updates := make(map[int]string)
		executed := false
		for i := range input.examples {
			example := &input.examples[i]
			if len(example.Commands) == 0 {
				continue
			}
			executed = true
			code, err := runExample(ctx, root, opts, input.path, input.manifest, example, boePath, sharedData)
			if err != nil {
				return fmt.Errorf("extension %q example %q: %w", input.manifest.Name, example.Title, err)
			}
			if code != example.Code {
				updates[i] = code
			}
		}
		if len(updates) == 0 {
			if executed {
				if _, err := fmt.Fprintf(stdout, "%s: generated examples are current\n", input.manifest.Name); err != nil {
					return fmt.Errorf("write generator result: %w", err)
				}
			}
			continue
		}
		updated, err := patchExampleCodes(input.raw, updates)
		if err != nil {
			return err
		}
		var updatedManifest extensions.Manifest
		if err := yaml.Unmarshal(updated, &updatedManifest); err != nil {
			return fmt.Errorf("decode updated manifest %s: %w", input.manifest.Path, err)
		}
		if err := extensions.ValidateManifest(&updatedManifest); err != nil {
			return fmt.Errorf("validate updated manifest %s: %w", input.manifest.Path, err)
		}
		if !bytes.Equal(input.raw, updated) {
			stale = true
			changed = append(changed, manifestChange{path: input.manifest.Path, old: input.raw, data: updated})
			if opts.check {
				if _, err := fmt.Fprintf(stderr, "generated examples differ in %s\n", input.manifest.Path); err != nil {
					return fmt.Errorf("write stale example message: %w", err)
				}
				if err := writeUnifiedDiff(stderr, string(input.raw), string(updated)); err != nil {
					return fmt.Errorf("write example diff: %w", err)
				}
			}
		} else {
			if _, err := fmt.Fprintf(stdout, "%s: generated examples are current\n", input.manifest.Name); err != nil {
				return fmt.Errorf("write generator result: %w", err)
			}
		}
	}
	if opts.check && stale {
		return fmt.Errorf("generated examples are stale")
	}
	if err := writeManifestChanges(changed); err != nil {
		return err
	}
	for _, item := range changed {
		if _, err := fmt.Fprintf(stdout, "%s: updated generated examples\n", item.path); err != nil {
			return fmt.Errorf("write generator result: %w", err)
		}
	}
	return nil
}

func writeManifestChanges(changes []manifestChange) error {
	type stagedFile struct{ target, temp string }
	staged := make([]stagedFile, 0, len(changes))
	defer func() {
		for _, file := range staged {
			_ = os.Remove(file.temp)
		}
	}()
	for _, change := range changes {
		// This is the exact manifest path loaded and validated at the start of generation.
		current, err := os.ReadFile(change.path) // #nosec G304
		if err != nil {
			return fmt.Errorf("re-read manifest %s before update: %w", change.path, err)
		}
		if !bytes.Equal(current, change.old) {
			return fmt.Errorf("manifest %s changed while examples were running; refusing to overwrite it", change.path)
		}
		info, err := os.Stat(change.path)
		if err != nil {
			return fmt.Errorf("inspect manifest %s before update: %w", change.path, err)
		}
		file, err := os.CreateTemp(filepath.Dir(change.path), ".manifest-*.tmp")
		if err != nil {
			return fmt.Errorf("stage manifest update for %s: %w", change.path, err)
		}
		staged = append(staged, stagedFile{target: change.path, temp: file.Name()})
		if err := file.Chmod(info.Mode().Perm()); err != nil {
			_ = file.Close()
			return fmt.Errorf("set permissions on staged manifest %s: %w", change.path, err)
		}
		if _, err := file.Write(change.data); err != nil {
			_ = file.Close()
			return fmt.Errorf("write staged manifest %s: %w", change.path, err)
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return fmt.Errorf("sync staged manifest %s: %w", change.path, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close staged manifest %s: %w", change.path, err)
		}
	}
	for _, file := range staged {
		if err := os.Rename(file.temp, file.target); err != nil {
			return fmt.Errorf("replace manifest %s: %w", file.target, err)
		}
	}
	return nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "cli", "main.go")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repository root above %s", dir)
		}
		dir = parent
	}
}

func resolveBoe(root, provided string) (string, func(), error) {
	if provided != "" {
		path, err := filepath.Abs(provided)
		return path, func() {}, err
	}
	dir, err := os.MkdirTemp("", "boe-example-cli-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary build directory: %w", err)
	}
	path := filepath.Join(dir, "boe")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	// The executable and arguments are fixed; extension content is not interpolated into this build.
	cmd := exec.Command("go", "build", "-o", path, "./cli") // #nosec G204
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("build boe from current checkout: %w\n%s", err, output.String())
	}
	return path, func() { _ = os.RemoveAll(dir) }, nil
}

func runExample(ctx context.Context, root string, opts *options, extensionPath string, manifest *extensions.Manifest, example *extensions.Example, boe, sharedData string) (result string, returnErr error) {
	workDir, err := os.MkdirTemp("", "boe-example-*")
	if err != nil {
		return "", fmt.Errorf("create temporary working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()
	if fixtureErr := stageFixtures(extensionPath, workDir); fixtureErr != nil {
		return "", fixtureErr
	}
	boeHome := filepath.Join(workDir, ".boe")
	configHome := filepath.Join(boeHome, "config")
	dataHome := sharedData
	stateHome := filepath.Join(boeHome, "state")
	runtimeDir := filepath.Join(boeHome, "runtime")
	for _, dir := range []string{configHome, stateHome, runtimeDir} {
		if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
			return "", fmt.Errorf("create isolated BOE directory: %w", mkdirErr)
		}
	}
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("allocate Envoy listener port: %w", err)
	}
	adminListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = proxyListener.Close()
		return "", fmt.Errorf("allocate Envoy admin port: %w", err)
	}
	proxyAddr, adminAddr := proxyListener.Addr().String(), adminListener.Addr().String()
	defer func() {
		if closeErr := proxyListener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) && returnErr == nil {
			returnErr = fmt.Errorf("release reserved Envoy listener port: %w", closeErr)
		}
		if closeErr := adminListener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) && returnErr == nil {
			returnErr = fmt.Errorf("release reserved Envoy admin port: %w", closeErr)
		}
	}()
	values := map[string]string{
		"PROXY_URL":        "http://" + proxyAddr,
		"ADMIN_URL":        "http://" + adminAddr,
		"UPSTREAM_ADDRESS": "httpbin.org:443",
		"WORK_DIR":         workDir,
	}
	relExtension, err := filepath.Rel(root, filepath.Dir(manifest.Path))
	if err != nil {
		return "", fmt.Errorf("make extension path portable: %w", err)
	}
	relExtension = filepath.ToSlash(relExtension)
	fixturePath := "."
	if hasFixtures(extensionPath) {
		fixturePath = filepath.ToSlash(filepath.Join(relExtension, "examples"))
	}
	displayConfigValues := map[string]string{
		"PROXY_URL":        "http://localhost:10000",
		"ADMIN_URL":        "http://127.0.0.1:9901",
		"UPSTREAM_ADDRESS": "httpbin.org:443",
		"WORK_DIR":         fixturePath,
	}
	displayCommandValues := make(map[string]string, len(displayConfigValues))
	for key, value := range displayConfigValues {
		displayCommandValues[key] = value
	}
	// Commands run after the optional Terminal 2 cd, so their work directory is always `.`.
	displayCommandValues["WORK_DIR"] = "."
	displayPreStartValues := make(map[string]string, len(displayConfigValues))
	for key, value := range displayConfigValues {
		displayPreStartValues[key] = value
	}
	outputActualValues := make(map[string]string, len(values)+2)
	for key, value := range values {
		outputActualValues[key] = value
	}
	outputActualValues["ADMIN_AUTHORITY"] = adminAddr
	outputActualValues["PROXY_AUTHORITY"] = proxyAddr
	outputDisplayValues := make(map[string]string, len(displayCommandValues)+2)
	for key, value := range displayCommandValues {
		outputDisplayValues[key] = value
	}
	outputDisplayValues["ADMIN_AUTHORITY"] = "127.0.0.1:9901"
	outputDisplayValues["PROXY_AUTHORITY"] = "localhost:10000"
	preStartOutputDisplayValues := make(map[string]string, len(outputDisplayValues))
	for key, value := range outputDisplayValues {
		preStartOutputDisplayValues[key] = value
	}
	preStartOutputDisplayValues["WORK_DIR"] = fixturePath
	configJSON := ""
	displayConfigJSON := ""
	if example.Config != nil {
		config, configErr := expandMap(*example.Config, values)
		if configErr != nil {
			return "", fmt.Errorf("expand config: %w", configErr)
		}
		b, configErr := json.Marshal(config)
		if configErr != nil {
			return "", fmt.Errorf("encode config: %w", configErr)
		}
		configJSON = string(b)
		displayConfig, displayErr := expandMap(*example.Config, displayConfigValues)
		if displayErr != nil {
			return "", fmt.Errorf("expand displayed config: %w", displayErr)
		}
		displayConfigJSON, displayErr = formatDisplayJSON(displayConfig)
		if displayErr != nil {
			return "", fmt.Errorf("encode displayed config: %w", displayErr)
		}
	}
	args := []string{"run", "--local", filepath.Dir(manifest.Path), "--listen-port", portOf(proxyAddr), "--admin-port", portOf(adminAddr)}
	if opts.envoyPath != "" {
		args = append(args, "--envoy-path", opts.envoyPath)
	} else {
		args = append(args, "--envoy-version", opts.envoyVersion)
	}
	if example.Config != nil {
		args = append(args, "--config", configJSON)
	}
	transcriptArgs := []string{"run", "--local", relExtension}
	if opts.envoyPath != "" {
		transcriptArgs = append(transcriptArgs, "--envoy-path", "/path/to/envoy")
	} else {
		transcriptArgs = append(transcriptArgs, "--envoy-version", opts.envoyVersion)
	}
	if example.Config != nil {
		transcriptArgs = append(transcriptArgs, "--config", displayConfigJSON)
	}
	var transcript strings.Builder
	var terminalTwoPrefix strings.Builder
	terminalTwoPrefix.WriteString("# Terminal 2 (after Envoy is ready, from the repository root)\n")
	if hasFixtures(extensionPath) {
		terminalTwoPrefix.WriteString(formatShellCommand([]string{"cd", fixturePath}) + "\n")
	}
	commandTranscripts := make([]commandTranscript, 0, len(example.Commands))
	preStartTranscripts := make([]commandTranscript, 0, len(example.PreStart))
	for i, command := range example.PreStart {
		argv, argvErr := expandArgv(command.Argv, values)
		if argvErr != nil {
			return "", fmt.Errorf("pre-start command %d: %w", i+1, argvErr)
		}
		displayArgv, displayArgvErr := expandArgv(command.Argv, displayPreStartValues)
		if displayArgvErr != nil {
			return "", fmt.Errorf("display pre-start command %d: %w", i+1, displayArgvErr)
		}
		result, commandErr := executeExampleCommand(ctx, opts.timeout, "pre-start command", command, argv, displayArgv, workDir, values, outputActualValues, preStartOutputDisplayValues)
		if commandErr != nil {
			return "", fmt.Errorf("pre-start command %d: %w", i+1, commandErr)
		}
		preStartTranscripts = append(preStartTranscripts, result)
	}

	_ = proxyListener.Close()
	_ = adminListener.Close()
	proc, err := startBoe(ctx, boe, args, root, configHome, dataHome, stateHome, runtimeDir)
	if err != nil {
		return "", err
	}
	defer func() {
		if stopErr := stopProcess(proc); stopErr != nil && returnErr == nil {
			returnErr = fmt.Errorf("stop boe process: %w", stopErr)
		}
	}()
	if waitErr := waitAdmin(ctx, proc, adminAddr, opts.timeout); waitErr != nil {
		return "", waitErr
	}

	for i, command := range example.Commands {
		argv, argvErr := expandArgv(command.Argv, values)
		if argvErr != nil {
			return "", fmt.Errorf("command %d: %w", i+1, argvErr)
		}
		if len(argv) == 0 {
			return "", fmt.Errorf("command %d has empty argv", i+1)
		}
		displayArgv, displayArgvErr := expandArgv(command.Argv, displayCommandValues)
		if displayArgvErr != nil {
			return "", fmt.Errorf("display command %d: %w", i+1, displayArgvErr)
		}
		result, commandErr := executeExampleCommand(ctx, opts.timeout, "command", command, argv, displayArgv, workDir, values, outputActualValues, outputDisplayValues)
		if commandErr != nil {
			return "", fmt.Errorf("command %d: %w", i+1, commandErr)
		}
		commandTranscripts = append(commandTranscripts, result)
	}
	appendTerminalOneTranscript(&transcript, preStartTranscripts, opts, transcriptArgs)
	for index, command := range commandTranscripts {
		var shellBlock strings.Builder
		if index == 0 {
			shellBlock.WriteString(terminalTwoPrefix.String())
		}
		shellBlock.WriteString(command.displayed)
		transcript.WriteString("\n\n" + renderFencedBlock("sh", shellBlock.String()))
		for _, output := range command.output {
			transcript.WriteString("\n\n" + output)
		}
	}
	generatedCode := strings.TrimRight(transcript.String(), "\n") + "\n"
	oldCode := example.Code
	equal, err := equivalentTranscript(oldCode, generatedCode, example.Comparison, example.VolatileHeaders)
	if err != nil {
		return "", fmt.Errorf("compare generated transcript: %w", err)
	}
	if equal {
		return oldCode, nil
	}
	return generatedCode, nil
}

func executeExampleCommand(ctx context.Context, timeout time.Duration, phase string, command extensions.ExampleCommand, argv, displayArgv []string, workDir string, values, outputActualValues, outputDisplayValues map[string]string) (commandTranscript, error) {
	displayed := renderCommandComment(command.Comment)
	if command.Retry != nil {
		displayed += fmt.Sprintf("# Repeat until HTTP %d, up to %d attempts.\n", command.Retry.HTTPStatus, command.Retry.MaxAttempts)
	}
	displayed += formatShellCommand(displayArgv) + "\n"
	if command.Retry == nil {
		result, err := executeExampleAttempt(ctx, timeout, phase, command, nil, argv, workDir, values, outputActualValues, outputDisplayValues)
		result.displayed = displayed
		return result, err
	}
	capture, err := retryCaptureMode(argv)
	if err != nil {
		return commandTranscript{}, fmt.Errorf("validate retry capture mode for %s %q: %w", phase, shellJoin(command.Argv), err)
	}

	groupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastStatus int
	for attempt := 1; attempt <= command.Retry.MaxAttempts; attempt++ {
		result, err := executeExampleAttempt(groupCtx, timeout, phase, command, &capture, argv, workDir, values, outputActualValues, outputDisplayValues)
		if err != nil {
			return commandTranscript{}, err
		}
		lastStatus, err = retryHTTPStatus(result.stdout, result.filteredStderr, capture)
		if err != nil {
			return commandTranscript{}, fmt.Errorf("parse HTTP status for %s %q attempt %d: %w", phase, shellJoin(command.Argv), attempt, err)
		}
		if lastStatus == command.Retry.HTTPStatus {
			result.displayed = displayed
			return result, nil
		}
	}
	return commandTranscript{}, fmt.Errorf("%s %q exhausted after %d attempts; last HTTP status %d, wanted %d", phase, shellJoin(command.Argv), command.Retry.MaxAttempts, lastStatus, command.Retry.HTTPStatus)
}

func executeExampleAttempt(ctx context.Context, timeout time.Duration, phase string, command extensions.ExampleCommand, capture *retryCapture, argv []string, workDir string, values, outputActualValues, outputDisplayValues map[string]string) (commandTranscript, error) {
	result := commandTranscript{}
	if len(argv) == 0 {
		return commandTranscript{}, errors.New("empty argv")
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Commands are authored in an extension manifest and intentionally run as subprocesses.
	cmd := exec.CommandContext(cmdCtx, argv[0], argv[1:]...) // #nosec G204
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "PROXY_URL="+values["PROXY_URL"], "ADMIN_URL="+values["ADMIN_URL"], "UPSTREAM_ADDRESS="+values["UPSTREAM_ADDRESS"], "WORK_DIR="+workDir)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdoutBuf, &stderrBuf
	runErr := cmd.Run()
	if cmd.Process != nil {
		if killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			return commandTranscript{}, fmt.Errorf("clean up %s process group: %w", phase, errors.Join(runErr, killErr))
		}
	}
	if cmdCtx.Err() != nil {
		return commandTranscript{}, fmt.Errorf("%s %q timed out or canceled: %w; stderr: %s", phase, shellJoin(command.Argv), cmdCtx.Err(), stderrBuf.String())
	}
	actualExit := exitCode(runErr)
	if runErr != nil && actualExit < 0 {
		return commandTranscript{}, fmt.Errorf("%s %q failed: %w; stderr: %s", phase, shellJoin(command.Argv), runErr, stderrBuf.String())
	}
	if actualExit != command.ExpectedExit {
		mismatch := fmt.Sprintf("%s %q exited %d, expected %d", phase, shellJoin(command.Argv), actualExit, command.ExpectedExit)
		if runErr != nil {
			return commandTranscript{}, fmt.Errorf("%s: %w; stderr: %s", mismatch, runErr, stderrBuf.String())
		}
		return commandTranscript{}, fmt.Errorf("%s; stderr: %s", mismatch, stderrBuf.String())
	}
	if stdoutBuf.Len() > 0 {
		rendered, renderErr := renderOutput(stdoutBuf.String(), outputActualValues, outputDisplayValues, "stdout")
		if renderErr != nil {
			return commandTranscript{}, fmt.Errorf("render %s stdout: %w", phase, renderErr)
		}
		result.output = append(result.output, rendered)
	}
	stderrText := filterCommandDiagnostics(argv, stderrBuf.String())
	if capture != nil && capture.verbose {
		stderrText = filterCurlTransportDiagnostics(stderrBuf.String())
	}
	result.stdout = stdoutBuf.String()
	result.filteredStderr = stderrText
	if stderrText != "" {
		rendered, renderErr := renderOutput(stderrText, outputActualValues, outputDisplayValues, "stderr")
		if renderErr != nil {
			return commandTranscript{}, fmt.Errorf("render %s stderr: %w", phase, renderErr)
		}
		result.output = append(result.output, rendered)
	}
	return result, nil
}

func appendTerminalOneTranscript(transcript *strings.Builder, preStart []commandTranscript, opts *options, transcriptArgs []string) {
	for index, command := range preStart {
		var shellBlock strings.Builder
		if index == 0 {
			shellBlock.WriteString("# Terminal 1 (from the repository root)\n")
		}
		shellBlock.WriteString(command.displayed)
		transcript.WriteString(renderFencedBlock("sh", shellBlock.String()))
		for _, output := range command.output {
			transcript.WriteString("\n\n" + output)
		}
		transcript.WriteString("\n\n")
	}
	var boeBlock strings.Builder
	if len(preStart) == 0 {
		boeBlock.WriteString("# Terminal 1 (from the repository root)\n")
	} else {
		boeBlock.WriteString("# Start BOE in Terminal 1 after the pre-start commands complete.\n")
	}
	if opts.envoyPath != "" {
		boeBlock.WriteString("# Replace /path/to/envoy with a compatible Envoy binary.\n")
	}
	boeBlock.WriteString(formatShellCommand(append([]string{"boe"}, transcriptArgs...)) + "\n")
	transcript.WriteString(renderFencedBlock("sh", boeBlock.String()))
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

type boeProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
	stdout  *lockedBuffer
	stderr  *lockedBuffer
}

func startBoe(ctx context.Context, boe string, args []string, root, configHome, dataHome, stateHome, runtimeDir string) (*boeProcess, error) {
	// #nosec G204 -- boe is the generator's explicit input or a binary built from this checkout.
	cmd := exec.CommandContext(ctx, boe, args...)
	cmd.Dir = root
	cmd.Env = replaceEnv(os.Environ(), map[string]string{
		"BOE_CONFIG_HOME":   configHome,
		"BOE_DATA_HOME":     dataHome,
		"BOE_STATE_HOME":    stateHome,
		"BOE_RUNTIME_DIR":   runtimeDir,
		"BOE_RUN_ID":        "examples",
		"BOE_RUN_DOCKER":    "false",
		"BOE_ADMIN_ADDRESS": "127.0.0.1:" + portOfFromAdmin(args),
		"ENVOY_PATH":        "",
		"ENVOY_VERSION":     "",
	})
	stdout, stderr := &lockedBuffer{}, &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start boe: %w", err)
	}
	proc := &boeProcess{cmd: cmd, done: make(chan struct{}), stdout: stdout, stderr: stderr}
	go func() {
		proc.waitErr = cmd.Wait()
		close(proc.done)
	}()
	return proc, nil
}

func replaceEnv(existing []string, replacements map[string]string) []string {
	result := make([]string, 0, len(existing)+len(replacements))
	for _, item := range existing {
		key, _, found := strings.Cut(item, "=")
		if !found {
			result = append(result, item)
			continue
		}
		if _, replaced := replacements[key]; replaced {
			continue
		}
		result = append(result, item)
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+replacements[key])
	}
	return result
}

func portOfFromAdmin(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--admin-port" {
			return args[i+1]
		}
	}
	return "9901"
}

func waitAdmin(ctx context.Context, proc *boeProcess, adminAddress string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: 250 * time.Millisecond}
	url := "http://" + adminAddress + "/ready"
	for {
		select {
		case <-proc.done:
			return fmt.Errorf("boe exited before Envoy became ready: %w; stderr: %s", proc.waitErr, proc.stderr.String())
		default:
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("envoy did not become ready at %s within %s; boe stdout: %s; stderr: %s", url, timeout, proc.stdout.String(), proc.stderr.String())
		case <-proc.done:
			return fmt.Errorf("boe exited before Envoy became ready: %w; stderr: %s", proc.waitErr, proc.stderr.String())
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				select {
				case <-proc.done:
					return fmt.Errorf("boe exited before Envoy became ready: %w; stderr: %s", proc.waitErr, proc.stderr.String())
				default:
				}
				continue
			}
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK && strings.EqualFold(strings.TrimSpace(string(body)), "live") {
				return nil
			}
		}
	}
}

func stopProcess(proc *boeProcess) error {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return nil
	}
	groupID := -proc.cmd.Process.Pid
	if err := syscall.Kill(groupID, syscall.SIGINT); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("interrupt boe process group: %w", err)
	}
	select {
	case <-proc.done:
		if err := syscall.Kill(groupID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("clean up boe process group: %w", err)
		}
		return expectedSignalExit(proc.waitErr)
	case <-time.After(3 * time.Second):
		if err := syscall.Kill(groupID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("force stop boe process group: %w", err)
		}
		<-proc.done
		return expectedSignalExit(proc.waitErr)
	}
}

func expectedSignalExit(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ProcessState != nil {
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if ok && status.Signaled() && (status.Signal() == syscall.SIGINT || status.Signal() == syscall.SIGKILL) {
			return nil
		}
	}
	return err
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func stageFixtures(extension, workDir string) error {
	source := filepath.Join(extension, "examples")
	sourceRoot, err := os.OpenRoot(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open examples fixture directory: %w", err)
	}
	defer func() { _ = sourceRoot.Close() }()
	destinationRoot, err := os.OpenRoot(workDir)
	if err != nil {
		return fmt.Errorf("open fixture destination directory: %w", err)
	}
	defer func() { _ = destinationRoot.Close() }()
	return fs.WalkDir(sourceRoot.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("fixture symlinks are not supported: %s", path)
		}
		if entry.IsDir() {
			return destinationRoot.MkdirAll(path, 0o700)
		}
		info, err := sourceRoot.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect fixture %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("fixture must be a regular file: %s", path)
		}
		data, err := sourceRoot.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read fixture %s: %w", path, err)
		}
		mode := info.Mode().Perm() & 0o700
		if mode == 0 {
			mode = 0o600
		}
		if err := destinationRoot.WriteFile(path, data, mode); err != nil {
			return fmt.Errorf("write fixture %s: %w", path, err)
		}
		return nil
	})
}

func hasFixtures(extension string) bool {
	info, err := os.Stat(filepath.Join(extension, "examples"))
	return err == nil && info.IsDir()
}

func expandArgv(argv []string, values map[string]string) ([]string, error) {
	result := make([]string, len(argv))
	for i, arg := range argv {
		value, err := expandString(arg, values)
		if err != nil {
			return nil, err
		}
		result[i] = value
	}
	return result, nil
}

func expandMap(input map[string]any, values map[string]string) (map[string]any, error) {
	result := make(map[string]any, len(input))
	for key, value := range input {
		expanded, err := expandValue(value, values)
		if err != nil {
			return nil, fmt.Errorf("config field %q: %w", key, err)
		}
		result[key] = expanded
	}
	return result, nil
}

func expandValue(value any, values map[string]string) (any, error) {
	switch typed := value.(type) {
	case string:
		return expandString(typed, values)
	case map[string]any:
		return expandMap(typed, values)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			expanded, err := expandValue(item, values)
			if err != nil {
				return nil, err
			}
			result[i] = expanded
		}
		return result, nil
	default:
		return value, nil
	}
}

func expandString(value string, values map[string]string) (string, error) {
	var expansionErr error
	result := placeholderPattern.ReplaceAllStringFunc(value, func(match string) string {
		if expansionErr != nil {
			return match
		}
		key := placeholderPattern.FindStringSubmatch(match)[1]
		replacement, ok := values[key]
		if !ok {
			expansionErr = fmt.Errorf("unknown placeholder %s", match)
			return match
		}
		return replacement
	})
	return result, expansionErr
}

func renderOutput(value string, actual, display map[string]string, stream string) (string, error) {
	keys := make([]string, 0, len(actual))
	for key := range actual {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value = strings.ReplaceAll(value, actual[key], display[key])
	}
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
			return "", fmt.Errorf("encode trailing whitespace metadata: %w", err)
		}
		metadata.WriteString("; Trailing whitespace: " + base64.StdEncoding.EncodeToString(encoded))
	}
	if value != "" && !strings.HasSuffix(value, "\n") {
		metadata.WriteString("; No trailing newline")
	}
	metadata.WriteString(" -->\n")
	return renderFencedBlock("text", normalized) + "\n" + metadata.String(), nil
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

func isVerboseCurl(argv []string) bool {
	if len(argv) == 0 || filepath.Base(argv[0]) != "curl" {
		return false
	}
	for _, arg := range argv[1:] {
		if arg == "-v" || arg == "--verbose" {
			return true
		}
	}
	return false
}

func filterCommandDiagnostics(argv []string, stderr string) string {
	if !isVerboseCurl(argv) {
		return stderr
	}
	return filterCurlTransportDiagnostics(stderr)
}

func filterCurlTransportDiagnostics(stderr string) string {
	var filtered strings.Builder
	for _, line := range strings.SplitAfter(stderr, "\n") {
		content := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if strings.HasPrefix(line, "* ") || curlVerboseBodyDiagnostic.MatchString(content) {
			continue
		}
		filtered.WriteString(line)
	}
	return filtered.String()
}

func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func formatShellCommand(argv []string) string {
	var out strings.Builder
	column := 0
	for index, arg := range argv {
		word := shellQuote(arg)
		firstLine, _, _ := strings.Cut(word, "\n")
		limit := 80
		if index+1 < len(argv) {
			// Reserve the space and backslash if the next argument needs a new line.
			limit -= 2
		}
		if index > 0 {
			if column+1+utf8.RuneCountInString(firstLine) > limit {
				out.WriteString(" \\\n  ")
				column = 2
			} else {
				out.WriteByte(' ')
				column++
			}
		}
		out.WriteString(word)
		if newline := strings.LastIndexByte(word, '\n'); newline >= 0 {
			column = utf8.RuneCountInString(word[newline+1:])
		} else {
			column += utf8.RuneCountInString(word)
		}
	}
	return out.String()
}

func shellQuote(arg string) string {
	if strings.Contains(arg, "${") {
		matches := placeholderPattern.FindAllStringIndex(arg, -1)
		var out strings.Builder
		cursor := 0
		for _, match := range matches {
			if cursor < match[0] {
				out.WriteString(shellQuoteLiteral(arg[cursor:match[0]]))
			}
			out.WriteString("\"" + arg[match[0]:match[1]] + "\"")
			cursor = match[1]
		}
		if cursor < len(arg) {
			out.WriteString(shellQuoteLiteral(arg[cursor:]))
		}
		return out.String()
	}
	return shellQuoteLiteral(arg)
}

func shellQuoteLiteral(arg string) string {
	if arg != "" && strings.IndexFunc(arg, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("_./:-", r)
	}) == -1 {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
}

func stableJSON(config map[string]any) (string, error) {
	encoded, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func formatDisplayJSON(config map[string]any) (string, error) {
	pretty := len(config) > 1
	if len(config) == 1 {
		for _, value := range config {
			encoded, err := json.Marshal(value)
			if err != nil {
				return "", err
			}
			pretty = len(encoded) > 0 && (encoded[0] == '{' || encoded[0] == '[')
		}
	}
	if !pretty {
		return stableJSON(config)
	}
	encoded, err := json.MarshalIndent(config, "  ", "  ")
	if err != nil {
		return "", err
	}
	return "\n  " + string(encoded), nil
}

func renderCommandComment(comment string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(comment, "\r\n", "\n"), "\r", "\n")
	normalized = strings.TrimSuffix(normalized, "\n")
	if normalized == "" {
		return ""
	}
	var output strings.Builder
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			output.WriteString("#\n")
		} else {
			output.WriteString("# " + line + "\n")
		}
	}
	return output.String()
}

func portOf(address string) string {
	_, port, err := net.SplitHostPort(address)
	if err == nil {
		return port
	}
	return ""
}

func patchExampleCodes(raw []byte, updates map[int]string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse source manifest for code updates: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("manifest is empty")
	}
	root := doc.Content[0]
	examples := mappingValue(root, "examples")
	if examples == nil || examples.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("manifest examples must be a sequence")
	}
	if examples.Style&yaml.FlowStyle != 0 {
		return nil, fmt.Errorf("cannot safely update flow-style examples; use block-style YAML")
	}
	lines := strings.SplitAfter(string(raw), "\n")
	type edit struct {
		start, end  int
		replacement string
	}
	edits := make([]edit, 0, len(updates))
	for index, code := range updates {
		if index < 0 || index >= len(examples.Content) {
			return nil, fmt.Errorf("example index %d is out of range", index)
		}
		entry := examples.Content[index]
		if entry.Style&yaml.FlowStyle != 0 {
			return nil, fmt.Errorf("cannot safely update flow-style example %d; use block-style YAML", index+1)
		}
		codeKey, _ := mappingPair(entry, "code")
		if codeKey != nil {
			start := codeKey.Line - 1
			indent := codeKey.Column - 1
			end := scalarBlockEnd(lines, start, indent)
			header := strings.TrimSuffix(strings.TrimSuffix(lines[start], "\n"), "\r")
			colon := strings.IndexByte(header, ':')
			if colon < 0 {
				return nil, fmt.Errorf("invalid code key at line %d", codeKey.Line)
			}
			comment := ""
			_, codeValue := mappingPair(entry, "code")
			if codeValue != nil && (codeValue.Style&yaml.LiteralStyle != 0 || codeValue.Style&yaml.FoldedStyle != 0) {
				if commentIndex := strings.Index(header[colon+1:], "#"); commentIndex >= 0 {
					comment = " " + strings.TrimSpace(header[colon+1+commentIndex:])
				}
			}
			prefix := header[:colon+1] + " |" + comment + "\n"
			replacement := prefix + renderScalarContent(code, indent+2)
			edits = append(edits, edit{start: start, end: end, replacement: replacement})
			continue
		}
		entryLine := entry.Line - 1
		indent := sequenceItemIndent(lines, entryLine) + 2
		insertAt := entryEndLine(lines, entryLine, sequenceItemIndent(lines, entryLine))
		edits = append(edits, edit{start: insertAt, end: insertAt, replacement: strings.Repeat(" ", indent) + "code: |\n" + renderScalarContent(code, indent+2)})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		if e.start < 0 || e.end < e.start || e.end > len(lines) {
			return nil, fmt.Errorf("invalid code update range")
		}
		before := append([]string(nil), lines[:e.start]...)
		after := append([]string(nil), lines[e.end:]...)
		middle := strings.SplitAfter(e.replacement, "\n")
		if len(middle) > 0 && middle[len(middle)-1] == "" {
			middle = middle[:len(middle)-1]
		}
		lines = append(append(before, middle...), after...)
	}
	return []byte(strings.Join(lines, "")), nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	_, value := mappingPair(node, key)
	return value
}

func mappingPair(node *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i], node.Content[i+1]
		}
	}
	return nil, nil
}

func scalarBlockEnd(lines []string, start, indent int) int {
	end := start + 1
	for end < len(lines) {
		line := strings.TrimSuffix(strings.TrimSuffix(lines[end], "\n"), "\r")
		if strings.TrimSpace(line) != "" {
			currentIndent := len(line) - len(strings.TrimLeft(line, " "))
			if currentIndent <= indent {
				break
			}
		}
		end++
	}
	return end
}

func renderScalarContent(code string, indent int) string {
	lines := strings.Split(strings.TrimSuffix(code, "\n"), "\n")
	var out strings.Builder
	for _, line := range lines {
		if line == "" {
			out.WriteByte('\n')
		} else {
			out.WriteString(strings.Repeat(" ", indent) + line + "\n")
		}
	}
	return out.String()
}

func sequenceItemIndent(lines []string, line int) int {
	if line < 0 || line >= len(lines) {
		return 0
	}
	text := strings.TrimLeft(lines[line], " ")
	return len(lines[line]) - len(text)
}

func entryEndLine(lines []string, entryLine, indent int) int {
	for lineIndex := entryLine + 1; lineIndex < len(lines); lineIndex++ {
		line := strings.TrimSuffix(strings.TrimSuffix(lines[lineIndex], "\n"), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lineIndent := len(line) - len(strings.TrimLeft(line, " "))
		if lineIndent <= indent {
			return lineIndex
		}
	}
	return len(lines)
}

func writeUnifiedDiff(w io.Writer, old, generated string) error {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: difflib.SplitLines(old), B: difflib.SplitLines(generated),
		FromFile: "manifest.yaml", ToFile: "generated manifest.yaml", Context: 3,
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, diff)
	return err
}
