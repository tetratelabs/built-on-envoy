// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package relay

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The composer host can build a filter config factory more than once for the same
// config. Without interning, each one would join the group again and every datagram
// would be delivered N times, so this is the test that guards against duplication.
func TestRegistrySharesOneRelayPerConfig(t *testing.T) {
	ifi := multicastInterface(t)
	cfg := Config{
		Groups: []GroupSpec{{Address: "239.77.77.82", Port: freeUDPPort(t), Interface: ifi.Name}},
		Target: Target{Port: freeUDPPort(t)},
	}
	require.NoError(t, cfg.Validate())

	first, key, err := Acquire(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { first.Stop() })

	second, secondKey, err := Acquire(context.Background(), &cfg)
	require.NoError(t, err)

	require.Equal(t, key, secondKey)
	require.Same(t, first, second, "identical configs must share one relay")
	require.Equal(t, 2, refs(key))
	require.Len(t, first.LocalAddrs(), 1, "one socket, not one per holder")

	Release(key)
	require.Equal(t, 1, refs(key))
	require.False(t, first.stopped.Load(), "relay must survive while a holder remains")

	Release(key)
	require.Equal(t, 0, refs(key))
	require.True(t, first.stopped.Load(), "relay must stop once the last holder releases")
}

// Groups listing the same set in a different order describe the same relay. Keying them
// differently would start two, both would join, and every datagram would be delivered twice
// -- so this asserts the delivered count, not just pointer identity.
func TestRegistryInternsPermutedGroups(t *testing.T) {
	ifi := multicastInterface(t)
	sink := udpSink(t)
	port := freeUDPPort(t)
	first := GroupSpec{Address: "239.77.77.89", Port: port, Interface: ifi.Name}
	second := GroupSpec{Address: "239.77.77.90", Port: port, Interface: ifi.Name}

	forward := Config{Groups: []GroupSpec{first, second}, Target: Target{Port: sinkPort(t, sink)}}
	require.NoError(t, forward.Validate())
	reversed := Config{Groups: []GroupSpec{second, first}, Target: Target{Port: sinkPort(t, sink)}}
	require.NoError(t, reversed.Validate())

	relay, key, err := Acquire(context.Background(), &forward)
	require.NoError(t, err)
	defer Release(key)

	permuted, permutedKey, err := Acquire(context.Background(), &reversed)
	require.NoError(t, err)
	defer Release(permutedKey)

	require.Equal(t, key, permutedKey, "permuted groups must produce the same key")
	require.Same(t, relay, permuted)
	require.Equal(t, 2, refs(key))

	sendMulticast(t, ifi, first.Address, port, "solo")
	payload, _ := readSink(t, sink)
	require.Equal(t, "solo", payload)

	// A second relay would have joined the same group and delivered a duplicate.
	require.NoError(t, sink.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	buf := make([]byte, 128)
	_, _, err = sink.ReadFromUDP(buf)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "one send must deliver exactly one datagram")
}

func TestRegistryDistinctConfigsGetDistinctRelays(t *testing.T) {
	ifi := multicastInterface(t)
	target := freeUDPPort(t)

	first := Config{
		Groups: []GroupSpec{{Address: "239.77.77.83", Port: freeUDPPort(t), Interface: ifi.Name}},
		Target: Target{Port: target},
	}
	require.NoError(t, first.Validate())
	second := Config{
		Groups: []GroupSpec{{Address: "239.77.77.84", Port: freeUDPPort(t), Interface: ifi.Name}},
		Target: Target{Port: target},
	}
	require.NoError(t, second.Validate())

	firstRelay, firstKey, err := Acquire(context.Background(), &first)
	require.NoError(t, err)
	defer Release(firstKey)

	secondRelay, secondKey, err := Acquire(context.Background(), &second)
	require.NoError(t, err)
	defer Release(secondKey)

	require.NotEqual(t, firstKey, secondKey)
	require.NotSame(t, firstRelay, secondRelay)
}

func TestReleaseUnknownKeyIsNoop(_ *testing.T) {
	Release("not-a-key")
}
