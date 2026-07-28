// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package relay joins IPv4 multicast groups on sockets it owns and re-emits each
// received datagram as unicast UDP to a local Envoy UDP listener.
//
// The join cannot happen inside Envoy: nothing in Envoy joins an IGMP group, and the UDP
// dynamic-module ABI exposes no socket, fd, or setsockopt callback.
//
// Forwarding to loopback also fixes udp_proxy's reply path. udp_proxy uses the received
// datagram's header destination as the reply source IP, which the kernel rejects with
// EINVAL for a multicast group but accepts for a unicast loopback address.
package relay

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

const (
	defaultTargetAddress  = "127.0.0.1"
	defaultReadBufferSize = 65535

	minReadBufferSize = 512
	maxReadBufferSize = 1 << 16

	// Rate-limits forward failure logs so an unreachable target cannot flood the log.
	forwardErrorLogEvery = 1000
)

// Logger matches the composer SDK's logging signature.
type Logger func(level shared.LogLevel, format string, args ...any)

// GroupSpec is one multicast group to join.
type GroupSpec struct {
	Address   string `json:"address"`
	Port      int    `json:"port"`
	Interface string `json:"interface,omitempty"`
}

// Target is the local Envoy UDP listener that relayed datagrams are sent to.
type Target struct {
	Address string `json:"address,omitempty"`
	Port    int    `json:"port"`
}

// Config is the relay configuration, deserialized from the extension's config JSON.
type Config struct {
	Groups         []GroupSpec `json:"groups"`
	Target         Target      `json:"target"`
	ReadBufferSize int         `json:"read_buffer_size,omitempty"`

	// Logger is not part of the config JSON; the plugin sets it.
	Logger Logger `json:"-"`
}

// Validate applies defaults and reports every problem at once.
func (c *Config) Validate() error {
	if c.Target.Address == "" {
		c.Target.Address = defaultTargetAddress
	}
	if c.ReadBufferSize == 0 {
		c.ReadBufferSize = defaultReadBufferSize
	}

	var errs []error
	if len(c.Groups) == 0 {
		errs = append(errs, errors.New("groups must contain at least one entry"))
	}

	seen := map[string]bool{}
	for i, g := range c.Groups {
		switch ip := net.ParseIP(g.Address); {
		case g.Address == "":
			errs = append(errs, fmt.Errorf("groups[%d]: address is required", i))
		case ip == nil:
			errs = append(errs, fmt.Errorf("groups[%d]: address %q is not a valid IP", i, g.Address))
		case ip.To4() == nil:
			errs = append(errs, fmt.Errorf("groups[%d]: address %q must be IPv4", i, g.Address))
		case !ip.IsMulticast():
			errs = append(errs, fmt.Errorf("groups[%d]: address %q is not a multicast address", i, g.Address))
		}
		if g.Port <= 0 || g.Port > 65535 {
			errs = append(errs, fmt.Errorf("groups[%d]: port %d out of range 1-65535", i, g.Port))
		}
		key := fmt.Sprintf("%s:%d@%s", g.Address, g.Port, g.Interface)
		if seen[key] {
			errs = append(errs, fmt.Errorf("groups[%d]: duplicate group %s", i, key))
		}
		seen[key] = true
	}

	if c.Target.Port <= 0 || c.Target.Port > 65535 {
		errs = append(errs, fmt.Errorf("target.port %d out of range 1-65535", c.Target.Port))
	}
	if c.ReadBufferSize < minReadBufferSize || c.ReadBufferSize > maxReadBufferSize {
		errs = append(errs, fmt.Errorf("read_buffer_size %d out of range %d-%d",
			c.ReadBufferSize, minReadBufferSize, maxReadBufferSize))
	}

	return errors.Join(errs...)
}

// key canonicalizes the config so the registry can dedupe identical relays.
func (c *Config) key() string {
	groups := slices.Clone(c.Groups)
	slices.SortFunc(groups, func(a, b GroupSpec) int {
		return cmp.Or(
			strings.Compare(a.Address, b.Address),
			cmp.Compare(a.Port, b.Port),
			strings.Compare(a.Interface, b.Interface),
		)
	})

	b, _ := json.Marshal(struct {
		Groups         []GroupSpec `json:"groups"`
		Target         Target      `json:"target"`
		ReadBufferSize int         `json:"read_buffer_size"`
	}{groups, c.Target, c.ReadBufferSize})
	return string(b)
}

// Stats is a point-in-time snapshot of relay counters.
type Stats struct {
	Received  uint64
	Forwarded uint64
	Dropped   uint64
	Bytes     uint64
	Foreign   uint64
}

// socket is one wildcard-bound UDP socket serving every group on a single port.
type socket struct {
	port   int
	pc     *ipv4.PacketConn
	groups map[string]bool
}

// Relay owns the multicast sockets and the forwarding goroutines.
type Relay struct {
	cfg     *Config
	sockets []*socket
	target  *net.UDPAddr

	cancel  context.CancelFunc
	wg      sync.WaitGroup
	stopped atomic.Bool

	received  atomic.Uint64
	forwarded atomic.Uint64
	dropped   atomic.Uint64
	bytes     atomic.Uint64
	foreign   atomic.Uint64

	// sender is the single socket every relayed datagram is written from, so udp_proxy
	// sees one source 4-tuple and keeps one session for the group. Safe to read without a
	// lock: Stop closes the multicast sockets and waits for the read loops before closing
	// it, so no forward can be in flight.
	sender *net.UDPConn
}

// New returns a Relay ready to Start. cfg must already have passed Validate, and is
// retained rather than copied, so callers must not mutate it afterwards.
func New(cfg *Config) *Relay {
	return &Relay{cfg: cfg}
}

func (r *Relay) logf(level shared.LogLevel, format string, args ...any) {
	if r.cfg.Logger != nil {
		r.cfg.Logger(level, format, args...)
	}
}

// Start binds one socket per distinct group port, joins every group on it, and launches
// a forwarding goroutine per socket. Closes anything already opened if it fails.
func (r *Relay) Start(ctx context.Context) (err error) {
	target, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(
		r.cfg.Target.Address, fmt.Sprint(r.cfg.Target.Port)))
	if err != nil {
		return fmt.Errorf("resolve target: %w", err)
	}
	r.target = target

	sharedConn, err := net.DialUDP("udp4", nil, target)
	if err != nil {
		return fmt.Errorf("dial target %s: %w", target, err)
	}
	r.sender = sharedConn

	defer func() {
		if err != nil {
			// Sockets opened before the failure hold fds and group memberships, so a
			// rejected config must not leave them behind.
			r.closeSockets()
			r.closeSender()
		}
	}()

	byPort := map[int][]GroupSpec{}
	var ports []int
	for _, g := range r.cfg.Groups {
		if _, ok := byPort[g.Port]; !ok {
			ports = append(ports, g.Port)
		}
		byPort[g.Port] = append(byPort[g.Port], g)
	}

	for _, port := range ports {
		s, err := r.openSocket(ctx, port, byPort[port])
		if err != nil {
			return err
		}
		r.sockets = append(r.sockets, s)
	}

	loopCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	for _, s := range r.sockets {
		r.wg.Add(1)
		go func(s *socket) {
			defer r.wg.Done()
			r.readLoop(loopCtx, s)
		}(s)
	}

	r.logf(shared.LogLevelInfo, "multicast-relay: joined %d group(s) on %d socket(s), forwarding to %s",
		len(r.cfg.Groups), len(r.sockets), target)
	return nil
}

// openSocket binds the wildcard address on port and joins each group on it, so groups
// sharing a port share a socket.
func (r *Relay) openSocket(ctx context.Context, port int, groups []GroupSpec) (*socket, error) {
	lc := net.ListenConfig{Control: setSocketReuse}
	conn, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen udp4 :%d: %w", port, err)
	}
	udp, ok := conn.(*net.UDPConn)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("listen udp4 :%d: unexpected conn type %T", port, conn)
	}
	if bufErr := udp.SetReadBuffer(r.cfg.ReadBufferSize); bufErr != nil {
		// Not fatal; the kernel default still works, just with a smaller queue.
		r.logf(shared.LogLevelWarn, "multicast-relay: set read buffer on :%d: %v", port, bufErr)
	}

	pc := ipv4.NewPacketConn(udp)
	// The destination address is how readLoop tells joined-group traffic apart from
	// anything else arriving on this wildcard-bound port.
	if err = pc.SetControlMessage(ipv4.FlagDst, true); err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("enable destination control message on :%d: %w", port, err)
	}

	s := &socket{port: port, pc: pc, groups: map[string]bool{}}
	for _, g := range groups {
		var ifi *net.Interface
		if g.Interface != "" {
			ifi, err = net.InterfaceByName(g.Interface)
			if err != nil {
				_ = pc.Close()
				return nil, fmt.Errorf("group %s:%d: interface %q: %w", g.Address, g.Port, g.Interface, err)
			}
		}
		ip := net.ParseIP(g.Address)
		if err := pc.JoinGroup(ifi, &net.UDPAddr{IP: ip}); err != nil {
			_ = pc.Close()
			return nil, fmt.Errorf("join group %s:%d on %q: %w", g.Address, g.Port, g.Interface, err)
		}
		s.groups[ip.String()] = true
		r.logf(shared.LogLevelInfo, "multicast-relay: joined %s:%d on interface %q",
			g.Address, g.Port, g.Interface)
	}
	return s, nil
}

// setSocketReuse allows an old and new relay to briefly overlap on the same port
// during a config reload.
func setSocketReuse(_, _ string, c syscall.RawConn) error {
	var setErr error
	if err := c.Control(func(fd uintptr) {
		if setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); setErr != nil {
			return
		}
		setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	}); err != nil {
		return err
	}
	return setErr
}

func (r *Relay) readLoop(ctx context.Context, s *socket) {
	buf := make([]byte, r.cfg.ReadBufferSize)
	for {
		n, cm, _, err := s.pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || r.stopped.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			r.logf(shared.LogLevelWarn, "multicast-relay: read on :%d: %v", s.port, err)
			continue
		}
		r.received.Add(1)

		// A wildcard bind also receives unicast and other groups on this port.
		if cm != nil && cm.Dst != nil && !s.groups[cm.Dst.String()] {
			r.foreign.Add(1)
			continue
		}
		r.forward(buf[:n])
	}
}

func (r *Relay) forward(payload []byte) {
	if _, err := r.sender.Write(payload); err != nil {
		n := r.dropped.Add(1)
		if n == 1 || n%forwardErrorLogEvery == 0 {
			r.logf(shared.LogLevelWarn, "multicast-relay: forward to %s failed (%d dropped): %v",
				r.target, n, err)
		}
		return
	}
	r.forwarded.Add(1)
	r.bytes.Add(uint64(len(payload)))
}

// Stop closes the sockets, which implicitly leaves the groups, and waits for the
// forwarding goroutines. Safe to call twice.
func (r *Relay) Stop() {
	if !r.stopped.CompareAndSwap(false, true) {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	// Close first so a blocked ReadFrom returns.
	r.closeSockets()
	r.wg.Wait()
	r.closeSender()

	st := r.Stats()
	r.logf(shared.LogLevelInfo,
		"multicast-relay: stopped; received=%d forwarded=%d dropped=%d foreign=%d bytes=%d",
		st.Received, st.Forwarded, st.Dropped, st.Foreign, st.Bytes)
}

// closeSockets closes the multicast sockets, which also drops the group memberships.
// Kept separate from closeAll because Stop must close these before waiting on the
// forwarding goroutines, so a blocked ReadFrom returns.
func (r *Relay) closeSockets() {
	for _, s := range r.sockets {
		_ = s.pc.Close()
	}
	r.sockets = nil
}

func (r *Relay) closeSender() {
	if r.sender != nil {
		_ = r.sender.Close()
		r.sender = nil
	}
}

// Stats returns a snapshot of the relay counters.
func (r *Relay) Stats() Stats {
	return Stats{
		Received:  r.received.Load(),
		Forwarded: r.forwarded.Load(),
		Dropped:   r.dropped.Load(),
		Bytes:     r.bytes.Load(),
		Foreign:   r.foreign.Load(),
	}
}

// LocalAddrs reports the bound address of each multicast socket.
func (r *Relay) LocalAddrs() []string {
	out := make([]string, 0, len(r.sockets))
	for _, s := range r.sockets {
		out = append(out, s.pc.LocalAddr().String())
	}
	return out
}
