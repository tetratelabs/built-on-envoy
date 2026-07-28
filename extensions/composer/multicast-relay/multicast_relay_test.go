// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package multicastrelay

import (
	"fmt"
	"net"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/multicast-relay/relay"
)

func TestParseConfigErrors(t *testing.T) {
	for _, tc := range []struct {
		name         string
		raw          string
		errorMessage string
	}{{
		name:         "empty",
		raw:          "",
		errorMessage: "empty config",
	}, {
		name:         "not JSON",
		raw:          "foo",
		errorMessage: "parse config: invalid character 'o' in literal false (expecting 'a')",
	}, {
		name:         "unknown field",
		raw:          `{"groups":[{"address":"239.1.1.1","port":5000}],"target":{"port":1},"bogus":true}`,
		errorMessage: `parse config: json: unknown field "bogus"`,
	}, {
		name:         "fails validation",
		raw:          `{"groups":[],"target":{"port":10000}}`,
		errorMessage: "groups must contain at least one entry",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseConfig([]byte(tc.raw))
			require.EqualError(t, err, tc.errorMessage)
			_ = cfg
		})
	}
}

func TestParseConfigAppliesDefaults(t *testing.T) {
	cfg, err := parseConfig([]byte(`{
		"groups":[{"address":"239.1.1.1","port":5000,"interface":"lo0"}],
		"target":{"port":10000}
	}`))
	require.NoError(t, err)
	require.Equal(t, &relay.Config{
		Groups:         []relay.GroupSpec{{Address: "239.1.1.1", Port: 5000, Interface: "lo0"}},
		Target:         relay.Target{Address: "127.0.0.1", Port: 10000},
		ReadBufferSize: 65535,
	}, cfg)
}

func TestPluginConfigFactoryCreateRejectsBadConfig(t *testing.T) {
	handle := mocks.NewMockHttpFilterConfigHandle(gomock.NewController(t))
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory, err := (&PluginConfigFactory{}).Create(handle, []byte(`{"groups":[]}`))
	require.Error(t, err)
	require.Nil(t, factory)
}

// A group join that cannot succeed must fail the filter config, so Envoy rejects it
// rather than running a relay that silently receives nothing.
func TestPluginConfigFactoryCreateRejectsUnknownInterface(t *testing.T) {
	handle := mocks.NewMockHttpFilterConfigHandle(gomock.NewController(t))
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory, err := (&PluginConfigFactory{}).Create(handle, []byte(
		`{"groups":[{"address":"239.1.1.1","port":5000,"interface":"nope0"}],"target":{"port":10000}}`))
	require.ErrorContains(t, err, `interface "nope0"`)
	require.Nil(t, factory)
}

func TestPluginConfigFactoryCreatePerRouteIsNoOp(t *testing.T) {
	perRoute, err := (&PluginConfigFactory{}).CreatePerRoute([]byte(`{"any":"thing"}`))
	require.NoError(t, err)
	require.Nil(t, perRoute)
}

func TestPluginConfigFactoryCreateStartsAndStopsTheRelay(t *testing.T) {
	handle := mocks.NewMockHttpFilterConfigHandle(gomock.NewController(t))
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory, err := (&PluginConfigFactory{}).Create(handle, []byte(fmt.Sprintf(
		`{"groups":[{"address":"239.77.77.90","port":%d,"interface":%q}],"target":{"port":%d}}`,
		freePort(t), loopbackInterface(t), freePort(t))))
	require.NoError(t, err)

	pluginFactory, ok := factory.(*PluginFactory)
	require.True(t, ok)
	require.NotEmpty(t, pluginFactory.key)

	// The filter itself is inert and shared across streams.
	require.Same(t, pluginInstance, pluginFactory.Create(nil))

	pluginFactory.OnDestroy()
	require.Empty(t, pluginFactory.key, "OnDestroy must clear the key so a second call cannot double-release")
	pluginFactory.OnDestroy()
}

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.Len(t, factories, 1)
	require.Contains(t, factories, ExtensionName)
	require.NotNil(t, factories[ExtensionName])
}

func loopbackInterface(t *testing.T) string {
	t.Helper()

	ifaces, err := net.Interfaces()
	require.NoError(t, err)
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagLoopback != 0 && ifi.Flags&net.FlagUp != 0 && ifi.Flags&net.FlagMulticast != 0 {
			return ifi.Name
		}
	}
	t.Skip("no multicast-capable loopback interface available")
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	require.NoError(t, conn.Close())
	return addr.Port
}
