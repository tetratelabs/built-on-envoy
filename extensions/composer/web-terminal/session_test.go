// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPTYSessionEchoAndResize(t *testing.T) {
	sess, err := newPTYSession(&terminalConfig{Command: "cat", Writable: true}, 80, 24)
	require.NoError(t, err)
	t.Cleanup(sess.close)

	out := make(chan []byte, 64)
	var mu sync.Mutex
	var buf strings.Builder
	go sess.pump(func(chunk []byte) {
		mu.Lock()
		buf.Write(chunk)
		mu.Unlock()
		select {
		case out <- chunk:
		default:
		}
	}, func() { close(out) })

	sess.resize(100, 40) // must not panic
	require.NoError(t, sess.write([]byte("echo-me\n")))

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-out:
			mu.Lock()
			got := buf.String()
			mu.Unlock()
			if strings.Contains(got, "echo-me") {
				return
			}
		case <-deadline:
			t.Fatalf("did not observe echoed input")
		}
	}
}

func TestPTYSessionCloseIdempotent(t *testing.T) {
	sess, err := newPTYSession(&terminalConfig{Command: "cat"}, 80, 24)
	require.NoError(t, err)
	sess.close()
	sess.close() // must be safe to call again
}

func TestRegistry(t *testing.T) {
	r := newRegistry()
	sess, err := newPTYSession(&terminalConfig{Command: "cat"}, 80, 24)
	require.NoError(t, err)
	t.Cleanup(sess.close)

	_, ok := r.get("a")
	require.False(t, ok)

	r.set("a", sess)
	got, ok := r.get("a")
	require.True(t, ok)
	require.Same(t, sess, got)

	r.remove("a")
	_, ok = r.get("a")
	require.False(t, ok)
}
