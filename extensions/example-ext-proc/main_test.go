// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package main

import (
	"net"
	"testing"
	"time"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestRun_RegistersAndServes(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() { errCh <- run(lis) }()

	// Poll until the server accepts connections.
	addr := lis.Addr().String()
	var conn *grpc.ClientConn
	require.Eventually(t, func() bool {
		conn, err = grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		return err == nil
	}, 3*time.Second, 10*time.Millisecond)
	defer func() { _ = conn.Close() }()

	// Confirm the ExternalProcessor service is reachable.
	stream, err := extprocv3.NewExternalProcessorClient(conn).Process(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.CloseSend())
}
