// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package main is the entry point for the example-ext-proc ext_proc server.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
)

func main() {
	port := flag.Int("port", 50051, "gRPC server port")
	flag.Parse()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	log.Printf("ext_proc server listening on :%d", *port)
	if err = run(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func run(lis net.Listener) error {
	handler := &handlerImpl{}
	processor := &processor{handler: handler}

	srv := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(srv, processor)

	return srv.Serve(lis)
}
