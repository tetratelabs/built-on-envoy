// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AdvertisementServer serves this Envoy's current advertisement to peer pullers.
type AdvertisementServer struct {
	envoyID string
	table   *AtomicTable
	srv     *http.Server
	lis     net.Listener
}

// NewAdvertisementServer binds the listener and prepares (but does not start) the HTTP server.
func NewAdvertisementServer(envoyID, listen string, table *AtomicTable) (*AdvertisementServer, error) {
	lis, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("advertise listen %q: %w", listen, err)
	}
	as := &AdvertisementServer{envoyID: envoyID, table: table, lis: lis}
	mux := http.NewServeMux()
	mux.HandleFunc("/advertisements", as.handle)
	as.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return as, nil
}

// Addr returns the bound listener address.
func (as *AdvertisementServer) Addr() string { return as.lis.Addr().String() }

// Start begins serving advertisements in a background goroutine.
func (as *AdvertisementServer) Start() {
	go func() { _ = as.srv.Serve(as.lis) }()
}

// Stop gracefully shuts the advertisement server down.
func (as *AdvertisementServer) Stop(ctx context.Context) error {
	return as.srv.Shutdown(ctx)
}

func (as *AdvertisementServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	peerID := r.URL.Query().Get("peer")
	t := as.table.Load()
	adv := Advertisement{EnvoyID: as.envoyID, Routes: AdvertiseTo(t, peerID)}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(adv)
}

// FetchAdvertisement identifies the caller via the `peer` query parameter so
// the server can apply split horizon. client must be non-nil; the caller
// owns the timeout policy.
func FetchAdvertisement(ctx context.Context, client *http.Client, peerEndpoint, localEnvoyID string) (Advertisement, error) {
	u, err := url.Parse(strings.TrimRight(peerEndpoint, "/") + "/advertisements")
	if err != nil {
		return Advertisement{}, fmt.Errorf("parse peer url: %w", err)
	}
	q := u.Query()
	q.Set("peer", localEnvoyID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Advertisement{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Advertisement{}, fmt.Errorf("peer request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Advertisement{}, fmt.Errorf("peer status %d", resp.StatusCode)
	}
	var adv Advertisement
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&adv); err != nil {
		return Advertisement{}, fmt.Errorf("decode advertisement: %w", err)
	}
	return adv, nil
}
