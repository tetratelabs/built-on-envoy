// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// ptySession owns a shell process running inside a PTY for one client stream.
type ptySession struct {
	ptmx *os.File
	cmd  *exec.Cmd
	once sync.Once
}

func newPTYSession(cfg *terminalConfig, cols, rows uint16) (*ptySession, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...) //nolint:gosec // operator-configured, not attacker-controlled
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	if cols > 0 && rows > 0 {
		_ = pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows})
	}
	return &ptySession{ptmx: ptmx, cmd: cmd}, nil
}

func (s *ptySession) write(b []byte) error {
	_, err := s.ptmx.Write(b)
	return err
}

func (s *ptySession) resize(cols, rows uint16) {
	if cols > 0 && rows > 0 {
		_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
	}
}

// pump forwards PTY output to onOutput until the PTY closes, then calls onClose
// once. Each chunk is a fresh copy safe to retain.
func (s *ptySession) pump(onOutput func([]byte), onClose func()) {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			onOutput(chunk)
		}
		if err != nil {
			onClose()
			return
		}
	}
}

// close releases the PTY and reaps the shell. Safe to call more than once.
func (s *ptySession) close() {
	s.once.Do(func() {
		_ = s.ptmx.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		_ = s.cmd.Wait()
	})
}

// registry maps session id -> ptySession, shared across the stream, input, and
// resize requests (separate HTTP requests correlated by that id).
type registry struct {
	mu sync.Mutex
	m  map[string]*ptySession
}

func newRegistry() *registry {
	return &registry{m: make(map[string]*ptySession)}
}

func (r *registry) set(id string, s *ptySession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[id] = s
}

func (r *registry) get(id string) (*ptySession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.m[id]
	return s, ok
}

func (r *registry) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}
