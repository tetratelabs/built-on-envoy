// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// syncScheduler runs scheduled functions inline so tests observe SendResponseData
// deterministically.
type syncScheduler struct{}

func (syncScheduler) Schedule(f func()) { f() }

type frame struct {
	data []byte
	end  bool
}

func newHandle(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

func headerMap(method, path string) shared.HeaderMap {
	return fake.NewFakeHeaderMap(map[string][]string{":method": {method}, ":path": {path}})
}

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig(nil)
	require.NoError(t, err)
	require.Equal(t, "/bin/bash", cfg.Command)
	require.True(t, cfg.Writable)
	require.False(t, cfg.ServeFrontend) // frontend off by default

	cfg, err = parseConfig([]byte(`{"command":"sh","args":["-c","x"],"writable":false,"serve_frontend":true}`))
	require.NoError(t, err)
	require.Equal(t, "sh", cfg.Command)
	require.Equal(t, []string{"-c", "x"}, cfg.Args)
	require.False(t, cfg.Writable)
	require.True(t, cfg.ServeFrontend)

	_, err = parseConfig([]byte(`{`))
	require.Error(t, err)
	_, err = parseConfig([]byte(`{"command":""}`))
	require.Error(t, err)
}

func TestFrontendServedWhenEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	h := newHandle(ctrl)
	var body []byte
	h.EXPECT().SendResponseHeaders(gomock.Any(), false)
	h.EXPECT().SendResponseData(gomock.Any(), true).Do(func(b []byte, _ bool) { body = b })

	f := &terminalFilter{cfg: &terminalConfig{Command: "cat", Writable: true, ServeFrontend: true}, handle: h, reg: newRegistry()}
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, f.OnRequestHeaders(headerMap("GET", "/"), true))
	require.Contains(t, string(body), "xterm")
}

func TestFrontendDisabledByDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	h := newHandle(ctrl)
	h.EXPECT().SendLocalResponse(uint32(404), gomock.Any(), gomock.Any(), "web_terminal_frontend_disabled")

	f := &terminalFilter{cfg: &terminalConfig{Command: "cat", Writable: true}, handle: h, reg: newRegistry()}
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, f.OnRequestHeaders(headerMap("GET", "/"), true))
}

func TestFrontendWrongMethod(t *testing.T) {
	ctrl := gomock.NewController(t)
	h := newHandle(ctrl)
	h.EXPECT().SendLocalResponse(uint32(405), gomock.Any(), gomock.Any(), "web_terminal_method")

	f := &terminalFilter{cfg: &terminalConfig{Command: "cat", Writable: true, ServeFrontend: true}, handle: h, reg: newRegistry()}
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer, f.OnRequestHeaders(headerMap("POST", "/"), true))
}

func TestRoutingErrors(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus uint32
	}{
		{"unknown path", "GET", "/nope", 404},
		{"stream wrong method", "POST", "/stream?sid=a", 405},
		{"stream missing sid", "GET", "/stream", 400},
		{"input wrong method", "GET", "/input?sid=a", 405},
		{"resize unknown session", "POST", "/resize?sid=ghost", 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			h := newHandle(ctrl)
			h.EXPECT().SendLocalResponse(tt.wantStatus, gomock.Any(), gomock.Any(), gomock.Any())
			f := &terminalFilter{cfg: &terminalConfig{Command: "cat", Writable: true}, handle: h, reg: newRegistry()}
			require.Equal(t, shared.HeadersStatusStopAllAndBuffer, f.OnRequestHeaders(headerMap(tt.method, tt.path), true))
		})
	}
}

func TestStreamOutputAndExit(t *testing.T) {
	ctrl := gomock.NewController(t)
	h := newHandle(ctrl)
	frames := make(chan frame, 128)
	h.EXPECT().GetScheduler().Return(syncScheduler{}).AnyTimes()
	h.EXPECT().SendResponseHeaders(gomock.Any(), false).AnyTimes()
	h.EXPECT().SendResponseData(gomock.Any(), gomock.Any()).Do(func(b []byte, end bool) {
		cp := make([]byte, len(b))
		copy(cp, b)
		frames <- frame{cp, end}
	}).AnyTimes()

	f := (&filterFactory{cfg: &terminalConfig{Command: "sh", Args: []string{"-c", "printf hello-world"}, Writable: true}, reg: newRegistry()}).
		Create(h).(*terminalFilter)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer,
		f.OnRequestHeaders(headerMap("GET", "/stream?sid=s1&cols=80&rows=24"), true))

	var sawOutput, sawExit bool
	deadline := time.After(5 * time.Second)
	for !sawOutput || !sawExit {
		select {
		case fr := <-frames:
			if fr.end {
				sawExit = true
			} else if strings.Contains(decodeSSE(fr.data), "hello-world") {
				sawOutput = true
			}
		case <-deadline:
			t.Fatalf("output=%v exit=%v", sawOutput, sawExit)
		}
	}
	f.OnStreamComplete()
}

func TestStreamInputEcho(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := &filterFactory{cfg: &terminalConfig{Command: "cat", Writable: true}, reg: newRegistry()}

	streamH := newHandle(ctrl)
	frames := make(chan frame, 128)
	streamH.EXPECT().GetScheduler().Return(syncScheduler{}).AnyTimes()
	streamH.EXPECT().SendResponseHeaders(gomock.Any(), false).AnyTimes()
	streamH.EXPECT().SendResponseData(gomock.Any(), gomock.Any()).Do(func(b []byte, end bool) {
		cp := make([]byte, len(b))
		copy(cp, b)
		frames <- frame{cp, end}
	}).AnyTimes()
	streamF := factory.Create(streamH).(*terminalFilter)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer,
		streamF.OnRequestHeaders(headerMap("GET", "/stream?sid=s1&cols=80&rows=24"), true))

	inputH := newHandle(ctrl)
	inputH.EXPECT().SendLocalResponse(uint32(204), gomock.Any(), gomock.Any(), gomock.Any())
	inputF := factory.Create(inputH).(*terminalFilter)
	require.Equal(t, shared.HeadersStatusStop,
		inputF.OnRequestHeaders(headerMap("POST", "/input?sid=s1"), false))
	require.Equal(t, shared.BodyStatusStopAndBuffer,
		inputF.OnRequestBody(fake.NewFakeBodyBuffer([]byte("echoed-input\n")), true))

	deadline := time.After(5 * time.Second)
	for {
		select {
		case fr := <-frames:
			if !fr.end && strings.Contains(decodeSSE(fr.data), "echoed-input") {
				streamF.OnStreamComplete()
				time.Sleep(100 * time.Millisecond)
				return
			}
		case <-deadline:
			t.Fatal("did not observe echoed input on the stream")
		}
	}
}

func TestInputUnknownSession(t *testing.T) {
	ctrl := gomock.NewController(t)
	h := newHandle(ctrl)
	h.EXPECT().SendLocalResponse(uint32(404), gomock.Any(), gomock.Any(), gomock.Any())
	f := &terminalFilter{cfg: &terminalConfig{Command: "cat", Writable: true}, handle: h, reg: newRegistry()}
	require.Equal(t, shared.HeadersStatusStop, f.OnRequestHeaders(headerMap("POST", "/input?sid=ghost"), false))
	require.Equal(t, shared.BodyStatusStopAndBuffer, f.OnRequestBody(fake.NewFakeBodyBuffer([]byte("x")), true))
}

func TestResizeExistingSession(t *testing.T) {
	ctrl := gomock.NewController(t)
	factory := &filterFactory{cfg: &terminalConfig{Command: "cat", Writable: true}, reg: newRegistry()}

	streamH := newHandle(ctrl)
	streamH.EXPECT().GetScheduler().Return(syncScheduler{}).AnyTimes()
	streamH.EXPECT().SendResponseHeaders(gomock.Any(), false).AnyTimes()
	streamH.EXPECT().SendResponseData(gomock.Any(), gomock.Any()).AnyTimes()
	streamF := factory.Create(streamH).(*terminalFilter)
	streamF.OnRequestHeaders(headerMap("GET", "/stream?sid=s1&cols=80&rows=24"), true)
	t.Cleanup(streamF.OnStreamComplete)

	resizeH := newHandle(ctrl)
	resizeH.EXPECT().SendLocalResponse(uint32(204), gomock.Any(), gomock.Any(), gomock.Any())
	resizeF := factory.Create(resizeH).(*terminalFilter)
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer,
		resizeF.OnRequestHeaders(headerMap("POST", "/resize?sid=s1&cols=120&rows=40"), true))
}

func TestInputEmptyBodyAcknowledged(t *testing.T) {
	ctrl := gomock.NewController(t)
	h := newHandle(ctrl)
	h.EXPECT().SendLocalResponse(uint32(204), gomock.Any(), gomock.Any(), gomock.Any())
	f := &terminalFilter{cfg: &terminalConfig{Command: "cat", Writable: true}, handle: h, reg: newRegistry()}
	require.Equal(t, shared.HeadersStatusStopAllAndBuffer,
		f.OnRequestHeaders(headerMap("POST", "/input?sid=s1"), true))
}

func TestConfigFactory(t *testing.T) {
	ctrl := gomock.NewController(t)
	h := mocks.NewMockHttpFilterConfigHandle(ctrl)
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &configFactory{}
	ff, err := factory.Create(h, []byte(`{"command":"cat"}`))
	require.NoError(t, err)
	require.NotNil(t, ff)

	_, err = factory.Create(h, []byte(`{"command":""}`))
	require.Error(t, err)

	perRoute, err := factory.CreatePerRoute([]byte(`{}`))
	require.NoError(t, err)
	require.Nil(t, perRoute)
}

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.Len(t, factories, 1)
	require.Contains(t, factories, "web-terminal")
}

func TestParseDim(t *testing.T) {
	require.Equal(t, uint16(24), parseDim("", 24))
	require.Equal(t, uint16(24), parseDim("bogus", 24))
	require.Equal(t, uint16(120), parseDim("120", 24))
}

func decodeSSE(frame []byte) string {
	s := strings.TrimSpace(strings.TrimPrefix(string(frame), "data: "))
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	return string(b)
}
