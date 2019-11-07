/*
 *
 * Copyright 2019 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package fakexds provides a very basic fake implementation of the xDS server
// for unit testing purposes.
package fakexds

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	discoverypb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	adsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
)

// TODO: Make this a var or a field in the server if there is a need to use a
// value other than this default.
const defaultChannelBufferSize = 50

// Request wraps an xDS request and error.
type Request struct {
	Req *discoverypb.DiscoveryRequest
	Err error
}

// Response wraps an xDS response and error.
type Response struct {
	Resp *discoverypb.DiscoveryResponse
	Err  error
}

// Server is a very basic implementation of a fake xDS server. It provides a
// request and response channel for the user to control the requests that are
// expected and the responses that needs to be sent out.
type Server struct {
	// RequestChan is a buffered channel on which the fake server writes the
	// received requests onto.
	RequestChan chan *Request
	// ResponseChan is a buffered channel from which the fake server reads the
	// responses that it must send out to the client.
	ResponseChan chan *Response

	// onError is a callback which is invoked when the fake server encounters
	// errors during sending or receiving messages.
	onError func(error)
}

// New returns a fake xDS server which contains a pair of channels. On one
// channel, it writes the received requests and on the other, it reads
// responses that it must send out. The provided onError callback is invoked
// upon encountering errors during sending or receiving messages.
func New(onError func(error)) *Server {
	return &Server{
		RequestChan:  make(chan *Request, defaultChannelBufferSize),
		ResponseChan: make(chan *Response, defaultChannelBufferSize),
		onError:      onError,
	}
}

// StreamAggregatedResources is the fake implementation to handle an ADS
// stream.
func (fs *Server) StreamAggregatedResources(s adsgrpc.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	errCh := make(chan error, 2)
	go func() {
		for {
			req, err := s.Recv()
			if err != nil {
				errCh <- err
				return
			}
			fs.RequestChan <- &Request{req, err}
		}
	}()
	go func() {
		var retErr error
		defer func() {
			errCh <- retErr
		}()

		for {
			select {
			case r := <-fs.ResponseChan:
				if r.Err != nil {
					retErr = r.Err
					return
				}
				if err := s.Send(r.Resp); err != nil {
					retErr = err
					return
				}
			case <-s.Context().Done():
				retErr = s.Context().Err()
				return
			}
		}
	}()

	if err := <-errCh; err != nil {
		if fs.onError != nil {
			fs.onError(err)
		}
		return err
	}

	return nil
}

// DeltaAggregatedResources helps implement the ADS service.
func (fs *Server) DeltaAggregatedResources(adsgrpc.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return status.Error(codes.Unimplemented, "")
}
