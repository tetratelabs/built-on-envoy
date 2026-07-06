// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"io"
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/stretchr/testify/require"
)

// inlineScheduler runs scheduled work synchronously so Write is observable.
type inlineScheduler struct{}

func (inlineScheduler) Schedule(f func()) { f() }

// writeHandle captures Write; other handle methods are unused here.
type writeHandle struct {
	shared.NetworkFilterHandle
	got []byte
}

func (h *writeHandle) Write(b []byte, _ bool) { h.got = append(h.got, b...) }

func TestConnAdapterReadWriteClose(t *testing.T) {
	h := &writeHandle{}
	a := newConnAdapter(h, inlineScheduler{})

	n, err := a.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, "hello", string(h.got))

	a.feed([]byte("abc"))
	buf := make([]byte, 2)
	n, err = a.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "ab", string(buf[:n]))
	n, err = a.Read(buf)
	require.NoError(t, err)
	require.Equal(t, "c", string(buf[:n]))

	a.close()
	_, err = a.Read(buf)
	require.ErrorIs(t, err, io.EOF)
}

func TestConnAdapterBlocksUntilClose(t *testing.T) {
	a := newConnAdapter(&writeHandle{}, inlineScheduler{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		a.close()
	}()
	_, err := a.Read(make([]byte, 4)) // blocks until close, then EOF
	require.ErrorIs(t, err, io.EOF)
}
