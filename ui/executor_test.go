// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package ui

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAnsiToHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text unchanged",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "HTML special chars escaped",
			input: "<script>&\"quotes\"</script>",
			want:  "&lt;script&gt;&amp;&#34;quotes&#34;&lt;/script&gt;",
		},
		{
			name:  "bold code",
			input: "\x1b[1mhello\x1b[0m",
			want:  `<span class="ansi-bold">hello</span>`,
		},
		{
			name:  "dim code",
			input: "\x1b[2mtext\x1b[0m",
			want:  `<span class="ansi-dim">text</span>`,
		},
		{
			name:  "italic code",
			input: "\x1b[3mtext\x1b[0m",
			want:  `<span class="ansi-italic">text</span>`,
		},
		{
			name:  "underline code",
			input: "\x1b[4mtext\x1b[0m",
			want:  `<span class="ansi-underline">text</span>`,
		},
		{
			name:  "green color",
			input: "\x1b[32mok\x1b[0m",
			want:  `<span style="color:#86efac">ok</span>`,
		},
		{
			name:  "red color",
			input: "\x1b[31merror\x1b[0m",
			want:  `<span style="color:#fca5a5">error</span>`,
		},
		{
			name:  "bright cyan color",
			input: "\x1b[96minfo\x1b[0m",
			want:  `<span style="color:#67e8f9">info</span>`,
		},
		{
			name:  "reset closes open spans",
			input: "\x1b[1mbold\x1b[0m plain",
			want:  `<span class="ansi-bold">bold</span> plain`,
		},
		{
			name:  "combined bold and color",
			input: "\x1b[1;32mgreen bold\x1b[0m",
			want:  `<span class="ansi-bold"><span style="color:#86efac">green bold</span></span>`,
		},
		{
			name:  "unclosed span closed at end",
			input: "\x1b[1munclosed",
			want:  `<span class="ansi-bold">unclosed</span>`,
		},
		{
			name:  "no ANSI codes",
			input: "just text with no codes",
			want:  "just text with no codes",
		},
		{
			name:  "unknown color code ignored",
			input: "\x1b[999mtext\x1b[0m",
			want:  "text",
		},
		{
			name:  "HTML chars inside ANSI span",
			input: "\x1b[32m<ok>\x1b[0m",
			want:  `<span style="color:#86efac">&lt;ok&gt;</span>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ansiToHTML(tt.input))
		})
	}
}

func TestBuildArgs(t *testing.T) {
	exts := []*ExtensionConfig{
		{Name: "opa", Config: `{"policy":"allow"}`},
		{Name: "cedar", Config: ""},
	}

	t.Run("run action includes base args and listen and admin ports", func(t *testing.T) {
		// We can only test that the flags are present (actual port values are dynamic)
		// Skip the darwin/amd64 docker flag since that's platform-specific
		args := buildArgs("run", []string{"--dev"}, exts)
		require.Equal(t, "run", args[0])
		require.Contains(t, args, "--listen-port")
		require.Contains(t, args, "--admin-port")
		require.Contains(t, args, "--dev")

		joined := strings.Join(args, " ")
		require.Contains(t, joined, "--extension opa")
		require.Contains(t, joined, "--extension cedar")
		require.Contains(t, joined, `--config {"policy":"allow"}`)
	})
}

func TestFreePorts(t *testing.T) {
	t.Run("returns requested number of ports", func(t *testing.T) {
		ports, err := freePorts(3)
		require.NoError(t, err)
		require.Len(t, ports, 3)

		seen := make(map[int]bool)
		for _, p := range ports {
			require.Positive(t, p)
			require.False(t, seen[p], "duplicate port %d", p)
			seen[p] = true
		}
	})

	t.Run("zero ports returns empty slice", func(t *testing.T) {
		ports, err := freePorts(0)
		require.NoError(t, err)
		require.Empty(t, ports)
	})
}

func TestSendSSE(t *testing.T) {
	var buf bytes.Buffer
	flushed := false
	flusher := &mockFlusherOnly{flush: func() { flushed = true }}
	sendSSE(&buf, flusher, "status", "started")
	require.Equal(t, "event: status\ndata: started\n\n", buf.String())
	require.True(t, flushed)
}

// mockFlusherOnly implements http.Flusher for use with sendSSE (which takes io.Writer + http.Flusher).
type mockFlusherOnly struct{ flush func() }

func (m *mockFlusherOnly) Flush() { m.flush() }

func TestRunParams_Args(t *testing.T) {
	require.Empty(t, (&RunParams{}).Args())

	args := (&RunParams{LogLevel: "debug", EnvoyVersion: "v1", EnvoyVersionsURL: "http://x", EnvoyPath: "/bin/envoy", Dev: true}).Args()
	require.Equal(t, []string{"--log-level", "debug", "--envoy-version", "v1", "--envoy-versions-url", "http://x", "--envoy-path", "/bin/envoy", "--dev"}, args)

	require.Equal(t, []string{"--log-level", "warn"}, (&RunParams{LogLevel: "warn"}).Args())
	require.Equal(t, []string{"--dev"}, (&RunParams{Dev: true}).Args())
}

func TestExecutorSelfExe(t *testing.T) {
	e := &Executor{logger: discardLogger()}
	require.NotEmpty(t, e.selfExe())
}

func TestExecutorStop_NoProcess(t *testing.T) {
	e := &Executor{logger: discardLogger()}
	err := e.Stop()
	require.Error(t, err)
	require.Contains(t, err.Error(), "no running process")
}

func TestExecutorStop_Running(t *testing.T) {
	e := &Executor{logger: discardLogger()}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "/bin/sleep", "30")
	require.NoError(t, cmd.Start())

	e.mu.Lock()
	e.running = cmd
	e.cancel = cancel
	e.mu.Unlock()

	require.NoError(t, e.Stop())
	require.Nil(t, e.running)
	_ = cmd.Wait() // reap
}

func TestKillProcessGroup_NilProcess(t *testing.T) {
	e := &Executor{logger: discardLogger()}
	cmd := &exec.Cmd{} // Process is nil
	require.NotPanics(t, func() { e.killProcessGroup(cmd) })
}

func TestRunStreaming(t *testing.T) {
	e := &Executor{logger: discardLogger(), exe: "/bin/echo"}
	w := &flushingRecorder{httptest.NewRecorder()}

	e.RunStreaming(context.Background(), []*ExtensionConfig{
		{Name: "opa", Config: ""},
		{Name: "local", Config: "", LocalPath: "/tmp/local"},
	}, w, w)

	body := w.Body.String()
	require.Contains(t, body, "event: status\ndata: started")
	require.Contains(t, body, "--extension opa")
	require.Contains(t, body, "--local /tmp/local")
	require.Contains(t, body, "event: status\ndata: completed")
}

func TestStreamCommand_Success(t *testing.T) {
	e := &Executor{logger: discardLogger(), exe: "/bin/echo"}
	w := &flushingRecorder{httptest.NewRecorder()}

	e.streamCommand(context.Background(), []string{"hello"}, w, w)

	body := w.Body.String()
	require.Contains(t, body, "event: status\ndata: started")
	require.Contains(t, body, "event: output")
	require.Contains(t, body, "event: status\ndata: completed")
}

func TestStreamCommand_ExitError(t *testing.T) {
	e := &Executor{logger: discardLogger(), exe: "/usr/bin/false"}
	w := &flushingRecorder{httptest.NewRecorder()}

	e.streamCommand(context.Background(), []string{}, w, w)

	body := w.Body.String()
	require.Contains(t, body, "event: status\ndata: started")
	require.Contains(t, body, "event: error")
}

func TestStreamCommand_Stopped(t *testing.T) {
	e := &Executor{logger: discardLogger(), exe: "/bin/sleep"}
	w := &flushingRecorder{httptest.NewRecorder()}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.streamCommand(context.Background(), []string{"30"}, w, w)
	}()

	// Wait until the process has started (SSE "started" is written).
	require.Eventually(t, func() bool {
		return strings.Contains(w.Body.String(), "event: status\ndata: started")
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, e.Stop())
	wg.Wait()

	require.Contains(t, w.Body.String(), "event: status\ndata: stopped")
}
