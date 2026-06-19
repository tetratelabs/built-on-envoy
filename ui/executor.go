// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package ui

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Executor manages boe command execution.
type Executor struct {
	logger  *slog.Logger
	mu      sync.Mutex
	running *exec.Cmd
	cancel  context.CancelFunc
	params  *RunParams
	exe     string // overrides selfExe(); used in tests
}

// freePorts returns n distinct free TCP ports. All listeners are held open
// until all ports are allocated, preventing the OS from reusing a port before
// the full set is returned.
func freePorts(n int) ([]int, error) {
	listeners := make([]net.Listener, 0, n)
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()

	ports := make([]int, 0, n)
	for range n {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("failed to find free port: %w", err)
		}
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}

func buildArgs(action string, baseArgs []string, exts []*ExtensionConfig) []string {
	args := []string{action}
	args = append(args, baseArgs...)
	if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
		args = append(args, "--docker")
	}
	if ports, err := freePorts(2); err == nil {
		args = append(args, "--listen-port", strconv.Itoa(ports[0]), "--admin-port", strconv.Itoa(ports[1]))
	}
	for _, ext := range exts {
		if ext.LocalPath != "" {
			args = append(args, "--local", ext.LocalPath)
		} else {
			args = append(args, "--extension", ext.Name)
		}
	}
	for _, ext := range exts {
		args = append(args, "--config", ext.Config)
	}
	// Pass --filter-type positionally only when at least one extension has an
	// override set. Extensions without an override receive an empty string so
	// the positional index stays aligned; the CLI ignores empty filter-type values
	// for single-filter-type extensions.
	hasFilterType := false
	for _, ext := range exts {
		if ext.FilterType != "" {
			hasFilterType = true
			break
		}
	}
	if hasFilterType {
		for _, ext := range exts {
			args = append(args, "--filter-type", ext.FilterType)
		}
	}
	return args
}

// ansiRegex matches ANSI escape sequences: ESC[ followed by params and a letter command.
var ansiRegex = regexp.MustCompile("\x1b\\[([0-9;]*)m")

// ansiColors maps ANSI color codes to CSS colors for dark terminal backgrounds.
var ansiColors = map[int]string{
	30: "#4b5563", 31: "#fca5a5", 32: "#86efac", 33: "#fde68a",
	34: "#93c5fd", 35: "#d8b4fe", 36: "#67e8f9", 37: "#e2e8f0",
	90: "#9ca3af", 91: "#fca5a5", 92: "#86efac", 93: "#fde68a",
	94: "#93c5fd", 95: "#d8b4fe", 96: "#67e8f9", 97: "#f8fafc",
}

var defaultCodeColors = map[int]string{
	1: "ansi-bold",
	2: "ansi-dim",
	3: "ansi-italic",
	4: "ansi-underline",
}

// ansiToHTML converts ANSI escape codes in text to HTML spans.
func ansiToHTML(text string) string {
	escaped := html.EscapeString(text)
	var b strings.Builder
	openTags := 0
	lastIdx := 0

	for _, loc := range ansiRegex.FindAllStringSubmatchIndex(escaped, -1) {
		// Append text before this match
		b.WriteString(escaped[lastIdx:loc[0]])
		lastIdx = loc[1]

		// Extract the code string between [ and m
		codeStr := escaped[loc[2]:loc[3]]
		codes := strings.Split(codeStr, ";")

		for _, cs := range codes {
			code, err := strconv.Atoi(cs)
			if err != nil || code == 0 {
				// Reset — close all open tags
				for openTags > 0 {
					b.WriteString("</span>")
					openTags--
				}
				continue
			}

			color, ok := defaultCodeColors[code]
			if ok {
				b.WriteString(`<span class="` + color + `">`)
				openTags++
				continue
			}
			if color, ok = ansiColors[code]; ok {
				_, _ = b.WriteString(`<span style="color:` + color + `">`)
				openTags++
			}
		}
	}

	// Append remaining text
	b.WriteString(escaped[lastIdx:])
	for openTags > 0 {
		b.WriteString("</span>")
		openTags--
	}
	return b.String()
}

// selfExe returns the path to the current executable, falling back to "boe" if it cannot be determined.
func (e *Executor) selfExe() string {
	exe, err := os.Executable()
	if err != nil {
		return "boe" // fallback to PATH lookup
	}
	e.logger.Debug("determined executable path", "path", exe)
	return exe
}

// RunStreaming executes boe run and streams output via SSE.
func (e *Executor) RunStreaming(ctx context.Context, exts []*ExtensionConfig, w http.ResponseWriter, flusher http.Flusher) {
	e.streamCommand(ctx, buildArgs("run", e.params.Args(), exts), w, flusher)
}

// streamCommand runs the given boe command and streams its output via SSE.
func (e *Executor) streamCommand(ctx context.Context, args []string, w http.ResponseWriter, flusher http.Flusher) {
	ctx, cancel := context.WithCancel(ctx)

	exePath := e.exe
	if exePath == "" {
		exePath = e.selfExe()
	}
	cmd := exec.CommandContext(ctx, exePath, args...) // #nosec G204

	// Create a new process group so we can kill boe and all its children (Envoy, etc.)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Override the default kill behavior — we handle it ourselves via killProcessGroup.
	cmd.Cancel = func() error {
		e.killProcessGroup(cmd)
		return nil
	}

	e.mu.Lock()
	if e.running != nil {
		e.killProcessGroup(e.running)
	}
	e.running = cmd
	e.cancel = cancel
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		if e.running == cmd {
			e.running = nil
			e.cancel = nil
		}
		e.mu.Unlock()
		cancel()
	}()

	e.logger.Info("executing boe", "args", args)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendSSE(w, flusher, "error", fmt.Sprintf("Failed to create stdout pipe: %v", err))
		return
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		sendSSE(w, flusher, "error", fmt.Sprintf("Failed to start boe: %v", err))
		return
	}

	sendSSE(w, flusher, "status", "started")

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		sendSSE(w, flusher, "output", ansiToHTML(scanner.Text()))
	}
	if scanner.Err() != nil {
		sendSSE(w, flusher, "error", fmt.Sprintf("Error reading boe output: %v", scanner.Err()))
		return
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			sendSSE(w, flusher, "status", "stopped")
		} else {
			sendSSE(w, flusher, "error", fmt.Sprintf("Process exited with error: %v", err))
		}
	} else {
		sendSSE(w, flusher, "status", "completed")
	}
}

// Stop kills the currently running boe process and all its children.
func (e *Executor) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running != nil && e.cancel != nil {
		e.killProcessGroup(e.running)
		e.cancel()
		e.running = nil
		e.cancel = nil
		return nil
	}
	return fmt.Errorf("no running process")
}

// killProcessGroup sends SIGTERM to stop the running boe process and its children.
// On darwin (where boe spawns a Docker container), we signal only the boe process
// directly so that boe's own signal handler can forward SIGTERM to the Docker
// container, allowing it to stop gracefully. Killing the whole process group would
// terminate the docker client before it has a chance to stop the container, leaking it.
// On other platforms we kill the process group to catch all child processes (e.g. Envoy).
func (e *Executor) killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if runtime.GOOS == "darwin" {
		e.logger.Info("sending SIGTERM to boe process", "pid", cmd.Process.Pid)
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return
	}
	pgid := -cmd.Process.Pid
	e.logger.Info("killing process group", "pgid", pgid)
	_ = syscall.Kill(pgid, syscall.SIGTERM)
}

func sendSSE(w io.Writer, flusher http.Flusher, event, data string) {
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
}
