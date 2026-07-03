// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"io"
	"sync"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// connAdapter presents the network filter's callback I/O as a blocking
// io.ReadWriter so a WebSocket library can drive the connection from a
// goroutine. OnRead feeds bytes in from the worker thread. Write marshals bytes
// back onto the worker thread via the scheduler, because handle.Write is not
// safe to call from another goroutine.
type connAdapter struct {
	handle shared.NetworkFilterHandle
	sched  shared.Scheduler

	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newConnAdapter(handle shared.NetworkFilterHandle, sched shared.Scheduler) *connAdapter {
	a := &connAdapter{handle: handle, sched: sched}
	a.cond = sync.NewCond(&a.mu)
	return a
}

// feed appends downstream bytes (called from OnRead) and wakes a blocked Read.
func (a *connAdapter) feed(b []byte) {
	a.mu.Lock()
	a.buf = append(a.buf, b...)
	a.mu.Unlock()
	a.cond.Signal()
}

// close makes subsequent Reads drain remaining bytes then return io.EOF.
func (a *connAdapter) close() {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	a.cond.Broadcast()
}

func (a *connAdapter) Read(p []byte) (int, error) {
	a.mu.Lock()
	for len(a.buf) == 0 && !a.closed {
		a.cond.Wait()
	}
	if len(a.buf) == 0 {
		a.mu.Unlock()
		return 0, io.EOF
	}
	n := copy(p, a.buf)
	a.buf = a.buf[n:]
	a.mu.Unlock()
	return n, nil
}

func (a *connAdapter) Write(p []byte) (int, error) {
	b := append([]byte(nil), p...) // p is only valid for this call, the closure runs later
	a.sched.Schedule(func() { a.handle.Write(b, false) })
	return len(p), nil
}
