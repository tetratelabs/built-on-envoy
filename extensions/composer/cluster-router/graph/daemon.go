// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"
)

// DaemonConfig configures a Daemon.
type DaemonConfig struct {
	EnvoyID         string
	AdvertiseListen string
	Peers           []PeerSpec
	Terminals       []string
	PollInterval    time.Duration
	StaleAfter      time.Duration
	HTTPClient      *http.Client
}

// Daemon owns the background poll loop and serves /advertisements.
type Daemon struct {
	cfg    DaemonConfig
	Table  *AtomicTable
	server *AdvertisementServer

	mu         sync.Mutex
	lastPeer   map[string]peerCacheEntry
	httpClient *http.Client

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type peerCacheEntry struct {
	adv      Advertisement
	received time.Time
}

// NewDaemon returns a Daemon ready to Start.
func NewDaemon(cfg *DaemonConfig) *Daemon {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Daemon{
		cfg:        *cfg,
		Table:      NewAtomicTable(cfg.EnvoyID),
		lastPeer:   map[string]peerCacheEntry{},
		httpClient: client,
	}
}

// Start launches the advertisement server and the poll loop.
func (d *Daemon) Start(ctx context.Context) error {
	s, err := NewAdvertisementServer(d.cfg.EnvoyID, d.cfg.AdvertiseListen, d.Table)
	if err != nil {
		return err
	}
	s.Start()
	d.server = s

	loopCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.loop(loopCtx)
	}()
	return nil
}

// Stop shuts down the poll loop and the advertisement server.
func (d *Daemon) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
	if d.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.server.Stop(ctx)
	}
}

func (d *Daemon) loop(ctx context.Context) {
	d.tick(ctx)
	t := time.NewTicker(d.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.tick(ctx)
		}
	}
}

func (d *Daemon) tick(ctx context.Context) {
	locals := d.buildLocals()
	advs := d.collectPeers(ctx)

	d.Table.Store(&Table{
		EnvoyID:       d.cfg.EnvoyID,
		Routes:        Compute(d.cfg.EnvoyID, locals, advs),
		LocalClusters: locals,
	})
}

// buildLocals synthesizes the LocalClusters map from configured peers and terminals.
func (d *Daemon) buildLocals() map[string]LocalCluster {
	out := make(map[string]LocalCluster, len(d.cfg.Peers)+len(d.cfg.Terminals))
	for _, p := range d.cfg.Peers {
		out[p.LocalCluster] = LocalCluster{Name: p.LocalCluster, Role: RolePeer, PeerID: p.ID}
	}
	for _, name := range d.cfg.Terminals {
		out[name] = LocalCluster{Name: name, Role: RoleTerminal}
	}
	return out
}

func (d *Daemon) collectPeers(ctx context.Context) []PeerAdvertisement {
	now := time.Now()
	results := make([]PeerAdvertisement, len(d.cfg.Peers))
	used := make([]bool, len(d.cfg.Peers))

	var wg sync.WaitGroup
	for i, p := range d.cfg.Peers {
		wg.Add(1)
		go func(i int, p PeerSpec) {
			defer wg.Done()
			adv, err := FetchAdvertisement(ctx, d.httpClient, p.Endpoint, d.cfg.EnvoyID)
			if err == nil {
				d.mu.Lock()
				d.lastPeer[p.ID] = peerCacheEntry{adv: adv, received: now}
				d.mu.Unlock()
				results[i] = PeerAdvertisement{Peer: p, Adv: adv}
				used[i] = true
				return
			}
			log.Printf("cluster-router: peer %s fetch failed: %v", p.ID, err)
			d.mu.Lock()
			entry, ok := d.lastPeer[p.ID]
			d.mu.Unlock()
			if ok && now.Sub(entry.received) <= d.cfg.StaleAfter {
				results[i] = PeerAdvertisement{Peer: p, Adv: entry.adv}
				used[i] = true
			}
		}(i, p)
	}
	wg.Wait()

	out := make([]PeerAdvertisement, 0, len(d.cfg.Peers))
	for i, ok := range used {
		if ok {
			out = append(out, results[i])
		}
	}
	return out
}
