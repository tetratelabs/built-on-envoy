// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package main

import (
	"errors"
	"io"
	"log"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type handler interface {
	ProcessRequestHeaders(*extprocv3.HttpHeaders) *extprocv3.ProcessingResponse
	ProcessRequestBody(*extprocv3.HttpBody) *extprocv3.ProcessingResponse
	ProcessRequestTrailers(*extprocv3.HttpTrailers) *extprocv3.ProcessingResponse
	ProcessResponseHeaders(*extprocv3.HttpHeaders) *extprocv3.ProcessingResponse
	ProcessResponseBody(*extprocv3.HttpBody) *extprocv3.ProcessingResponse
	ProcessResponseTrailers(*extprocv3.HttpTrailers) *extprocv3.ProcessingResponse
}

// processor implements ExternalProcessorServer.
type processor struct {
	extprocv3.UnimplementedExternalProcessorServer
	handler handler
}

// Process handles the bidirectional gRPC stream for a single HTTP request.
// It dispatches each phase to the appropriate handler method.
func (p *processor) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "failed to receive request: %v", err)
		}

		var resp *extprocv3.ProcessingResponse

		switch r := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			resp = p.handler.ProcessRequestHeaders(r.RequestHeaders)
		case *extprocv3.ProcessingRequest_RequestBody:
			resp = p.handler.ProcessRequestBody(r.RequestBody)
		case *extprocv3.ProcessingRequest_RequestTrailers:
			resp = p.handler.ProcessRequestTrailers(r.RequestTrailers)
		case *extprocv3.ProcessingRequest_ResponseHeaders:
			resp = p.handler.ProcessResponseHeaders(r.ResponseHeaders)
		case *extprocv3.ProcessingRequest_ResponseBody:
			resp = p.handler.ProcessResponseBody(r.ResponseBody)
		case *extprocv3.ProcessingRequest_ResponseTrailers:
			resp = p.handler.ProcessResponseTrailers(r.ResponseTrailers)
		}

		if err = stream.Send(resp); err != nil {
			return status.Errorf(codes.Unknown, "failed to send response: %v", err)
		}
	}
}

type handlerImpl struct{}

func (h *handlerImpl) ProcessRequestHeaders(headers *extprocv3.HttpHeaders) *extprocv3.ProcessingResponse {
	log.Printf("received request headers: %v", headers.Headers)
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{},
			},
		},
	}
}

func (h *handlerImpl) ProcessRequestBody(body *extprocv3.HttpBody) *extprocv3.ProcessingResponse {
	log.Printf("received request body: %d bytes", len(body.Body))
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestBody{
			RequestBody: &extprocv3.BodyResponse{
				Response: &extprocv3.CommonResponse{},
			},
		},
	}
}

func (h *handlerImpl) ProcessRequestTrailers(trailers *extprocv3.HttpTrailers) *extprocv3.ProcessingResponse {
	log.Printf("received request trailers: %v", trailers.Trailers)
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestTrailers{
			RequestTrailers: &extprocv3.TrailersResponse{},
		},
	}
}

func (h *handlerImpl) ProcessResponseHeaders(headers *extprocv3.HttpHeaders) *extprocv3.ProcessingResponse {
	log.Printf("received response headers: %v", headers.Headers)
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{
					HeaderMutation: &extprocv3.HeaderMutation{
						SetHeaders: []*corev3.HeaderValueOption{
							{
								Header: &corev3.HeaderValue{
									Key:      "x-ext-proc",
									RawValue: []byte("processed"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (h *handlerImpl) ProcessResponseBody(body *extprocv3.HttpBody) *extprocv3.ProcessingResponse {
	log.Printf("received response body: %d bytes", len(body.Body))
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ResponseBody{
			ResponseBody: &extprocv3.BodyResponse{
				Response: &extprocv3.CommonResponse{},
			},
		},
	}
}

func (h *handlerImpl) ProcessResponseTrailers(trailers *extprocv3.HttpTrailers) *extprocv3.ProcessingResponse {
	log.Printf("received response trailers: %v", trailers.Trailers)
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ResponseTrailers{
			ResponseTrailers: &extprocv3.TrailersResponse{},
		},
	}
}
