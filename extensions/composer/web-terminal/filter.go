// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package webterminal is an Envoy L4 network-filter dynamic module that hosts an
// interactive terminal over WebSocket inside the Envoy process. It terminates
// the WebSocket (gobwas/ws over an io.ReadWriter adapter) and bridges it to a
// PTY running a configured shell. See the manifest for details.
package webterminal

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const extensionName = "web-terminal"

// Wire protocol over WebSocket binary frames. Client to server frames are
// prefixed with an opcode byte ('0'=input, '1'=resize JSON {"cols","rows"}).
// Server to client frames are raw PTY output.
const (
	opInput  = '0'
	opResize = '1'
)

type terminalFilter struct {
	shared.EmptyNetworkFilter
	cfg     *config
	handle  shared.NetworkFilterHandle
	adapter *connAdapter
	once    sync.Once
	ptmx    *os.File
	cmd     *exec.Cmd
}

// OnNewConnection takes over the connection. Returning Stop keeps Envoy's
// terminal filter (appended after us) from ever running.
func (f *terminalFilter) OnNewConnection() shared.NetworkFilterStatus {
	f.adapter = newConnAdapter(f.handle, f.handle.GetScheduler())
	go f.serve()
	return shared.NetworkFilterStatusStop
}

func (f *terminalFilter) OnRead(data shared.NetworkBuffer, endOfStream bool) shared.NetworkFilterStatus {
	if n := data.GetSize(); n > 0 {
		b := make([]byte, 0, n)
		for _, c := range data.GetChunks() {
			b = append(b, c.ToUnsafeBytes()...) // chunks are only valid this callback
		}
		data.Drain(n)
		f.adapter.feed(b)
	}
	if endOfStream {
		f.adapter.close()
	}
	return shared.NetworkFilterStatusStop
}

func (f *terminalFilter) OnEvent(event shared.NetworkConnectionEvent) {
	if event == shared.NetworkConnectionEventRemoteClose || event == shared.NetworkConnectionEventLocalClose {
		f.shutdown()
	}
}

func (f *terminalFilter) OnDestroy() { f.shutdown() }

func (f *terminalFilter) shutdown() {
	f.once.Do(func() {
		if f.adapter != nil {
			f.adapter.close()
		}
		if f.ptmx != nil {
			_ = f.ptmx.Close()
		}
		if f.cmd != nil && f.cmd.Process != nil {
			_ = f.cmd.Process.Kill()
		}
	})
}

// serve bridges the WebSocket to a PTY, then closes the connection once either
// side ends.
func (f *terminalFilter) serve() {
	runSession(f.adapter, f.cfg, func(ptmx *os.File, cmd *exec.Cmd) {
		f.ptmx, f.cmd = ptmx, cmd // published so shutdown() can reap on disconnect
	}, f.handle.Log)
	f.closeConn()
}

// runSession bridges a WebSocket to a PTY. A plain GET gets the bundled page
// when serve_frontend is set.
func runSession(rw io.ReadWriter, cfg *config, onStart func(*os.File, *exec.Cmd), logf func(shared.LogLevel, string, ...any)) {
	conn := rw
	if cfg.ServeFrontend {
		br := bufio.NewReaderSize(rw, maxRequestHead)
		if !peekIsWebSocket(br) {
			serveFrontend(rw)
			return
		}
		conn = bufConn{r: br, w: rw}
	}
	if _, err := (ws.Upgrader{}).Upgrade(conn); err != nil {
		return
	}
	cmd := exec.Command(cfg.Command, cfg.Args...) //nolint:gosec // operator-configured, not attacker-controlled
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		if logf != nil {
			logf(shared.LogLevelError, "web-terminal: pty start: %s", err.Error())
		}
		return
	}
	if onStart != nil {
		onStart(ptmx, cmd)
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

	go func() {
		defer stop()
		buf := make([]byte, 4096)
		for {
			n, rerr := ptmx.Read(buf)
			if n > 0 {
				if wsutil.WriteServerBinary(conn, buf[:n]) != nil {
					return
				}
			}
			if rerr != nil {
				return
			}
		}
	}()

	go func() {
		defer stop()
		for {
			msg, _, rerr := wsutil.ReadClientData(conn)
			if rerr != nil {
				return
			}
			if len(msg) == 0 {
				continue
			}
			switch msg[0] {
			case opInput:
				if _, werr := ptmx.Write(msg[1:]); werr != nil {
					return
				}
			case opResize:
				var sz struct {
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if json.Unmarshal(msg[1:], &sz) == nil && sz.Cols > 0 && sz.Rows > 0 {
					_ = pty.Setsize(ptmx, &pty.Winsize{Cols: sz.Cols, Rows: sz.Rows})
				}
			}
		}
	}()

	<-done
}

// closeConn marshals a connection close onto Envoy's worker thread.
func (f *terminalFilter) closeConn() {
	f.adapter.sched.Schedule(func() {
		f.handle.Close(shared.NetworkConnectionCloseTypeFlushWrite)
	})
}

type filterFactory struct {
	shared.EmptyNetworkFilterFactory
	cfg *config
}

func (ff *filterFactory) Create(handle shared.NetworkFilterHandle) shared.NetworkFilter {
	return &terminalFilter{cfg: ff.cfg, handle: handle}
}

type configFactory struct {
	shared.EmptyNetworkFilterConfigFactory
}

func (cf *configFactory) Create(handle shared.NetworkFilterConfigHandle, unparsed []byte) (shared.NetworkFilterFactory, error) {
	cfg, err := parseConfig(unparsed)
	if err != nil {
		handle.Log(shared.LogLevelError, "web-terminal: %s", err.Error())
		return nil, err
	}
	return &filterFactory{cfg: cfg}, nil
}

// WellKnownNetworkFilterConfigFactories returns the factories this module registers with the host.
func WellKnownNetworkFilterConfigFactories() map[string]shared.NetworkFilterConfigFactory { //nolint:revive
	return map[string]shared.NetworkFilterConfigFactory{extensionName: &configFactory{}}
}
