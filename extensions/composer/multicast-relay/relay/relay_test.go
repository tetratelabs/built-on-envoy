// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package relay

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/ipv4"
)

func TestConfigValidateErrors(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cfg          Config
		errorMessage string
	}{{
		name:         "no groups",
		cfg:          Config{Target: Target{Port: 10000}},
		errorMessage: "groups must contain at least one entry",
	}, {
		name: "address missing",
		cfg: Config{
			Groups: []GroupSpec{{Port: 5000}},
			Target: Target{Port: 10000},
		},
		errorMessage: "groups[0]: address is required",
	}, {
		name: "address not an IP",
		cfg: Config{
			Groups: []GroupSpec{{Address: "foo", Port: 5000}},
			Target: Target{Port: 10000},
		},
		errorMessage: `groups[0]: address "foo" is not a valid IP`,
	}, {
		name: "address is IPv6",
		cfg: Config{
			Groups: []GroupSpec{{Address: "ff02::1", Port: 5000}},
			Target: Target{Port: 10000},
		},
		errorMessage: `groups[0]: address "ff02::1" must be IPv4`,
	}, {
		name: "address is not multicast",
		cfg: Config{
			Groups: []GroupSpec{{Address: "10.0.0.1", Port: 5000}},
			Target: Target{Port: 10000},
		},
		errorMessage: `groups[0]: address "10.0.0.1" is not a multicast address`,
	}, {
		name: "group port out of range",
		cfg: Config{
			Groups: []GroupSpec{{Address: "239.1.1.1", Port: 70000}},
			Target: Target{Port: 10000},
		},
		errorMessage: "groups[0]: port 70000 out of range 1-65535",
	}, {
		name: "duplicate group",
		cfg: Config{
			Groups: []GroupSpec{
				{Address: "239.1.1.1", Port: 5000, Interface: "lo0"},
				{Address: "239.1.1.1", Port: 5000, Interface: "lo0"},
			},
			Target: Target{Port: 10000},
		},
		errorMessage: "groups[1]: duplicate group 239.1.1.1:5000@lo0",
	}, {
		name: "target port missing",
		cfg: Config{
			Groups: []GroupSpec{{Address: "239.1.1.1", Port: 5000}},
		},
		errorMessage: "target.port 0 out of range 1-65535",
	}, {
		name: "read buffer too small",
		cfg: Config{
			Groups:         []GroupSpec{{Address: "239.1.1.1", Port: 5000}},
			Target:         Target{Port: 10000},
			ReadBufferSize: 8,
		},
		errorMessage: "read_buffer_size 8 out of range 512-65536",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			require.EqualError(t, cfg.Validate(), tc.errorMessage)
		})
	}
}

func TestConfigValidateAppliesDefaults(t *testing.T) {
	cfg := Config{
		Groups: []GroupSpec{{Address: "239.1.1.1", Port: 5000}},
		Target: Target{Port: 10000},
	}
	require.NoError(t, cfg.Validate())
	require.Equal(t, Config{
		Groups:         []GroupSpec{{Address: "239.1.1.1", Port: 5000}},
		Target:         Target{Address: "127.0.0.1", Port: 10000},
		ReadBufferSize: 65535,
	}, cfg)
}

func TestRelayStartRejectsUnknownInterface(t *testing.T) {
	cfg := Config{
		Groups: []GroupSpec{{Address: "239.77.77.77", Port: freeUDPPort(t), Interface: "nope0"}},
		Target: Target{Port: freeUDPPort(t)},
	}
	require.NoError(t, cfg.Validate())

	r := New(&cfg)
	require.ErrorContains(t, r.Start(context.Background()), `interface "nope0"`)
}

// A partially-started relay must not keep the sockets it already opened: each one holds
// an fd and a group membership, so a config that Envoy rejects would otherwise leak both
// on every reload attempt.
func TestRelayStartClosesSocketsOnFailure(t *testing.T) {
	ifi := multicastInterface(t)
	cfg := Config{
		Groups: []GroupSpec{
			{Address: "239.77.77.85", Port: freeUDPPort(t), Interface: ifi.Name},
			{Address: "239.77.77.86", Port: freeUDPPort(t), Interface: "nope0"},
		},
		Target: Target{Port: freeUDPPort(t)},
	}
	require.NoError(t, cfg.Validate())

	r := New(&cfg)
	require.ErrorContains(t, r.Start(context.Background()), `interface "nope0"`)
	require.Empty(t, r.sockets, "the socket opened for the first group must be closed and dropped")
	require.Empty(t, r.LocalAddrs())
}

// TestRelayForwardsToTarget is the load-bearing test: it proves a real IGMP join
// receives real multicast traffic and that the payload lands on the unicast target
// with a unicast source address, which is the whole reason for the loopback hop.
func TestRelayForwardsToTarget(t *testing.T) {
	ifi := multicastInterface(t)
	sink := udpSink(t)
	group, port := "239.77.77.78", freeUDPPort(t)

	cfg := Config{
		Groups: []GroupSpec{{Address: group, Port: port, Interface: ifi.Name}},
		Target: Target{Port: sinkPort(t, sink)},
	}
	require.NoError(t, cfg.Validate())

	r := New(&cfg)
	require.NoError(t, r.Start(context.Background()))
	defer r.Stop()

	sendMulticast(t, ifi, group, port, "🍎", "🧨")

	payload, src := readSink(t, sink)
	require.Equal(t, "🍎", payload)
	require.True(t, src.IP.IsLoopback(),
		"forwarded source must be unicast loopback so udp_proxy's reply path is valid, got %s", src.IP)

	payload, _ = readSink(t, sink)
	require.Equal(t, "🧨", payload)

	require.Equal(t, Stats{Received: 2, Forwarded: 2, Bytes: 8}, r.Stats())
}

// TestRelayIgnoresTrafficForOtherDestinations covers the wildcard bind: the socket
// also receives unicast on the group's port, which must not be relayed.
func TestRelayIgnoresTrafficForOtherDestinations(t *testing.T) {
	ifi := multicastInterface(t)
	sink := udpSink(t)
	group, port := "239.77.77.79", freeUDPPort(t)

	cfg := Config{
		Groups: []GroupSpec{{Address: group, Port: port, Interface: ifi.Name}},
		Target: Target{Port: sinkPort(t, sink)},
	}
	require.NoError(t, cfg.Validate())

	r := New(&cfg)
	require.NoError(t, r.Start(context.Background()))
	defer r.Stop()

	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.Write([]byte("bar"))
	require.NoError(t, err)

	require.Eventually(t, func() bool { return r.Stats().Foreign == 1 }, 2*time.Second, 10*time.Millisecond,
		"unicast datagram on the group port should be counted foreign, stats: %+v", r.Stats())
	require.Equal(t, Stats{Received: 1, Foreign: 1}, r.Stats())
}

func TestRelayCountsDropsWhenTargetIsUnwritable(t *testing.T) {
	ifi := multicastInterface(t)
	group, port := "239.77.77.87", freeUDPPort(t)

	cfg := Config{
		Groups: []GroupSpec{{Address: group, Port: port, Interface: ifi.Name}},
		Target: Target{Port: freeUDPPort(t)},
	}
	require.NoError(t, cfg.Validate())

	r := New(&cfg)
	require.NoError(t, r.Start(context.Background()))
	defer r.Stop()

	// Closing the sender is what an unwritable target looks like from the relay's side.
	require.NoError(t, r.sender.Close())

	sendMulticast(t, ifi, group, port, "foo")

	require.Eventually(t, func() bool { return r.Stats().Dropped == 1 }, 2*time.Second, 10*time.Millisecond,
		"a failed write must be counted, stats: %+v", r.Stats())
	require.Zero(t, r.Stats().Forwarded, "a dropped datagram must not also count as forwarded")
	require.Zero(t, r.Stats().Bytes)
}

// Start must fail loudly on an unresolvable target rather than come up and drop everything.
func TestRelayStartRejectsUnresolvableTarget(t *testing.T) {
	cfg := Config{
		Groups: []GroupSpec{{Address: "239.77.77.92", Port: freeUDPPort(t)}},
		// A host containing a colon cannot resolve, so this fails before any socket is opened.
		Target: Target{Address: "1.2.3.4:5", Port: freeUDPPort(t)},
	}
	require.NoError(t, cfg.Validate())

	r := New(&cfg)
	require.ErrorContains(t, r.Start(context.Background()), "resolve target")
	require.Empty(t, r.sockets)
}

// Two entries for one group on one port differ only by interface, so Validate allows them,
// but they share a socket and the second join is rejected by the kernel. Start must surface
// that instead of reporting a relay that joined less than it was asked to.
func TestRelayStartRejectsDuplicateJoinOnOneSocket(t *testing.T) {
	ifi := multicastInterface(t)
	group, port := "239.77.77.93", freeUDPPort(t)

	cfg := Config{
		Groups: []GroupSpec{
			{Address: group, Port: port, Interface: ifi.Name},
			{Address: group, Port: port},
		},
		Target: Target{Port: freeUDPPort(t)},
	}
	require.NoError(t, cfg.Validate())

	r := New(&cfg)
	require.ErrorContains(t, r.Start(context.Background()), "join group "+group)
	require.Empty(t, r.sockets, "the socket must not be retained after a failed join")
}

// The plugin passes Envoy's log handle through Config.Logger; this proves the relay actually
// calls it, so join and drop diagnostics reach Envoy's log.
func TestRelayLogsThroughConfiguredLogger(t *testing.T) {
	ifi := multicastInterface(t)
	group, port := "239.77.77.94", freeUDPPort(t)

	var mu sync.Mutex
	var lines []string
	cfg := Config{
		Groups: []GroupSpec{{Address: group, Port: port, Interface: ifi.Name}},
		Target: Target{Port: freeUDPPort(t)},
		Logger: func(_ shared.LogLevel, format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, fmt.Sprintf(format, args...))
		},
	}
	require.NoError(t, cfg.Validate())

	r := New(&cfg)
	require.NoError(t, r.Start(context.Background()))
	r.Stop()

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, lines, "the relay must log through the configured logger")
	require.Contains(t, strings.Join(lines, "\n"), "joined "+group)
}

func TestRelayStopIsIdempotent(t *testing.T) {
	ifi := multicastInterface(t)
	cfg := Config{
		Groups: []GroupSpec{{Address: "239.77.77.81", Port: freeUDPPort(t), Interface: ifi.Name}},
		Target: Target{Port: freeUDPPort(t)},
	}
	require.NoError(t, cfg.Validate())

	r := New(&cfg)
	require.NoError(t, r.Start(context.Background()))
	r.Stop()
	r.Stop()
}

// multicastInterface prefers loopback so the test needs no real network, falling
// back to any multicast-capable interface.
func multicastInterface(t *testing.T) *net.Interface {
	t.Helper()

	ifaces, err := net.Interfaces()
	require.NoError(t, err)

	var fallback *net.Interface
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		if ifi.Flags&net.FlagLoopback != 0 {
			return ifi
		}
		if fallback == nil {
			fallback = ifi
		}
	}
	if fallback == nil {
		t.Skip("no multicast-capable interface available")
	}
	return fallback
}

func udpSink(t *testing.T) *net.UDPConn {
	t.Helper()

	sink, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sink.Close() })
	return sink
}

func sinkPort(t *testing.T, sink *net.UDPConn) int {
	t.Helper()

	addr, ok := sink.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	return addr.Port
}

func readSink(t *testing.T, sink *net.UDPConn) (string, *net.UDPAddr) {
	t.Helper()

	require.NoError(t, sink.SetReadDeadline(time.Now().Add(3*time.Second)))
	buf := make([]byte, 2048)
	n, src, err := sink.ReadFromUDP(buf)
	require.NoError(t, err, "no datagram reached the target")
	return string(buf[:n]), src
}

// sendMulticast sends from a fresh socket, so each call is a distinct source.
func sendMulticast(t *testing.T, ifi *net.Interface, group string, port int, payloads ...string) {
	t.Helper()

	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	pc := ipv4.NewPacketConn(conn)
	require.NoError(t, pc.SetMulticastInterface(ifi))
	require.NoError(t, pc.SetMulticastLoopback(true))
	require.NoError(t, pc.SetMulticastTTL(1))

	dst := &net.UDPAddr{IP: net.ParseIP(group), Port: port}
	for _, p := range payloads {
		_, err := pc.WriteTo([]byte(p), nil, dst)
		require.NoError(t, err)
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	require.NoError(t, conn.Close())
	return addr.Port
}
