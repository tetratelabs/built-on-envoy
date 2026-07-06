// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package webterminal hosts an interactive terminal inside Envoy: it streams a
// PTY to the browser over Server-Sent Events and takes input via short POSTs.
// See the extension manifest for the protocol and usage.
package webterminal

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"sync/atomic"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

const extensionName = "web-terminal"

type terminalConfig struct {
	Command       string   `json:"command"`
	Args          []string `json:"args"`
	Writable      bool     `json:"writable"`
	ServeFrontend bool     `json:"serve_frontend"`
}

func parseConfig(raw []byte) (*terminalConfig, error) {
	cfg := &terminalConfig{Command: "/bin/bash", Writable: true}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
	}
	if cfg.Command == "" {
		return nil, fmt.Errorf("invalid config: command must not be empty")
	}
	return cfg, nil
}

type action int

const (
	actionNone action = iota
	actionInput
)

type terminalFilter struct {
	shared.EmptyHttpFilter
	cfg    *terminalConfig
	handle shared.HttpFilterHandle
	reg    *registry

	act     action
	sid     string
	input   []byte
	session *ptySession
	closed  atomic.Bool
}

func (f *terminalFilter) OnRequestHeaders(headers shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	u, err := url.Parse(headers.GetOne(":path").ToUnsafeString())
	if err != nil {
		f.sendPlain(400, "Bad Request", "web_terminal_bad_path")
		return shared.HeadersStatusStopAllAndBuffer
	}
	method := headers.GetOne(":method").ToUnsafeString()
	q := u.Query()

	switch u.Path {
	case "/", "/index.html":
		switch {
		case !f.cfg.ServeFrontend:
			f.sendPlain(404, "Not Found", "web_terminal_frontend_disabled")
		case method != "GET":
			f.sendPlain(405, "Method Not Allowed", "web_terminal_method")
		default:
			f.serveFrontend()
		}
		return shared.HeadersStatusStopAllAndBuffer

	case "/stream":
		if method != "GET" {
			f.sendPlain(405, "Method Not Allowed", "web_terminal_method")
			return shared.HeadersStatusStopAllAndBuffer
		}
		return f.startStream(q)

	case "/input":
		if method != "POST" {
			f.sendPlain(405, "Method Not Allowed", "web_terminal_method")
			return shared.HeadersStatusStopAllAndBuffer
		}
		f.sid = q.Get("sid")
		f.act = actionInput
		if endOfStream {
			f.sendPlain(204, "", "web_terminal_input")
			return shared.HeadersStatusStopAllAndBuffer
		}
		return shared.HeadersStatusStop

	case "/resize":
		if method != "POST" {
			f.sendPlain(405, "Method Not Allowed", "web_terminal_method")
			return shared.HeadersStatusStopAllAndBuffer
		}
		if sess, ok := f.reg.get(q.Get("sid")); ok {
			sess.resize(parseDim(q.Get("cols"), 0), parseDim(q.Get("rows"), 0))
			f.sendPlain(204, "", "web_terminal_resize")
		} else {
			f.sendPlain(404, "No such session", "web_terminal_no_session")
		}
		return shared.HeadersStatusStopAllAndBuffer

	default:
		f.sendPlain(404, "Not Found", "web_terminal_not_found")
		return shared.HeadersStatusStopAllAndBuffer
	}
}

func (f *terminalFilter) startStream(q url.Values) shared.HeadersStatus {
	sid := q.Get("sid")
	if sid == "" {
		f.sendPlain(400, "missing sid", "web_terminal_missing_sid")
		return shared.HeadersStatusStopAllAndBuffer
	}
	sess, err := newPTYSession(f.cfg, parseDim(q.Get("cols"), 80), parseDim(q.Get("rows"), 24))
	if err != nil {
		f.handle.Log(shared.LogLevelError, "web-terminal: failed to start pty: %s", err.Error())
		f.sendPlain(500, "failed to start terminal", "web_terminal_pty_error")
		return shared.HeadersStatusStopAllAndBuffer
	}
	f.reg.set(sid, sess)
	f.sid = sid
	f.session = sess

	f.handle.SendResponseHeaders([][2]string{
		{":status", "200"},
		{"content-type", "text/event-stream"},
		{"cache-control", "no-cache"},
		{"x-accel-buffering", "no"},
	}, false)

	sched := f.handle.GetScheduler()
	go sess.pump(
		func(chunk []byte) {
			frame := []byte("data: " + base64.StdEncoding.EncodeToString(chunk) + "\n\n")
			sched.Schedule(func() {
				if !f.closed.Load() {
					f.handle.SendResponseData(frame, false)
				}
			})
		},
		func() {
			sched.Schedule(func() {
				// closed guards against writing after OnStreamComplete releases the handle.
				if !f.closed.Swap(true) {
					f.handle.SendResponseData([]byte("event: exit\ndata: \n\n"), true)
				}
			})
		},
	)
	return shared.HeadersStatusStopAllAndBuffer
}

func (f *terminalFilter) OnRequestBody(body shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if f.act != actionInput {
		return shared.BodyStatusContinue
	}
	if body != nil {
		for _, chunk := range body.GetChunks() {
			f.input = append(f.input, chunk.ToUnsafeBytes()...)
		}
	}
	if !endOfStream {
		return shared.BodyStatusStopAndBuffer
	}

	sess, ok := f.reg.get(f.sid)
	if !ok {
		f.sendPlain(404, "No such session", "web_terminal_no_session")
		return shared.BodyStatusStopAndBuffer
	}
	if f.cfg.Writable {
		if err := sess.write(f.input); err != nil {
			f.sendPlain(500, "write failed", "web_terminal_write_error")
			return shared.BodyStatusStopAndBuffer
		}
	}
	f.sendPlain(204, "", "web_terminal_input")
	return shared.BodyStatusStopAndBuffer
}

func (f *terminalFilter) OnStreamComplete() {
	f.closed.Store(true)
	if f.session != nil {
		f.session.close()
		f.reg.remove(f.sid)
	}
}

func (f *terminalFilter) sendPlain(status uint32, body, detail string) {
	f.handle.SendLocalResponse(status, [][2]string{{"content-type", "text/plain"}}, []byte(body), detail)
}

func parseDim(s string, def uint16) uint16 {
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return def
	}
	return uint16(n)
}

type filterFactory struct {
	shared.EmptyHttpFilterFactory
	cfg *terminalConfig
	reg *registry
}

func (f *filterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &terminalFilter{cfg: f.cfg, handle: handle, reg: f.reg}
}

type configFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *configFactory) Create(handle shared.HttpFilterConfigHandle, unparsed []byte) (shared.HttpFilterFactory, error) {
	cfg, err := parseConfig(unparsed)
	if err != nil {
		handle.Log(shared.LogLevelError, "web-terminal: %s", err.Error())
		return nil, err
	}
	return &filterFactory{cfg: cfg, reg: newRegistry()}, nil
}

func (f *configFactory) CreatePerRoute(_ []byte) (any, error) { return nil, nil }

// WellKnownHttpFilterConfigFactories returns the factories this plugin registers with the composer host.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		extensionName: &configFactory{},
	}
}
