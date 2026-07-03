// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import (
	"testing"
	"time"
	"unsafe"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestFilterLifecycle exercises the SDK glue: OnNewConnection starts serve(),
// OnRead feeds the adapter, a non-WebSocket request makes serve() close, and
// OnEvent/OnDestroy tear down idempotently. A scheduler that runs inline lets
// the scheduled handle.Close happen synchronously.
func TestFilterLifecycle(t *testing.T) {
	ctrl := gomock.NewController(t)
	sch := mocks.NewMockScheduler(ctrl)
	sch.EXPECT().Schedule(gomock.Any()).Do(func(f func()) { f() }).AnyTimes()
	h := mocks.NewMockNetworkFilterHandle(ctrl)
	h.EXPECT().GetScheduler().Return(sch).AnyTimes()
	h.EXPECT().Write(gomock.Any(), gomock.Any()).AnyTimes()
	h.EXPECT().Close(gomock.Any()).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	f := (&filterFactory{cfg: &config{Command: "cat"}}).Create(h).(*terminalFilter)
	require.Equal(t, shared.NetworkFilterStatusStop, f.OnNewConnection())

	data := []byte("GET / HTTP/1.1\r\nhost: x\r\n\r\n") // valid HTTP, but not a WS upgrade
	buf := mocks.NewMockNetworkBuffer(ctrl)
	buf.EXPECT().GetSize().Return(uint64(len(data)))
	buf.EXPECT().GetChunks().Return([]shared.UnsafeEnvoyBuffer{{Ptr: unsafe.SliceData(data), Len: uint64(len(data))}}) //nolint:gosec // an Envoy buffer view over test bytes
	buf.EXPECT().Drain(uint64(len(data))).Return(true)
	require.Equal(t, shared.NetworkFilterStatusStop, f.OnRead(buf, false))

	time.Sleep(150 * time.Millisecond) // let serve() consume the request and close
	f.OnEvent(shared.NetworkConnectionEventRemoteClose)
	f.OnDestroy() // idempotent
}

func TestConfigFactory(t *testing.T) {
	ctrl := gomock.NewController(t)
	h := mocks.NewMockNetworkFilterConfigHandle(ctrl)
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	ff, err := (&configFactory{}).Create(h, []byte(`{"command":"cat"}`))
	require.NoError(t, err)
	require.NotNil(t, ff)

	_, err = (&configFactory{}).Create(h, []byte(`{"command":""}`))
	require.Error(t, err)
}

func TestWellKnownNetworkFilterConfigFactories(t *testing.T) {
	require.Contains(t, WellKnownNetworkFilterConfigFactories(), "web-terminal")
}
