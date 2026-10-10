// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
)

// debugserverPaths are the locations of LLDB's debugserver on macOS, from the Xcode Command Line Tools and Xcode.
var debugserverPaths = []string{
	"/Library/Developer/CommandLineTools/Library/PrivateFrameworks/LLDB.framework/Resources/debugserver",
	"/Applications/Xcode.app/Contents/SharedFrameworks/LLDB.framework/Resources/debugserver",
}

// gdbRemoteServer debugs native extensions (e.g. Rust) with a server speaking the GDB remote serial protocol: gdbserver on Linux, and
// LLDB's debugserver on macOS.
//
// These servers stop the process when they attach, until a client connects and continues it, and they detach
// and exit when the client disconnects. To keep Envoy running when no debugger is connected, and to let clients
// reconnect, boe listens on the debug port itself and only attaches the debug server to Envoy when a client
// connects, proxying the connection to it. Only one client can be connected at a time.
type gdbRemoteServer struct {
	goos string
	path string
	port uint32

	envoyPath string // path of the Envoy binary, used to print the IDE configuration

	listener net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex
	cancel   context.CancelFunc // cancels the active session, if any
}

// newGDBRemoteServer returns a GDB remote debug server, looking for the platform's debug server binary.
func newGDBRemoteServer(d *DebugOptions, logger *slog.Logger) (*gdbRemoteServer, error) {
	s := &gdbRemoteServer{goos: runtime.GOOS, port: d.Port}
	var err error
	switch {
	case d.ServerPath != "":
		s.path, err = findExecutable(d.ServerPath)
	case s.goos == "darwin":
		s.path, err = findFirst(debugserverPaths,
			"debugserver not found: install the Xcode Command Line Tools with 'xcode-select --install', or set --debug-server-path")
	default:
		s.path, err = exec.LookPath("gdbserver")
		if err != nil {
			err = errors.New("gdbserver not found: install it (e.g. 'apt install gdbserver' or 'dnf install gdb-gdbserver'), or set --debug-server-path")
		}
	}
	if err != nil {
		return nil, err
	}
	logger.Debug("using GDB remote debug server", "path", s.path)
	return s, nil
}

// findFirst returns the first existing path, or an error with the given message.
func findFirst(paths []string, msg string) (string, error) {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New(msg)
}

// args returns the arguments to start the debug server attached to the given process, listening on the given
// address.
func (s *gdbRemoteServer) args(pid int, addr string) []string {
	if s.goos == "darwin" {
		return []string{addr, "--attach=" + strconv.Itoa(pid)}
	}
	return []string{"--once", "--attach", addr, strconv.Itoa(pid)}
}

func (s *gdbRemoteServer) start(runCtx, ctx context.Context, logger *slog.Logger, pid int) error {
	// The IDE needs the Envoy binary to create the debug target before connecting to the debug server.
	if p, err := process.NewProcessWithContext(ctx, int32(pid)); err == nil { //nolint:gosec // pid never overflows int32
		s.envoyPath, _ = p.ExeWithContext(ctx)
	}
	addr := net.JoinHostPort(listenHost(), strconv.FormatUint(uint64(s.port), 10))
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = l
	s.wg.Go(func() {
		s.serve(runCtx, logger, pid)
	})
	_, _ = fmt.Fprintf(os.Stderr, "%s🐞 Waiting for a debugger on 127.0.0.1:%d%s (attached to Envoy PID %d on connect)\n",
		internal.ANSIBold, s.port, internal.ANSIReset, pid)
	return nil
}

// serve accepts debugger connections, one at a time, attaching a debug server to Envoy for each of them.
func (s *gdbRemoteServer) serve(ctx context.Context, logger *slog.Logger, pid int) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		if err = s.session(ctx, logger, pid, conn); err != nil {
			logger.Error("debug session failed", "error", err)
			_, _ = fmt.Fprintf(os.Stderr, "%s⚠ Debug session failed: %v%s\n", internal.ANSIBold, err, internal.ANSIReset)
		}
	}
}

// session attaches a debug server to Envoy and proxies the given client connection to it until either side
// closes the connection. Envoy keeps running when the session ends.
func (s *gdbRemoteServer) session(ctx context.Context, logger *slog.Logger, pid int, client net.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
		cancel()
	}()
	// Closing the client connection when the session is cancelled ends the proxy.
	go func() {
		<-ctx.Done()
		_ = client.Close()
	}()

	// The debug server listens on a free loopback port, only reachable through the proxy.
	port, err := freePort()
	if err != nil {
		return err
	}
	serverAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	// The debug server is stopped (SIGTERM, which detaches it from Envoy) when the session is cancelled.
	cmd, err := startDebugServer(ctx, logger, "debug", s.path, s.args(pid, serverAddr))
	if err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait() // the only Wait call for this process
		close(exited)
	}()
	defer func() {
		cancel()
		<-exited
	}()

	// The debug servers serve a single connection, so the first successful connection must be the proxied one:
	// a separate readiness probe would end the session.
	server, err := dialWithRetry(ctx, serverAddr, debugServerReadyTimeout, exited)
	if err != nil {
		return fmt.Errorf("debug server did not become ready: %w", err)
	}
	defer func() { _ = server.Close() }()

	_, _ = fmt.Fprintf(os.Stderr, "%s🐞 Debugger connected, attached to Envoy (PID %d)%s\n", internal.ANSIBold, pid, internal.ANSIReset)
	proxy(client, server)
	_, _ = fmt.Fprintf(os.Stderr, "%s🐞 Debugger disconnected, Envoy keeps running%s\n", internal.ANSIBold, internal.ANSIReset)
	return nil
}

// dialWithRetry connects to addr, retrying until it succeeds, the timeout elapses, the context is cancelled, or
// exited is closed.
func dialWithRetry(ctx context.Context, addr string, timeout time.Duration, exited <-chan struct{}) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			return conn, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-exited:
			return nil, errors.New("debug server exited")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("timed out connecting to %s after %s", addr, timeout)
}

// proxy copies data between the two connections until one of them is closed.
func proxy(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}

func (s *gdbRemoteServer) stop(_ *slog.Logger) {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *gdbRemoteServer) connectHint() string {
	hint := fmt.Sprintf("lldb -o 'gdb-remote 127.0.0.1:%d'", s.port)
	if s.goos != "darwin" {
		hint = fmt.Sprintf("gdb -ex 'target remote 127.0.0.1:%d'  or  %s", s.port, hint)
	}
	if s.envoyPath != "" {
		// CodeLLDB requires a target to attach: the Envoy binary.
		hint += fmt.Sprintf(`
  → %sVS Code (CodeLLDB):%s "targetCreateCommands": ["target create %s"],
                        "processCreateCommands": ["gdb-remote 127.0.0.1:%d"]`,
			internal.ANSIBold, internal.ANSIReset, s.envoyPath, s.port)
	}
	return hint
}

// freePort returns a free TCP port on the loopback interface.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}
